package claude

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func toolItem(block contentBlock, cwd string) map[string]any {
	item := map[string]any{"id": block.ID, "type": "dynamicToolCall", "tool": block.Name, "arguments": block.Input, "cwd": cwd, "status": "inProgress"}
	if block.Name == "Bash" {
		var input struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(block.Input, &input)
		item["type"], item["command"] = "commandExecution", input.Command
	}
	return item
}

func (r *execution) project(message frame) {
	if r.identity.ID == "" {
		return
	}
	if len(r.seen) >= agent.MaxQueueEvents || len(r.tools) >= agent.MaxQueueEvents || len(r.blocks) >= agent.MaxQueueEvents || len(r.offsets) >= agent.MaxQueueEvents {
		r.mu.Lock()
		r.problem = fmt.Errorf("Claude 单轮显示索引超过容量，请核验原生历史")
		r.mu.Unlock()
		go r.stopProcess()
		return
	}
	switch message.Type {
	case "stream_event":
		if message.ParentTool != "" {
			return
		}
		event := message.Event
		switch event.Type {
		case "message_start":
			r.messageID = event.Message.ID
			r.blocks = make(map[int]*contentBlock)
		case "content_block_start":
			block := event.Block
			r.blocks[event.Index] = &block
			if block.Type == "text" {
				block.ID = r.messageID + "-" + strconv.Itoa(event.Index)
				r.item("item/started", map[string]any{"id": block.ID, "type": "agentMessage", "text": block.Text})
			}
		case "content_block_delta":
			block := r.blocks[event.Index]
			if block == nil {
				return
			}
			if event.Delta.Type == "text_delta" {
				remaining := max(0, (2<<20)-len(block.Text))
				delta := event.Delta.Text
				if len(delta) > remaining {
					delta = delta[:remaining]
					for !utf8.ValidString(delta) {
						delta = delta[:len(delta)-1]
					}
					r.emit("fuyu/eventGap", nil)
				}
				block.Text += delta
				if delta != "" {
					r.emit("item/agentMessage/delta", map[string]any{"threadId": r.session.ID, "turnId": r.identity.ID, "itemId": block.ID, "delta": delta})
				}
			}
		case "content_block_stop":
			if block := r.blocks[event.Index]; block != nil && block.Type == "text" {
				r.item("item/completed", map[string]any{"id": block.ID, "type": "agentMessage", "text": block.Text})
			}
		}
	case "assistant":
		if message.UUID != "" && r.seen[message.UUID] {
			return
		}
		r.seen[message.UUID] = true
		if r.offsets == nil {
			r.offsets = make(map[string]int)
		}
		offset := r.offsets[message.Message.ID]
		r.offsets[message.Message.ID] += len(message.Message.Content)
		if message.ParentTool == "" {
			r.lastUUID = message.UUID
			if identity, ok := r.client.turns.Lookup(r.identity.Session, r.identity.Generation, r.identity.ID); ok {
				identity.TranscriptUUID, identity.MessageID = message.UUID, message.Message.ID
				_ = r.client.turns.Bind(identity)
			}
		}
		for index, block := range message.Message.Content {
			switch block.Type {
			case "text":
				if message.ParentTool != "" {
					continue
				}
				id := message.Message.ID + "-" + strconv.Itoa(offset+index)
				r.item("item/completed", map[string]any{"id": id, "type": "agentMessage", "text": block.Text})
			case "tool_use":
				item := toolItem(block, r.session.Cwd)
				if message.ParentTool != "" {
					item["parentToolUseId"] = message.ParentTool
				}
				r.tools[block.ID] = item
				var input struct {
					Background bool `json:"run_in_background"`
				}
				_ = json.Unmarshal(block.Input, &input)
				if input.Background {
					r.background[block.ID] = true
				}
				r.item("item/started", item)
			}
		}
	case "user":
		for _, block := range message.Message.Content {
			if block.Type != "tool_result" {
				continue
			}
			item := r.tools[block.ToolUseID]
			if item == nil {
				item = map[string]any{"id": block.ToolUseID, "type": "dynamicToolCall", "tool": "Claude tool"}
				r.tools[block.ToolUseID] = item
			}
			status := "completed"
			if block.IsError {
				status = "failed"
			}
			if r.background[block.ToolUseID] && !block.IsError {
				status = "inProgress"
			}
			item["status"] = status
			if item["type"] == "commandExecution" {
				var text string
				if json.Unmarshal(block.Content, &text) != nil {
					text = string(block.Content)
				}
				item["aggregatedOutput"] = text
			} else {
				item["result"] = block.Content
			}
			method := "item/completed"
			if status == "inProgress" {
				method = "item/started"
			}
			r.item(method, item)
			if !r.background[block.ToolUseID] {
				delete(r.tools, block.ToolUseID)
			}
		}
	case "system":
		if message.Subtype == "init" {
			r.client.mu.Lock()
			r.session.EffectiveModel, r.session.Version = message.Model, message.Version
			r.client.mu.Unlock()
			r.emit("fuyu/runtime", map[string]any{"threadId": r.session.ID, "model": message.Model, "version": message.Version})
		}
		if message.Subtype == "task_notification" {
			id := message.ToolUseID
			if id == "" {
				id = message.TaskID
			}
			if item := r.tools[id]; item != nil {
				item["status"], item["result"] = message.Status, message.Summary
				r.item("item/completed", item)
				delete(r.background, id)
			}
		}
	}
}

