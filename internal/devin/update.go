package devin

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shenming0821/Fuyu/internal/agent"
)

// toolState accumulates one ACP tool_call across its update notifications.
type toolState struct {
	kind       string
	changeKind string
	status     string
	title      string
	command    string
	cwd        string
	name       string
	arguments  json.RawMessage
	changes    []map[string]any
	output     string
	exitCode   *int
	done       bool
}

// permission tracks an inbound session/request_permission until Respond or
// the turn ends; option kinds select the ACP option each app decision maps to.
type permission struct {
	session   *acpSession
	options   []permissionOption
	requestID json.RawMessage
}

type permissionOption struct {
	ID   string `json:"optionId"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type toolLocation struct {
	Path string `json:"path"`
}

func (c *Client) onUpdate(sessionID string, raw json.RawMessage) {
	var update struct {
		Kind string `json:"sessionUpdate"`
	}
	if json.Unmarshal(raw, &update) != nil {
		return
	}
	c.mu.Lock()
	s := c.sessions[sessionID]
	if s != nil && s.loading {
		s.replayBuf = append(s.replayBuf, json.RawMessage(append([]byte(nil), raw...)))
	}
	c.mu.Unlock()
	if s == nil || s.loading {
		return
	}
	switch update.Kind {
	case "agent_message_chunk":
		var chunk struct {
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &chunk) != nil || chunk.Content.Text == "" {
			return
		}
		c.mu.Lock()
		turn := s.turn
		c.mu.Unlock()
		if turn.ID == "" {
			return
		}
		c.emit(s, "item/agentMessage/delta", map[string]any{
			"threadId": s.id, "turnId": string(turn.ID), "itemId": string(turn.ID) + "-assistant", "delta": chunk.Content.Text,
		})
	case "tool_call":
		c.toolStart(s, raw)
	case "tool_call_update":
		c.toolUpdate(s, raw)
	case "available_commands_update":
		c.updateCommands(s, raw)
	case "config_option_update":
		var u struct {
			Options json.RawMessage `json:"configOptions"`
		}
		if json.Unmarshal(raw, &u) != nil {
			return
		}
		c.mu.Lock()
		previous := ""
		if s.options["model"] != nil {
			previous = s.options["model"].Current
		}
		for id, option := range parseConfigOptions(u.Options) {
			s.options[id] = option
		}
		syncModes(s)
		current := ""
		if s.options["model"] != nil {
			current = s.options["model"].Current
		}
		c.mu.Unlock()
		if current != "" && current != previous {
			c.emitRuntime(s)
		}
	case "current_mode_update":
		var u struct {
			Mode string `json:"currentModeId"`
		}
		if json.Unmarshal(raw, &u) == nil && u.Mode != "" {
			c.mu.Lock()
			s.mode = u.Mode
			if s.modeAck != nil {
				select {
				case s.modeAck <- u.Mode:
				default:
				}
			}
			c.mu.Unlock()
			c.emitRuntime(s)
		}
	case "session_info_update":
		var u struct {
			Title string `json:"title"`
		}
		if json.Unmarshal(raw, &u) == nil && u.Title != "" {
			c.mu.Lock()
			s.title = u.Title
			c.mu.Unlock()
		}
	}
	// agent_thought_chunk, plan, usage_update, user_message(_chunk) outside
	// replay and unknown update kinds are intentionally not projected.
}

var toolKinds = map[string]bool{
	"read": true, "edit": true, "delete": true, "move": true, "search": true,
	"execute": true, "think": true, "fetch": true, "switch_mode": true, "other": true,
}

func (c *Client) toolStart(s *acpSession, raw json.RawMessage) {
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
		return
	}
	c.mu.Lock()
	turn := s.turn
	if s.tools == nil {
		s.tools = make(map[string]*toolState)
	}
	state := s.tools[call.ToolCallID]
	if state == nil {
		state = &toolState{}
		s.tools[call.ToolCallID] = state
	}
	c.mu.Unlock()
	if turn.ID == "" || state.done {
		return
	}
	state.title, state.cwd = call.Title, firstNonEmpty(call.Meta.Cwd, s.cwd)
	if !toolKinds[call.Kind] {
		call.Kind = "other"
	}
	switch call.Kind {
	case "execute":
		state.kind = "commandExecution"
		state.command = rawInputCommand(call.RawInput)
		if state.command == "" {
			state.command = call.Title
		}
	case "edit", "delete", "move":
		state.kind = "fileChange"
	default:
		state.kind = "dynamicToolCall"
		state.name = firstNonEmpty(call.Meta.ToolName, call.Kind)
		state.arguments = call.RawInput
	}
	if state.command == "" && state.kind != "fileChange" {
		state.command = call.Title
	}
	state.changeKind = call.Kind
	state.setChanges(call.Content, call.Locations)
	state.status, state.output = "inProgress", contentText(call.Content)
	method := "item/started"
	if call.Status == "completed" || call.Status == "failed" {
		state.status, state.done, method = call.Status, true, "item/completed"
	}
	c.emit(s, method, map[string]any{"threadId": s.id, "turnId": string(turn.ID), "item": state.item(call.ToolCallID)})
	if state.done {
		if item := state.changeItem(call.ToolCallID); item != nil {
			c.emit(s, method, map[string]any{"threadId": s.id, "turnId": string(turn.ID), "item": item})
		}
	}
	if state.output != "" && state.kind == "commandExecution" {
		c.emit(s, "item/commandExecution/outputDelta", map[string]any{
			"threadId": s.id, "turnId": string(turn.ID), "itemId": call.ToolCallID, "delta": state.output,
		})
	}
}

func (c *Client) toolUpdate(s *acpSession, raw json.RawMessage) {
	var u struct {
		ToolCallID string         `json:"toolCallId"`
		Title      string         `json:"title"`
		Status     string         `json:"status"`
		Content    []toolContent  `json:"content"`
		Locations  []toolLocation `json:"locations"`
		Meta       struct {
			ToolName string `json:"cognition.ai/inferenceToolName"`
			Cwd      string `json:"cognition.ai/cwd"`
			Exit     *struct {
				Code *int `json:"exit_code"`
			} `json:"cognition.ai/terminal_exit"`
		} `json:"_meta"`
	}
	if json.Unmarshal(raw, &u) != nil || u.ToolCallID == "" {
		return
	}
	c.mu.Lock()
	turn := s.turn
	state := s.tools[u.ToolCallID]
	c.mu.Unlock()
	if turn.ID == "" || state == nil || state.done {
		return
	}
	if u.Title != "" {
		state.title = u.Title
	}
	if u.Meta.Cwd != "" {
		state.cwd = u.Meta.Cwd
	}
	if u.Meta.ToolName != "" && state.kind == "dynamicToolCall" {
		state.name = u.Meta.ToolName
	}
	if u.Meta.Exit != nil && u.Meta.Exit.Code != nil {
		state.exitCode = u.Meta.Exit.Code
	}
	state.setChanges(u.Content, u.Locations)
	if text := contentText(u.Content); text != "" && !state.done {
		if strings.HasPrefix(text, state.output) {
			if state.kind == "commandExecution" && len(text) > len(state.output) {
				c.emit(s, "item/commandExecution/outputDelta", map[string]any{
					"threadId": s.id, "turnId": string(turn.ID), "itemId": u.ToolCallID, "delta": text[len(state.output):],
				})
			}
		}
		state.output = text
	}
	switch u.Status {
	case "completed", "failed":
		if state.done {
			return
		}
		state.done = true
		state.status = u.Status
		c.emit(s, "item/completed", map[string]any{"threadId": s.id, "turnId": string(turn.ID), "item": state.item(u.ToolCallID)})
		if item := state.changeItem(u.ToolCallID); item != nil {
			c.emit(s, "item/completed", map[string]any{"threadId": s.id, "turnId": string(turn.ID), "item": item})
		}
	case "in_progress", "pending":
		state.status = "inProgress"
	}
}

type toolContent struct {
	Type    string  `json:"type"`
	Path    string  `json:"path"`
	OldText *string `json:"oldText"`
	NewText *string `json:"newText"`
	Content struct {
		Text string `json:"text"`
	} `json:"content"`
}

// contentText joins text payloads carried in a tool call's content blocks.
func contentText(blocks []toolContent) string {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == "content" && block.Content.Text != "" {
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(block.Content.Text)
		}
	}
	return text.String()
}

func diffChanges(blocks []toolContent, kind string, locations []toolLocation) []map[string]any {
	var changes []map[string]any
	changeKind := map[string]string{"edit": "update", "delete": "delete", "move": "update"}[kind]
	if changeKind == "" {
		changeKind = "update"
	}
	seen := map[string]bool{}
	for _, block := range blocks {
		if block.Type != "diff" || block.Path == "" {
			continue
		}
		change := map[string]any{"type": changeKind, "snapshot": true}
		if block.OldText == nil && changeKind == "update" {
			change["type"] = "add"
		}
		var diff strings.Builder
		if block.NewText == nil || (block.OldText == nil && *block.NewText == "") {
			change = map[string]any{"type": changeKind, "region": true}
		} else {
			oldText := ""
			if block.OldText != nil {
				oldText = *block.OldText
			}
			oldLines, newLines := snapshotLines(oldText), snapshotLines(*block.NewText)
			oldStart, newStart := 1, 1
			if len(oldLines) == 0 {
				oldStart = 0
			}
			if len(newLines) == 0 {
				newStart = 0
			}
			fmt.Fprintf(&diff, "@@ -%d,%d +%d,%d @@\n", oldStart, len(oldLines), newStart, len(newLines))
			writeSnapshotLines(&diff, "-", oldText, oldLines)
			writeSnapshotLines(&diff, "+", *block.NewText, newLines)
		}
		raw, _ := json.Marshal(change)
		changes = append(changes, map[string]any{"path": block.Path, "kind": json.RawMessage(raw), "diff": diff.String()})
		seen[block.Path] = true
	}
	for _, location := range locations {
		if location.Path != "" && !seen[location.Path] {
			raw, _ := json.Marshal(map[string]any{"type": changeKind, "region": true})
			changes = append(changes, map[string]any{"path": location.Path, "kind": json.RawMessage(raw), "diff": ""})
		}
	}
	return changes
}

func snapshotLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func writeSnapshotLines(diff *strings.Builder, prefix, text string, lines []string) {
	for index, line := range lines {
		diff.WriteString(prefix + line + "\n")
		if index == len(lines)-1 && !strings.HasSuffix(text, "\n") {
			diff.WriteString("\\ No newline at end of file\n")
		}
	}
}

func (t *toolState) setChanges(content []toolContent, locations []toolLocation) {
	if t.kind != "fileChange" {
		locations = nil
	}
	changes := diffChanges(content, t.changeKind, locations)
	provided := map[string]bool{}
	for _, block := range content {
		if block.Type == "diff" {
			provided[block.Path] = true
		}
	}
	for index, change := range changes {
		path, _ := change["path"].(string)
		if provided[path] {
			continue
		}
		for _, previous := range t.changes {
			if previous["path"] == path {
				changes[index] = previous
				break
			}
		}
	}
	if len(changes) > 0 {
		t.changes = changes
	}
}

func (t *toolState) changeItem(id string) map[string]any {
	if t.kind == "fileChange" || len(t.changes) == 0 {
		return nil
	}
	return map[string]any{"id": id + ":changes", "type": "fileChange", "status": t.status, "changes": t.changes}
}

func rawInputCommand(raw json.RawMessage) string {
	var input struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(raw, &input) == nil {
		return input.Command
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (t *toolState) item(id string) map[string]any {
	item := map[string]any{
		"id": id, "type": t.kind, "status": firstNonEmpty(t.status, "inProgress"),
		"command": t.command, "cwd": t.cwd,
	}
	if t.name != "" {
		item["tool"], item["server"] = t.name, "Devin"
	}
	if len(t.arguments) > 0 {
		item["arguments"] = t.arguments
	}
	if len(t.changes) > 0 && t.kind == "fileChange" {
		item["changes"] = t.changes
	}
	if t.output != "" {
		item["aggregatedOutput"] = t.output
	}
	if t.exitCode != nil {
		item["exitCode"] = *t.exitCode
	}
	return item
}

// updateCommands caches the runtime's native command surface for the
// completion catalog. Commands keep their hint and cognition category.
func (c *Client) updateCommands(s *acpSession, raw json.RawMessage) {
	var u struct {
		Commands []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Input       *struct {
				Hint string `json:"hint"`
			} `json:"input"`
			Meta struct {
				Category string `json:"cognition.ai/category"`
			} `json:"_meta"`
		} `json:"availableCommands"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return
	}
	commands := make([]agent.Command, 0, len(u.Commands))
	for _, command := range u.Commands {
		entry := agent.Command{Name: command.Name, Description: command.Description, Source: "Devin", Cwd: s.cwd, IsSkill: command.Meta.Category == "Skills"}
		if command.Input != nil && command.Input.Hint != "" {
			entry.Arguments = []string{command.Input.Hint}
		}
		commands = append(commands, entry)
	}
	c.mu.Lock()
	c.commands = commands
	c.mu.Unlock()
}

