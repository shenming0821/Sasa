package devin

import (
	"context"
	"fmt"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

// defaultMode is the permission mode Fuyu requests for every attached Devin
// session; the runtime's advertised list decides whether it exists at all
// (sandbox sessions and org policy can hide Bypass).
const defaultMode = "bypass"

// syncModes refreshes the session's advertised mode list from the "mode"
// config option, which carries the same choices as ACP availableModes plus
// display names; the session/new|load availableModes are the fallback when
// the option is absent. Callers hold c.mu or own the session outright.
func syncModes(s *acpSession) {
	option := s.options["mode"]
	if option == nil {
		return
	}
	modes := make([]agent.SessionMode, 0, len(option.Order))
	for _, id := range option.Order {
		modes = append(modes, agent.SessionMode{ID: id, Name: option.Choices[id]})
	}
	s.modes = modes
	if s.mode == "" {
		s.mode = option.Current
	}
}

// applyDefaultMode switches a freshly attached session to the default mode
// when the runtime advertises it. Best-effort: the reported policy always
// mirrors whatever mode the session actually holds afterwards.
func (c *Client) applyDefaultMode(ctx context.Context, s *acpSession) {
	c.mu.Lock()
	available := false
	for _, mode := range s.modes {
		if mode.ID == defaultMode {
			available = true
		}
	}
	current := s.mode
	c.mu.Unlock()
	if !available || current == defaultMode {
		return
	}
	_ = c.setMode(ctx, s, defaultMode)
}

// setMode calls session/set_mode and waits for the matching
// current_mode_update; ACP confirms the switch only through that notification
// (the empty result also covers silently rejected ids), so the applied id is
// verified rather than assumed.
func (c *Client) setMode(ctx context.Context, s *acpSession, modeID string) error {
	c.mu.Lock()
	if s.mode == modeID {
		c.mu.Unlock()
		return nil
	}
	if s.modeAck != nil {
		c.mu.Unlock()
		return fmt.Errorf("Devin 权限模式切换进行中")
	}
	s.modeAck = make(chan string, 1)
	ack := s.modeAck
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		s.modeAck = nil
		c.mu.Unlock()
	}()
	if _, err := c.conn.call(ctx, "session/set_mode", map[string]any{"sessionId": s.id, "modeId": modeID}); err != nil {
		return fmt.Errorf("设置 Devin 权限模式失败：%w", err)
	}
	c.mu.Lock()
	applied := s.mode == modeID
	c.mu.Unlock()
	if applied {
		return nil
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case got := <-ack:
		if got != modeID {
			return fmt.Errorf("Devin 权限模式未切换到 %s（当前 %s）", modeID, got)
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("Devin 权限模式切换未确认")
	case <-ctx.Done():
		return ctx.Err()
	case <-c.conn.done:
		return c.conn.disconnectErr()
	}
}

// SetPermissionMode switches the session's permission mode
// (agent.PermissionModeChanger); the new state reaches the UI through the
// current_mode_update projection, not this return value.
func (c *Client) SetPermissionMode(ctx context.Context, change agent.PermissionModeChange) error {
	s, err := c.session(change.Session.SessionID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	known := len(s.modes) == 0
	for _, mode := range s.modes {
		if mode.ID == change.Mode {
			known = true
		}
	}
	c.mu.Unlock()
	if !known {
		return fmt.Errorf("Devin 会话不支持权限模式：%s", change.Mode)
	}
	return c.setMode(ctx, s, change.Mode)
}
