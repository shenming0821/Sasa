package claude

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/shenming0821/Fuyu/internal/agent"
)

type transcriptEntry struct {
	Type           string          `json:"type"`
	UUID           string          `json:"uuid"`
	Parent         string          `json:"parentUuid"`
	Sidechain      bool            `json:"isSidechain"`
	Meta           bool            `json:"isMeta"`
	Summary        bool            `json:"isCompactSummary"`
	Team           string          `json:"teamName"`
	SessionID      string          `json:"sessionId"`
	PromptID       string          `json:"promptId"`
	RepeatsSummary bool            `json:"repeatsCompactSummary"`
	Subtype        string          `json:"subtype"`
	Content        json.RawMessage `json:"content"`
	Level          string          `json:"level"`
	CommandRun     struct {
		Command string `json:"command"`
		Args    string `json:"args"`
	} `json:"commandRun"`
	Compact struct {
		Segment struct {
			Head   string `json:"headUuid"`
			Anchor string `json:"anchorUuid"`
			Tail   string `json:"tailUuid"`
		} `json:"preservedSegment"`
		Messages struct {
			Anchor   string   `json:"anchorUuid"`
			UUIDs    []string `json:"uuids"`
			AllUUIDs []string `json:"allUuids"`
		} `json:"preservedMessages"`
	} `json:"compactMetadata"`
	Message struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func (c *Client) transcript(id string) ([]byte, error) {
	if !validID(id) {
		return nil, fmt.Errorf("Claude 会话标识无效")
	}
	projects, err := os.ReadDir(filepath.Join(c.home, "projects"))
	if err != nil {
		return nil, err
	}
	var found string
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		path := filepath.Join(c.home, "projects", project.Name(), id+".jsonl")
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			if found != "" {
				return nil, fmt.Errorf("Claude 同一会话存在多个原生记录，请核验来源")
			}
			found = path
		}
	}
	if found == "" {
		return nil, fmt.Errorf("Claude 原生记录不存在：%s", id)
	}
	file, err := os.Open(found)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 64<<20 {
		return nil, fmt.Errorf("Claude 原生历史超过 64 MiB 读取上限，请使用原生 CLI 核验")
	}
	after, err := os.Stat(found)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("Claude 原生历史正在写入或已替换，请稍后重新核验")
	}
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		return nil, fmt.Errorf("Claude 原生历史尾行尚未写完")
	}
	return raw, nil
}

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func (c *Client) transcriptDigest(id string) (string, error) {
	raw, err := c.transcript(id)
	if err != nil {
		return "", err
	}
	return digest(raw), nil
}

