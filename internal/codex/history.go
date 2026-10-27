package codex

import (
	"context"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func IsActiveWriter(err error, id string) bool { return agent.IsActiveWriter(err, id) }

func (c *Client) ReadThreadMeta(ctx context.Context, id string) (ThreadMeta, error) {
	var result struct {
		Thread ThreadMeta `json:"thread"`
	}
	err := c.Call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &result)
	return result.Thread, err
}

func (c *Client) ForkThread(ctx context.Context, id string) (ResumedThread, error) {
	var result ResumedThread
	err := c.Call(ctx, "thread/fork", map[string]any{
		"threadId": id, "excludeTurns": true, "deferGoalContinuation": true,
	}, &result)
	return result, err
}

type HistoryTurn = agent.HistoryTurn

type ThreadMeta = agent.ThreadMeta

type ThreadStatus = agent.ThreadStatus

type ResumedThread = agent.ResumedThread

type TurnsPage = agent.TurnsPage

type ThreadsPage = agent.ThreadsPage

func (c *Client) ResumeThread(ctx context.Context, id string) (ResumedThread, error) {
	var result ResumedThread
	// Preserve the native cwd, model and permission configuration.
	err := c.Call(ctx, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &result)
	return result, err
}

func (c *Client) ListTurns(ctx context.Context, id string, cursor *string) (TurnsPage, error) {
	var result TurnsPage
	err := c.Call(ctx, "thread/turns/list", map[string]any{
		"threadId": id, "cursor": cursor, "limit": 20, "sortDirection": "desc", "itemsView": "full",
	}, &result)
	return result, err
}

func (c *Client) ListThreads(ctx context.Context, cursor *string) (ThreadsPage, error) {
	var result ThreadsPage
	err := c.Call(ctx, "thread/list", map[string]any{
		"cursor": cursor, "limit": 20, "sortKey": "updated_at", "sortDirection": "desc",
	}, &result)
	return result, err
}