func (r *execution) permissionRequest(message frame, size int) {
	if r.turnDone != nil {
		select {
		case <-r.turnDone:
			_ = r.write(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": message.RequestID, "error": "Turn already completed"}})
			return
		default:
		}
	}
	var request struct {
		Subtype string `json:"subtype"`
		permission
	}
	if json.Unmarshal(message.Request, &request) != nil || message.RequestID == "" {
		return
	}
	if r.identity.ID == "" || request.Subtype != "can_use_tool" || size > 256<<10 {
		_ = r.write(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": message.RequestID, "error": "Sasa does not support this control request"}})
		return
	}
	r.mu.Lock()
	if r.resolved[message.RequestID] {
		r.mu.Unlock()
		return
	}
	if len(r.pending) >= 16 || len(r.resolved) >= agent.MaxQueueEvents {
		r.mu.Unlock()
		_ = r.write(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": message.RequestID, "error": "Too many pending permission requests"}})
		go r.stopProcess()
		return
	}
	if _, exists := r.pending[message.RequestID]; exists {
		r.mu.Unlock()
		return
	}
	request.permission.ID = message.RequestID
	r.pending[message.RequestID] = request.permission
	r.mu.Unlock()
	itemID := request.ToolID
	if itemID == "" {
		itemID = "permission-" + message.RequestID
	}
	params := map[string]any{"threadId": r.session.ID, "turnId": r.identity.ID, "itemId": itemID, "command": request.Tool + "\n" + string(request.Input), "cwd": r.session.Cwd, "availableDecisions": []string{"accept", "decline", "cancel"}}
	method := "item/commandExecution/requestApproval"
	if request.Tool == "AskUserQuestion" {
		delete(params, "command")
		var input struct {
			Questions []struct {
				Question    string `json:"question"`
				Header      string `json:"header"`
				MultiSelect bool   `json:"multiSelect"`
				Options     []struct {
					Label       string `json:"label"`
					Description string `json:"description"`
				} `json:"options"`
			} `json:"questions"`
		}
		if json.Unmarshal(request.Input, &input) != nil || len(input.Questions) == 0 {
			id, _ := json.Marshal(r.requestID(message.RequestID))
			_ = r.client.Respond(id, map[string]any{"answers": map[string]any{}})
			return
		}
		questions := []any{}
		for index, q := range input.Questions {
			questions = append(questions, map[string]any{"id": strconv.Itoa(index), "header": q.Header, "question": q.Question, "isOther": true, "multiSelect": q.MultiSelect, "options": q.Options})
		}
		params["questions"], params["isBlocking"] = questions, true
		method = "item/tool/requestUserInput"
	}
	raw, _ := json.Marshal(params)
	id, _ := json.Marshal(r.requestID(message.RequestID))
	if !r.client.queue.push(r.requestEvent(method, id, raw)) {
		go r.client.failQueue()
	}
}

func (r *execution) requestEvent(method string, id, raw json.RawMessage) agent.Event {
	return agent.Event{Session: r.identity.Session, Generation: r.identity.Generation, Method: method, ID: id, Params: raw}
}

func (r *execution) requestID(nativeID string) string {
	return r.session.ID + "/" + strconv.FormatUint(uint64(r.identity.Generation), 10) + "/" + nativeID
}

func (r *execution) cancelRequest(id string) {
	r.mu.Lock()
	_, exists := r.pending[id]
	delete(r.pending, id)
	if exists {
		if r.resolved == nil {
			r.resolved = make(map[string]bool)
		}
		r.resolved[id] = true
	}
	r.mu.Unlock()
	if exists {
		r.emit("serverRequest/resolved", map[string]any{"threadId": r.session.ID, "requestId": r.requestID(id)})
	}
}

func (c *Client) Respond(id json.RawMessage, result any) error {
	var scopedID string
	if json.Unmarshal(id, &scopedID) != nil {
		return fmt.Errorf("Claude 请求标识无效")
	}
	parts := strings.SplitN(scopedID, "/", 3)
	if len(parts) != 3 {
		return fmt.Errorf("Claude 请求缺少会话与进程归属")
	}
	requestID := parts[2]
	c.mu.Lock()
	var owner *execution
	var request permission
	for _, s := range c.sessions {
		if s.run == nil || s.ID != parts[0] || s.run.requestID(requestID) != scopedID {
			continue
		}
		s.run.mu.Lock()
		entry, exists := s.run.pending[requestID]
		if exists {
			owner, request = s.run, entry
		}
		s.run.mu.Unlock()
		if exists {
			break
		}
	}
	c.mu.Unlock()
	if owner == nil {
		return fmt.Errorf("Claude 请求已取消、提交或过期")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var response struct {
		Decision string `json:"decision"`
		Feedback string `json:"feedback"`
		Answers  map[string]struct {
			Answers []string `json:"answers"`
		} `json:"answers"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return fmt.Errorf("Claude 响应无效")
	}
	decision := map[string]any{"behavior": "deny", "message": "User denied this action"}
	if feedback := strings.TrimSpace(response.Feedback); feedback != "" {
		if len(feedback) > agent.MaxToolOutputBytes {
			return fmt.Errorf("Claude 拒绝反馈超过容量")
		}
		decision["message"] = feedback
	}
	if request.Tool == "AskUserQuestion" {
		if len(response.Answers) > 0 {
			var input map[string]any
			_ = json.Unmarshal(request.Input, &input)
			questions, _ := input["questions"].([]any)
			answers := map[string]string{}
			for index, question := range questions {
				value, _ := question.(map[string]any)
				q, _ := value["question"].(string)
				answer, ok := response.Answers[strconv.Itoa(index)]
				if !ok || len(answer.Answers) == 0 {
					return fmt.Errorf("Claude 问题缺少回答")
				}
				answers[q] = strings.Join(answer.Answers, ", ")
			}
			input["answers"] = answers
			decision = map[string]any{"behavior": "allow", "updatedInput": input}
		}
	} else {
		switch response.Decision {
		case "accept":
			decision = map[string]any{"behavior": "allow", "updatedInput": request.Input}
		case "decline":
		case "cancel":
			decision["interrupt"] = true
		default:
			return fmt.Errorf("Claude 只支持本次允许、拒绝或停止")
		}
	}
	owner.mu.Lock()
	if _, exists := owner.pending[requestID]; !exists || owner.resolved[requestID] {
		owner.mu.Unlock()
		return fmt.Errorf("Claude 请求已取消、提交或过期")
	}
	delete(owner.pending, requestID)
	if owner.resolved == nil {
		owner.resolved = make(map[string]bool)
	}
	owner.resolved[requestID] = true
	owner.mu.Unlock()
	if err := owner.write(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": requestID, "response": decision}}); err != nil {
		go owner.stopProcess()
		return fmt.Errorf("Claude 审批写入结果不确定，不能重复提交：%w", err)
	}
	owner.emit("serverRequest/resolved", map[string]any{"threadId": owner.session.ID, "requestId": owner.requestID(requestID)})
	return nil
}
