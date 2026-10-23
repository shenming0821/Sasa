package codex

import (
	"context"
	"encoding/json"
)

type HistoryTurn struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	ItemsView string            `json:"itemsView"`
	Items     []json.RawMessage `json:"items"`
	Error     json.RawMessage   `json:"error"`
}

type ThreadMeta struct {
	ID      string        `json:"id"`
	Cwd     string        `json:"cwd"`
	Preview string        `json:"preview"`
	Name    *string       `json:"name"`
	Status  ThreadStatus  `json:"status"`
	Turns   []HistoryTurn `json:"turns"`
}

type ThreadStatus struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags"`
}

type ResumedThread struct {
	ThreadMeta ThreadMeta      `json:"thread"`
	Model      string          `json:"model"`
	Cwd        string          `json:"cwd"`
	Policy     json.RawMessage `json:"approvalPolicy"`
	Sandbox    json.RawMessage `json:"sandbox"`
}

type TurnsPage struct {
	Data       []HistoryTurn `json:"data"`
	NextCursor *string       `json:"nextCursor"`
}

type ThreadsPage struct {
	Data       []ThreadMeta `json:"data"`
	NextCursor *string      `json:"nextCursor"`
}

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