func conversation(raw []byte, id string) ([]transcriptEntry, error) {
	nodes := map[string]transcriptEntry{}
	order := []string{}
	parents := map[string]bool{}
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if len(line) > agent.MaxFrameBytes {
			return nil, fmt.Errorf("Claude 历史行超过协议上限")
		}
		var entry transcriptEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, fmt.Errorf("Claude 原生历史 JSON 损坏：%w", err)
		}
		if entry.SessionID != "" && entry.SessionID != id {
			return nil, fmt.Errorf("Claude 历史会话归属不匹配")
		}
		if entry.UUID == "" {
			continue
		}
		switch entry.Type {
		case "user", "assistant", "progress", "system", "attachment":
		default:
			continue
		}
		if _, exists := nodes[entry.UUID]; !exists {
			order = append(order, entry.UUID)
		}
		nodes[entry.UUID] = entry
		if entry.Parent != "" {
			parents[entry.Parent] = true
		}
		if len(nodes) > 100000 {
			return nil, fmt.Errorf("Claude 历史记录数量超过读取上限")
		}
	}
	for _, key := range slices.Clone(order) {
		boundary := nodes[key]
		segment := boundary.Compact.Segment
		if boundary.Subtype != "compact_boundary" || segment.Head == "" {
			continue
		}
		head, headExists := nodes[segment.Head]
		anchor, anchorExists := nodes[segment.Anchor]
		if !headExists || !anchorExists || !anchor.Summary || anchor.Parent != key {
			return nil, fmt.Errorf("Claude compact 保留片段缺少主链或摘要")
		}
		head.Parent = segment.Anchor
		nodes[segment.Head] = head
		preserved := boundary.Compact.Messages.AllUUIDs
		if len(preserved) == 0 {
			preserved = boundary.Compact.Messages.UUIDs
		}
		if len(preserved) == 0 {
			return nil, fmt.Errorf("Claude compact 保留片段缺少顺序信息")
		}
		for _, id := range boundary.Compact.Messages.UUIDs {
			if _, exists := nodes[id]; !exists {
				return nil, fmt.Errorf("Claude compact 保留消息不存在")
			}
		}
		moving := map[string]bool{}
		available := []string{}
		for _, id := range preserved {
			if id == segment.Anchor || id == key || moving[id] {
				return nil, fmt.Errorf("Claude compact 保留片段标识无效")
			}
			if _, exists := nodes[id]; !exists {
				continue
			}
			moving[id] = true
			available = append(available, id)
		}
		var reordered []string
		for _, id := range order {
			if moving[id] {
				continue
			}
			reordered = append(reordered, id)
			if id == segment.Anchor {
				reordered = append(reordered, available...)
			}
		}
		order = reordered
	}
	clear(parents)
	for _, entry := range nodes {
		if entry.Parent != "" {
			parents[entry.Parent] = true
		}
	}
	positions := map[string]int{}
	for index, key := range order {
		positions[key] = index
	}
	leaf := ""
	best := -1
	for _, key := range order {
		if parents[key] {
			continue
		}
		seen := map[string]bool{}
		for key != "" {
			if seen[key] {
				return nil, fmt.Errorf("Claude 历史父链存在循环")
			}
			seen[key] = true
			entry, exists := nodes[key]
			if !exists {
				break
			}
			if entry.Type == "user" || entry.Type == "assistant" || entry.Type == "system" && entry.Subtype == "local_command" {
				if !entry.Sidechain && !entry.Meta && entry.Team == "" && positions[key] > best {
					leaf, best = key, positions[key]
				}
				break
			}
			key = entry.Parent
		}
	}
	var chain []transcriptEntry
	seen := map[string]bool{}
	for leaf != "" {
		if seen[leaf] {
			return nil, fmt.Errorf("Claude 历史父链存在循环")
		}
		seen[leaf] = true
		entry, exists := nodes[leaf]
		if !exists {
			return nil, fmt.Errorf("Claude 历史缺少父记录，无法重建有效分支")
		}
		chain = append(chain, entry)
		leaf = entry.Parent
	}
	slices.Reverse(chain)
	responses := map[string]bool{}
	calls := map[string]bool{}
	for _, entry := range chain {
		if entry.Meta {
			continue
		}
		if entry.Type == "assistant" && entry.Message.ID != "" {
			responses[entry.Message.ID] = true
		}
	}
	for _, key := range order {
		entry := nodes[key]
		if entry.Sidechain || entry.Team != "" || entry.Meta {
			continue
		}
		if entry.Type == "assistant" && responses[entry.Message.ID] {
			var blocks []contentBlock
			_ = json.Unmarshal(entry.Message.Content, &blocks)
			for _, block := range blocks {
				if block.Type == "tool_use" {
					calls[block.ID] = true
				}
			}
		}
	}
	for _, key := range order {
		if seen[key] {
			continue
		}
		entry := nodes[key]
		if entry.Sidechain || entry.Team != "" || entry.Meta {
			continue
		}
		recover := entry.Type == "assistant" && responses[entry.Message.ID]
		if entry.Type == "user" {
			var blocks []contentBlock
			_ = json.Unmarshal(entry.Message.Content, &blocks)
			for _, block := range blocks {
				if block.Type == "tool_result" && calls[block.ToolUseID] {
					recover = true
				}
			}
		}
		if recover {
			chain = append(chain, entry)
			seen[key] = true
		}
	}
	sort.SliceStable(chain, func(first, second int) bool { return positions[chain[first].UUID] < positions[chain[second].UUID] })
	return chain, nil
}

