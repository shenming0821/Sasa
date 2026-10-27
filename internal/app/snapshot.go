package app

import (
	"encoding/json"
	"fmt"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func (s *State) Reconcile(raw json.RawMessage) error {
	var snapshot struct {
		Thread struct {
			ID string `json:"id"`
			Turns []struct {
				ID     string            `json:"id"`
				Status string            `json:"status"`
				Error  json.RawMessage   `json:"error"`
				Items  []json.RawMessage `json:"items"`
			} `json:"turns"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	if snapshot.Thread.ID != s.ThreadID {
		return fmt.Errorf("会话不匹配")
	}
	for _, turn := range snapshot.Thread.Turns {
		if turn.ID != s.TurnID {
			continue
		}
		if turn.Status != "completed" && turn.Status != "failed" && turn.Status != "interrupted" {
			return fmt.Errorf("任务尚未到达可核验终态")
		}
		// Apply authoritative items through the same projection in an isolated
		// state; the live state's completed-turn guard remains intact.
		projected := New(s.Cwd)
		projected.ThreadID, projected.TurnID = s.ThreadID, s.TurnID
		for _, item := range turn.Items {
			params, _ := json.Marshal(map[string]any{"threadId": s.ThreadID, "turnId": s.TurnID, "item": item})
			projected.Apply((agent.Event{Method: "item/completed", Params: params}).ForDisplay())
		}
		for _, m := range projected.Messages {
			target := s.item(m.ID, m.Role)
			if target.Tool != nil && m.Tool != nil {
				m.Tool.Expanded = target.Tool.Expanded
			}
			*target = *m
		}
		terminal, _ := json.Marshal(map[string]any{"threadId": s.ThreadID, "turn": map[string]any{
			"id": turn.ID, "status": turn.Status, "error": turn.Error,
		}})
		projected.Apply(agent.Event{Method: "turn/completed", Params: terminal})
		s.Phase = projected.Phase
		if projected.Error != "" {
			s.Error = projected.Error
		}
		s.Incomplete = false
		s.clearUnfinishedToolError(turn.ID)
		s.Notice = "已按原生记录核验本轮终态；显示内容仍受容量上限约束。"
		s.limitDisplay()
		return nil
	}
	return fmt.Errorf("记录中缺少当前任务")
}
