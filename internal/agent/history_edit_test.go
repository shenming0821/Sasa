package agent

import (
	"encoding/json"
	"testing"
)

func TestEditBoundaryInputTypes(t *testing.T) {
	for _, inputType := range []string{"text", "image", "localImage", "skill", "mention", "futureInput"} {
		t.Run(inputType, func(t *testing.T) {
			turn := HistoryTurn{ID: "turn", Status: "completed", ItemsView: "full", Items: []json.RawMessage{
				json.RawMessage(`{"id":"user","type":"userMessage","content":[{"type":"` + inputType + `"}]}`),
			}}
			_, err := EditBoundary([]HistoryTurn{turn}, "turn", "user")
			allowed := inputType != "mention" && inputType != "futureInput"
			if (err == nil) != allowed {
				t.Fatalf("allowed=%v error=%v", allowed, err)
			}
		})
	}
}

func TestEditSkillsRejectsIncompleteReference(t *testing.T) {
	for _, content := range []string{`{"type":"skill","name":"specflow"}`, `{"type":"skill","path":"/skills/specflow/SKILL.md"}`} {
		turn := HistoryTurn{Items: []json.RawMessage{json.RawMessage(`{"id":"user","type":"userMessage","content":[` + content + `]}`)}}
		if _, err := EditSkills(turn, "user"); err == nil {
			t.Fatal("incomplete skill accepted")
		}
	}
}
