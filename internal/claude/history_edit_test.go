package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func TestClaudeEditHistoryUsesNativePrefixAndSurvivesRestart(t *testing.T) {
	client := fixtureClient(t)
	ctx := context.Background()
	thread, err := client.StartThread(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := client.sessions[thread.Thread.ID]
	entries := []map[string]any{}
	parent := ""
	for _, number := range []string{"1", "2", "3"} {
		user, prompt, assistant, hook := "user-"+number, "prompt-"+number, "assistant-"+number, "hook-"+number
		entries = append(entries,
			map[string]any{"type": "user", "uuid": user, "parentUuid": parent, "promptId": prompt, "sessionId": source.ID, "timestamp": "2026-11-03T03:11:00Z", "message": map[string]any{"content": "question " + number}},
			map[string]any{"type": "assistant", "uuid": assistant, "parentUuid": user, "sessionId": source.ID, "message": map[string]any{"id": "response-" + number, "content": []map[string]any{{"type": "text", "text": "answer " + number}}}},
			map[string]any{"type": "system", "uuid": hook, "parentUuid": assistant, "subtype": "stop_hook_summary", "sessionId": source.ID})
		source.Turns = append(source.Turns, checkpoint{ID: "turn-" + number, UserUUID: prompt, LastUUID: assistant, Status: "completed"})
		parent = hook
	}
	directory := filepath.Join(client.home, "projects", "fixture")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, source.ID+".jsonl")
	writeEntries := func() {
		raw := []byte{}
		for _, entry := range entries {
			line, _ := json.Marshal(entry)
			raw = append(raw, line...)
			raw = append(raw, '\n')
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		source.Digest = digest(raw)
		if err := client.save(source); err != nil {
			t.Fatal(err)
		}
	}
	writeEntries()
	for _, number := range []string{"1", "2", "3"} {
		result, err := client.EditHistory(ctx, source.ID, "turn-"+number, "prompt-"+number)
		if err != nil {
			t.Fatal(number, err)
		}
		want := int(number[0] - '1')
		if len(result.Turns) != want {
			t.Fatalf("prefix=%d want=%d", len(result.Turns), want)
		}
		if want == 0 {
			continue
		}
		reopened, err := New(client.executable, client.home, client.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		resumed, err := reopened.ResumeThread(ctx, result.Thread.ThreadMeta.ID)
		if err != nil || resumed.ThreadMeta.Status.Type != "idle" {
			t.Fatal("prepared edit lost on restart", resumed, err)
		}
		turns, err := agent.ReadHistory(ctx, reopened, result.Thread.ThreadMeta.ID)
		if err != nil || len(turns) != want {
			t.Fatal("prefix lost", err)
		}
		reopened.Close()
	}
	entries = append(entries, map[string]any{"type": "system", "uuid": "compact", "parentUuid": parent, "subtype": "compact_boundary", "sessionId": source.ID})
	entries = append(entries, map[string]any{"type": "user", "uuid": "summary", "parentUuid": "compact", "isCompactSummary": true, "sessionId": source.ID, "message": map[string]any{"content": "compacted history"}})
	writeEntries()
	if _, err := client.EditHistory(ctx, source.ID, "turn-2", "prompt-2"); err == nil {
		t.Fatal("compacted history was editable")
	}
	if _, err := client.EditHistory(ctx, source.ID, "turn-2", fmt.Sprint("missing")); err == nil {
		t.Fatal("missing message was editable")
	}
}
