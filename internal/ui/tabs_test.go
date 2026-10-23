package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/storage"
)

type lazyTabsBackend struct {
	recoveryBackend
	requests chan string
}

func (b *lazyTabsBackend) ResumeThread(_ context.Context, id string) (codex.ResumedThread, error) {
	b.requests <- id
	return codex.ResumedThread{ThreadMeta: codex.ThreadMeta{ID: id, Status: codex.ThreadStatus{Type: "idle"}}, Cwd: "/project"}, nil
}

func TestStartupLoadsOnlySelectedTabAndOpensHistoryOnDemand(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot := storage.Snapshot{Selected: 2, SidebarOpen: true}
	for id := 1; id <= 3; id++ {
		snapshot.Sessions = append(snapshot.Sessions, storage.Session{
			ID: id, Cwd: "/project", ThreadID: fmt.Sprint(id), Draft: fmt.Sprintf("draft %d", id),
			Active: id == 3, TurnID: "last-turn", Anchor: "old-item", Expanded: map[string]bool{"tool": true},
		})
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	backend := &lazyTabsBackend{requests: make(chan string, 8)}
	backend.pages = []codex.TurnsPage{{}}
	a := New(backend, "test", "/project", nil)
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	queue := make(chan func(), 8)
	a.dispatch = func(fn func()) { queue <- fn }
	tt := native.NewTester(a.view, 1000, 760)
	if tt.HasText("会话标签 1") || !tt.HasText("会话标签 2") || tt.HasText("会话标签 3") || len(backend.requests) != 0 {
		t.Fatal("startup opened all indexed sessions or requested history too early")
	}
	a.recoverSelectedSession()
	pumpUpdate(t, queue)
	if id := <-backend.requests; id != "2" {
		t.Fatalf("resumed %s instead of selected thread", id)
	}
	if a.sessions[0].loaded || a.sessions[2].loaded || !a.session.loaded || a.State.Draft != "draft 2" {
		t.Fatal("unselected history loaded or selected draft lost")
	}
	if err := tt.Click("切换会话 1"); err != nil {
		t.Fatal(err)
	}
	pumpUpdate(t, queue)
	if id := <-backend.requests; id != "1" {
		t.Fatalf("opened wrong history: %s", id)
	}
	tt.Frame()
	if !tt.HasText("会话标签 1") || !tt.HasText("会话标签 2") || tt.HasText("会话标签 3") {
		t.Fatal("on-demand navigation opened unrelated tabs")
	}
	first := a.session
	if err := tt.Click("关闭会话标签 1"); err != nil {
		t.Fatal(err)
	}
	if a.session.id != 2 || first.open || first.State.Draft != "draft 1" {
		t.Fatal("close lost draft or selected wrong neighbor")
	}
	if err := tt.Click("切换会话 1"); err != nil {
		t.Fatal(err)
	}
	if a.session != first || len(backend.requests) != 0 {
		t.Fatal("reopening loaded history duplicated runtime resume")
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	dormant := saved.Sessions[2]
	if !dormant.Active || dormant.TurnID != "last-turn" || dormant.Anchor != "old-item" || !dormant.Expanded["tool"] {
		t.Fatal("saving lazy history cleared unverified task or reading state")
	}
	b := New(nil, "test", "/project", nil)
	if err := b.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	for _, s := range b.sessions {
		if s.open != (s.id == first.id) {
			t.Fatal("restart reopened all prior tabs")
		}
	}
}

func TestCloseTabsPreservesBackgroundWorkflowAndLastTabFallback(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	first := a.session
	first.State.ThreadID = "background"
	first.State.Begin("background task")
	first.State.TurnID = "turn"
	first.State.Draft = "keep draft"
	first.State.Messages = append(first.State.Messages, &app.Message{ID: "tool", Tool: &app.Tool{Expanded: true}})
	a.newSession("/other")
	second := a.session
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.Click("关闭会话标签 1"); err != nil {
		t.Fatal(err)
	}
	if a.session != second || first.open || !a.anyBusy() || len(a.activeTasks()) != 1 {
		t.Fatal("closing background tab changed task ownership or quit protection")
	}
	a.applyBatch([]codex.Event{{ID: json.RawMessage(`42`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"threadId":"background","turnId":"turn","itemId":"approval","command":"pwd","cwd":"/project"}`)}})
	if len(first.State.PendingInteractions()) != 1 || len(second.State.PendingInteractions()) != 0 {
		t.Fatal("closed tab lost approval ownership")
	}
	if err := tt.Click("切换会话 1"); err != nil {
		t.Fatal(err)
	}
	if a.session != first || first.State.Draft != "keep draft" || !first.State.Messages[1].Tool.Expanded {
		t.Fatal("reopening discarded draft or tool state")
	}
	if err := tt.Click("关闭会话标签 1"); err != nil {
		t.Fatal(err)
	}
	if a.session != second {
		t.Fatal("selected close did not choose remaining tab")
	}
	if err := tt.Click("关闭会话标签 2"); err != nil {
		t.Fatal(err)
	}
	if a.session == second || !a.open || a.State.Cwd != "/other" || a.State.ThreadID != "" {
		t.Fatal("last tab did not leave a usable blank conversation")
	}
	if len(a.sessions) != 3 || first.open || second.open {
		t.Fatal("closing removed history or kept closed tab open")
	}
	a.applyBatch([]codex.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"background","turn":{"id":"turn","status":"completed"}}`)}})
	if a.anyBusy() || len(first.State.PendingInteractions()) != 0 {
		t.Fatal("closed background task did not receive terminal state")
	}
}

func TestClosingRecoveringTabDoesNotReopenOnLateResult(t *testing.T) {
	backend := &lazyTabsBackend{requests: make(chan string, 8)}
	backend.pages = []codex.TurnsPage{{}}
	a := New(backend, "test", "/project", nil)
	first := a.session
	first.State.ThreadID, first.loaded, first.recoveryBlocked = "history", false, true
	a.newSession("/project")
	second := a.session
	queue := make(chan func(), 8)
	a.dispatch = func(fn func()) { queue <- fn }
	a.selectSession(first)
	a.closeSession(first)
	pumpUpdate(t, queue)
	if first.open || !first.loaded || a.session != second {
		t.Fatal("late history callback reopened closed tab or replaced current selection")
	}
	a.selectSession(first)
	if len(backend.requests) != 1 || a.session != first {
		t.Fatal("reopening completed recovery started another request")
	}
}

func TestTabCloseButtonLayout(t *testing.T) {
	for _, width := range []int{640, 1000} {
		a := New(stubBackend{}, "test", "/project", nil)
		a.saved.Title = "A long conversation title that should truncate"
		a.newSession("/project")
		tt := native.NewTester(a.view, width, 760)
		title, ok := tt.Find("会话标签 2")
		close, found := tt.Find("关闭会话标签 2")
		if !ok || !found || close.W < 24 || close.H < 24 || title.X+title.W > close.X+1 {
			t.Fatalf("tab title and close overlap at width %d: title=%+v close=%+v", width, title, close)
		}
	}
}
