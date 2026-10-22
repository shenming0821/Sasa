package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt in explicitly: this test uses the existing Codex login and real model service.
func TestLiveCodexLoop(t *testing.T) {
	if os.Getenv("FUYU_LIVE") != "1" {
		t.Skip("set FUYU_LIVE=1 to run real Codex")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	dir := t.TempDir()
	c, err := Start(ctx, "codex", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	thread, err := c.StartThread(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("cwd=%s model=%s approvalPolicy=%s sandbox=%s", thread.Cwd, thread.Model, thread.ApprovalPolicy, thread.Sandbox)
	if thread.Cwd != dir {
		t.Fatalf("cwd mismatch: %s", thread.Cwd)
	}
	wait := func(id string, interrupt bool) string {
		t.Helper()
		text := ""
		stopped := false
		for {
			select {
			case e, ok := <-c.Events():
				if !ok {
					t.Fatal("disconnected before completion")
				}
				if e.Method == "fuyu/disconnected" || e.Method == "fuyu/unsupported" {
					t.Fatalf("%s: %v", e.Method, e.Err)
				}
				var p struct {
					ThreadID string `json:"threadId"`
					TurnID   string `json:"turnId"`
					Delta    string `json:"delta"`
					Turn     Turn   `json:"turn"`
					Item     struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"item"`
				}
				if json.Unmarshal(e.Params, &p) != nil {
					continue
				}
				if p.ThreadID != "" && p.ThreadID != thread.Thread.ID {
					continue
				}
				if e.Method == "item/agentMessage/delta" {
					text += p.Delta
				}
				if e.Method == "item/completed" && p.Item.Type == "agentMessage" && p.Item.Text != "" {
					text = p.Item.Text
				}
				if interrupt && !stopped && e.Method == "item/started" && p.Item.Type == "commandExecution" {
					if err := c.Interrupt(ctx, thread.Thread.ID, id); err != nil {
						t.Fatal(err)
					}
					stopped = true
				}
				if e.Method == "turn/completed" && p.Turn.ID == id {
					want := "completed"
					if interrupt {
						want = "interrupted"
					}
					if p.Turn.Status != want {
						t.Fatalf("turn=%s status=%s want=%s error=%+v", id, p.Turn.Status, want, p.Turn.Error)
					}
					if interrupt && !stopped {
						t.Fatal("no command observed to interrupt")
					}
					t.Logf("turn=%s status=%s", id, p.Turn.Status)
					return text
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	nonce := "fuyu-" + time.Now().Format("150405.000000")
	first, err := c.Send(ctx, thread.Thread.ID, "Create a file named proof.txt in the working directory containing exactly "+nonce+" followed by a newline. Remember this token for our next turn. Do not modify any other files.")
	if err != nil {
		t.Fatal(err)
	}
	wait(first.ID, false)
	data, err := os.ReadFile(filepath.Join(dir, "proof.txt"))
	if err != nil || string(data) != nonce+"\n" {
		t.Fatalf("file proof=%q err=%v", data, err)
	}
	second, err := c.Send(ctx, thread.Thread.ID, "Without using tools, reply with only the exact token you remembered from my previous request.")
	if err != nil {
		t.Fatal(err)
	}
	if text := wait(second.ID, false); !strings.Contains(text, nonce) {
		t.Fatalf("context not retained: %q", text)
	}
	third, err := c.Send(ctx, thread.Thread.ID, "Run the shell command sleep 120 now. Do not do any other work. I will stop it from the UI.")
	if err != nil {
		t.Fatal(err)
	}
	wait(third.ID, true)
	c.Close()
	select {
	case <-c.done:
	case <-ctx.Done():
		t.Fatal("owned process did not exit")
	}
	t.Log("real file, dependent second turn, native interrupt and owned server exit verified")
}
