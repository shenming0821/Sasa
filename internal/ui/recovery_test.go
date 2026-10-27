package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/storage"
)

type recoveryBackend struct {
	stubBackend
	resumed codex.ResumedThread
	pages   []codex.TurnsPage
	err     error
}

func (b *recoveryBackend) ResumeThread(context.Context, string) (codex.ResumedThread, error) {
	return b.resumed, b.err
}
func (b *recoveryBackend) ListTurns(_ context.Context, _ string, cursor *string) (codex.TurnsPage, error) {
	if cursor != nil {
		return b.pages[1], nil
	}
	return b.pages[0], nil
}
func (b *recoveryBackend) ListThreads(context.Context, *string) (codex.ThreadsPage, error) {
	return codex.ThreadsPage{}, nil
}

func pumpUpdate(t *testing.T, queue <-chan func()) {
	t.Helper()
	select {
	case fn := <-queue:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("missing UI callback")
	}
}

func recoveredBackend(status string) *recoveryBackend {
	return &recoveryBackend{
		resumed: codex.ResumedThread{
			ThreadMeta: codex.ThreadMeta{ID: "native", Status: codex.ThreadStatus{Type: "idle"}},
			Cwd:        "/first", Model: "native-model", Policy: json.RawMessage(`"on-request"`), Sandbox: json.RawMessage(`{"type":"readOnly"}`),
		},
		pages: []codex.TurnsPage{{Data: []codex.HistoryTurn{{
			ID: "old-turn", Status: status, Items: []json.RawMessage{
				json.RawMessage(`{"type":"userMessage","id":"u","content":[{"type":"text","text":"old task"}]}`),
				json.RawMessage(`{"type":"commandExecution","id":"tool","status":"completed","command":"pwd","cwd":"/first"}`),
				json.RawMessage(`{"type":"agentMessage","id":"reply","text":"native result"}`),
			},
		}}}},
	}
}

func TestStartupKeepsSelectedSessionWhenDirectoryMatches(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Save(storage.Snapshot{Selected: 1, Sessions: []storage.Session{
		{ID: 1, Cwd: "/same", Draft: "selected draft"},
		{ID: 2, Cwd: "/same", Draft: "other draft"},
	}}); err != nil {
		t.Fatal(err)
	}
	a := New(nil, "test", "/same", nil)
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if a.session.id != 1 || a.State.Draft != "selected draft" {
		t.Fatal("startup replaced selected session with another session in the same directory")
	}
}

func TestSaveFailureBeforeRPCPreservesDraftAndConnection(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(stubBackend{}, "test", "/first", nil)
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	store.Close()
	a.State.Draft = "unsent task"
	a.send()
	if a.State.Busy || a.State.Disconnected || a.State.Draft != "unsent task" ||
		a.State.Phase != "未发送" || a.storageError == "" {
		t.Fatal("local checkpoint failure became uncertain native execution")
	}
}

func TestCheckpointPreservesUnloadedToolExpansionAndStableTitle(t *testing.T) {
	a := New(nil, "test", "/first", nil)
	a.saved.Title = "original task"
	a.saved.Expanded = map[string]bool{"older-tool": true, "visible-tool": true}
	a.State.Messages = []*app.Message{
		{ID: "later-user", Role: "user", Text: "later task"},
		{ID: "visible-tool", Tool: &app.Tool{}},
	}
	saved := a.snapshot().Sessions[0]
	if saved.Title != "original task" || !saved.Expanded["older-tool"] || saved.Expanded["visible-tool"] {
		t.Fatalf("checkpoint lost page-independent state: %+v", saved)
	}
	if !a.saved.Expanded["visible-tool"] {
		t.Fatal("snapshot mutated previous checkpoint before save succeeded")
	}
}

func TestReopenRestoresNativeHistoryAndApplicationState(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	initial := storage.Snapshot{Selected: 2, SidebarOpen: true, Sessions: []storage.Session{
		{ID: 1, ThreadID: "native", Cwd: "/first", Draft: "first draft", Active: true, TurnID: "old-turn",
			AtEnd: false, Anchor: "u", Expanded: map[string]bool{"tool": true}},
		{ID: 2, Cwd: "/second", Draft: "second draft"},
	}}
	if err := store.Save(initial); err != nil {
		t.Fatal(err)
	}
	a := New(recoveredBackend("completed"), "test", "", nil)
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if a.session.id != 2 || a.State.Draft != "second draft" || !a.sessions[0].recoveryBlocked {
		t.Fatal("application selection/drafts not restored, or history accepted before verification")
	}
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	s := a.sessions[0]
	a.recoverSession(s)
	pumpUpdate(t, queue)
	if !s.loaded || s.recoveryBlocked || s.State.Busy || s.State.ThreadID != "native" ||
		s.State.Model != "native-model" || s.State.Draft != "first draft" || len(s.State.PendingInteractions()) != 0 {
		t.Fatalf("recovery not verified: %+v", s)
	}
	if len(s.State.Messages) != 3 || !s.State.Messages[1].Tool.Expanded || a.State.Cwd != "/second" {
		t.Fatal("history/collapse state crossed selected session")
	}
	a.selectSession(s)
	tt := native.NewTester(a.view, 1000, 760)
	if !tt.HasText("native result") || !a.canSend() {
		t.Fatal("verified history cannot continue")
	}
	// New-process state uses native item IDs, never cached user messages.
	a.State.Draft = "next draft"
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	b := New(recoveredBackend("completed"), "test", "", nil)
	if err := b.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if b.State.Draft != "next draft" || len(b.State.Messages) != 0 || b.canSend() {
		t.Fatal("restart bypassed native history verification")
	}
}

