package claude

import (
	"context"
	"fmt"

	"github.com/shenming0821/Fuyu/internal/agent"
)

// The native mode control is acknowledged before a user frame or an approved
// ExitPlanMode is written. An idle process must not retain the previous mode.
func (r *execution) setMode(ctx context.Context, mode agent.InteractionMode) error {
	c := r.client
	c.mu.Lock()
	current := r.connection.permissionMode
	base := r.session.BasePermissionMode
	c.mu.Unlock()
	if base == "" && current != "plan" {
		base = current
	}
	target := current
	switch mode {
	case agent.ModePlan:
		if current != "plan" {
			base = current
		}
		target = "plan"
	case agent.ModeDefault:
		target = base
		if target == "" || target == "plan" {
			target = "default"
		}
	}
	if target != current {
		if _, err := r.control(ctx, map[string]any{"subtype": "set_permission_mode", "mode": target}); err != nil {
			return fmt.Errorf("切换 Claude 计划模式失败，未继续任务：%w", err)
		}
	}
	c.mu.Lock()
	r.connection.permissionMode = target
	r.session.PermissionMode, r.session.BasePermissionMode = target, base
	err := c.save(r.session)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	effective := agent.ModeDefault
	if target == "plan" {
		effective = agent.ModePlan
	}
	if mode != "" || target == "plan" {
		r.emit("fuyu/mode", map[string]any{"threadId": r.session.ID, "mode": effective, "permissionMode": target})
	}
	return nil
}