func (c *Client) history(s session) ([]agent.HistoryTurn, string, error) {
	if len(s.Turns) == 0 {
		return nil, "", nil
	}
	raw, err := c.transcript(s.ID)
	if err != nil {
		return nil, "", err
	}
	chain, err := conversation(raw, s.ID)
	if err != nil {
		return nil, "", err
	}
	checkpoints := map[string]checkpoint{}
	for _, saved := range s.Turns {
		checkpoints[saved.UserUUID] = saved
	}
	var turns []agent.HistoryTurn
	var current *agent.HistoryTurn
	currentPrompt := ""
	tools := map[string]map[string]any{}
	offsets := map[string]int{}
	appendItem := func(item map[string]any) {
		encoded, _ := json.Marshal(item)
		current.Items = append(current.Items, encoded)
	}
	for _, entry := range chain {
		if entry.Meta {
			continue
		}
		var blocks []contentBlock
		if saved, ok := checkpoints[entry.Parent]; ok && current != nil && current.ID == saved.ID {
			if output, ok := localCommandOutput(entry, saved); ok {
				appendItem(map[string]any{"id": entry.UUID, "type": "agentMessage", "text": output})
			}
		}
		if json.Unmarshal(entry.Message.Content, &blocks) != nil {
			var text string
			if json.Unmarshal(entry.Message.Content, &text) == nil {
				blocks = []contentBlock{{Type: "text", Text: text}}
			}
		}
		human := entry.Type == "user" && len(blocks) > 0
		for _, block := range blocks {
			if block.Type == "tool_result" {
				human = false
			}
		}
		if human && current != nil && current.Status == "interrupted" && currentPrompt != "" && entry.PromptID == currentPrompt && len(blocks) == 1 && blocks[0].Type == "text" && (blocks[0].Text == "[Request interrupted by user]" || blocks[0].Text == "[Request interrupted by user for tool use]") {
			if _, owned := checkpoints[entry.UUID]; !owned {
				continue
			}
		}
		if entry.RepeatsSummary {
			if current != nil {
				appendItem(map[string]any{"id": entry.UUID, "type": "agentMessage", "text": "上下文已压缩"})
			}
			continue
		}
		if human {
			status, turnID := "inProgress", entry.UUID
			if saved, exists := checkpoints[entry.UUID]; exists {
				status, turnID = saved.Status, saved.ID
			} else if saved, exists := checkpoints[entry.PromptID]; exists && !entry.Summary {
				status, turnID = saved.Status, saved.ID
				if saved.Command != "" {
					blocks = []contentBlock{{Type: "text", Text: saved.Command}}
				}
			}
			if status == "unknown" {
				status = "inProgress"
			}
			if entry.Summary {
				status = "completed"
				turnID = "compact-" + entry.UUID
			}
			turns = append(turns, agent.HistoryTurn{ID: turnID, Status: status, ItemsView: "full"})
			current = &turns[len(turns)-1]
			currentPrompt = entry.PromptID
			content := []any{}
			for _, block := range blocks {
				if block.Type == "text" {
					content = append(content, map[string]any{"type": "text", "text": block.Text})
				} else if block.Type == "image" && block.Source.Type == "base64" && strings.HasPrefix(block.Source.MediaType, "image/") {
					content = append(content, map[string]any{"type": "image", "url": "data:" + block.Source.MediaType + ";base64," + block.Source.Data})
				}
			}
			appendItem(map[string]any{"id": entry.UUID, "type": "userMessage", "content": content})
		}
		if current == nil {
			continue
		}
		for index, block := range blocks {
			switch {
			case entry.Type == "assistant" && block.Type == "text":
				appendItem(map[string]any{"id": entry.Message.ID + "-" + strconv.Itoa(offsets[entry.Message.ID]+index), "type": "agentMessage", "text": block.Text})
			case entry.Type == "assistant" && block.Type == "tool_use":
				item := toolItem(block, s.Cwd)
				tools[block.ID] = item
				appendItem(item)
			case block.Type == "tool_result":
				item := tools[block.ToolUseID]
				if item == nil {
					return nil, "", fmt.Errorf("Claude 历史工具结果缺少调用：%s", block.ToolUseID)
				}
				item["status"] = "completed"
				if block.IsError {
					item["status"] = "failed"
				}
				if item["type"] == "commandExecution" {
					var text string
					if json.Unmarshal(block.Content, &text) != nil {
						text = string(block.Content)
					}
					item["aggregatedOutput"] = text
				} else {
					item["result"] = block.Content
				}
				appendItem(item)
			}
		}
		if entry.Type == "assistant" {
			offsets[entry.Message.ID] += len(blocks)
		}
	}
	return turns, digest(raw), nil
}