// onRequest routes agent-initiated requests. Only permission requests are
// supported; fs/terminal are declined by the negotiated capabilities.
func (c *Client) onRequest(id json.RawMessage, method string, params json.RawMessage) {
	if method != "session/request_permission" {
		_ = c.conn.respondError(id, -32601, "unsupported request")
		return
	}
	var request struct {
		SessionID string `json:"sessionId"`
		ToolCall  struct {
			ID       string          `json:"toolCallId"`
			Title    string          `json:"title"`
			RawInput json.RawMessage `json:"rawInput"`
			Meta     struct {
				Cwd string `json:"cognition.ai/cwd"`
			} `json:"_meta"`
		} `json:"toolCall"`
		Options []permissionOption `json:"options"`
	}
	if json.Unmarshal(params, &request) != nil || len(request.Options) == 0 {
		_ = c.conn.respond(id, map[string]any{"outcome": map[string]any{"outcome": "cancelled"}})
		return
	}
	c.mu.Lock()
	s := c.sessions[request.SessionID]
	var turn agent.TurnIdentity
	if s != nil {
		turn = s.turn
	}
	c.mu.Unlock()
	if s == nil || turn.ID == "" {
		_ = c.conn.respond(id, map[string]any{"outcome": map[string]any{"outcome": "cancelled"}})
		return
	}
	itemID := request.ToolCall.ID
	if itemID == "" {
		itemID = "permission-" + string(id)
	}
	has := map[string]bool{}
	for _, option := range request.Options {
		has[option.Kind] = true
	}
	decisions := []string{}
	if has["allow_once"] {
		decisions = append(decisions, "accept", "acceptForSession")
	}
	if has["allow_always"] && !has["allow_once"] {
		decisions = append(decisions, "acceptForSession")
	}
	if has["reject_once"] || has["reject_always"] {
		decisions = append(decisions, "decline")
	}
	decisions = append(decisions, "cancel")
	raw := make([]json.RawMessage, 0, len(decisions))
	for _, decision := range decisions {
		value, _ := json.Marshal(decision)
		raw = append(raw, value)
	}
	c.mu.Lock()
	if c.permissions == nil {
		c.permissions = make(map[string]*permission)
	}
	c.permissions[string(id)] = &permission{session: s, options: request.Options, requestID: append(json.RawMessage(nil), id...)}
	c.mu.Unlock()
	c.emitID(s, "item/commandExecution/requestApproval", id, map[string]any{
		"threadId": s.id, "turnId": string(turn.ID), "itemId": itemID,
		"command": firstNonEmpty(rawInputCommand(request.ToolCall.RawInput), request.ToolCall.Title),
		"cwd":     firstNonEmpty(request.ToolCall.Meta.Cwd, s.cwd),
		"reason":  request.ToolCall.Title, "availableDecisions": raw,
	})
}

