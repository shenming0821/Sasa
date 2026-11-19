package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
)

func TestEditHistoryUsesNativeBoundaryAndPagination(t *testing.T) {
	directory := t.TempDir()
	turns := []HistoryTurn{}
	for index := 0; index < 25; index++ {
		turns = append(turns, HistoryTurn{ID: fmt.Sprintf("turn-%d", index), Status: "completed", ItemsView: "full", Items: []json.RawMessage{json.RawMessage(fmt.Sprintf(`{"id":"user-%d","type":"userMessage","content":[{"type":"text","text":"question %d"}]}`, index, index))}})
	}
	turns[0].Items[0] = json.RawMessage(`{"id":"user-0","type":"userMessage","content":[{"type":"text","text":"$specflow question 0"},{"type":"skill","name":"specflow","path":"/skills/specflow/SKILL.md"}]}`)
	raw, _ := json.Marshal(map[string]any{"source": map[string]any{"cwd": directory, "turns": turns}})
	if err := os.WriteFile(filepath.Join(directory, "native-history.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	script, _ := filepath.Abs("testdata/p3_server.py")
	client, err := startProcess("/usr/bin/python3", []string{script, "--fixture-dir", directory}, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, boundary := range []int{0, 1, 24} {
		result, err := client.EditHistory(ctx, "source", fmt.Sprintf("turn-%d", boundary), fmt.Sprintf("user-%d", boundary))
		if err != nil || len(result.Turns) != boundary || result.Thread.ThreadMeta.ID == "source" {
			t.Fatalf("boundary=%d result=%+v error=%v", boundary, result, err)
		}
		if boundary == 0 && (len(result.Skills) != 1 || result.Skills[0].Name != "specflow" || result.Skills[0].Path != "/skills/specflow/SKILL.md") {
			t.Fatalf("original skill reference lost: %+v", result.Skills)
		}
	}
	if _, err := client.EditHistory(ctx, "source", "turn-1", "wrong-message"); err == nil {
		t.Fatal("mismatched message was editable")
	}
	page, err := client.ListTurns(ctx, "source", nil)
	if err != nil || len(page.Data) != 20 || page.NextCursor == nil {
		t.Fatal("original history changed", err)
	}
}

func TestLiveEditSkillMessage(t *testing.T) {
	if os.Getenv("SASA_LIVE_EDIT_SKILL") != "1" {
		t.Skip("set SASA_LIVE_EDIT_SKILL=1 to verify skill editing with real Codex")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	directory := t.TempDir()
	skillPath := filepath.Join(directory, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: sasa-edit-proof\ndescription: Verify skill message editing\n---\nReply with the requested token only. Do not use tools or modify files.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := Start(ctx, "codex", directory)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	thread, err := client.StartThread(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	wait := func(threadID, turnID string) {
		t.Helper()
		for {
			select {
			case event, ok := <-client.Events():
				if !ok {
					t.Fatal("runtime disconnected")
				}
				var params struct {
					ThreadID string `json:"threadId"`
					Turn     Turn   `json:"turn"`
				}
				if event.Method == "turn/completed" && json.Unmarshal(event.Params, &params) == nil && params.ThreadID == threadID && params.Turn.ID == turnID {
					if params.Turn.Status != "completed" {
						t.Fatalf("turn failed: %+v", params.Turn)
					}
					return
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	skills := []Skill{{Name: "sasa-edit-proof", Path: skillPath}}
	turn, err := client.SendWithSkills(ctx, thread.Thread.ID, "$sasa-edit-proof Reply only ORIGINAL. Do not use tools.", skills)
	if err != nil {
		t.Fatal(err)
	}
	wait(thread.Thread.ID, turn.ID)
	turns, err := agent.ReadHistory(ctx, client, thread.Thread.ID)
	if err != nil || len(turns) != 1 {
		t.Fatalf("history=%+v error=%v", turns, err)
	}
	messageID := ""
	for _, raw := range turns[0].Items {
		var item struct{ ID, Type string }
		if json.Unmarshal(raw, &item) == nil && item.Type == "userMessage" {
			messageID = item.ID
		}
	}
	result, err := client.EditHistory(ctx, thread.Thread.ID, turn.ID, messageID)
	if err != nil || len(result.Turns) != 0 || len(result.Skills) != 1 || result.Skills[0].Path != skillPath {
		t.Fatalf("edited history=%+v error=%v", result, err)
	}
	edited, err := client.SendWithSkills(ctx, result.Thread.ThreadMeta.ID, "$sasa-edit-proof Reply only EDITED. Do not use tools.", result.Skills)
	if err != nil {
		t.Fatal(err)
	}
	wait(result.Thread.ThreadMeta.ID, edited.ID)
	retained, err := agent.ReadHistory(ctx, client, result.Thread.ThreadMeta.ID)
	if err != nil || len(retained) != 1 {
		t.Fatalf("replacement history=%+v error=%v", retained, err)
	}
	attached, err := agent.EditSkills(retained[0], func() string {
		for _, raw := range retained[0].Items {
			var item struct{ ID, Type string }
			if json.Unmarshal(raw, &item) == nil && item.Type == "userMessage" {
				return item.ID
			}
		}
		return ""
	}())
	if err != nil || len(attached) != 1 || attached[0].Path != skillPath {
		t.Fatalf("resent skill=%+v error=%v", attached, err)
	}
	t.Log("real skill message edited, original turn excluded, skill retained on resend")
}
