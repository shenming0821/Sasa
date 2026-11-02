package ui

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/project"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func TestFolderTabsFilterAndKeepBackgroundState(t *testing.T) {
	a := New(stubBackend{}, "test", "/folder-a", nil)
	first := a.session
	first.State.Draft, first.State.Busy = "draft a", true
	a.newSession("/folder-a")
	second := a.session
	for range 3 {
		a.newSession("/folder-b")
	}
	last := a.session
	tester := native.NewTester(func(c *native.Context) { a.sessionTabs(c, true, true) }, 1200, 100)
	assertTabs := func(ids ...int) {
		t.Helper()
		tester.Frame()
		for _, target := range a.sessions {
			want := false
			for _, id := range ids {
				want = want || target.id == id
			}
			if tester.HasText(fmt.Sprintf("会话标签 %d", target.id)) != want {
				t.Fatalf("wrong visibility for session %d", target.id)
			}
		}
	}
	assertTabs(3, 4, 5)
	if !a.restoreFolderTab("/folder-a") || a.session != second {
		t.Fatal("folder a did not restore its selected tab")
	}
	assertTabs(1, 2)
	if !a.restoreFolderTab("/folder-b") || a.session != last {
		t.Fatal("folder b did not restore its selected tab")
	}
	assertTabs(3, 4, 5)
	if len(a.tabs) != 5 || !first.open || !first.State.Busy || first.State.Draft != "draft a" {
		t.Fatal("switching folders changed background state")
	}
}

func TestFolderTabsCloseUsesSameFolderNeighbors(t *testing.T) {
	a := New(stubBackend{}, "test", "/a", nil)
	first := a.session
	a.newSession("/b")
	background := a.session
	a.newSession("/a")
	middle := a.session
	a.newSession("/b")
	a.newSession("/a")
	last := a.session
	a.selectSession(middle)
	a.closeSession(middle)
	if a.session != first {
		t.Fatal("closing middle tab did not select same-folder predecessor")
	}
	a.closeSession(first)
	if a.session != last {
		t.Fatal("closing first tab did not select same-folder successor")
	}
	a.closeSession(last)
	if a.activeTab != "" || a.activeWorkspace != "/a" || !background.open || a.workspaceCwd() != "/a" {
		t.Fatal("closing last folder tab did not retain empty folder")
	}
	a.closeCurrentPage()
	if !background.open || len(a.tabs) != 2 {
		t.Fatal("closing empty workspace closed another folder's tab")
	}
	tester := native.NewTester(a.view, 1000, 760)
	if tester.HasText("任务输入") || tester.HasText("会话标签 2") {
		t.Fatal("empty workspace rendered background session")
	}
}

func TestFolderTabsWorktreesRestoreMixedPages(t *testing.T) {
	a, queue, root := p4App(t)
	other := t.TempDir()
	a.repositories = []project.Repository{{CommonDir: "/common", Path: root, Worktrees: []project.Worktree{{Path: root}, {Path: other}}}}
	tester := native.NewTester(a.view, 1100, 760)
	a.openFile(root, filepath.Join(root, "README.md"))
	p4Wait(t, tester, queue, func() bool { return a.currentFile().loaded })
	fileID := a.activeTab
	file := a.currentFile()
	file.buffer.Set("keep unsaved draft")
	file.observe()
	diffTab := storage.Tab{ID: "test-diff", Kind: "diff", Workspace: root, Path: file.path}
	a.files[diffTab.ID] = &filePage{id: diffTab.ID, kind: "diff", root: root, path: file.path, loaded: true}
	a.tabs = append(a.tabs, diffTab)
	a.terminalFactory = func(string) (terminalConnection, error) { return &testTerminal{}, nil }
	a.newTerminal()
	terminal := a.currentTerminal()
	a.newSession(other)
	otherSession := a.session
	a.openWorktree(a.repositories[0].Worktrees[0])
	if a.currentTerminal() != terminal || terminal.connection.(*testTerminal).closed.Load() {
		t.Fatal("worktree switch did not restore the running terminal")
	}
	for _, tab := range a.tabs {
		if tab.ID == fileID {
			a.selectWorkspaceTab(tab)
		}
	}
	a.openWorktree(a.repositories[0].Worktrees[1])
	if a.session != otherSession || a.activeWorkspace != other {
		t.Fatal("second worktree did not remain independent")
	}
	a.openWorktree(a.repositories[0].Worktrees[0])
	if a.activeTab != fileID || len(a.folderTabs(root)) != 4 || len(a.folderTabs(other)) != 1 {
		t.Fatal("file selection or mixed-page grouping lost")
	}
	a.quitPrompt = func(mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 0}, nil
	}
	a.closeFile(file)
	p4Wait(t, tester, queue, func() bool { return !a.quitDialog })
	if a.activeTab != fileID || file.buffer.String() != "keep unsaved draft" || !file.dirty() {
		t.Fatal("canceling dirty file close changed folder selection or draft")
	}
	a.selectWorkspaceTab(diffTab)
	a.openWorktree(a.repositories[0].Worktrees[1])
	a.openWorktree(a.repositories[0].Worktrees[0])
	if a.currentFile() == nil || a.currentFile().kind != "diff" || a.activeTab != diffTab.ID {
		t.Fatal("worktree switch did not restore the diff page")
	}
	a.newSession(filepath.Join(root, "src"))
	if a.activeWorkspace != root || len(a.folderTabs(root)) != 5 {
		t.Fatal("subdirectory session escaped its worktree")
	}
}

