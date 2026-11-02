package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/storage"
)

type deliveryBackend struct {
	stubBackend
	reads chan struct{}
	err   error
}

func (backend *deliveryBackend) ReadThread(context.Context, string) (json.RawMessage, error) {
	backend.reads <- struct{}{}
	return json.RawMessage(`{"thread":{"id":"thread","turns":[{"id":"turn","status":"interrupted","items":[]}]}}`), backend.err
}

func TestInterruptedDeliveryCheckPreservesSessionAndDraft(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			backend := &deliveryBackend{reads: make(chan struct{}, 4)}
			if failed {
				backend.err = fmt.Errorf("read unavailable")
			}
			desktop := New(backend, "test", "/project", nil)
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := desktop.AttachStore(store); err != nil {
				t.Fatal(err)
			}
			desktop.State.ThreadID = "thread"
			desktop.State.Begin("say ok")
			desktop.State.Apply(agent.Event{Method: "turn/started", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn"}}`)})
			queue := make(chan func(), 8)
			desktop.dispatch = func(update func()) { queue <- update }
			owner := desktop.session
			desktop.applySessionBatch([]agent.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)}})
			desktop.State.Draft = "continue"
			if desktop.canSend() || !owner.snapshotPending {
				t.Fatal("new input allowed before delivery was checked")
			}
			desktop.State.Draft = ""
			desktop.newSession("/other")
			desktop.State.Draft = "other draft"
			pumpUpdate(t, queue)
			if desktop.State.Draft != "other draft" {
				t.Fatal("delivery check modified another session")
			}
			bound := desktop.forSession(owner)
			if failed {
				owner.State.Draft = "continue"
				if bound.canSend() || !owner.State.Incomplete || owner.State.PendingSubmission() == nil {
					t.Fatal("failed check allowed an unresolved follow-up")
				}
				backend.err = nil
				owner.State.Draft = ""
				owner.State.NeedsSnapshot = true
				bound.reconcileIfNeeded()
				pumpUpdate(t, queue)
			}
			if owner.State.Draft != "say ok" || owner.State.Messages[0].Delivery != "unsent" || !bound.canSend() {
				t.Fatal("undelivered input was not restored for sending")
			}
			if err := desktop.persist(); err != nil {
				t.Fatal(err)
			}
			saved, err := store.Load()
			if err != nil || len(saved.Sessions) != 2 || saved.Sessions[0].Draft != "say ok" || saved.Sessions[1].Draft != "other draft" {
				t.Fatal("restored input was not saved with its owning session", err)
			}
			tester := native.NewTester(func(c *native.Context) { desktop.message(c, owner.State.Messages[0]) }, 640, 240)
			if _, found := tester.Find("未发送"); !found {
				t.Fatal("unsent bubble looks like a normal delivered message")
			}
		})
	}
}
