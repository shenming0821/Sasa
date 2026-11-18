package ui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/codex"
	"github.com/shenming0821/Sasa/internal/storage"
)

type occupiedBackend struct {
	*recoveryBackend
	forkGate chan struct{}
	forkErr  error
	metaErr  error
}

func (b *occupiedBackend) ResumeThread(ctx context.Context, id string) (codex.ResumedThread, error) {
	if id == "branch" {
		return codex.ResumedThread{}, errors.New("new fork must use returned state without resuming")
	}
	result, err := b.recoveryBackend.ResumeThread(ctx, id)
	result.ThreadMeta.ID = id
	return result, err
}

func (b *occupiedBackend) ReadThreadMeta(context.Context, string) (codex.ThreadMeta, error) {
	meta := b.resumed.ThreadMeta
	meta.Cwd = b.resumed.Cwd
	return meta, b.metaErr
}

func (b *occupiedBackend) ForkThread(context.Context, string) (codex.ResumedThread, error) {
	if b.forkGate != nil {
		<-b.forkGate
	}
	result := b.resumed
	result.ThreadMeta.ID = "branch"
	return result, b.forkErr
}

func occupiedApp(t *testing.T) (*App, *occupiedBackend, chan func()) {
	t.Helper()
	b := &occupiedBackend{recoveryBackend: recoveredBackend("completed")}
	b.err = &codex.RPCError{Code: -32600, Message: "thread native already has an active writer"}
	a := New(b, "test", "/first", nil)
	a.State.ThreadID, a.State.Draft = "native", "keep draft"
	queue := make(chan func(), 8)
	a.dispatch = func(fn func()) { queue <- fn }
	a.recoverSession(a.session)
	pumpUpdate(t, queue)
	return a, b, queue
}

func TestOccupiedHistoryRemainsReadableAndRetryUnlocksComposer(t *testing.T) {
	a, b, queue := occupiedApp(t)
	if !a.writerOccupied || !a.recoveryBlocked || !a.loaded || a.State.Busy ||
		a.canSend() || a.State.Error != "" || len(a.State.Messages) != 3 {
		t.Fatalf("occupied history not readable: %+v", a.session)
	}
	tt := native.NewTester(a.view, 1000, 480)
	for _, width := range []int{1000, 640} {
		tt.SetSize(width, 480)
		_, input := tt.Find("任务输入")
		retry, retryOK := tt.Find("重试")
		fork, forkOK := tt.Find("创建聊天分叉")
		if !tt.HasText("已在另一个应用中打开") || !tt.HasText("native result") ||
			input || !retryOK || !forkOK || retry.X < 0 || fork.X+fork.W > float32(width) ||
			fork.Y+fork.H > 480 {
			t.Fatal("occupied footer does not replace composer")
		}
	}
	a.applySessionBatch([]codex.Event{{Method: "turn/started", Params: json.RawMessage(`{"threadId":"native","turn":{"id":"external","status":"inProgress"}}`)}})
	if a.State.Busy || a.State.TurnID == "external" {
		t.Fatal("external events granted control over occupied thread")
	}
	// Saving an occupied view must not invent an uncertain Sasa submission.
	if a.snapshot().Sessions[0].Active {
		t.Fatal("external writer became an uncertain local submission")
	}
	b.err = nil
	a.recoverSession(a.session)
	pumpUpdate(t, queue)
	tt.SetSize(1000, 760)
	_, input := tt.Find("任务输入")
	if a.writerOccupied || a.recoveryBlocked || !a.canSend() ||
		a.State.Draft != "keep draft" || !input {
		t.Fatal("retry did not restore original conversation and draft")
	}
}

func TestForkKeepsOriginalAndDoesNotStealSelection(t *testing.T) {
	a, b, queue := occupiedApp(t)
	original := a.session
	b.forkGate = make(chan struct{})
	a.forkSession()
	a.forkSession()
	if !original.forking {
		t.Fatal("fork was not guarded")
	}
	a.newSession("/second")
	selected := a.session
	close(b.forkGate)
	pumpUpdate(t, queue)
	if len(a.sessions) != 3 || a.session != selected || original.forking ||
		original.State.ThreadID != "native" || original.State.Draft != "keep draft" {
		t.Fatal("late fork result replaced selection or original")
	}
	branch := a.sessions[2]
	if branch.State.ThreadID != "branch" || branch.State.Draft != "keep draft" ||
		branch.saved.Title != original.title()+"（分叉）" {
		t.Fatal("fork lost native identity or draft")
	}
	pumpUpdate(t, queue)
	if branch.recoveryBlocked || branch.writerOccupied || !branch.loaded {
		t.Fatal("independent fork did not become ready")
	}
}

func TestForkFailureRetainsCardAndPreventsUncertainDuplicate(t *testing.T) {
	for _, err := range []error{
		&codex.RPCError{Code: -32600, Message: "fork rejected"},
		errors.New("connection lost"),
	} {
		a, b, queue := occupiedApp(t)
		b.forkErr = err
		a.forkSession()
		pumpUpdate(t, queue)
		var rpc *codex.RPCError
		if len(a.sessions) != 1 || !a.writerOccupied || a.forking ||
			a.State.Draft != "keep draft" || a.State.Error == "" ||
			a.forkUncertain == errors.As(err, &rpc) {
			t.Fatal("fork failure lost original state or duplicate protection")
		}
	}
}

func TestOtherRecoveryErrorsDoNotClaimExternalWriter(t *testing.T) {
	for _, err := range []error{
		errors.New("already has an active writer"),
		&codex.RPCError{Code: -32600, Message: "thread different already has an active writer"},
		&codex.RPCError{Code: -32603, Message: "thread native already has an active writer"},
	} {
		a, b, queue := occupiedApp(t)
		b.err = err
		a.recoverSession(a.session)
		pumpUpdate(t, queue)
		if a.writerOccupied || !a.recoveryBlocked || a.State.Error == "" {
			t.Fatal("unrelated failure classified as external writer")
		}
	}
}

func TestOccupiedReadFailureKeepsCardAndExistingHistory(t *testing.T) {
	a, b, queue := occupiedApp(t)
	b.metaErr = errors.New("history unavailable")
	a.recoverSession(a.session)
	pumpUpdate(t, queue)
	if !a.writerOccupied || a.State.Error == "" || len(a.State.Messages) != 3 || a.canSend() {
		t.Fatal("read failure lost occupied state or existing history")
	}
}

func TestForkPersistsBothIdentitiesAndRestartChecksOwnership(t *testing.T) {
	a, b, queue := occupiedApp(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.store = store
	a.forkSession()
	pumpUpdate(t, queue)
	if a.State.ThreadID != "branch" || len(a.sessions) != 2 {
		t.Fatal("fork did not select independent chat")
	}
	// The identity is durable even before its history callback completes.
	saved, err := store.Load()
	if err != nil || len(saved.Sessions) != 2 || saved.Sessions[1].ThreadID != "branch" ||
		saved.Sessions[0].ThreadID != "native" || saved.Sessions[0].Active {
		t.Fatal("fork identity not persisted independently", err)
	}
	pumpUpdate(t, queue)
	if !a.canSend() {
		t.Fatal("fork cannot continue")
	}
	reopened := New(b, "test", "", nil)
	if err := reopened.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if reopened.canSend() || reopened.State.ThreadID != "branch" || reopened.State.Draft != "keep draft" {
		t.Fatal("restart bypassed native check or lost fork draft")
	}
}
