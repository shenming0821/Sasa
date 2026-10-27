package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const MaxToolBytes = 256 * 1024
const MaxDisplayBytes = 8 * 1024 * 1024
const MaxMessages = 1000

type FileChange struct {
	Path      string          `json:"path"`
	Kind      json.RawMessage `json:"kind"`
	Diff      string          `json:"diff"`
	Truncated bool            `json:"-"`
}

type Tool struct {
	Kind, Command, Cwd, Status, ProcessID, Name, Input, Output string
	ExitCode                                                   *int
	DurationMs                                                 *int64
	Changes                                                    []FileChange
	Truncated                                                  bool
	Expanded                                                   bool
	AwaitingFinal                                              bool
}

func (t *Tool) Title() string {
	switch t.Kind {
	case "commandExecution":
		return "执行命令"
	case "fileChange":
		return "修改文件"
	case "mcpToolCall", "dynamicToolCall":
		return "调用工具 " + t.Name
	case "webSearch":
		return "搜索网页"
	}
	return "工具"
}

func (t *Tool) StatusLabel() string {
	switch t.Status {
	case "inProgress":
		if t.AwaitingFinal {
			return "等待工具终态"
		}
		return "执行中"
	case "completed":
		if t.ExitCode != nil && *t.ExitCode != 0 {
			return "非零退出"
		}
		return "已完成"
	case "failed":
		return "执行失败"
	case "declined":
		return "已拒绝"
	}
	return "状态：" + t.Status
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.ValidString(s[:n]) {
		n--
	}
	return s[:n]
}

func appendOutput(old, delta string, truncated bool) (string, bool) {
	if truncated {
		return old, true
	}
	room := MaxToolBytes - len(old)
	if len(delta) <= room {
		return old + delta, false
	}
	return old + clip(delta, room), true
}

func messageBytes(m *Message) int {
	n := len(m.Text) + len(m.ID) + 128
	if t := m.Tool; t != nil {
		n += len(t.Input) + len(t.Output) + len(t.Command) + len(t.Cwd) + len(t.Name) + len(t.ProcessID)
		for _, f := range t.Changes {
			n += len(f.Path) + len(f.Kind) + len(f.Diff) + 64
		}
	}
	if r := m.Interaction; r != nil {
		raw, _ := json.Marshal(r.Request)
		n += len(raw)
		for _, v := range r.Answers {
			n += len(v)
		}
	}
	return n
}

// Evict the oldest display entries, never an unanswered interaction. The
// runtime remains authoritative; eviction is always visible to the user.
func (s *State) limitDisplay() {
	total := 0
	for _, m := range s.Messages {
		total += messageBytes(m)
	}
	for total > MaxDisplayBytes || len(s.Messages) > MaxMessages {
		drop := -1
		for i, m := range s.Messages {
			if m.Interaction == nil || !m.Interaction.Active() {
				drop = i
				break
			}
		}
		if drop < 0 {
			break
		}
		total -= messageBytes(s.Messages[drop])
		copy(s.Messages[drop:], s.Messages[drop+1:])
		s.Messages[len(s.Messages)-1] = nil
		s.Messages = s.Messages[:len(s.Messages)-1]
		s.DisplayTrimmed = true
		s.Notice = "较早的显示内容已移除以控制内存；完整记录仍由 Codex 保留，可刷新原生历史核验。"
	}
}

func (f FileChange) Summary() string {
	var kind struct {
		Type     string `json:"type"`
		MovePath string `json:"move_path"`
	}
	_ = json.Unmarshal(f.Kind, &kind)
	name := map[string]string{"add": "新增", "delete": "删除", "update": "修改"}[kind.Type]
	if name == "" {
		name = "变更"
	}
	added, removed := 0, 0
	if kind.Type == "add" || kind.Type == "delete" {
		if f.Diff != "" {
			lines := len(strings.Split(strings.TrimSuffix(f.Diff, "\n"), "\n"))
			if kind.Type == "add" {
				added = lines
			} else {
				removed = lines
			}
		}
	} else {
		for _, line := range strings.Split(f.Diff, "\n") {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				added++
			}
			if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				removed++
			}
		}
	}
	return fmt.Sprintf("%s %s  +%d −%d", name, f.Path, added, removed)
}
