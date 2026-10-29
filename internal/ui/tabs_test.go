package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

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
		if s.open != (s.id == 1 || s.id == 2) {
			t.Fatal("restart did not preserve open tabs")
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
	if a.open || tt.HasText("从一个清晰的任务开始") || tt.HasText("会话标签 2") {
		t.Fatal("last tab did not leave an empty workspace")
	}
	if len(a.sessions) != 2 || first.open || second.open {
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

func TestTabTooltipsShowOnlyTitleAndSkipClose(t *testing.T) {
	for _, kind := range []string{"session", "file", "diff"} {
		t.Run(kind, func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			a.saved.Title = "A long conversation title that should truncate"
			tab := a.tabs[0]
			label, closeLabel := "会话标签 1", "关闭会话标签 1"
			if kind != "session" {
				tab = storage.Tab{ID: "test-file", Kind: kind, Path: "/project/src/example.go", Workspace: "/project"}
				a.tabs = []storage.Tab{tab}
				label, closeLabel = "文件标签 "+tab.Path, "关闭文件标签 "+tab.Path
			}
			tt := native.NewTester(func(c *native.Context) { a.sessionTabs(c, true, true) }, 640, 200)
			before := tt.Texts()
			r, ok := tt.Find(label)
			if !ok {
				t.Fatal("missing title")
			}
			face := copyRegion(tt.Image(), r)
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Advance(150 * time.Millisecond)
			if !equalPixels(face, copyRegion(tt.Image(), r)) {
				t.Fatal("title hover changed tab background")
			}
			tt.Advance(time.Second)
			after := tt.Texts()
			if len(after) != len(before)+1 || after[len(after)-1] != a.tabTitle(tab) {
				t.Fatalf("expected one title-only tooltip: before=%v after=%v", before, after)
			}
			tt.Move(639, 199)
			before = tt.Texts()
			r, ok = tt.Find(closeLabel)
			if !ok {
				t.Fatal("missing accessible close button")
			}
			face = copyRegion(tt.Image(), r)
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Advance(150 * time.Millisecond)
			if !equalPixels(face, copyRegion(tt.Image(), r)) {
				t.Fatal("close hover changed tab background")
			}
			tt.Advance(time.Second)
			if !slices.Equal(before, tt.Texts()) {
				t.Fatal("close hover added tooltip text", tt.Texts())
			}
		})
	}
}

func TestRestartRestoresOpenTabsAndSelectionLazily(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := New(nil, "test", "/project", nil)
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	first := a.session
	first.State.ThreadID, first.State.Draft = "1", "first draft"
	a.newSession("/other")
	closed := a.session
	closed.State.ThreadID, closed.State.Draft = "2", "closed draft"
	a.newSession("/project")
	third := a.session
	third.State.ThreadID = "3"
	a.closeSession(closed)
	a.selectSession(first)
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	backend := &lazyTabsBackend{requests: make(chan string, 8)}
	backend.pages = []codex.TurnsPage{{}}
	b := New(backend, "test", "", nil)
	if err := b.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	queue := make(chan func(), 8)
	b.dispatch = func(fn func()) { queue <- fn }
	tt := native.NewTester(b.view, 1000, 760)
	if b.session.id != first.id || !tt.HasText("会话标签 1") || tt.HasText("会话标签 2") || !tt.HasText("会话标签 3") {
		t.Fatal("restart lost open tabs, restored a closed tab, or changed selection")
	}
	if len(backend.requests) != 0 || b.sessions[2].loaded || b.sessions[1].State.Draft != "closed draft" {
		t.Fatal("restoring tabs eagerly fetched history or lost a closed draft")
	}
	b.recoverSelectedSession()
	pumpUpdate(t, queue)
	if id := <-backend.requests; id != "1" {
		t.Fatal("startup resumed the wrong tab")
	}
	if b.sessions[2].loaded {
		t.Fatal("background tab loaded history at startup")
	}
	if err := tt.Click("会话标签 3"); err != nil {
		t.Fatal(err)
	}
	pumpUpdate(t, queue)
	if id := <-backend.requests; id != "3" {
		t.Fatal("clicking restored tab did not load its history")
	}
	b.closeSession(b.session)
	if err := b.persist(); err != nil {
		t.Fatal(err)
	}
	c := New(nil, "test", "", nil)
	if err := c.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	for _, s := range c.sessions {
		if s.open != (s.id == 1) {
			t.Fatal("closed tab returned after second restart")
		}
	}
}

func TestOpenTabSettingsCompatibilityAndLaunchDirectory(t *testing.T) {
	cases := []struct {
		name         string
		tabs         []int
		selected     int
		cwd          string
		wantSelected int
		wantTabs     []int
	}{
		{name: "legacy", selected: 2, wantSelected: 2, wantTabs: []int{2}},
		{name: "blank draft and multiple projects", tabs: []int{1, 3}, selected: 3, wantSelected: 3, wantTabs: []int{1, 3}},
		{name: "stale selection and IDs", tabs: []int{1, 99, 3}, selected: 99, wantSelected: 3, wantTabs: []int{1, 3}},
		{name: "closed selection", tabs: []int{1, 3}, selected: 2, wantSelected: 3, wantTabs: []int{1, 3}},
		{name: "explicit directory", tabs: []int{1, 3}, selected: 1, cwd: "/two", wantSelected: 2, wantTabs: []int{1, 2, 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.Save(storage.Snapshot{Selected: tc.selected, OpenTabs: tc.tabs, Sessions: []storage.Session{
				{ID: 1, Cwd: "/one", ThreadID: "first"},
				{ID: 2, Cwd: "/two", ThreadID: "second"},
				{ID: 3, Cwd: "/three", Draft: "blank draft"},
			}}); err != nil {
				t.Fatal(err)
			}
			a := New(nil, "test", tc.cwd, nil)
			if err := a.AttachStore(store); err != nil {
				t.Fatal(err)
			}
			if a.session.id != tc.wantSelected {
				t.Fatalf("selected=%d want=%d", a.session.id, tc.wantSelected)
			}
			if got := a.snapshot().OpenTabs; !slices.Equal(got, tc.wantTabs) {
				t.Fatalf("tabs=%v want=%v", got, tc.wantTabs)
			}
		})
	}
}

func TestLastClosedTabRestoresEmptyWorkspace(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := New(nil, "test", "/project", nil)
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	a.State.Draft = "preserved draft"
	a.closeSession(a.session)
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.OpenTabs == nil || len(saved.OpenTabs) != 0 || saved.Selected != 0 || len(saved.Sessions) != 1 {
		t.Fatal("empty tab selection was not persisted explicitly")
	}
	backend := &lazyTabsBackend{requests: make(chan string, 8)}
	b := New(backend, "test", "", nil)
	if err := b.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	b.recoverSelectedSession()
	if b.open || len(b.sessions) != 1 || len(backend.requests) != 0 || b.State.Draft != "preserved draft" {
		t.Fatal("restart created or recovered a tab, or lost the closed draft")
	}
	tt := native.NewTester(b.view, 1000, 760)
	if tt.HasText("从一个清晰的任务开始") || tt.HasText("会话标签 1") || !tt.HasText("使用 Codex 新建会话") || !tt.HasText("使用 Claude Code 新建会话") {
		t.Fatal("empty workspace shows conversation content or lacks a new-chat entry")
	}
	if err := tt.Click("切换会话 1"); err != nil {
		t.Fatal(err)
	}
	if !b.open || b.State.Draft != "preserved draft" || len(b.sessions) != 1 {
		t.Fatal("closed conversation could not be reopened")
	}
	b.closeSession(b.session)
	b.sidebarOpen = false
	tt.Frame()
	if err := tt.Click("使用 Codex 新建会话"); err != nil {
		t.Fatal(err)
	}
	if !b.open || len(b.sessions) != 2 || b.State.Cwd != "/project" {
		t.Fatalf("explicit new-chat action: open=%v sessions=%d cwd=%q", b.open, len(b.sessions), b.State.Cwd)
	}
}