func TestFolderTabsFollowDiscoveredWorktree(t *testing.T) {
	a := New(stubBackend{}, "test", "/repository/src", nil)
	a.repositories = []project.Repository{{Path: "/repository", Worktrees: []project.Worktree{{Path: "/repository"}}}}
	a.refreshFolderSelection()
	if a.activeWorkspace != "/repository" || len(a.folderTabs(a.activeWorkspace)) != 1 || a.folderSelectedTabs["/repository"] != a.activeTab {
		t.Fatal("worktree discovery hid the selected session's tab")
	}
}

func TestFolderTabsRestartDoesNotLoadHiddenHistory(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := New(stubBackend{}, "test", "/project", nil)
	a.State.ThreadID = "selected"
	first := a.session
	a.newSession("/other")
	a.State.ThreadID = "hidden"
	a.selectSession(first)
	if err := store.Save(a.snapshot()); err != nil {
		t.Fatal(err)
	}
	backend := &lazyTabsBackend{requests: make(chan string, 8)}
	backend.pages = []codex.TurnsPage{{}}
	restored := New(backend, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	queue := make(chan func(), 8)
	restored.dispatch = func(callback func()) { queue <- callback }
	restored.recoverSelectedSession()
	pumpUpdate(t, queue)
	if len(backend.requests) != 1 || <-backend.requests != "selected" || restored.sessions[1].loaded {
		t.Fatal("restart recovered hidden folder history")
	}
	restored.closeSession(restored.session)
	restored.recoverSelectedSession()
	if len(backend.requests) != 0 || restored.activeTab != "" || restored.sessions[1].loaded {
		t.Fatal("empty folder recovered another folder's history")
	}
}

func TestFolderTabsPersistSelectionAndEmptyWorkspace(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := New(stubBackend{}, "test", "/a", nil)
	first := a.session
	a.newSession("/a")
	a.newSession("/b")
	second := a.session
	a.newSession("/b")
	a.selectSession(first)
	a.selectSession(second)
	if err := store.Save(a.snapshot()); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.activeTab != sessionTabID(second.id) || !restored.restoreFolderTab("/a") || restored.activeTab != sessionTabID(first.id) {
		t.Fatal("restart lost per-folder selection")
	}
	for _, target := range restored.sessions {
		if target.State.Cwd == "/a" {
			restored.closeSession(target)
		}
	}
	if err := store.Save(restored.snapshot()); err != nil {
		t.Fatal(err)
	}
	empty := New(stubBackend{}, "test", "", nil)
	if err := empty.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if empty.activeTab != "" || empty.activeWorkspace != "/a" || len(empty.folderTabs("/b")) != 2 {
		t.Fatal("restart replaced empty folder with another folder's page")
	}
}

func TestFolderTabsRestoreValidatesRememberedSelection(t *testing.T) {
	a := New(stubBackend{}, "test", "/a", nil)
	a.newSession("/b")
	snapshot := a.snapshot()
	snapshot.FolderSelectedTabs = map[string]string{"/a": "session-missing", "/b": sessionTabID(1), "/gone": "missing"}
	snapshot.ActiveTabID = "missing"
	if err := a.restoreWorkspaceTabs(snapshot, ""); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.folderSelectedTabs, map[string]string{"/a": sessionTabID(1), "/b": sessionTabID(2)}) || a.activeTab != sessionTabID(2) {
		t.Fatal("invalid selections were not replaced with same-folder tabs", a.folderSelectedTabs)
	}
	snapshot.FolderSelectedTabs = nil
	if err := a.restoreWorkspaceTabs(snapshot, ""); err != nil || !a.restoreFolderTab("/a") {
		t.Fatal("legacy settings no longer restore folder tabs", err)
	}
}

func TestFolderTabsIndicatorAndDisclosureStayStable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/a", nil)
			tester := native.NewTester(a.view, 1000, 760)
			tester.SetDark(dark)
			before, found := tester.Find("展开项目 /a")
			if !found || !tester.HasText("有打开的页签 /a") {
				t.Fatal("open folder indicator missing")
			}
			dot, _ := tester.Find("有打开的页签 /a")
			pixel := tester.Image().RGBAAt(int(dot.X+dot.W/2), int(dot.Y+dot.H/2))
			if pixel.G <= pixel.R || pixel.G <= pixel.B {
				t.Fatal("folder indicator is not green", pixel)
			}
			selected := a.activeTab
			if err := tester.Click("展开项目 /a"); err != nil {
				t.Fatal(err)
			}
			tester.Advance(300 * time.Millisecond)
			if a.activeTab != selected || !a.collapsedProjects["/a"] {
				t.Fatal("folder heading stopped being disclosure-only")
			}
			a.closeSession(a.session)
			tester.Frame()
			after, _ := tester.Find("展开项目 /a")
			if tester.HasText("有打开的页签 /a") || before != after {
				t.Fatal("indicator did not clear or moved the folder heading", before, after)
			}
		})
	}
}

func TestFolderTabsIndicatorPreservesOriginalNameOrigin(t *testing.T) {
	for _, worktree := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("worktree=%t/dark=%t", worktree, dark), func(t *testing.T) {
				a := New(stubBackend{}, "test", "/folder-name", nil)
				if worktree {
					a.repositories = []project.Repository{{CommonDir: "/folder-name/.git", Path: "/folder-name", Name: "Repository", Worktrees: []project.Worktree{{Path: "/folder-name", Branch: "main"}}}}
				}
				tester := native.NewTester(a.view, 1000, 760)
				tester.SetDark(dark)
				name, found := tester.Find("folder-name")
				dot, active := tester.Find("有打开的页签 /folder-name")
				if !found || !active || name.X != 37 || dot.X+dot.W >= name.X {
					t.Fatal("indicator moved the original name or is not before it", name, dot)
				}
				a.closeSession(a.session)
				tester.Frame()
				after, found := tester.Find("folder-name")
				if !found || after != name {
					t.Fatal("clearing indicator moved or resized the folder name", name, after)
				}
			})
		}
	}
}
