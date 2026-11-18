package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/attachment"
)

type threadCompactor interface {
	CompactThread(context.Context, string) error
}

func isCompactCommand(text string) bool {
	parts := strings.Fields(text)
	return len(parts) > 0 && parts[0] == "/compact"
}

func (a *App) validateCompact(text string, images []attachment.Image) bool {
	switch {
	case len(images) > 0:
		a.State.Error = "/compact 不接受图片，请先移除附件"
	case a.kind.Normalized() == agent.Codex && strings.TrimSpace(text) != "/compact":
		a.State.Error = "Codex 的 /compact 不接受参数"
	case !a.State.Busy && (a.State.ThreadID == "" || len(a.State.Messages) == 0):
		a.State.Error = "暂无需要压缩的上下文"
	case a.kind.Normalized() == agent.Codex:
		if _, ok := a.sessionBackend().(threadCompactor); !ok {
			a.State.Error = "当前连接不支持压缩上下文"
			return false
		}
		return true
	default:
		return true
	}
	return false
}

func (a *App) sendCompact(text, messageID string) {
	a.State.BeginMessage(text, messageID)
	if message := a.State.PendingSubmission(); message != nil {
		message.Delivery = "command"
	}
	a.State.Phase = "正在压缩上下文"
	if err := a.persist(); err != nil {
		a.abortBeforeTurn(text, err)
		return
	}
	a.processScrollHold = false
	a.list.FollowEnd = true
	a.list.ScrollToEnd()
	sequence, threadID := a.State.Sequence, a.State.ThreadID
	sender := a.sessionBackend().(threadCompactor)
	ctx, cancel := agent.RequestContext()
	a.submitCancel, a.submitStop = cancel, false
	go func() {
		defer cancel()
		err := sender.CompactThread(ctx, threadID)
		a.update(func() {
			if a.State.Sequence != sequence {
				return
			}
			if !a.State.Busy {
				return
			}
			if err != nil {
				a.submitCancel = nil
				var rejected *agent.RPCError
				if errors.As(err, &rejected) && a.State.TurnID == "" {
					err = &agent.NotSubmittedError{Err: fmt.Errorf("压缩请求被拒绝：%w", err)}
				}
				a.applySubmissionFailure(sequence, text, err, true)
				return
			}
			if a.State.TurnID != "" {
				a.submitCancel = nil
			}
			if a.submitStop && a.State.TurnID != "" {
				a.stop()
			}
		})
	}()
}
