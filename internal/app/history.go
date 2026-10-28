package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/attachment"
)

// Project persisted native turns without reviving old transport requests.
// The caller decides whether any in-progress turn belongs to a live connection.
func History(cwd, threadID string, turns []agent.HistoryTurn) (State, bool, error) {
	s := New(cwd)
	s.ThreadID = threadID
	unfinished := false
	for _, turn := range turns {
		if turn.ID == "" || (turn.ItemsView != "" && turn.ItemsView != "full") {
			return s, false, fmt.Errorf("历史任务标识或完整度无法确认")
		}
		s.TurnID = turn.ID
		for _, raw := range turn.Items {
			var item struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
					Path string `json:"path"`
					URL  string `json:"url"`
				} `json:"content"`
			}
			if err := json.Unmarshal(raw, &item); err != nil || item.ID == "" {
				return s, false, fmt.Errorf("历史条目无法读取：%v", err)
			}
			if item.Type == "userMessage" {
				var texts []string
				var images []attachment.Image
				for _, input := range item.Content {
					if input.Type == "text" {
						texts = append(texts, input.Text)
					} else if input.Type == "localImage" || input.Type == "image" {
						images = append(images, attachment.Image{Path: input.Path, URL: input.URL})
					} else {
						texts = append(texts, "[非文本输入："+input.Type+"]")
					}
				}
				s.Messages = append(s.Messages, &Message{ID: item.ID, Role: "user", Images: images, Text: bounded(strings.Join(texts, "\n")), TurnID: turn.ID, Done: true})
				continue
			}
			params, _ := json.Marshal(map[string]any{"threadId": threadID, "turnId": turn.ID, "item": raw})
			s.Apply((agent.Event{Method: "item/completed", Params: params}).ForDisplay())
			if turn.Status == "inProgress" {
				for _, m := range s.Messages {
					if m.ID == item.ID && m.Tool != nil && m.Tool.Status == "inProgress" {
						m.Done = false
						m.Tool.AwaitingFinal = true
					}
				}
			}
		}
		if turn.Status == "inProgress" {
			unfinished = true
			s.Phase = "旧任务待核验"
			continue
		}
		if turn.Status != "completed" && turn.Status != "failed" && turn.Status != "interrupted" {
			return s, false, fmt.Errorf("未知历史终态：%s", turn.Status)
		}
		params, _ := json.Marshal(map[string]any{"threadId": threadID, "turn": map[string]any{
			"id": turn.ID, "status": turn.Status, "error": turn.Error,
		}})
		s.Apply(agent.Event{Method: "turn/completed", Params: params})
	}
	s.Sequence = len(turns)
	s.limitDisplay()
	return s, unfinished, nil
}
