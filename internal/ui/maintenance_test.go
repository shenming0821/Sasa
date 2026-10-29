package ui

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func savingApp(t *testing.T) *App {
	t.Helper()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	application := New(stubBackend{}, "test", "/tmp", nil)
	application.store = store
	application.dispatch = func(job func()) { job() }
	application.projectsRefreshed = time.Now()
	if err := application.persist(); err != nil {
		t.Fatal(err)
	}
	return application
}

func TestMaintenanceCoalescesUpdatesAndSavesLatestDraft(t *testing.T) {
	application := savingApp(t)
	original := bytes.Clone(application.lastSaved)
	application.update(func() { application.State.Draft = "first" })
	due := application.saveDue
	for range 100 {
		application.update(func() { application.State.Draft = "latest" })
	}
	if !bytes.Equal(original, application.lastSaved) || application.saveDue != due {
		t.Fatal("stream updates saved synchronously or postponed the deadline")
	}
	application.maintain(due.Add(-time.Millisecond))
	if !bytes.Equal(original, application.lastSaved) {
		t.Fatal("save ran before its deadline")
	}
	application.maintain(due)
	snapshot, err := application.store.Load()
	if err != nil || snapshot.Sessions[0].Draft != "latest" || application.savePending {
		t.Fatalf("deferred save did not persist latest input: %+v %v", snapshot, err)
	}
	if delay := application.maintenanceDelay(due); delay != 30*time.Second {
		t.Fatalf("idle maintenance still polls frequently: %s", delay)
	}
}

func TestMaintenanceSavesTerminalImmediatelyAndKeepsFailuresRetryable(t *testing.T) {
	application := savingApp(t)
	application.State.ThreadID = "thread"
	application.State.Begin("task")
	application.State.TurnID = "turn"
	if err := application.persist(); err != nil {
		t.Fatal(err)
	}
	application.State.Draft = "next input"
	application.applyBatch([]agent.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)}})
	snapshot, err := application.store.Load()
	if err != nil || snapshot.Sessions[0].Active || snapshot.Sessions[0].Draft != "next input" {
		t.Fatalf("terminal state was deferred: %+v %v", snapshot, err)
	}
	application.store.Close()
	application.update(func() { application.State.Draft = "unsaved" })
	application.maintain(application.saveDue)
	if application.storageError == "" || !application.savePending || application.saveDue.IsZero() {
		t.Fatal("save failure lost its error or retry deadline")
	}
}

func TestMaintenanceKeepsExitAndEditorChecksResponsive(t *testing.T) {
	application := New(nil, "test", "/tmp", nil)
	now := time.Now()
	application.fileExitRequested = true
	if delay := application.maintenanceDelay(now); delay != 250*time.Millisecond {
		t.Fatalf("exit delayed: %s", delay)
	}
	application.fileExitRequested = false
	application.savePending, application.saveDue = true, now.Add(200*time.Millisecond)
	if delay := application.maintenanceDelay(now); delay != 200*time.Millisecond {
		t.Fatalf("pending draft save delayed: %s", delay)
	}
}
