package ui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/storage"
)

type resourceBackend struct {
	recoveryBackend
	requests chan string
	results  chan bool
	stops    chan string
	stopErr  error
}

func (b *resourceBackend) Interrupt(_ context.Context, thread, turn string) error {
	b.stops <- thread + "/" + turn
	return b.stopErr
}

func TestCloseRunningSessionStopsItsOwnerAndReleasesAfterTerminal(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			a, backend, queue := resourceApp(t, kind)
			target := a.session
			target.State.Begin("running task")
			target.State.TurnID = "running-turn"
			target.State.BindTiming("running-turn")
			target.State.Draft = "keep draft"
			target.State.Messages[0].Role = "tool"
			target.State.Messages[0].TurnID = "running-turn"
			target.State.Messages[0].Tool.Status = "inProgress"
			a.applyBatch([]agent.Event{{
				Method: "item/started", Session: agent.SessionKey{Agent: kind, Runtime: target.runtimeKey, SessionID: "native"},
				Params: json.RawMessage(`{"threadId":"native","turnId":"running-turn","item":{"id":"user","type":"userMessage"}}`),
			}})
			a.newSessionForAgent("/second", target.kind, target.runtimeKey)
			a.State.Begin("other task")
			a.State.ThreadID, a.State.TurnID = "other", "other-turn"
			a.closeSession(target)
			select {
			case stopped := <-backend.stops:
				if stopped != "native/running-turn" {
					t.Fatal("close stopped a different owner", stopped)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("close did not immediately interrupt the task")
			}
			if target.open || !target.State.Busy || target.State.Phase != "正在停止" || target.releasing {
				t.Fatal("close did not wait for the native terminal state")
			}
			a.releaseClosedSessions(time.Now())
			if len(backend.stops) != 0 || len(backend.requests) != 0 || !a.State.Busy {
				t.Fatal("close duplicated interruption, released early, or stopped another task")
			}
			a.applyBatch([]agent.Event{{
				Method: "turn/completed", Session: agent.SessionKey{Agent: kind, Runtime: target.runtimeKey, SessionID: "native"},
				Params: json.RawMessage(`{"threadId":"native","turn":{"id":"running-turn","status":"interrupted"}}`),
			}})
			if target.State.Busy || !target.releasing || target.navigationStatus() == "正在执行" || !a.State.Busy {
				t.Fatalf("terminal event did not immediately start release: busy=%v releasing=%v status=%q otherBusy=%v snapshot=%v pending=%v", target.State.Busy, target.releasing, target.navigationStatus(), a.State.Busy, target.State.NeedsSnapshot, target.snapshotPending)
			}
			backend.results <- true
			pumpUpdate(t, queue)
			if target.loaded || len(target.State.Messages) != 0 || target.State.Draft != "keep draft" {
				t.Fatal("closed owner retained resources or lost its draft")
			}
		})
	}
}

func TestCloseSessionPreservesUnconfirmedStop(t *testing.T) {
	a, backend, queue := resourceApp(t)
	backend.stopErr = errors.New("transport unavailable")
	a.State.Begin("running task")
	a.State.TurnID = "running-turn"
	a.closeSession(a.session)
	pumpUpdate(t, queue)
	a.releaseClosedSessions(time.Now())
	if !a.State.Busy || a.State.Phase != "停止待确认" || a.releasing || len(backend.requests) != 0 {
		t.Fatal("failed interruption was treated as a stopped and releasable session")
	}
}

func TestCloseSessionCancelsPendingSubmission(t *testing.T) {
	a, backend, _ := resourceApp(t)
	a.State.Begin("pending task")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.submitCancel = cancel
	a.closeSession(a.session)
	if ctx.Err() != context.Canceled || !a.submitStop || !a.State.Stopping() || a.releasing || len(backend.stops) != 0 {
		t.Fatal("close did not cancel submission before the native turn was known")
	}
}

func (b *resourceBackend) ReleaseIdleThread(ctx context.Context, id string) (bool, error) {
	b.requests <- id
	select {
	case released := <-b.results:
		if !released {
			return false, errors.New("native check failed")
		}
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func resourceApp(t *testing.T, kinds ...agent.AgentKind) (*App, *resourceBackend, chan func()) {
	t.Helper()
	backend := &resourceBackend{
		recoveryBackend: *recoveredBackend("completed"),
		requests:        make(chan string, 8), results: make(chan bool, 8),
		stops: make(chan string, 8),
	}
	a := New(backend, "test", "/first", nil)
	if len(kinds) > 0 && kinds[0] == agent.Claude {
		a = New(&stubBackend{}, "test", "/first", nil)
		if err := a.RegisterBackend(agent.Claude, "/claude", backend); err != nil {
			t.Fatal(err)
		}
		a.kind, a.runtimeKey = agent.Claude, "/claude"
	}
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	a.store = store
	queue := make(chan func(), 8)
	a.dispatch = func(fn func()) { queue <- fn }
	a.State.ThreadID, a.State.Draft = "native", "keep draft"
	a.State.TurnID = "old-turn"
	a.State.Messages = []*app.Message{{ID: "tool", Tool: &app.Tool{Status: "completed", Expanded: true}}}
	a.session.saved.Title = "keep title"
	t.Cleanup(func() {
		// Every test resolves and applies its outstanding callback before return.
		if a.session.releasing {
			t.Error("release worker still active at test exit")
		}
	})
	return a, backend, queue
}

func TestClosedSessionReleaseRetainsDraftAndResumesOnReopen(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) { testClosedSessionReleaseRetainsDraftAndResumesOnReopen(t, kind) })
	}
}

