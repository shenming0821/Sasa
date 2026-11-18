package ui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/shenming0821/Sasa/internal/codex"
)

type quitBackend struct {
	stubBackend
	stops chan string
}

func (b *quitBackend) Interrupt(_ context.Context, thread, turn string) error {
	b.stops <- thread + "/" + turn
	return nil
}

func TestQuitStopsAllOwnedTurnsOnceAndWaitsForNativeTerminal(t *testing.T) {
	b := &quitBackend{stops: make(chan string, 4)}
	a := New(b, "test", "/first", nil)
	a.State.ThreadID = "first"
	a.State.Begin("first task")
	a.State.TurnID = "one"
	a.newSession("/second")
	a.State.ThreadID = "second"
	a.State.Begin("second task")
	a.State.TurnID = "two"
	a.beginQuit()
	a.advanceQuit()
	a.advanceQuit()
	got := make(map[string]bool)
	for range 2 {
		select {
		case stop := <-b.stops:
			got[stop] = true
		case <-time.After(3 * time.Second):
			t.Fatal("missing native interrupt")
		}
	}
	if !got["first/one"] || !got["second/two"] || len(b.stops) != 0 {
		t.Fatalf("incorrect or duplicate interrupts: %v", got)
	}
	a.State.Draft = "next task"
	if a.canSend() || len(a.activeTasks()) != 2 {
		t.Fatal("interrupt RPC success incorrectly confirmed task completion")
	}
	a.applyBatch([]codex.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"first","turn":{"id":"one","status":"interrupted"}}`)}})
	if len(a.activeTasks()) != 1 || !a.quitStopping {
		t.Fatal("one terminal event prematurely finished shared shutdown")
	}
	a.applyBatch([]codex.Event{{Method: "sasa/disconnected"}})
	if len(a.activeTasks()) != 1 {
		// A completed task also becomes disconnected when its shared transport
		// exits, but only an unfinished target must block the quit confirmation.
		t.Fatalf("connection loss lost unresolved task boundary: %v", a.activeTasks())
	}
	a.advanceQuit()
	if !a.quitStopping || a.quitting {
		t.Fatal("connection loss was accepted as confirmed shutdown")
	}
}

func TestQuitWaitsForAllTerminalsThenQuits(t *testing.T) {
	a := New(stubBackend{}, "test", "/first", nil)
	a.State.ThreadID = "first"
	a.State.Begin("task")
	a.State.TurnID = "one"
	a.beginQuit()
	exited := false
	a.quitApp = func() { exited = true }
	a.applyBatch([]codex.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"first","turn":{"id":"one","status":"interrupted"}}`)}})
	a.advanceQuit()
	if !exited || !a.quitting || a.quitStopping {
		t.Fatal("confirmed terminal did not allow exit")
	}
}

func TestQuitTimeoutRequiresExplicitForceAndPreservesUncertainty(t *testing.T) {
	for _, button := range []int{0, 1} {
		t.Run([]string{"return to verification", "force quit"}[button], func(t *testing.T) {
			a := New(stubBackend{}, "test", "/first", nil)
			a.State.ThreadID = "first"
			a.State.Begin("task")
			a.State.TurnID = "one"
			a.beginQuit()
			a.quitDeadline = time.Now().Add(-time.Second)
			a.quitSent["1/one"] = true
			queue := make(chan func(), 2)
			a.dispatch = func(fn func()) { queue <- fn }
			exited := false
			a.quitApp = func() { exited = true }
			prompt := make(chan mygo.MessageOptions, 1)
			a.quitPrompt = func(options mygo.MessageOptions) (mygo.MessageResult, error) {
				prompt <- options
				return mygo.MessageResult{Button: button}, nil
			}
			a.advanceQuit()
			pumpUpdate(t, queue)
			options := <-prompt
			if options.Title != "停止尚未确认" || options.Buttons[0] != "返回核验" ||
				options.Buttons[1] != "强制退出" || options.DefaultButton != 0 {
				t.Fatalf("unsafe timeout choices: %+v", options)
			}
			if exited != (button == 1) || a.quitDialog || a.quitStopping {
				t.Fatal("timeout ignored explicit user choice")
			}
			saved := a.snapshot().Sessions[0]
			if !saved.Active || saved.TurnID != "one" {
				t.Fatal("force/cancel erased unresolved task marker")
			}
		})
	}
}
