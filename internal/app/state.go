package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/shenming0821/Fuyu/internal/codex"
)

const MaxMessageBytes = 2 * 1024 * 1024

type Message struct {
	ID          string
	Role        string
	Text        string
	Done        bool
	TurnID      string
	Tool        *Tool
	Interaction *Interaction
}

// State is owned by the UI thread, including all event application.
type State struct {
	Cwd                 string
	ThreadID            string
	TurnID              string
	Model               string
	Policy              string
	Sandbox             string
	Draft               string
	Error               string
	Notice              string
	Incomplete          bool
	NeedsSnapshot       bool
	Phase               string
	Busy                bool
	Disconnected        bool
	Messages            []*Message
	Sequence            int
	finished            map[string]bool
	retryError          string
	unfinishedToolError string
	stopRequested       bool
}

func New(cwd string) State { return State{Cwd: cwd, Phase: "就绪"} }

func (s *State) Begin(text string) {
	s.Sequence++
	s.Messages = append(s.Messages, &Message{
		ID: fmt.Sprintf("user-%d", s.Sequence), Role: "user", Text: text, Done: true,
	})
	s.Draft, s.Error, s.TurnID, s.retryError = "", "", "", ""
	s.unfinishedToolError = ""
	s.stopRequested = false
	s.Busy, s.Phase = true, "正在发送"
}