func testClosedSessionReleaseRetainsDraftAndResumesOnReopen(t *testing.T, kind agent.AgentKind) {
	a, backend, queue := resourceApp(t, kind)
	s := a.session
	a.closeSession(s)
	if !s.releasing {
		t.Fatal("closed idle session was not released immediately")
	}
	backend.results <- true
	pumpUpdate(t, queue)
	if id := <-backend.requests; id != "native" {
		t.Fatal("released a different thread", id)
	}
	if s.loaded || s.open || s.State.Draft != "keep draft" || s.saved.Title != "keep title" || !s.saved.Expanded["tool"] || len(s.State.Messages) != 0 || len(s.projectedMessages) != 0 {
		t.Fatal("release lost draft, title, fold state, or subscription state")
	}
	snapshot, err := a.store.Load()
	if err != nil || snapshot.Sessions[0].Draft != "keep draft" || snapshot.Sessions[0].Active {
		t.Fatal("release corrupted persisted state", err)
	}
	a.selectSession(s)
	pumpUpdate(t, queue)
	if !s.loaded || s.recoveryBlocked || !a.canSend() || s.State.Draft != "keep draft" {
		t.Fatal("reopened session did not recover its native history and draft")
	}
}

func TestReopenDuringReleaseWaitsForResumeBeforeSending(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) { testReopenDuringReleaseWaitsForResumeBeforeSending(t, kind) })
	}
}

func testReopenDuringReleaseWaitsForResumeBeforeSending(t *testing.T, kind agent.AgentKind) {
	a, backend, queue := resourceApp(t, kind)
	s := a.session
	a.closeSession(s)
	a.releaseClosedSessions(s.releaseAfter)
	a.selectSession(s)
	if a.canSend() || s.recovering {
		t.Fatal("reopen raced a send or resume against unsubscribe")
	}
	backend.results <- true
	pumpUpdate(t, queue)
	if !s.recovering || a.canSend() {
		t.Fatal("unsubscribe completion did not start guarded recovery")
	}
	pumpUpdate(t, queue)
	if s.releasing || !s.loaded || !a.canSend() {
		t.Fatal("reopen did not become usable after resume")
	}
}

func TestClosedSessionReleaseGuardsTasksAndRetriesNativeCheck(t *testing.T) {
	for _, scenario := range []struct {
		name string
		set  func(*session)
	}{
		{"active task", func(s *session) { s.State.Busy = true }},
		{"no persisted turn yet", func(s *session) { s.State.TurnID = "" }},
		{"pending approval", func(s *session) {
			s.State.Messages = append(s.State.Messages, &app.Message{Interaction: &app.Interaction{Status: "待处理"}})
		}},
		{"unconfirmed history", func(s *session) { s.recoveryBlocked = true }},
		{"recovering", func(s *session) { s.recovering = true }},
		{"history page loading", func(s *session) { s.historyLoading = true }},
		{"foreign writer", func(s *session) { s.writerOccupied = true }},
		{"inflight send", func(s *session) { s.submitCancel = func() {} }},
		{"unfinished background tool", func(s *session) { s.State.Messages[0].Tool.AwaitingFinal = true }},
		{"active tool", func(s *session) { s.State.Messages[0].Tool.Status = "inProgress" }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
				t.Run(string(kind), func(t *testing.T) {
					a, backend, _ := resourceApp(t, kind)
					scenario.set(a.session)
					a.closeSession(a.session)
					a.releaseClosedSessions(a.releaseAfter)
					if a.releasing || len(backend.requests) != 0 {
						t.Fatal("release touched a protected session")
					}
				})
			}
		})
	}
	a, backend, queue := resourceApp(t)
	a.closeSession(a.session)
	a.releaseClosedSessions(a.releaseAfter)
	backend.results <- false
	pumpUpdate(t, queue)
	if !a.loaded || a.releasing {
		t.Fatal("failed native check discarded subscription state")
	}
	a.releaseClosedSessions(a.releaseAfter.Add(-time.Millisecond))
	if a.releasing {
		t.Fatal("native failure retried before its cooldown")
	}
	a.releaseClosedSessions(a.releaseAfter)
	backend.results <- true
	pumpUpdate(t, queue)
	if a.loaded {
		t.Fatal("native check was not retryable")
	}
}

func TestReleaseRequiresSavedStateAndDeletionDoesNotResurrectSession(t *testing.T) {
	t.Run("save failure", func(t *testing.T) {
		a, backend, _ := resourceApp(t)
		a.store.Close()
		a.closeSession(a.session)
		a.releaseClosedSessions(a.releaseAfter)
		if a.releasing || len(backend.requests) != 0 || a.storageError == "" {
			t.Fatal("release lost the live owner when its state could not be saved")
		}
	})
	t.Run("delete", func(t *testing.T) {
		a, backend, queue := resourceApp(t)
		target := a.session
		a.deleteSession(target)
		backend.results <- true
		pumpUpdate(t, queue)
		snapshot, err := a.store.Load()
		if err != nil {
			t.Fatal(err)
		}
		for _, saved := range snapshot.Sessions {
			if saved.ID == target.id {
				t.Fatal("resource cleanup restored a deleted session")
			}
		}
		if target.releasing || len(backend.requests) != 1 {
			t.Fatal("deletion left its runtime behind")
		}
	})
}
