package devin

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
)

func changeNotification(t *testing.T, value map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func changeMessages(t *testing.T, items []json.RawMessage) []*app.Message {
	t.Helper()
	var messages []*app.Message
	for _, raw := range items {
		var item struct {
			ID      string           `json:"id"`
			Type    string           `json:"type"`
			Status  string           `json:"status"`
			Changes []app.FileChange `json:"changes"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, &app.Message{ID: item.ID, Tool: &app.Tool{Kind: item.Type, Status: item.Status, Changes: item.Changes}})
	}
	return messages
}

func TestFileSnapshotsLiveAndReplay(t *testing.T) {
	tests := []struct {
		name           string
		kind           string
		texts          []string
		added, deleted int
	}{
		{"repeated edit", "edit", []string{"a\nb\n", "x\nb\n", "x\nc\n"}, 2, 2},
		{"reverted edit", "edit", []string{"a\n", "b\n", "a\n"}, 0, 0},
		{"duplicate lines", "edit", []string{"A\nX\n", "X\nX\n", "X\nA\n"}, 1, 1},
		{"delete and restore", "edit", []string{"A\nX\nB\n", "A\nB\n", "A\nX\nB\n"}, 0, 0},
		{"newline only", "edit", []string{"a\n", "a"}, 1, 1},
		{"empty existing file", "edit", []string{"", "a\n", ""}, 0, 0},
		{"command edits", "execute", []string{"a\n", "b\n", "c\n"}, 1, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var updates []json.RawMessage
			for index := 1; index < len(test.texts); index++ {
				id := fmt.Sprintf("edit-%d", index)
				updates = append(updates, changeNotification(t, map[string]any{
					"sessionUpdate": "tool_call", "toolCallId": id, "kind": test.kind,
					"status": "in_progress", "locations": []any{map[string]any{"path": "/repo/a.go"}},
					"content": []any{map[string]any{"type": "diff", "path": "/repo/a.go", "newText": ""}},
				}), changeNotification(t, map[string]any{
					"sessionUpdate": "tool_call_update", "toolCallId": id, "status": "completed",
					"content": []any{map[string]any{"type": "diff", "path": "/repo/a.go", "oldText": test.texts[index-1], "newText": test.texts[index]}},
				}))
			}
			turns, _ := replayTurns(updates)
			if len(turns) != 1 {
				t.Fatalf("turns=%d", len(turns))
			}
			replayed := app.SummarizeChanges("/repo", changeMessages(t, turns[0].Items))
			session := &acpSession{id: "session", cwd: "/repo", turn: agent.TurnIdentity{ID: "turn"}}
			client := &Client{sessions: map[string]*acpSession{"session": session}}
			for _, update := range updates {
				client.onUpdate("session", update)
			}
			var completed []json.RawMessage
			for _, event := range client.queue.pending {
				if event.Method != "item/completed" {
					continue
				}
				var envelope struct {
					Item json.RawMessage `json:"item"`
				}
				if err := json.Unmarshal(event.Params, &envelope); err != nil {
					t.Fatal(err)
				}
				completed = append(completed, envelope.Item)
			}
			live := app.SummarizeChanges("/repo", changeMessages(t, completed))
			for _, summary := range []app.ChangeSummary{replayed, live} {
				if !summary.Exact || len(summary.Files) != 1 || summary.Added != test.added || summary.Deleted != test.deleted {
					t.Fatalf("summary=%+v", summary)
				}
			}
			if live.Files[0].Patch != replayed.Files[0].Patch {
				t.Fatal("live/replay patches differ")
			}
		})
	}
}

func TestFileSnapshotMissingAndConflictingContent(t *testing.T) {
	tests := []struct {
		name           string
		blocks         []string
		exact          bool
		added, deleted int
	}{
		{"placeholder", []string{`{"type":"diff","path":"a.go","newText":""}`}, false, 0, 0},
		{"missing after", []string{`{"type":"diff","path":"a.go","oldText":"a\n"}`}, false, 0, 0},
		{"new file", []string{`{"type":"diff","path":"a.go","oldText":null,"newText":"a\n"}`}, true, 1, 0},
		{"delete file", []string{`{"type":"diff","path":"a.go","oldText":"a\n","newText":""}`}, true, 0, 1},
		{"shorter before", []string{`{"type":"diff","path":"a.go","oldText":"a\nb\n","newText":"c\nb\n"}`, `{"type":"diff","path":"a.go","oldText":"c\n","newText":"d\n"}`}, false, 0, 0},
		{"conflicting before", []string{`{"type":"diff","path":"a.go","oldText":"a\n","newText":"b\n"}`, `{"type":"diff","path":"a.go","oldText":"x\n","newText":"c\n"}`}, false, 0, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var items []json.RawMessage
			for index, raw := range test.blocks {
				var block toolContent
				if err := json.Unmarshal([]byte(raw), &block); err != nil {
					t.Fatal(err)
				}
				items = append(items, changeNotification(t, map[string]any{"id": fmt.Sprint(index), "type": "fileChange", "status": "completed", "changes": diffChanges([]toolContent{block}, "edit", nil)}))
			}
			summary := app.SummarizeChanges("/repo", changeMessages(t, items))
			if len(summary.Files) != 1 || summary.Exact != test.exact || summary.Added != test.added || summary.Deleted != test.deleted {
				t.Fatalf("summary=%+v", summary)
			}
		})
	}
}

func TestFileChangeCompletionUpdates(t *testing.T) {
	for _, kind := range []string{"edit", "execute"} {
		for _, terminal := range []string{"completed", "failed"} {
			t.Run(kind+"/"+terminal, func(t *testing.T) {
				start := changeNotification(t, map[string]any{
					"sessionUpdate": "tool_call", "toolCallId": "call", "kind": kind, "status": "in_progress",
					"content": []any{map[string]any{"type": "diff", "path": "/repo/a.go", "oldText": "a\n", "newText": "b\n"}},
				})
				finish := changeNotification(t, map[string]any{
					"sessionUpdate": "tool_call_update", "toolCallId": "call", "status": terminal,
					"locations": []any{map[string]any{"path": "/repo/a.go"}},
				})
				for _, completedStart := range []bool{false, true} {
					updates := []json.RawMessage{start, finish, finish}
					if completedStart {
						var initial map[string]any
						if err := json.Unmarshal(start, &initial); err != nil {
							t.Fatal(err)
						}
						initial["status"] = terminal
						updates = []json.RawMessage{changeNotification(t, initial), finish}
					}
					turns, _ := replayTurns(updates)
					summary := app.SummarizeChanges("/repo", changeMessages(t, turns[0].Items))
					expectedFiles := 1
					if terminal == "failed" {
						expectedFiles = 0
					}
					if !summary.Exact || len(summary.Files) != expectedFiles {
						t.Fatalf("summary=%+v", summary)
					}
					if terminal == "completed" && (summary.Added != 1 || summary.Deleted != 1) {
						t.Fatalf("summary=%+v", summary)
					}
					expectedItems := 1
					if kind == "execute" {
						expectedItems = 2
					}
					if len(turns[0].Items) != expectedItems {
						t.Fatalf("items=%d", len(turns[0].Items))
					}
				}
			})
		}
	}
}
