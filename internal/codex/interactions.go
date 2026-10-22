package codex

import (
	"encoding/json"
	"errors"
)

const MaxRequestBytes = 256 * 1024
const MaxPendingRequests = 16

type serverRequest struct {
	thread, turn string
	sending      bool
}

func (c *Client) handleRequest(msg envelope) {
	switch msg.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval",
		"item/permissions/requestApproval", "item/tool/requestUserInput":
	default:
		c.rejectUnsupported(msg)
		return
	}
	owner, decodeErr := DecodeInteraction(Event{ID: msg.ID, Method: msg.Method, Params: msg.Params})
	if len(msg.Params) > MaxRequestBytes || decodeErr != nil {
		_ = c.write(map[string]any{"id": msg.ID, "error": RPCError{Code: -32602, Message: "Invalid or oversized interaction"}})
		c.enqueue(Event{Method: "fuyu/unsupported", Err: errors.New("交互内容过大或格式无效，已拒绝；请停止后核验任务")})
		return
	}
	c.mu.Lock()
	key := string(msg.ID)
	if _, exists := c.server[key]; exists {
		c.mu.Unlock()
		return
	}
	if len(c.server) >= MaxPendingRequests {
		c.mu.Unlock()
		_ = c.write(map[string]any{"id": msg.ID, "error": RPCError{Code: -32000, Message: "Too many pending interactions"}})
		c.enqueue(Event{Method: "fuyu/unsupported", Err: errors.New("待处理交互达到上限，请先处理或停止任务")})
		return
	}
	c.server[key] = &serverRequest{thread: owner.ThreadID, turn: owner.TurnID}
	c.mu.Unlock()
	c.enqueue(Event{ID: msg.ID, Method: msg.Method, Params: msg.Params})
}

// Respond reserves a native request ID exactly once, including failed writes.
// A failed write is uncertain, so callers must not retry an approval blindly.
func (c *Client) Respond(id json.RawMessage, result any) error {
	c.mu.Lock()
	r := c.server[string(id)]
	if r == nil || r.sending {
		c.mu.Unlock()
		return errors.New("interaction is resolved, expired or already submitted")
	}
	select {
	case <-c.done:
		c.mu.Unlock()
		return errors.New("Codex connection closed")
	default:
	}
	r.sending = true
	c.mu.Unlock()
	err := c.write(map[string]any{"id": id, "result": result})
	// Keep the reservation until serverRequest/resolved or turn completion.
	return err
}

func (c *Client) expireRequests(msg envelope) {
	if msg.Method != "serverRequest/resolved" && msg.Method != "turn/completed" {
		return
	}
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
		ThreadID  string          `json:"threadId"`
		Turn      struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(msg.Params, &p) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if msg.Method == "serverRequest/resolved" {
		if r := c.server[string(p.RequestID)]; r != nil && r.thread == p.ThreadID {
			delete(c.server, string(p.RequestID))
		}
		return
	}
	for key, r := range c.server {
		if r.thread == p.ThreadID && r.turn == p.Turn.ID {
			delete(c.server, key)
		}
	}
}