// Respond maps an app decision onto the narrowest matching ACP option kind.
// Unmatched or cancelled intents resolve to the cancelled outcome.
func (c *Client) Respond(id json.RawMessage, result any) error {
	c.mu.Lock()
	perm := c.permissions[string(id)]
	delete(c.permissions, string(id))
	c.mu.Unlock()
	if perm == nil {
		return fmt.Errorf("Devin 审批请求已失效")
	}
	var response struct {
		Decision json.RawMessage `json:"decision"`
	}
	raw, _ := json.Marshal(result)
	if json.Unmarshal(raw, &response) != nil {
		return fmt.Errorf("Devin 审批响应无效")
	}
	var decision string
	_ = json.Unmarshal(response.Decision, &decision)
	optionID := pickOption(perm.options, decision)
	outcome := map[string]any{"outcome": "cancelled"}
	if optionID != "" {
		outcome = map[string]any{"outcome": "selected", "optionId": optionID}
	}
	if err := c.conn.respond(id, map[string]any{"outcome": outcome}); err != nil {
		c.mu.Lock()
		c.permissions[string(id)] = perm
		c.mu.Unlock()
		return err
	}
	c.emit(perm.session, "serverRequest/resolved", map[string]any{"requestId": perm.requestID})
	return nil
}

func pickOption(options []permissionOption, decision string) string {
	want := map[string][]string{
		"accept":           {"allow_once"},
		"acceptForSession": {"allow_always", "allow_once"},
		"decline":          {"reject_once", "reject_always"},
	}[decision]
	for _, kind := range want {
		for _, option := range options {
			if option.Kind == kind {
				return option.ID
			}
		}
	}
	return ""
}

// expirePermissions cancels a session's pending approvals when its turn ends
// or the connection dies, so the agent never waits on a dead card.
func (c *Client) expirePermissions(s *acpSession) {
	c.mu.Lock()
	var stale []*permission
	for key, perm := range c.permissions {
		if perm.session == s {
			stale = append(stale, perm)
			delete(c.permissions, key)
		}
	}
	c.mu.Unlock()
	for _, perm := range stale {
		_ = c.conn.respond(perm.requestID, map[string]any{"outcome": map[string]any{"outcome": "cancelled"}})
		c.emit(s, "serverRequest/resolved", map[string]any{"requestId": perm.requestID})
	}
}