func (c *Client) sessionCopy(id string) (session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.sessions[id]
	if s == nil {
		return session{}, fmt.Errorf("仅支持恢复由 Fuyu 创建并记录的 Claude 会话")
	}
	copy := *s
	copy.Turns = slices.Clone(s.Turns)
	return copy, nil
}

func (c *Client) ResumeThread(ctx context.Context, id string) (agent.ResumedThread, error) {
	s, err := c.sessionCopy(id)
	if err != nil {
		return agent.ResumedThread{}, err
	}
	_, currentDigest, err := c.history(s)
	if err != nil {
		return agent.ResumedThread{}, err
	}
	if s.run == nil && len(s.Turns) > 0 && s.Turns[len(s.Turns)-1].Status == "unknown" && s.Digest != "" && s.Digest == currentDigest {
		if uuid, ok := c.localCommandCompletion(s, currentDigest); ok {
			c.mu.Lock()
			live := c.sessions[id]
			last := len(s.Turns) - 1
			if live != nil && live.run == nil && live.Generation == s.Generation && live.Digest == s.Digest && len(live.Turns) == len(s.Turns) && live.Turns[last].ID == s.Turns[last].ID && live.Turns[last].Status == "unknown" {
				s.Turns[last].Status, s.Turns[last].LastUUID = "completed", uuid
				err = ctx.Err()
				if err == nil {
					err = c.save(&s)
				}
				if err == nil {
					live.Turns[last] = s.Turns[last]
				}
			}
			c.mu.Unlock()
			if err != nil {
				return agent.ResumedThread{}, fmt.Errorf("保存 Claude 本地命令核验结果：%w", err)
			}
		}
	}
	status := "idle"
	if s.run != nil || len(s.Turns) > 0 && (s.Turns[len(s.Turns)-1].Status == "inProgress" || s.Turns[len(s.Turns)-1].Status == "unknown" || currentDigest != s.Digest) {
		status = "notLoaded"
	}
	model := s.EffectiveModel
	if model == "" {
		model = s.Model
	}
	return agent.ResumedThread{ThreadMeta: agent.ThreadMeta{ID: id, Cwd: s.Cwd, Preview: s.Title, Status: agent.ThreadStatus{Type: status}}, Cwd: s.Cwd, Model: model, RuntimeVersion: s.Version, Policy: json.RawMessage(`"default"`), Sandbox: json.RawMessage(`{"type":"claudePermissions"}`)}, ctx.Err()
}

func (c *Client) ListTurns(ctx context.Context, id string, cursor *string) (agent.TurnsPage, error) {
	s, err := c.sessionCopy(id)
	if err != nil {
		return agent.TurnsPage{}, err
	}
	turns, hash, err := c.history(s)
	if err != nil {
		return agent.TurnsPage{}, err
	}
	end := len(turns)
	if cursor != nil {
		prefix, value, ok := strings.Cut(*cursor, ":")
		if !ok || prefix != hash {
			return agent.TurnsPage{}, fmt.Errorf("Claude 历史已变化，请重新加载首屏")
		}
		end, err = strconv.Atoi(value)
		if err != nil || end < 0 || end > len(turns) {
			return agent.TurnsPage{}, fmt.Errorf("Claude 历史分页游标无效")
		}
	}
	start := max(0, end-20)
	data := slices.Clone(turns[start:end])
	slices.Reverse(data)
	page := agent.TurnsPage{Data: data}
	if start > 0 {
		next := hash + ":" + strconv.Itoa(start)
		page.NextCursor = &next
	}
	return page, ctx.Err()
}

func (c *Client) ListThreads(ctx context.Context, cursor *string) (agent.ThreadsPage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	page := agent.ThreadsPage{}
	for _, s := range c.sessions {
		if len(s.Turns) == 0 {
			continue
		}
		page.Data = append(page.Data, agent.ThreadMeta{ID: s.ID, Cwd: s.Cwd, Preview: s.Title, Status: agent.ThreadStatus{Type: "notLoaded"}})
	}
	sort.Slice(page.Data, func(first, second int) bool { return page.Data[first].ID > page.Data[second].ID })
	return page, ctx.Err()
}

func (c *Client) ReadThread(ctx context.Context, id string) (json.RawMessage, error) {
	s, err := c.sessionCopy(id)
	if err != nil {
		return nil, err
	}
	turns, _, err := c.history(s)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(map[string]any{"thread": map[string]any{"id": id, "turns": turns}})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return raw, err
}
