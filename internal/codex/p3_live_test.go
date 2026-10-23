package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLiveP3DirectoriesAndRestart(t *testing.T) {
	if os.Getenv("FUYU_LIVE") != "1" {
		t.Skip("set FUYU_LIVE=1 for real native runtime recovery")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	root := t.TempDir()
	firstDir, secondDir := filepath.Join(root, "first"), filepath.Join(root, "second")
	os.Mkdir(firstDir, 0700)
	os.Mkdir(secondDir, 0700)
	client, err := Start(ctx, "codex", root)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	first, err := client.StartThread(ctx, firstDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.StartThread(ctx, secondDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, thread := range []Thread{first, second} {
		t.Logf("thread=%s cwd=%s model=%s policy=%s sandbox=%s", thread.Thread.ID, thread.Cwd, thread.Model, thread.ApprovalPolicy, thread.Sandbox)
	}
	firstTurn, err := client.Send(ctx, first.Thread.ID, "In this working directory only, implement a small Python module sum_numbers.py with function sum_numbers(values) returning the sum of a list of numbers. Add test_sum_numbers.py using unittest covering empty input, negative values, and multiple numbers. Run /usr/bin/python3 -m unittest -v and fix any failures. Do not modify files outside the working directory. Remember token FUYU_P3_FIRST for our next turn.")
	if err != nil {
		t.Fatal(err)
	}
	secondTurn, err := client.Send(ctx, second.Thread.ID, "In this working directory only, create proof.txt containing exactly FUYU_P3_SECOND followed by a newline. Do not touch the first directory or any other files.")
	if err != nil {
		t.Fatal(err)
	}
	wait := func(c *Client, turns map[string]string) {
		t.Helper()
		for len(turns) > 0 {
			select {
			case e, ok := <-c.Events():
				if !ok || e.Method == "fuyu/disconnected" || e.Method == "fuyu/unsupported" {
					t.Fatalf("runtime disconnected or unsupported: %s %v", e.Method, e.Err)
				}
				if len(e.ID) > 0 {
					r, err := DecodeInteraction(e)
					if err != nil || (r.ThreadID != first.Thread.ID && r.ThreadID != second.Thread.ID) {
						t.Fatalf("unexpected request owner: %+v %v", r, err)
					}
					if r.Method != "item/commandExecution/requestApproval" && r.Method != "item/fileChange/requestApproval" {
						t.Fatalf("unsupported live interaction: %s", r.Method)
					}
					if err := c.Respond(e.ID, map[string]string{"decision": "accept"}); err != nil {
						t.Fatal(err)
					}
				}
				var p struct {
					ThreadID string `json:"threadId"`
					Turn     Turn   `json:"turn"`
				}
				if e.Method == "turn/completed" && json.Unmarshal(e.Params, &p) == nil {
					if id, exists := turns[p.ThreadID]; exists && id == p.Turn.ID {
						if p.Turn.Status != "completed" {
							t.Fatalf("turn=%s status=%s error=%+v", id, p.Turn.Status, p.Turn.Error)
						}
						delete(turns, p.ThreadID)
						t.Logf("completed thread=%s turn=%s", p.ThreadID, id)
					}
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	wait(client, map[string]string{first.Thread.ID: firstTurn.ID, second.Thread.ID: secondTurn.ID})
	cmd := exec.CommandContext(ctx, "/usr/bin/python3", "-m", "unittest", "-v")
	cmd.Dir = firstDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("real development task failed: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
	if raw, err := os.ReadFile(filepath.Join(secondDir, "proof.txt")); err != nil || string(raw) != "FUYU_P3_SECOND\n" {
		t.Fatalf("second cwd proof=%q err=%v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(firstDir, "proof.txt")); !os.IsNotExist(err) {
		t.Fatal("second task leaked into first cwd")
	}
	client.Close()
	reopened, err := Start(ctx, "codex", root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	resumed, err := reopened.ResumeThread(ctx, first.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ThreadMeta.ID != first.Thread.ID || resumed.Cwd != firstDir || resumed.ThreadMeta.Status.Type != "idle" {
		t.Fatalf("recovery identity/status: %+v", resumed)
	}
	page, err := reopened.ListTurns(ctx, first.Thread.ID, nil)
	if err != nil || len(page.Data) == 0 || page.Data[0].Status != "completed" {
		t.Fatalf("native history unavailable: %+v %v", page, err)
	}
	t.Logf("resumed thread=%s nativeTurns=%d nextCursor=%v status=%s", first.Thread.ID, len(page.Data), page.NextCursor, resumed.ThreadMeta.Status.Type)
	if _, err := reopened.ListThreads(ctx, nil); err != nil {
		t.Fatal(err)
	}
	next, err := reopened.Send(ctx, first.Thread.ID, "Continue the previous task. Write followup.txt containing only the exact FUYU token I asked you to remember in our first turn, followed by a newline. Do not change other files.")
	if err != nil {
		t.Fatal(err)
	}
	wait(reopened, map[string]string{first.Thread.ID: next.ID})
	if raw, err := os.ReadFile(filepath.Join(firstDir, "followup.txt")); err != nil || string(raw) != "FUYU_P3_FIRST\n" {
		t.Fatalf("resumed context proof=%q err=%v", raw, err)
	}
	t.Log("two independent cwd tasks, small development task, native history and same-thread continuation passed")
}
