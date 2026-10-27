package app

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/shenming0821/Fuyu/internal/agent"
)

type Interaction struct {
	Request         agent.Interaction
	Status          string
	Error           string
	Outcome         string
	InvalidQuestion string
	FocusInvalid    bool
	Answers         []string
}

func (r *Interaction) Active() bool { return r.Status == "待处理" || r.Status == "提交中" }

func (s *State) addInteraction(e agent.Event) {
	r, err := agent.DecodeInteraction(e)
	if err != nil {
		s.Error = "无法显示交互：" + err.Error() + "。请停止当前任务。"
		return
	}
	if r.ThreadID != s.ThreadID {
		return
	}
	for _, m := range s.Messages {
		if m.Interaction != nil && string(m.Interaction.Request.ID) == string(e.ID) {
			return
		}
	}
	prompt := &Interaction{Request: r, Status: "待处理", Answers: make([]string, len(r.Questions))}
	if !s.Busy || s.finished[r.TurnID] || (s.TurnID != "" && s.TurnID != r.TurnID) || s.Stopping() {
		prompt.Status = "已失效"
	}
	s.Messages = append(s.Messages, &Message{ID: "request-" + string(e.ID), Role: "interaction", Interaction: prompt})
}

func (s *State) PendingInteractions() []*Interaction {
	var result []*Interaction
	for _, m := range s.Messages {
		if m.Interaction != nil && m.Interaction.Active() {
			result = append(result, m.Interaction)
		}
	}
	return result
}

func (s *State) resolveInteraction(id json.RawMessage) {
	for _, m := range s.Messages {
		if r := m.Interaction; r != nil && string(r.Request.ID) == string(id) {
			r.Status, r.Error = "已解决", ""
			clear(r.Answers)
		}
	}
}

func (s *State) expireInteractions(status string) {
	for _, m := range s.Messages {
		if r := m.Interaction; r != nil && (r.Active() || r.Status == "已响应" || r.Status == "发送待核验") {
			r.Status = status
			clear(r.Answers)
		}
	}
}

func (s *State) CancelInteractions() { s.expireInteractions("已取消") }

func (s *State) BeginStop() {
	s.stopRequested, s.Phase = true, "正在停止"
	s.CancelInteractions()
}

func (s *State) Stopping() bool {
	return s.stopRequested || s.Phase == "正在停止" || s.Phase == "停止待确认"
}

func (r *Interaction) Submit(result any) (any, error) {
	if r.Status != "待处理" {
		return nil, errors.New("请求已提交或失效")
	}
	if r.Request.Method == "item/tool/requestUserInput" && result == nil {
		answers := map[string]any{}
		for i, q := range r.Request.Questions {
			if strings.TrimSpace(r.Answers[i]) == "" {
				r.Error = "请回答：" + q.Header
				r.InvalidQuestion, r.FocusInvalid = q.ID, true
				return nil, errors.New(r.Error)
			}
			allowed := len(q.Options) == 0 || q.IsOther
			for _, option := range q.Options {
				allowed = allowed || r.Answers[i] == option.Label
			}
			if !allowed {
				r.Error = "请从选项中选择：" + q.Header
				r.InvalidQuestion, r.FocusInvalid = q.ID, true
				return nil, errors.New(r.Error)
			}
			answers[q.ID] = map[string]any{"answers": []string{r.Answers[i]}}
		}
		result = map[string]any{"answers": answers}
	}
	r.Outcome = "回答已发送"
	if r.Request.Method != "item/tool/requestUserInput" {
		var response struct {
			Decision    json.RawMessage `json:"decision"`
			Scope       string          `json:"scope"`
			Permissions json.RawMessage `json:"permissions"`
		}
		raw, _ := json.Marshal(result)
		if json.Unmarshal(raw, &response) != nil || result == nil {
			return nil, errors.New("无效的交互响应")
		}
		if r.Request.Method == "item/permissions/requestApproval" {
			r.Outcome = "已拒绝额外权限"
			if string(response.Permissions) != "{}" {
				r.Outcome = "已允许请求的权限，范围：" + response.Scope
			}
		} else {
			r.Outcome = ""
			for _, d := range r.Request.Decisions() {
				var a, b any
				json.Unmarshal(d.Value, &a)
				json.Unmarshal(response.Decision, &b)
				left, _ := json.Marshal(a)
				right, _ := json.Marshal(b)
				if string(left) == string(right) {
					r.Outcome = d.Label + " · " + d.Detail
					break
				}
			}
			if r.Outcome == "" {
				return nil, errors.New("响应不在当前允许选项中")
			}
		}
	} else {
		if raw, _ := json.Marshal(result); string(raw) == `{"answers":{}}` {
			r.Outcome = "已取消回答"
		}
	}
	r.Status, r.Error = "提交中", ""
	r.InvalidQuestion, r.FocusInvalid = "", false
	// Answers are sent only in the response, never copied into transcript text.
	clear(r.Answers)
	return result, nil
}
