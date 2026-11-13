package app

import (
	"encoding/json"
	"testing"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func TestPlanFinalReplacesStreamAndSurvivesNativeRecovery(t *testing.T) {
	s := New("/workspace")
	s.ThreadID = "thread"
	s.Begin("设计方案")
	apply(&s, "turn/started", `{"threadId":"thread","turn":{"id":"turn"}}`)
	apply(&s, "item/plan/delta", `{"threadId":"thread","turnId":"turn","itemId":"plan","delta":"草稿方案"}`)
	if len(s.Messages) != 2 || !s.Messages[1].Plan || s.Messages[1].Done {
		t.Fatal("plan stream missing", s.Messages)
	}
	final := json.RawMessage(`{"id":"plan","type":"plan","text":"# 最终方案\n\n1. 实现功能\n2. 验证结果"}`)
	params, _ := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "item": final})
	s.Apply(agent.Event{Method: "item/completed", Params: params})
	apply(&s, "item/plan/delta", `{"threadId":"thread","turnId":"turn","itemId":"plan","delta":"最终条目之后的增量"}`)
	apply(&s, "turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)
	if s.Messages[1].Text == "草稿方案" || !s.Messages[1].Done || !s.Messages[1].Plan {
		t.Fatal("final plan did not replace deltas")
	}
	apply(&s, "item/plan/delta", `{"threadId":"thread","turnId":"turn","itemId":"plan","delta":"晚到增量"}`)
	if s.Messages[1].Text != "# 最终方案\n\n1. 实现功能\n2. 验证结果" {
		t.Fatal("late plan delta corrupted the final result")
	}
	restored, unfinished, err := History("/workspace", "thread", []agent.HistoryTurn{{
		ID: "turn", Status: "completed", ItemsView: "full", Items: []json.RawMessage{final},
	}})
	if err != nil || unfinished || len(restored.Messages) != 1 || !restored.Messages[0].Plan || restored.Messages[0].Text != s.Messages[1].Text {
		t.Fatal("plan lost on recovery", restored.Messages, err)
	}
	s.Incomplete = true
	snapshot := json.RawMessage(`{"thread":{"id":"thread","turns":[{"id":"turn","status":"completed","itemsView":"full","items":[{"id":"plan","type":"plan","text":"重新核验的方案"}]}]}}`)
	if err := s.Reconcile(snapshot); err != nil || s.Messages[1].Text != "重新核验的方案" || !s.Messages[1].Plan {
		t.Fatal("plan snapshot not reconciled", err)
	}
}