func (s *State) Apply(event codex.Event) {
	defer s.limitDisplay()
	if event.Method == "fuyu/eventGap" {
		s.Incomplete, s.NeedsSnapshot = true, true
		s.Notice = "输出过快，部分增量未保留；任务结束后将核验原生记录。"
		return
	}
	if event.Method == "fuyu/disconnected" {
		s.expireInteractions("已失效")
		s.Disconnected, s.Busy, s.Phase = true, false, "连接已断开"
		s.Error = "Codex 连接已断开，请重新启动 Fuyu。当前结果尚需核验。"
		if event.Err != nil {
			s.Error += " " + event.Err.Error()
		}
		return
	}
	if s.Disconnected {
		return
	}
	if event.Method == "fuyu/unsupported" {
		s.Error = "当前交互未能处理，请核验请求或停止任务。"
		if event.Err != nil {
			s.Error += " " + event.Err.Error()
		}
		return
	}
	if len(event.ID) > 0 {
		s.addInteraction(event)
		return
	}
	switch event.Method {
	case "turn/started", "turn/completed", "item/agentMessage/delta",
		"item/commandExecution/outputDelta", "item/fileChange/outputDelta",
		"item/started", "item/completed", "serverRequest/resolved", "error":
	default:
		return
	}
	var p struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		ItemID    string          `json:"itemId"`
		Delta     string          `json:"delta"`
		WillRetry bool            `json:"willRetry"`
		RequestID json.RawMessage `json:"requestId"`
		Error     *struct {
			Message string `json:"message"`
		} `json:"error"`
		Turn codex.Turn `json:"turn"`
		Item struct {
			ID               string          `json:"id"`
			Type             string          `json:"type"`
			Text             string          `json:"text"`
			Command          string          `json:"command"`
			Status           string          `json:"status"`
			Cwd              string          `json:"cwd"`
			AggregatedOutput *string         `json:"aggregatedOutput"`
			ExitCode         *int            `json:"exitCode"`
			DurationMs       *int64          `json:"durationMs"`
			ProcessID        string          `json:"processId"`
			Changes          []FileChange    `json:"changes"`
			Tool             string          `json:"tool"`
			Server           string          `json:"server"`
			Arguments        json.RawMessage `json:"arguments"`
			Result           json.RawMessage `json:"result"`
			Error            json.RawMessage `json:"error"`
		} `json:"item"`
	}
	if err := json.Unmarshal(event.Params, &p); err != nil {
		s.Error = "无法读取 Codex 事件：" + err.Error()
		return
	}
	if p.ThreadID != "" && p.ThreadID != s.ThreadID {
		return
	}
	id := p.TurnID
	if p.Turn.ID != "" {
		id = p.Turn.ID
	}
	lateTool := false
	if id != "" && (s.finished[id] || (s.TurnID != "" && id != s.TurnID)) {
		itemID := p.ItemID
		if p.Item.ID != "" {
			itemID = p.Item.ID
		}
		if event.Method == "item/completed" || event.Method == "item/commandExecution/outputDelta" || event.Method == "item/fileChange/outputDelta" {
			for _, m := range s.Messages {
				if m.ID == itemID && m.Tool != nil && m.TurnID == id {
					lateTool = true
					break
				}
			}
		}
		if !lateTool {
			return
		}
	}
	switch event.Method {
	case "serverRequest/resolved":
		s.resolveInteraction(p.RequestID)
	case "turn/started":
		s.TurnID, s.Busy, s.Phase = p.Turn.ID, true, "正在执行"
		if s.stopRequested {
			s.Phase = "正在停止"
		}
	case "item/agentMessage/delta":
		s.clearRetry()
		m := s.item(p.ItemID, "assistant")
		m.TurnID = id
		m.Text = bounded(m.Text + p.Delta)
	case "item/commandExecution/outputDelta", "item/fileChange/outputDelta":
		if !lateTool {
			s.clearRetry()
		}
		m := s.item(p.ItemID, "tool")
		if m.Done {
			return
		}
		m.TurnID = id
		if m.Tool == nil {
			m.Tool = &Tool{Kind: "commandExecution", Status: "inProgress"}
		}
		m.Tool.Output, m.Tool.Truncated = appendOutput(m.Tool.Output, p.Delta, m.Tool.Truncated)
	case "item/started", "item/completed":
		switch p.Item.Type {
		case "agentMessage":
			m := s.item(p.Item.ID, "assistant")
			m.TurnID = id
			if p.Item.Text != "" {
				m.Text = bounded(p.Item.Text)
				if event.Truncated {
					m.Text += "\n\n[显示已截断]"
				}
			}
			m.Done = event.Method == "item/completed"
		case "commandExecution", "fileChange", "mcpToolCall", "dynamicToolCall", "webSearch":
			m := s.item(p.Item.ID, "tool")
			if m.Done && event.Method == "item/started" {
				return
			}
			m.TurnID = id
			if m.Tool == nil {
				m.Tool = &Tool{}
			}
			t := m.Tool
			t.Kind, t.Command, t.Cwd, t.Status = p.Item.Type, bounded(p.Item.Command), bounded(p.Item.Cwd), p.Item.Status
			t.ExitCode, t.DurationMs, t.ProcessID = p.Item.ExitCode, p.Item.DurationMs, p.Item.ProcessID
			t.Name = strings.Trim(p.Item.Server+"/"+p.Item.Tool, "/")
			t.Changes = p.Item.Changes
			for i := range t.Changes {
				t.Changes[i].Diff = clip(t.Changes[i].Diff, MaxToolBytes)
			}
			if p.Item.AggregatedOutput != nil {
				t.Output, t.Truncated = appendOutput("", *p.Item.AggregatedOutput, false)
			}
			if len(p.Item.Result) > 0 && string(p.Item.Result) != "null" {
				t.Output, t.Truncated = appendOutput("", string(p.Item.Result), false)
			}
			if len(p.Item.Error) > 0 && string(p.Item.Error) != "null" {
				t.Output, t.Truncated = appendOutput(t.Output, string(p.Item.Error), t.Truncated)
			}
			if len(p.Item.Arguments) > 0 {
				t.Input = clip(string(p.Item.Arguments), MaxToolBytes)
			}
			t.Truncated = t.Truncated || event.Truncated
			m.Done = event.Method == "item/completed"
			if m.Done {
				t.AwaitingFinal = false
				s.clearUnfinishedToolError(id)
			}
			if t.Status == "" {
				if m.Done {
					t.Status = "completed"
				} else {
					t.Status = "inProgress"
				}
			}
			// Keep legacy summary text for copying and existing consumers.
			m.Text = t.Title() + "\n" + t.Command
			if m.Done {
				m.Text += "\n" + t.Status
			}
		}
	case "turn/completed":
		if s.TurnID != "" && p.Turn.ID != s.TurnID {
			return
		}
		s.clearRetry()
		s.expireInteractions("已失效")
		s.TurnID, s.Busy = p.Turn.ID, false
		s.stopRequested = false
		for _, m := range s.Messages {
			if m.Tool != nil && m.TurnID == p.Turn.ID && !m.Done {
				m.Tool.AwaitingFinal = true
			}
		}
		if s.finished == nil {
			s.finished = make(map[string]bool)
		}
		s.finished[p.Turn.ID] = true
		switch p.Turn.Status {
		case "completed":
			s.Phase = "本轮已完成"
		case "interrupted":
			s.Phase = "已中断"
			for _, m := range s.Messages {
				if m.Role == "tool" && m.TurnID == p.Turn.ID && !m.Done {
					s.unfinishedToolError = "本轮已中断，部分工具尚未报告结束，请核验后台命令。"
					s.Error = s.unfinishedToolError
					break
				}
			}
		case "failed":
			s.Phase = "执行失败"
		default:
			s.Phase, s.Error = "状态待核验", "收到未知的执行终态："+p.Turn.Status
		}
		if p.Turn.Error != nil {
			s.Error = p.Turn.Error.Message
		}
	case "error":
		s.retryError = ""
		if p.Error != nil {
			s.Error = p.Error.Message
		}
		if p.WillRetry {
			if !s.Stopping() {
				s.Phase = "Codex 正在重试"
			}
			s.retryError = s.Error
		}
	}
}

func (s *State) clearUnfinishedToolError(turnID string) {
	if turnID != s.TurnID || s.unfinishedToolError == "" {
		return
	}
	for _, m := range s.Messages {
		if m.Tool != nil && m.TurnID == turnID && !m.Done {
			return
		}
	}
	if s.Error == s.unfinishedToolError {
		s.Error = ""
	}
	s.unfinishedToolError = ""
}

func (s *State) clearRetry() {
	if s.retryError == "" {
		return
	}
	if s.Error == s.retryError {
		s.Error = ""
	}
	s.retryError = ""
	if s.Phase == "Codex 正在重试" {
		s.Phase = "正在执行"
	}
}

func (s *State) item(id, role string) *Message {
	for _, m := range s.Messages {
		if m.ID == id {
			return m
		}
	}
	m := &Message{ID: id, Role: role}
	s.Messages = append(s.Messages, m)
	return m
}

func bounded(text string) string {
	if len(text) <= MaxMessageBytes {
		return text
	}
	i := MaxMessageBytes
	for !utf8.ValidString(text[:i]) {
		i--
	}
	return strings.TrimSpace(text[:i]) + "\n\n[显示已截断]"
}
