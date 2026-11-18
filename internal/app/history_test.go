package app

import (
	"encoding/json"
	"testing"

	"github.com/shenming0821/Sasa/internal/codex"
)

func TestNativeHistoryProjectsTurnsAndDoesNotInventCompletion(t *testing.T) {
	turns := []codex.HistoryTurn{
		{ID: "one", Status: "completed", Items: []json.RawMessage{
			json.RawMessage(`{"id":"user","type":"userMessage","content":[{"type":"text","text":"task"}]}`),
			json.RawMessage(`{"id":"reply","type":"agentMessage","text":"done"}`),
		}},
		{ID: "two", Status: "inProgress", Items: []json.RawMessage{
			json.RawMessage(`{"id":"tool","type":"commandExecution","status":"inProgress","command":"pwd","cwd":"/first"}`),
		}},
	}
	s, unfinished, err := History("/first", "native", turns)
	if err != nil || !unfinished || s.Busy || s.Phase != "旧任务待核验" {
		t.Fatalf("state=%+v unfinished=%v err=%v", s, unfinished, err)
	}
	if len(s.Messages) != 3 || s.Messages[0].Role != "user" || s.Messages[0].Text != "task" ||
		s.Messages[2].Done || !s.Messages[2].Tool.AwaitingFinal || len(s.PendingInteractions()) != 0 {
		t.Fatalf("incorrect history projection: %+v", s.Messages)
	}
	s.Begin("next")
	if s.Sequence != 3 || s.Messages[3].ID == "user" {
		t.Fatal("next turn reused local sequence")
	}
	turns[0].ItemsView = "summary"
	if _, _, err := History("/first", "native", turns); err == nil {
		t.Fatal("partial summary treated as full history")
	}
}
