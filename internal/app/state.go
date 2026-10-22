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
	ID   string
	Role string
	Text string
	Done bool
}

// State is owned by the UI thread, including all event application.
type State struct {
	Cwd          string
	ThreadID     string
	TurnID       string
	Model        string
	Policy       string
	Sandbox      string
	Draft        string
	Error        string
	Phase        string
	Busy         bool
	Disconnected bool
	Messages     []*Message
	Sequence     int
	finished     map[string]bool
	retryError   string
}

func New(cwd string) State { return State{Cwd: cwd, Phase: "就绪"} }

func (s *State) Begin(text string) {
	s.Sequence++
	s.Messages = append(s.Messages, &Message{
		ID: fmt.Sprintf("user-%d", s.Sequence), Role: "user", Text: text, Done: true,
	})
	s.Draft, s.Error, s.TurnID, s.retryError = "", "", "", ""
	s.Busy, s.Phase = true, "正在发送"
}

func (s *State) Apply(event codex.Event) {
	if event.Method == "fuyu/disconnected" {
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
		s.Error = "当前版本暂不支持此交互。完整审批与问题表单将在后续加入。"
		if event.Err != nil {
			s.Error += " " + event.Err.Error()
		}
		return
	}
	var p struct {
		ThreadID  string `json:"threadId"`
		TurnID    string `json:"turnId"`
		ItemID    string `json:"itemId"`
		Delta     string `json:"delta"`
		WillRetry bool   `json:"willRetry"`
		Error     *struct {
			Message string `json:"message"`
		} `json:"error"`
		Turn codex.Turn `json:"turn"`
		Item struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Text    string `json:"text"`
			Command string `json:"command"`
			Status  string `json:"status"`
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
	if id != "" && (s.finished[id] || (s.TurnID != "" && id != s.TurnID)) {
		return
	}
	switch event.Method {
	case "turn/started":
		s.TurnID, s.Busy, s.Phase = p.Turn.ID, true, "正在执行"
	case "item/agentMessage/delta":
		s.clearRetry()
		m := s.item(p.ItemID, "assistant")
		m.Text = bounded(m.Text + p.Delta)
	case "item/started", "item/completed":
		switch p.Item.Type {
		case "agentMessage":
			m := s.item(p.Item.ID, "assistant")
			if p.Item.Text != "" {
				m.Text = bounded(p.Item.Text)
			}
			m.Done = event.Method == "item/completed"
		case "commandExecution":
			m := s.item(p.Item.ID, "tool")
			m.Text = "执行命令\n" + p.Item.Command
			m.Done = event.Method == "item/completed"
			if m.Done {
				m.Text += "\n" + p.Item.Status
			}
		case "fileChange":
			m := s.item(p.Item.ID, "tool")
			m.Text, m.Done = "修改文件", event.Method == "item/completed"
			if m.Done {
				m.Text += " · " + p.Item.Status
			}
		}
	case "turn/completed":
		if s.TurnID != "" && p.Turn.ID != s.TurnID {
			return
		}
		s.clearRetry()
		s.TurnID, s.Busy = p.Turn.ID, false
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
				if m.Role == "tool" && !m.Done {
					s.Error = "本轮已中断，部分工具尚未报告结束，请核验后台命令。"
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
			s.Phase = "Codex 正在重试"
			s.retryError = s.Error
		}
	}
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