func TestUnfinishedLostAndUncertainSubmissionRemainExplicit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		id     string
		err    error
		review bool
	}{
		{"unfinished", "inProgress", "old-turn", nil, false},
		{"request failed", "completed", "old-turn", errors.New("offline"), false},
		{"unknown submission", "completed", "", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := recoveredBackend(tc.status)
			b.err = tc.err
			a := New(b, "test", "/first", nil)
			s := a.session
			s.State.ThreadID = "native"
			s.saved = storage.Session{Active: true, TurnID: tc.id}
			s.loaded = false
			s.State.Draft = "never auto resend"
			queue := make(chan func(), 4)
			a.dispatch = func(fn func()) { queue <- fn }
			a.recoverSession(s)
			pumpUpdate(t, queue)
			if !s.recoveryBlocked || a.canSend() || s.State.Busy || s.reviewRequired != tc.review {
				t.Fatalf("unconfirmed history enabled execution: %+v", s)
			}
		})
	}
}

func TestOlderPageDoesNotOverwriteLiveTurnAndDraft(t *testing.T) {
	b := recoveredBackend("completed")
	cursor := "older"
	b.pages[0].NextCursor = &cursor
	b.pages = append(b.pages, codex.TurnsPage{Data: []codex.HistoryTurn{{
		ID: "earlier", Status: "completed",
		Items: []json.RawMessage{json.RawMessage(`{"id":"older-reply","type":"agentMessage","text":"earlier result"}`)},
	}}})
	a := New(b, "test", "/first", nil)
	a.State.ThreadID = "native"
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	a.recoverSession(a.session)
	pumpUpdate(t, queue)
	a.State.Begin("new task")
	a.State.TurnID, a.State.Draft = "live", "draft"
	a.loadOlder()
	pumpUpdate(t, queue)
	if a.State.Messages[0].ID != "older-reply" || !a.State.Busy || a.State.TurnID != "live" ||
		a.State.Draft != "draft" || a.historyCursor != nil {
		t.Fatal("older history replaced active state")
	}
}

func TestRecoveryAutomaticallyLoadsSavedReadingAnchor(t *testing.T) {
	b := recoveredBackend("completed")
	cursor := "older"
	b.pages[0].NextCursor = &cursor
	b.pages = append(b.pages, codex.TurnsPage{Data: []codex.HistoryTurn{{
		ID: "earlier", Status: "completed", Items: []json.RawMessage{
			json.RawMessage(`{"id":"old-anchor","type":"agentMessage","text":"earlier result"}`),
		},
	}}})
	a := New(b, "test", "/first", nil)
	a.State.ThreadID = "native"
	a.saved = storage.Session{Anchor: "old-anchor", AtEnd: false, AnchorOffset: 25}
	a.loaded = false
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	a.recoverSession(a.session)
	pumpUpdate(t, queue)
	pumpUpdate(t, queue)
	if !a.hasSavedAnchor() || !a.restoreOffset || a.historyCursor != nil {
		t.Fatal("recovery did not automatically find old native reading anchor")
	}
}

func TestHistoryCapacityEndsUnresolvedAnchorRestoration(t *testing.T) {
	b := recoveredBackend("completed")
	b.pages = append(b.pages, codex.TurnsPage{Data: []codex.HistoryTurn{{
		ID: "older", Status: "completed", Items: []json.RawMessage{
			json.RawMessage(`{"id":"extra","type":"agentMessage","text":"one more"}`),
		},
	}}})
	a := New(b, "test", "/first", nil)
	a.State.ThreadID = "native"
	for i := range app.MaxMessages {
		a.State.Messages = append(a.State.Messages, &app.Message{ID: fmt.Sprintf("row-%d", i), Role: "assistant", Text: "result"})
	}
	cursor := "older"
	a.historyCursor, a.restoreOffset = &cursor, true
	a.saved.Anchor = "unavailable-anchor"
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	a.loadOlder()
	pumpUpdate(t, queue)
	if a.restoreOffset || a.historyCursor != nil || len(a.State.Messages) != app.MaxMessages {
		t.Fatal("capacity boundary kept an impossible anchor restoration alive")
	}
}

