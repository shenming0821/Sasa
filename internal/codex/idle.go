package codex

import (
	"context"
	"fmt"
)

func (c *Client) ReleaseIdleThread(ctx context.Context, id string) (bool, error) {
	thread, err := c.ReadThreadMeta(ctx, id)
	if err != nil {
		return false, err
	}
	if thread.ID != id {
		return false, nil
	}
	if thread.Status.Type == "notLoaded" {
		return true, nil
	}
	if thread.Status.Type != "idle" {
		return false, nil
	}
	// Parent turns can finish while a delegated agent continues working.
	// Keep that family intact, including idle children with live terminals.
	protected, err := c.hasLiveDescendants(ctx, id)
	if err != nil || protected {
		return false, err
	}
	// A completed turn can still own a running background terminal. Never
	// unsubscribe it merely because the conversational turn has ended.
	var terminals struct {
		Data       []struct{} `json:"data"`
		NextCursor *string    `json:"nextCursor"`
	}
	if err := c.Call(ctx, "thread/backgroundTerminals/list", map[string]any{"threadId": id, "limit": 1}, &terminals); err != nil {
		return false, err
	}
	if len(terminals.Data) != 0 || terminals.NextCursor != nil {
		return false, nil
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := c.Call(ctx, "thread/unsubscribe", map[string]any{"threadId": id}, &result); err != nil {
		return true, err
	}
	switch result.Status {
	case "unsubscribed", "notLoaded", "notSubscribed":
		return true, nil
	default:
		return true, fmt.Errorf("unexpected thread/unsubscribe status %q", result.Status)
	}
}

func (c *Client) hasLiveDescendants(ctx context.Context, id string) (bool, error) {
	type member struct {
		ID       string       `json:"id"`
		ParentID string       `json:"parentThreadId"`
		Status   ThreadStatus `json:"status"`
	}
	members := map[string]member{}
	var cursor *string
	seen := map[string]bool{}
	for {
		var page struct {
			Data       []string `json:"data"`
			NextCursor *string  `json:"nextCursor"`
		}
		if err := c.Call(ctx, "thread/loaded/list", map[string]any{"cursor": cursor, "limit": 100}, &page); err != nil {
			return false, err
		}
		for _, loadedID := range page.Data {
			if loadedID == id {
				continue
			}
			var result struct {
				Thread member `json:"thread"`
			}
			if err := c.Call(ctx, "thread/read", map[string]any{"threadId": loadedID, "includeTurns": false}, &result); err != nil {
				return false, err
			}
			if result.Thread.ID != loadedID {
				return false, fmt.Errorf("loaded thread identity changed")
			}
			members[loadedID] = result.Thread
		}
		if page.NextCursor == nil {
			break
		}
		if seen[*page.NextCursor] {
			return false, fmt.Errorf("loaded thread cursor repeated")
		}
		seen[*page.NextCursor] = true
		cursor = page.NextCursor
	}
	for _, child := range members {
		ancestor, visited := child.ParentID, map[string]bool{}
		for ancestor != "" && ancestor != id {
			if visited[ancestor] {
				return false, fmt.Errorf("loaded thread ancestry repeated")
			}
			visited[ancestor] = true
			ancestor = members[ancestor].ParentID
		}
		if ancestor != id {
			continue
		}
		if child.Status.Type != "idle" && child.Status.Type != "notLoaded" {
			return true, nil
		}
		if child.Status.Type == "idle" {
			var terminals struct {
				Data       []struct{} `json:"data"`
				NextCursor *string    `json:"nextCursor"`
			}
			if err := c.Call(ctx, "thread/backgroundTerminals/list", map[string]any{"threadId": child.ID, "limit": 1}, &terminals); err != nil {
				return false, err
			}
			if len(terminals.Data) > 0 || terminals.NextCursor != nil {
				return true, nil
			}
		}
	}
	return false, nil
}
