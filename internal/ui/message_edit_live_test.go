package ui

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/claude"
	"github.com/shenming0821/Fuyu/internal/codex"
)

func TestLiveMessageEditContext(t *testing.T) {
	if os.Getenv("FUYU_LIVE_EDIT") != "1" {
		t.Skip("set FUYU_LIVE_EDIT=1 to verify native history editing")
	}
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			cwd, checkpointDir := t.TempDir(), t.TempDir()
			var backend agent.Backend
			var reopen func() agent.Backend
			if kind == agent.Codex {
				reopen = func() agent.Backend {
					client, err := codex.Start(ctx, "codex", cwd)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(client.Close)
					return client
				}
			} else {
				executable, err := claude.ResolveExecutable()
				if err != nil {
					t.Fatal(err)
				}
				reopen = func() agent.Backend {
					client, err := claude.New(executable, "", checkpointDir)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(client.Close)
					return client
				}
			}
			backend = reopen()
			thread, err := backend.StartThread(ctx, cwd)
			if err != nil {
				t.Fatal(err)
			}
			wait := func(id string) string {
				t.Helper()
				text := ""
				for {
					select {
					case event := <-backend.Events():
						if len(event.ID) != 0 {
							t.Fatal("unexpected tool or approval", event.Method)
						}
						var params struct {
							Turn agent.Turn                  `json:"turn"`
							Item struct{ Type, Text string } `json:"item"`
						}
						_ = json.Unmarshal(event.Params, &params)
						if event.Method == "item/completed" && params.Item.Type == "agentMessage" {
							text += params.Item.Text
						}
						if event.Method == "turn/completed" && params.Turn.ID == id {
							if params.Turn.Status != "completed" {
								t.Fatal("failed turn", params.Turn)
							}
							return text
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
			}
			send := func(id, text string) string {
				t.Helper()
				turn, err := backend.Send(ctx, id, text)
				if err != nil {
					t.Fatal(err)
				}
				return wait(turn.ID)
			}
			for _, marker := range []string{"FIRST_928431", "SECOND_736092", "THIRD_614087"} {
				send(thread.Thread.ID, "Do not use tools or memory files. Remember the exact marker "+marker+" only in this conversation. Reply only OK.")
			}
			history := backend.(agent.HistoryBackend)
			turns, err := agent.ReadHistory(ctx, history, thread.Thread.ID)
			if err != nil || len(turns) != 3 {
				t.Fatalf("history=%d err=%v", len(turns), err)
			}
			var input struct{ ID, Type string }
			for _, raw := range turns[1].Items {
				_ = json.Unmarshal(raw, &input)
				if input.Type == "userMessage" {
					break
				}
			}
			result, err := backend.(agent.HistoryEditor).EditHistory(ctx, thread.Thread.ID, turns[1].ID, input.ID)
			if err != nil || len(result.Turns) != 1 {
				t.Fatalf("edit=%+v err=%v", result, err)
			}
			newID := result.Thread.ThreadMeta.ID
			t.Logf("source=%s replacement=%s prefix=%d", thread.Thread.ID, newID, len(result.Turns))
			text := send(newID, "Do not use tools. Reply only with the exact marker beginning FIRST_ from the earlier user message, then list any earlier markers beginning SECOND_ or THIRD_ if they exist; if none exist append NONE.")
			if !strings.Contains(text, "FIRST_928431") || strings.Contains(text, "SECOND_736092") || strings.Contains(text, "THIRD_614087") {
				t.Fatal("context leaked or lost", text)
			}
			if releaser, ok := backend.(agent.IdleThreadReleaser); ok {
				_, _ = releaser.ReleaseIdleThread(ctx, newID)
			}
			backend.(interface{ Close() }).Close()
			backend = reopen()
			resumed, err := backend.(agent.HistoryBackend).ResumeThread(ctx, newID)
			if err != nil || resumed.ThreadMeta.Status.Type != "idle" {
				t.Fatalf("resume=%+v err=%v", resumed, err)
			}
			turns, err = agent.ReadHistory(ctx, backend.(agent.HistoryBackend), newID)
			if err != nil || len(turns) != 2 {
				t.Fatalf("reopened history=%d err=%v", len(turns), err)
			}
			encoded, _ := json.Marshal(turns)
			if strings.Contains(string(encoded), "SECOND_736092") || strings.Contains(string(encoded), "THIRD_614087") {
				t.Fatal("restart restored discarded context")
			}
			t.Logf("context=%q restart turns=%d", text, len(turns))
		})
	}
}
