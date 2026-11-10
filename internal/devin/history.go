package devin

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/shenming0821/Fuyu/internal/agent"
)

// listEntry matches the cognition.ai session/list response shape.
type listEntry struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updatedAt"`
}

// listSessions fetches Devin's native session index.
func (c *Client) listSessions(ctx context.Context, cursor *string) ([]listEntry, *string, error) {
	c.mu.Lock()
	ready := !c.closed && !c.failed && c.conn != nil
	link := c.conn
	c.mu.Unlock()
	if !ready {
		return nil, nil, fmt.Errorf("Devin 连接已关闭")
	}
	params := map[string]any{}
	if cursor != nil && *cursor != "" {
		params["cursor"] = *cursor
	}
	result, err := link.call(ctx, "session/list", params)
	if err != nil {
		return nil, nil, err
	}
	var response struct {
		Sessions   []listEntry `json:"sessions"`
		NextCursor *string     `json:"nextCursor"`
	}
	if json.Unmarshal(result, &response) != nil {
		return nil, nil, fmt.Errorf("Devin 会话列表响应不完整")
	}
	return response.Sessions, response.NextCursor, nil
}

// ResumeThread attaches a native Devin session: session/load replays its
// updates into the recorded chat turns and exposes the current options.
func (c *Client) ResumeThread(ctx context.Context, id string) (agent.ResumedThread, error) {
	c.mu.Lock()
	ready := !c.closed && !c.failed && c.conn != nil
	link := c.conn
	s := c.sessions[id]
	c.mu.Unlock()
	if !ready {
		return agent.ResumedThread{}, fmt.Errorf("Devin 连接已关闭")
	}
	if s == nil {
		s = &acpSession{id: id, tools: make(map[string]*toolState), state: c.loadState(id)}
		c.mu.Lock()
		c.sessions[id] = s
		c.mu.Unlock()
	}
	c.mu.Lock()
	if s.loading {
		c.mu.Unlock()
		return agent.ResumedThread{}, fmt.Errorf("Devin 会话正在恢复中")
	}
	if s.cwd == "" {
		s.cwd = s.state.Cwd
	}
	needsMeta := s.cwd == "" || s.title == ""
	s.loading, s.replayBuf = true, nil
	c.mu.Unlock()
	if needsMeta {
		c.lookupSessionMeta(ctx, s)
	}
	c.mu.Lock()
	cwd := s.cwd
	c.mu.Unlock()
	if cwd == "" {
		c.mu.Lock()
		s.replayBuf, s.loading = nil, false
		c.mu.Unlock()
		return agent.ResumedThread{}, fmt.Errorf("Devin 会话 %s 的工作目录未知", id)
	}
	result, err := link.call(ctx, "session/load", map[string]any{"sessionId": id, "cwd": cwd, "mcpServers": []any{}})
	c.mu.Lock()
	replay := append([]json.RawMessage(nil), s.replayBuf...)
	s.replayBuf, s.loading = nil, false
	c.mu.Unlock()
	if err != nil {
		return agent.ResumedThread{}, err
	}
	var response struct {
		Modes struct {
			Current   string              `json:"currentModeId"`
			Available []agent.SessionMode `json:"availableModes"`
		} `json:"modes"`
		ConfigOptions json.RawMessage `json:"configOptions"`
	}
	if json.Unmarshal(result, &response) != nil {
		return agent.ResumedThread{}, fmt.Errorf("Devin 会话响应不完整")
	}
	turns, users := replayTurns(replay)
	s.state.assignTurnIDs(turns, users)
	c.mu.Lock()
	for _, raw := range replay {
		var info struct {
			Kind  string `json:"sessionUpdate"`
			Title string `json:"title"`
		}
		if json.Unmarshal(raw, &info) == nil && info.Kind == "session_info_update" && info.Title != "" {
			s.title = info.Title
		}
	}
	s.loaded = true
	s.mode = response.Modes.Current
	s.modes = response.Modes.Available
	if s.options == nil {
		s.options = map[string]*configOption{}
	}
	for optionID, option := range parseConfigOptions(response.ConfigOptions) {
		s.options[optionID] = option
	}
	syncModes(s)
	s.replay = turns
	newCwd := s.state.Cwd != s.cwd
	s.state.Cwd = s.cwd
	c.mu.Unlock()
	if newCwd {
		_ = c.saveState(s.state)
	}
	c.applyDefaultMode(ctx, s)
	c.emitRuntime(s)
	c.mu.Lock()
	model, mode := "", s.mode
	if s.options["model"] != nil {
		model = s.options["model"].Current
	}
	c.mu.Unlock()
	return agent.ResumedThread{
		ThreadMeta: agent.ThreadMeta{ID: s.id, Cwd: s.cwd, Preview: s.title, Status: agent.ThreadStatus{Type: "idle"}},
		Model:      model, Cwd: s.cwd, Policy: json.RawMessage(fmt.Sprintf("%q", "devin:"+mode)),
		Sandbox: json.RawMessage(`{"type":"devinAcp"}`), RuntimeVersion: c.version,
	}, ctx.Err()
}

