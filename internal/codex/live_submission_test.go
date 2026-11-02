package codex

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/app"
)

func TestLiveCodexEarlyInterruptDelivery(t *testing.T) {
	if os.Getenv("FUYU_LIVE") != "1" {
		t.Skip("set FUYU_LIVE=1 to run real Codex")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	directory := t.TempDir()
	client, err := Start(ctx, "codex", directory)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	thread, err := client.StartThread(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model=%s approvalPolicy=%s sandbox=%s cwd=%s", thread.Model, thread.ApprovalPolicy, thread.Sandbox, directory)
	state := app.New(directory)
	state.ThreadID = thread.Thread.ID
	wait := func(turnID string, interrupt bool) {
		t.Helper()
		for {
			select {
			case event, open := <-client.Events():
				if !open {
					t.Fatal("runtime disconnected")
				}
				state.Apply(event)
				var params struct {
					Turn Turn `json:"turn"`
				}
				if interrupt && event.Method == "turn/started" && state.TurnID == turnID {
					if err := client.Interrupt(ctx, state.ThreadID, turnID); err != nil {
						t.Fatal(err)
					}
				}
				if event.Method == "turn/completed" && json.Unmarshal(event.Params, &params) == nil && params.Turn.ID == turnID {
					return
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	state.Begin("say ok")
	turn, err := client.Send(ctx, state.ThreadID, "say ok")
	if err != nil {
		t.Fatal(err)
	}
	wait(turn.ID, true)
	raw, err := client.ReadThread(ctx, state.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Reconcile(raw); err != nil {
		t.Fatal(err)
	}
	if state.Messages[0].Delivery == "unsent" {
		if state.Draft != "say ok" {
			t.Fatal("undelivered input was not restored")
		}
		t.Log("native early interruption omitted user input; unsent prompt restored")
	} else if state.Messages[0].Delivery == "sent" && state.Draft == "" {
		t.Log("native early interruption retained user input; no automatic resend")
	} else {
		t.Fatalf("delivery remained uncertain: delivery=%s draft=%q", state.Messages[0].Delivery, state.Draft)
	}
	state.Begin("say ok")
	turn, err = client.Send(ctx, state.ThreadID, state.Messages[len(state.Messages)-1].Text)
	if err != nil {
		t.Fatal(err)
	}
	wait(turn.ID, false)
	found := false
	for _, message := range state.Messages {
		if message.Role == "assistant" && strings.TrimSpace(message.Text) == "ok" {
			found = true
		}
	}
	if !found || state.PendingSubmission() != nil {
		t.Fatal("resending restored input did not produce an acknowledged ok response")
	}
	t.Log("subsequent say ok produced ok with native input acknowledgement")
}

func TestLiveCodexInterruptedHistoryDelivery(t *testing.T) {
	threadID, turnID := os.Getenv("FUYU_LIVE_INTERRUPTED_THREAD"), os.Getenv("FUYU_LIVE_INTERRUPTED_TURN")
	if os.Getenv("FUYU_LIVE") != "1" || threadID == "" || turnID == "" {
		t.Skip("set FUYU_LIVE and interrupted thread/turn IDs to verify an existing native record")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := Start(ctx, "codex", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	state := app.New("")
	state.ThreadID = threadID
	state.Begin("say ok")
	params, _ := json.Marshal(map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "status": "interrupted"}})
	state.Apply(Event{Method: "turn/started", Params: params})
	state.Apply(Event{Method: "turn/completed", Params: params})
	raw, err := client.ReadThread(ctx, threadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Reconcile(raw); err != nil {
		t.Fatal(err)
	}
	if state.Messages[0].Delivery != "unsent" || state.Draft != "say ok" || state.PendingSubmission() != nil {
		t.Fatal("original missing input was not recovered from its native record")
	}
	t.Log("read-only native record verification restored original unsent say ok")
}
