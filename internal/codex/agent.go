package codex

import (
	"context"

	"github.com/shenming0821/Sasa/internal/agent"
)

func (c *Client) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		Models: true, Skills: true, Images: true, Approval: true, SessionRules: true,
		History: true, Resume: true, Reconcile: true, Occupied: true, Fork: true,
	}
}

func (c *Client) RespondHistory(ctx context.Context, id string) (agent.RecoveryResult, error) {
	thread, err := c.ResumeThread(ctx, id)
	if err != nil {
		return agent.RecoveryResult{}, err
	}
	page, err := c.ListTurns(ctx, id, nil)
	if err != nil {
		return agent.RecoveryResult{}, err
	}
	result := agent.RecoveryResult{Thread: thread, History: page}
	result.WriterConfirmed = thread.ThreadMeta.ID == id && thread.Cwd != ""
	result.TerminalConfirmed = thread.ThreadMeta.Status.Type == "idle"
	for _, turn := range page.Data {
		switch turn.Status {
		case "completed", "interrupted", "failed":
		default:
			result.TerminalConfirmed = false
		}
	}
	result.PendingKnown = result.TerminalConfirmed
	result.CanSend = result.WriterConfirmed && result.TerminalConfirmed
	if !result.CanSend {
		result.Uncertainty = "原生会话或任务终态尚未确认"
	}
	return result, nil
}
