package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLiveP6ApprovalAndFork(t *testing.T) {
	if os.Getenv("SASA_LIVE") != "1" {
		t.Skip("set SASA_LIVE=1 for native approval and fork verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client, err := Start(ctx, "codex", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var thread Thread
	if err := client.Call(ctx, "thread/start", map[string]any{
		"cwd": dir, "sandbox": "read-only", "approvalPolicy": "on-request",
	}, &thread); err != nil {
		t.Fatal(err)
	}
	t.Logf("thread=%s cwd=%s model=%s policy=%s sandbox=%s", thread.Thread.ID, thread.Cwd, thread.Model, thread.ApprovalPolicy, thread.Sandbox)
	turn, err := client.Send(ctx, thread.Thread.ID, "Use the shell tool to run exactly printf 'SASA_P6_APPROVED\\n' > approved.txt in the working directory. Request sandbox_permissions=require_escalated for this single command because the thread is read-only. Do not use apply_patch or modify any other file. After approval and execution, reply DONE.")
	if err != nil {
		t.Fatal(err)
	}
	approved := false
	for {
		select {
		case event, ok := <-client.Events():
			if !ok || event.Method == "sasa/disconnected" || event.Method == "sasa/unsupported" {
				t.Fatalf("connection failed: %s %v", event.Method, event.Err)
			}
			if len(event.ID) > 0 {
				request, err := DecodeInteraction(event)
				if err != nil || request.ThreadID != thread.Thread.ID || request.TurnID != turn.ID || request.Method != "item/commandExecution/requestApproval" {
					t.Fatalf("unexpected approval identity: %+v %v", request, err)
				}
				if err := client.Respond(event.ID, map[string]string{"decision": "accept"}); err != nil {
					t.Fatal(err)
				}
				approved = true
				t.Log("native command approval accepted for this request only")
			}
			if event.Method != "turn/completed" {
				continue
			}
			var result struct {
				ThreadID string `json:"threadId"`
				Turn     Turn   `json:"turn"`
			}
			if err := json.Unmarshal(event.Params, &result); err != nil {
				t.Fatal(err)
			}
			if result.ThreadID != thread.Thread.ID || result.Turn.ID != turn.ID {
				continue
			}
			if result.Turn.Status != "completed" || !approved {
				t.Fatalf("status=%s approvalObserved=%v", result.Turn.Status, approved)
			}
			goto completed
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}

completed:
	if raw, err := os.ReadFile(filepath.Join(dir, "approved.txt")); err != nil || string(raw) != "SASA_P6_APPROVED\n" {
		t.Fatalf("approved file=%q err=%v", raw, err)
	}
	fork, err := client.ForkThread(ctx, thread.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fork.ThreadMeta.ID == "" || fork.ThreadMeta.ID == thread.Thread.ID || fork.Cwd != dir || fork.ThreadMeta.Status.Type != "idle" {
		t.Fatalf("invalid fork identity: %+v", fork)
	}
	page, err := client.ListTurns(ctx, fork.ThreadMeta.ID, nil)
	if err != nil || len(page.Data) == 0 || page.Data[0].Status != "completed" {
		t.Fatalf("fork history unavailable: %+v %v", page, err)
	}
	original, err := client.ReadThreadMeta(ctx, thread.Thread.ID)
	if err != nil || original.ID != thread.Thread.ID {
		t.Fatalf("original thread unavailable after fork: %+v %v", original, err)
	}
	t.Logf("fork=%s original=%s nativeTurns=%d policy=%s sandbox=%s; original preserved and fork idle", fork.ThreadMeta.ID, original.ID, len(page.Data), fork.Policy, fork.Sandbox)
}
