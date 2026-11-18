package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
)

func TestLiveClaudeTaskProjection(t *testing.T) {
	if os.Getenv("SASA_LIVE_CLAUDE") != "1" {
		t.Skip("set SASA_LIVE_CLAUDE=1 for real task lifecycle verification")
	}
	for _, mode := range []string{"subagent", "background"} {
		t.Run(mode, func(t *testing.T) {
			executable, err := ResolveExecutable()
			if err != nil {
				t.Fatal(err)
			}
			client, err := New(executable, "", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cwd := t.TempDir()
			thread, err := client.StartThread(ctx, cwd)
			if err != nil {
				t.Fatal(err)
			}
			prompt := "Use Agent once in the foreground with subagent_type=general-purpose. Ask the subagent to use Bash once to run printf SASA_CHILD_TOOL_OK in this working directory, then return its output. Do not write any files or memory. Return that exact output after the subagent completes. The parent must not run Bash itself."
			if mode == "background" {
				prompt = "Use Bash once with run_in_background=true to run exactly: sleep 2; printf 'SASA_BACKGROUND_OK\\n' > background-proof.txt . Wait for that background task to complete before replying SASA_BACKGROUND_OK. Work only in this directory. Do not write memory files."
			}
			state := app.New(cwd)
			state.Agent, state.ThreadID = agent.Claude, thread.Thread.ID
			state.Begin(prompt)
			turn, err := client.Send(ctx, thread.Thread.ID, prompt)
			if err != nil {
				t.Fatal(err)
			}
			completed := false
			for !completed {
				select {
				case event := <-client.Events():
					state.Apply(event)
					if len(event.ID) > 0 {
						if err := client.Respond(event.ID, map[string]string{"decision": "accept"}); err != nil {
							t.Fatal(err)
						}
					}
					if event.Method == "turn/completed" {
						var params struct {
							Turn agent.Turn `json:"turn"`
						}
						if err := json.Unmarshal(event.Params, &params); err != nil || params.Turn.ID != turn.ID || params.Turn.Status != "completed" {
							t.Fatalf("terminal=%s err=%v", event.Params, err)
						}
						completed = true
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			childTools, backgroundTools := 0, 0
			for _, message := range state.Messages {
				tool := message.Tool
				if tool == nil {
					continue
				}
				if !message.Done || tool.Status == "inProgress" || tool.AwaitingFinal {
					t.Fatalf("tool remains active after native completion: %+v", tool)
				}
				if tool.ParentToolID != "" {
					childTools++
				}
				var input struct {
					Background bool `json:"run_in_background"`
				}
				if json.Unmarshal([]byte(tool.Input), &input) == nil && input.Background {
					backgroundTools++
				}
			}
			if mode == "subagent" && childTools == 0 {
				t.Fatal("native subagent did not produce an associated child tool")
			}
			if mode == "background" {
				proof, err := os.ReadFile(filepath.Join(cwd, "background-proof.txt"))
				if backgroundTools == 0 || err != nil || string(proof) != "SASA_BACKGROUND_OK\n" {
					t.Fatalf("background=%d proof=%q err=%v", backgroundTools, proof, err)
				}
			}
			if state.Busy || state.Error != "" {
				t.Fatalf("busy=%t error=%s", state.Busy, state.Error)
			}
			t.Logf("thread=%s child-tools=%d background-tools=%d all-tools-terminal=true", thread.Thread.ID, childTools, backgroundTools)
		})
	}
}