func TestSecretFormAnswersNeverEnterApplicationIndex(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := New(stubBackend{}, "", "/first", nil)
	a.AttachStore(store)
	a.State.Messages = []*app.Message{{
		ID: "question", Role: "interaction", Interaction: &app.Interaction{
			Answers: []string{"secret-never-save"}, Status: "待处理",
		},
	}}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "secret-never-save") || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].Draft != "" {
		t.Fatal("index has unexpected content")
	}
}

func TestRestartRestoresOffsetInsideLongNativeMessage(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend := recoveredBackend("completed")
	raw, _ := json.Marshal(map[string]any{"id": "reply", "type": "agentMessage", "text": strings.Repeat("Long paragraph for a stable reading anchor.\n\n", 180)})
	backend.pages[0].Data[0].Items[2] = raw
	a := New(backend, "test", "/first", nil)
	a.AttachStore(store)
	a.State.ThreadID = "native"
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	a.recoverSession(a.session)
	pumpUpdate(t, queue)
	tt := native.NewTester(a.view, 1000, 760)
	tt.Scroll(500, 200, 0, -900)
	if a.list.AtEnd() {
		t.Fatal("failed to scroll into the long reply")
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	before := a.snapshot().Sessions[0]
	b := New(backend, "test", "", nil)
	if err := b.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	b.dispatch = func(fn func()) { queue <- fn }
	b.recoverSession(b.session)
	pumpUpdate(t, queue)
	bt := native.NewTester(b.view, 1000, 760)
	bt.Frame()
	bt.Frame()
	after := b.snapshot().Sessions[0]
	if before.Anchor != after.Anchor || math.Abs(float64(before.AnchorOffset-after.AnchorOffset)) > 2 || b.list.AtEnd() {
		t.Fatalf("lost intra-message anchor: before=%+v after=%+v", before, after)
	}
}

func TestUnknownThreadCreationSurvivesRepeatedRestartUntilReviewed(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Save(storage.Snapshot{Sessions: []storage.Session{
		{ID: 1, Cwd: "/first", Active: true, Draft: "original request"},
	}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		a := New(stubBackend{}, "", "", nil)
		if err := a.AttachStore(store); err != nil {
			t.Fatal(err)
		}
		if a.canSend() || !a.reviewRequired || !a.recoveryBlocked {
			t.Fatal("unconfirmed creation became safe after restart")
		}
		a.persist()
	}
}

func TestBufferedHistoryControlsReconcileWithoutDuplicatingDeltas(t *testing.T) {
	b := recoveredBackend("completed")
	a := New(b, "", "/first", nil)
	a.State.ThreadID = "native"
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	a.recoverSession(a.session)
	a.applyBatch([]codex.Event{
		{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"threadId":"native","turnId":"old-turn","itemId":"reply","delta":"native result"}`)},
		{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"native","turn":{"id":"old-turn","status":"completed"}}`)},
	})
	pumpUpdate(t, queue)
	for _, m := range a.State.Messages {
		if m.ID == "reply" && m.Text != "native result" {
			t.Fatalf("buffered delta duplicated persisted text: %q", m.Text)
		}
	}
	if !a.State.Incomplete {
		t.Fatal("overlap with no native snapshot support lost its incomplete boundary")
	}
}

func TestScrollToTopLoadsOlderHistoryWithoutButton(t *testing.T) {
	b := recoveredBackend("completed")
	b.pages = append(b.pages, codex.TurnsPage{Data: []codex.HistoryTurn{{
		ID: "earlier", Status: "completed",
		Items: []json.RawMessage{json.RawMessage(`{"id":"older-reply","type":"agentMessage","text":"earlier result"}`)},
	}}})
	a := New(b, "test", "/first", nil)
	a.State.ThreadID, a.State.Draft = "native", "keep draft"
	cursor := "older"
	a.historyCursor = &cursor
	for i := 0; i < 60; i++ {
		a.State.Messages = append(a.State.Messages, &app.Message{
			ID: fmt.Sprint(i), Role: "user", Text: fmt.Sprintf("Message %d", i), Done: true,
		})
	}
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	tt := native.NewTester(a.view, 1000, 760)
	if tt.HasText("加载更早消息") || a.historyLoading {
		t.Fatal("history button remains or paging started at the latest message")
	}
	a.list.ScrollTo(0, native.Start)
	tt.Frame()
	if !a.historyLoading {
		t.Fatal("scrolling to top did not load older history")
	}
	before := a.rows["0"].Bounds(a.readingContext)
	pumpUpdate(t, queue)
	tt.Frame()
	after := a.rows["0"].Bounds(a.readingContext)
	if a.State.Messages[0].ID != "older-reply" || a.State.Draft != "keep draft" || a.historyCursor != nil {
		t.Fatal("pagination lost history or draft")
	}
	if math.Abs(float64(before.Y-after.Y)) > 2 {
		t.Fatalf("pagination moved reading position: before=%+v after=%+v", before, after)
	}
}