// lookupSessionMeta fills a session's title and directory from the paged
// native index; session/load requires the recorded cwd even when loading by
// id, so the lookup keeps paging until the entry appears or the index ends.
func (c *Client) lookupSessionMeta(ctx context.Context, s *acpSession) {
	var cursor *string
	for pages := 0; pages < 20; pages++ {
		entries, next, err := c.listSessions(ctx, cursor)
		if err != nil {
			return
		}
		found := false
		for _, entry := range entries {
			if entry.SessionID != s.id {
				continue
			}
			found = true
			c.mu.Lock()
			if s.cwd == "" {
				s.cwd = entry.Cwd
			}
			if s.title == "" {
				s.title = entry.Title
			}
			c.mu.Unlock()
			break
		}
		if found || next == nil || *next == "" {
			return
		}
		cursor = next
	}
}

// ListTurns pages the recorded replay; the cursor is the count of turns kept
// from the end so a late page stays consistent within one attachment.
func (c *Client) ListTurns(ctx context.Context, id string, cursor *string) (agent.TurnsPage, error) {
	c.mu.Lock()
	s := c.sessions[id]
	replay := append([]agent.HistoryTurn(nil), s.replay...)
	c.mu.Unlock()
	if s == nil || len(replay) == 0 {
		return agent.TurnsPage{}, ctx.Err()
	}
	end := len(replay)
	if cursor != nil {
		value, err := strconv.Atoi(*cursor)
		if err != nil || value < 0 || value > len(replay) {
			return agent.TurnsPage{}, fmt.Errorf("Devin 历史分页游标无效")
		}
		end = value
	}
	start := max(0, end-20)
	data := slices.Clone(replay[start:end])
	slices.Reverse(data)
	page := agent.TurnsPage{Data: data}
	if start > 0 {
		next := strconv.Itoa(start)
		page.NextCursor = &next
	}
	return page, ctx.Err()
}

// ListThreads returns Devin's native session index; sessions the runtime
// reports keep their own title and directory.
func (c *Client) ListThreads(ctx context.Context, cursor *string) (agent.ThreadsPage, error) {
	entries, next, err := c.listSessions(ctx, cursor)
	if err != nil {
		return agent.ThreadsPage{}, err
	}
	page := agent.ThreadsPage{NextCursor: next}
	c.mu.Lock()
	for _, entry := range entries {
		if s := c.sessions[entry.SessionID]; s != nil && entry.Title != "" {
			s.title = entry.Title
		}
	}
	c.mu.Unlock()
	for _, entry := range entries {
		title := entry.Title
		if title == "" {
			title = "新会话"
		}
		page.Data = append(page.Data, agent.ThreadMeta{ID: entry.SessionID, Cwd: entry.Cwd, Preview: title, Status: agent.ThreadStatus{Type: "notLoaded"}})
	}
	return page, nil
}

// ReadThread hands the reconciler the recorded replay; it never fabricates
// turns the runtime did not stream.
func (c *Client) ReadThread(ctx context.Context, id string) (json.RawMessage, error) {
	c.mu.Lock()
	s := c.sessions[id]
	replay := append([]agent.HistoryTurn(nil), s.replay...)
	c.mu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("Devin 会话不存在")
	}
	raw, err := json.Marshal(map[string]any{"thread": map[string]any{"id": id, "turns": replay}})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return raw, err
}

// ReleaseIdleThread closes an idle attachment so its native session stops
// streaming updates; the session itself survives and reloads on demand.
func (c *Client) ReleaseIdleThread(ctx context.Context, id string) (bool, error) {
	c.mu.Lock()
	s := c.sessions[id]
	busy := s != nil && s.turn.ID != ""
	link := c.conn
	c.mu.Unlock()
	if s == nil {
		return true, nil
	}
	if busy {
		return false, nil
	}
	if link != nil {
		_, _ = link.call(ctx, "session/close", map[string]any{"sessionId": id})
	}
	c.mu.Lock()
	s.loaded = false
	s.replay = nil
	c.mu.Unlock()
	return true, nil
}

