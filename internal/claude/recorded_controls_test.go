package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
)

func TestRecordedControlResponsesMatchAdapter(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	script, err := filepath.Abs("testdata/replay_controls.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"allow", "deny", "question", "interrupt"} {
		t.Run(mode, func(t *testing.T) {
			executable := filepath.Join(t.TempDir(), "claude")
			if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec '"+python+"' -I '"+script+"' "+mode+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			client, err := New(executable, t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cwd := t.TempDir()
			models, err := client.ListModelsForDirectory(ctx, cwd)
			if err != nil || len(models) != 4 {
				t.Fatalf("recorded model catalog=%+v err=%v", models, err)
			}
			if models[0].Model != "opus" || models[0].DisplayName != "winning/deepseek-v4-1-flash-261017" {
				t.Fatal("native alias and display name were conflated", models[0])
			}
			for _, model := range models {
				if model.Model == "default" || strings.HasSuffix(model.Model, "[1m]") || !strings.Contains(model.DisplayName, "/") {
					t.Fatal("non-concrete model entry", model)
				}
				if len(model.SupportedReasoningEfforts) == 0 {
					t.Fatal("recorded effort levels lost", model)
				}
			}
			thread, err := client.StartThread(ctx, cwd)
			if err != nil {
				t.Fatal(err)
			}
			turn, err := client.Send(ctx, thread.Thread.ID, "recorded control replay")
			if err != nil {
				t.Fatal(err)
			}
			var requestID json.RawMessage
			for requestID == nil {
				select {
				case event := <-client.Events():
					if len(event.ID) == 0 {
						continue
					}
					request, err := agent.DecodeInteraction(event)
					if err != nil {
						t.Fatal(err)
					}
					requestID = event.ID
					if mode == "question" && (len(request.Questions) != 1 || !request.Questions[0].MultiSelect) {
						t.Fatal("recorded question lost its multi-select structure", request)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if mode == "interrupt" {
				if err := client.Interrupt(ctx, thread.Thread.ID, turn.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				var response any = map[string]string{"decision": "accept"}
				if mode == "deny" {
					response = map[string]string{"decision": "decline", "feedback": "SASA_CONTROL_DENIED"}
				} else if mode == "question" {
					response = map[string]any{"answers": map[string]any{"0": map[string]any{"answers": []string{"Mac", "Linux"}}}}
				}
				if err := client.Respond(requestID, response); err != nil {
					t.Fatal(err)
				}
			}
			expected := "completed"
			if mode == "interrupt" {
				expected = "interrupted"
			}
			if terminal := waitTerminal(t, client, thread.Thread.ID); terminal.Status != expected {
				t.Fatalf("terminal=%+v expected=%s", terminal, expected)
			}
			if mode != "interrupt" {
				matched, err := os.ReadFile(filepath.Join(cwd, "matched-response.txt"))
				if err != nil || string(matched) != mode {
					t.Fatalf("adapter did not match recorded response: %q err=%v", matched, err)
				}
			}
			if err := client.Respond(requestID, map[string]string{"decision": "accept"}); err == nil {
				t.Fatal("recorded request remained active after native completion")
			}
		})
	}
}
