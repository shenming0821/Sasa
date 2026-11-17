package codex

import "context"

func (c *Client) CompactThread(ctx context.Context, threadID string) error {
	return c.Call(ctx, "thread/compact/start", map[string]string{"threadId": threadID}, nil)
}
