package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func TestLiveClaudeReusedApprovals(t *testing.T) {
	if os.Getenv("FUYU_LIVE_CLAUDE") != "1" {
		t.Skip("set FUYU_LIVE_CLAUDE=1 for native reused-process approvals")
	}
	executable, err := ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	// This test wrapper selects default permissions so the real approval
	// protocol is exercised even when the user's normal mode bypasses prompts.
	wrapper := filepath.Join(t.TempDir(), "claude")
	command := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' --permission-mode default \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(command), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := New(wrapper, "", t.TempDir())
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
	var process *execution
	for _, mode := range []string{"warmup", "allow", "deny", "question"} {
		prompt := "Without tools or memory, reply only FUYU_REUSE_READY."
		switch mode {
		case "allow":
			prompt = "Use Bash once to run exactly: printf FUYU_ALLOWED > allowed.txt . Work only here and do not write memory."
		case "deny":
			prompt = "Use Bash once to run exactly: printf forbidden > denied.txt . If denied, stop without retry or another tool. Work only here and do not write memory."
		case "question":
			prompt = "Use AskUserQuestion now to ask which platform I want, with choices Mac and Linux. Wait for my answer, then repeat it. Use no other tools and do not write memory."
		}
		turn, err := client.Send(ctx, thread.Thread.ID, prompt)
		if err != nil {
			t.Fatal(err)
		}
		interacted := false
		complete := false
		for !complete {
			select {
			case event := <-client.Events():
				if len(event.ID) > 0 {
					interacted = true
					request, err := agent.DecodeInteraction(event)
					if err != nil {
						t.Fatal(err)
					}
					var response any = map[string]string{"decision": "accept"}
					if mode == "deny" {
						response = map[string]string{"decision": "decline", "feedback": "Stop. Do not write any file or retry."}
					} else if mode == "question" {
						answers := make(map[string]any)
						for _, q := range request.Questions {
							answers[q.ID] = map[string]any{"answers": []string{"Mac"}}
						}
						response = map[string]any{"answers": answers}
					}
					if err := client.Respond(event.ID, response); err != nil {
						t.Fatal(err)
					}
				}
				if event.Method == "turn/completed" {
					var p struct {
						Turn agent.Turn `json:"turn"`
					}
					_ = json.Unmarshal(event.Params, &p)
					if p.Turn.ID != turn.ID || p.Turn.Status != "completed" || mode != "warmup" && !interacted {
						t.Fatalf("mode=%s interaction=%v terminal=%+v", mode, interacted, p.Turn)
					}
					complete = true
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		run := idleExecution(t, client, thread.Thread.ID)
		if process != nil && run.process != process.process {
			t.Fatal("approval matrix did not use the same native process")
		}
		process = run
		t.Logf("mode=%s PID=%d completed=true interaction=%v", mode, run.process.Cmd.Process.Pid, interacted)
	}
	raw, err := os.ReadFile(filepath.Join(cwd, "allowed.txt"))
	if err != nil || string(raw) != "FUYU_ALLOWED" {
		t.Fatal("approved write missing", string(raw), err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "denied.txt")); !os.IsNotExist(err) {
		t.Fatal("denied write occurred", err)
	}
}