// replayTurns folds a captured session/load update stream into chat turns:
// user_message chunks open a turn; agent chunks and tool calls fill items.
func replayTurns(updates []json.RawMessage) ([]agent.HistoryTurn, []string) {
	var turns []agent.HistoryTurn
	var users []string
	var current *agent.HistoryTurn
	open := func() *agent.HistoryTurn {
		if current == nil {
			turns = append(turns, agent.HistoryTurn{Status: "completed", ItemsView: "full"})
			users = append(users, "")
			current = &turns[len(turns)-1]
		}
		return current
	}
	appendItem := func(item map[string]any) int {
		encoded, _ := json.Marshal(item)
		current.Items = append(current.Items, encoded)
		return len(current.Items) - 1
	}
	tools := map[string]struct {
		turn, item int
		state      *toolState
	}{}
	for _, raw := range updates {
		var envelope struct {
			Kind string `json:"sessionUpdate"`
		}
		if json.Unmarshal(raw, &envelope) != nil {
			continue
		}
		switch envelope.Kind {
		case "user_message_chunk", "user_message":
			var chunk struct {
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if json.Unmarshal(raw, &chunk) != nil {
				continue
			}
			current = nil
			turn := open()
			turn.ID = fmt.Sprintf("turn-%d", len(turns))
			users[len(users)-1] = userPrefix(chunk.Content.Text)
			appendItem(map[string]any{"id": turn.ID + "-user", "type": "userMessage", "status": "completed",
				"content": []any{map[string]any{"type": "text", "text": chunk.Content.Text}}})
		case "agent_message_chunk":
			var chunk struct {
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if json.Unmarshal(raw, &chunk) != nil || chunk.Content.Text == "" {
				continue
			}
			turn := open()
			if len(turn.Items) > 0 {
				var last map[string]any
				if json.Unmarshal(turn.Items[len(turn.Items)-1], &last) == nil && last["type"] == "agentMessage" {
					last["text"], _ = last["text"].(string)
					last["text"] = last["text"].(string) + chunk.Content.Text
					encoded, _ := json.Marshal(last)
					turn.Items[len(turn.Items)-1] = encoded
					continue
				}
			}
			appendItem(map[string]any{"id": fmt.Sprintf("turn-%d-assistant-%d", len(turns), len(turn.Items)), "type": "agentMessage", "status": "completed", "text": chunk.Content.Text})
		case "agent_thought_chunk":
			var chunk struct {
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if json.Unmarshal(raw, &chunk) != nil || chunk.Content.Text == "" {
				continue
			}
			turn := open()
			if len(turn.Items) > 0 {
				var last map[string]any
				if json.Unmarshal(turn.Items[len(turn.Items)-1], &last) == nil && last["type"] == "reasoning" {
					last["text"] = last["text"].(string) + chunk.Content.Text
					encoded, _ := json.Marshal(last)
					turn.Items[len(turn.Items)-1] = encoded
					continue
				}
			}
			appendItem(map[string]any{"id": fmt.Sprintf("turn-%d-reasoning-%d", len(turns), len(turn.Items)), "type": "reasoning", "status": "completed", "text": chunk.Content.Text})
		case "tool_call":
			var call struct {
				ToolCallID string          `json:"toolCallId"`
				Title      string          `json:"title"`
				Kind       string          `json:"kind"`
				Status     string          `json:"status"`
				Content    []toolContent   `json:"content"`
				Locations  []toolLocation  `json:"locations"`
				RawInput   json.RawMessage `json:"rawInput"`
				Meta       struct {
					ToolName string `json:"cognition.ai/inferenceToolName"`
					Cwd      string `json:"cognition.ai/cwd"`
				} `json:"_meta"`
			}
			if json.Unmarshal(raw, &call) != nil || call.ToolCallID == "" {
				continue
			}
			open()
			state := &toolState{title: call.Title, cwd: call.Meta.Cwd, command: rawInputCommand(call.RawInput), arguments: call.RawInput}
			state.changeKind = call.Kind
			state.status = map[string]string{"completed": "completed", "failed": "failed"}[call.Status]
			if state.status == "" {
				state.status = "inProgress"
			}
			switch call.Kind {
			case "execute":
				state.kind = "commandExecution"
			case "edit", "delete", "move":
				state.kind = "fileChange"
			default:
				state.kind = "dynamicToolCall"
				state.name = firstNonEmpty(call.Meta.ToolName, call.Kind)
			}
			if state.command == "" {
				state.command = call.Title
			}
			state.output = contentText(call.Content)
			state.setChanges(call.Content, call.Locations)
			index := appendItem(state.item(call.ToolCallID))
			state.done = state.status == "completed" || state.status == "failed"
			if state.done {
				if item := state.changeItem(call.ToolCallID); item != nil {
					appendItem(item)
				}
			}
			tools[call.ToolCallID] = struct {
				turn, item int
				state      *toolState
			}{len(turns) - 1, index, state}
		case "tool_call_update":
			var u struct {
				ToolCallID string         `json:"toolCallId"`
				Status     string         `json:"status"`
				Content    []toolContent  `json:"content"`
				Locations  []toolLocation `json:"locations"`
			}
			if json.Unmarshal(raw, &u) != nil {
				continue
			}
			ref, ok := tools[u.ToolCallID]
			if !ok || ref.turn >= len(turns) || ref.item >= len(turns[ref.turn].Items) {
				continue
			}
			state := ref.state
			if state.done {
				continue
			}
			state.setChanges(u.Content, u.Locations)
			if text := contentText(u.Content); text != "" {
				state.output = text
			}
			if u.Status == "completed" || u.Status == "failed" {
				state.status = u.Status
				state.done = true
			}
			encoded, _ := json.Marshal(state.item(u.ToolCallID))
			turns[ref.turn].Items[ref.item] = encoded
			if state.done {
				if item := state.changeItem(u.ToolCallID); item != nil {
					encoded, _ := json.Marshal(item)
					turns[ref.turn].Items = append(turns[ref.turn].Items, encoded)
				}
			}
		}
	}
	return turns, users
}
