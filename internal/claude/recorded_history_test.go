package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shenming0821/Fuyu/internal/app"
)

func TestInterruptedTranscriptMarkerKeepsConfirmedTurn(t *testing.T) {
	for _, scenario := range []struct {
		name, status, markerPrompt string
		unfinished                 bool
	}{
		{"confirmed", "interrupted", "native-prompt", false},
		{"failed", "failed", "native-prompt", false},
		{"completed", "completed", "native-prompt", false},
		{"unknown", "unknown", "native-prompt", true},
		{"different-prompt", "interrupted", "other-prompt", true},
		{"failed-different-prompt", "failed", "other-prompt", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			client := fixtureClient(t)
			saved := session{ID: "33333333-3333-4333-8333-333333333333", Cwd: t.TempDir(), Turns: []checkpoint{{ID: "owned-turn", UserUUID: "user-input", Status: scenario.status}}}
			entries := []map[string]any{
				{"type": "user", "uuid": "user-input", "promptId": "native-prompt", "message": map[string]any{"content": "write paragraphs"}},
				{"type": "assistant", "uuid": "assistant-output", "parentUuid": "user-input", "message": map[string]any{"id": "message-output", "content": []map[string]string{{"type": "text", "text": "partial output"}}}},
				{"type": "user", "uuid": "interrupt-marker", "parentUuid": "assistant-output", "promptId": scenario.markerPrompt, "message": map[string]any{"content": []map[string]string{{"type": "text", "text": "[Request interrupted by user]"}}}},
			}
			var raw []byte
			for _, entry := range entries {
				entry["sessionId"] = saved.ID
				line, err := json.Marshal(entry)
				if err != nil {
					t.Fatal(err)
				}
				raw = append(append(raw, line...), '\n')
			}
			project := filepath.Join(client.home, "projects", "marker")
			if err := os.MkdirAll(project, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, saved.ID+".jsonl"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			turns, _, err := client.history(saved)
			if err != nil {
				t.Fatal(err)
			}
			state, unfinished, err := app.History(saved.Cwd, saved.ID, turns)
			if err != nil || unfinished != scenario.unfinished {
				t.Fatalf("turns=%+v unfinished=%t err=%v", turns, unfinished, err)
			}
			if !scenario.unfinished && (len(turns) != 1 || turns[0].Status != scenario.status || len(state.Messages) != 2) {
				t.Fatalf("native marker became a new user turn: %+v", turns)
			}
			client.sessions[saved.ID] = &saved
			page, err := client.ListTurns(context.Background(), saved.ID, nil)
			if err != nil || len(page.Data) != len(turns) {
				t.Fatalf("page=%+v err=%v", page, err)
			}
		})
	}
}

func TestRecordedTranscriptCompactionAndInterruption(t *testing.T) {
	for _, mode := range []string{"compact", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "mac-transcript-"+mode+".jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			saved := session{Cwd: "/recorded-project", Digest: digest(raw)}
			prompts := map[string]bool{}
			for _, line := range bytes.Split(raw, []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				var entry transcriptEntry
				if err := json.Unmarshal(line, &entry); err != nil {
					t.Fatal(err)
				}
				if saved.ID == "" {
					saved.ID = entry.SessionID
				}
				if entry.Type != "user" || entry.PromptID == "" || prompts[entry.PromptID] {
					continue
				}
				prompts[entry.PromptID] = true
				status := "completed"
				if mode == "interrupted" {
					status = "interrupted"
				}
				checkpoint := checkpoint{ID: "turn-" + entry.PromptID, UserUUID: entry.PromptID, Status: status}
				if entry.Summary {
					checkpoint.Command = "/compact"
				}
				saved.Turns = append(saved.Turns, checkpoint)
			}
			client := fixtureClient(t)
			project := filepath.Join(client.home, "projects", "recorded-project")
			if err := os.MkdirAll(project, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, saved.ID+".jsonl"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			turns, _, err := client.history(saved)
			if err != nil || len(turns) == 0 {
				t.Fatalf("history=%+v err=%v", turns, err)
			}
			for _, turn := range turns {
				if turn.Status == "inProgress" || turn.Status == "unknown" {
					t.Fatalf("recorded native terminal lost: %+v", turn)
				}
			}
			state, unfinished, err := app.History(saved.Cwd, saved.ID, turns)
			if err != nil || unfinished || state.Busy {
				t.Fatalf("unfinished=%t busy=%t err=%v", unfinished, state.Busy, err)
			}
			encoded, _ := json.Marshal(turns)
			if mode == "compact" && (!strings.Contains(string(encoded), "FUYU_CLAUDE_LIVE") || strings.Contains(string(encoded), "<local-command-caveat>")) {
				t.Fatal("compaction lost context or leaked command metadata")
			}
			client.mu.Lock()
			client.sessions[saved.ID] = &saved
			client.mu.Unlock()
			resumed, err := client.ResumeThread(context.Background(), saved.ID)
			if err != nil || resumed.ThreadMeta.Status.Type != "idle" {
				t.Fatalf("resumed=%+v err=%v", resumed, err)
			}
		})
	}
}
