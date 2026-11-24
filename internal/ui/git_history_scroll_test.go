package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
)

func gitHistoryTester(a *App, w *workspacePage) *native.Tester {
	return native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Children(func() { a.gitHistoryView(c, w, false, 650) })
	}, 360, 650)
}
func advanceGitHistory(tt *native.Tester) {
	for range 3 {
		tt.Advance(time.Millisecond)
	}
}
func waitGitHistory(t *testing.T, tt *native.Tester, q chan func(), condition func() bool) {
	t.Helper()
	p4Wait(t, tt, q, func() bool {
		tt.Advance(time.Millisecond)
		return condition()
	})
}

func TestGitCommitsBackgroundRefreshKeepsVisibleHistory(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	w := a.workspace(root)
	a.refreshGit(w)
	tester := gitHistoryTester(a, w)
	advanceGitHistory(tester)
	if !tester.HasText("正在读取提交…") {
		t.Fatal("initial load did not show progress")
	}
	waitGitHistory(t, tester, q, func() bool { return !w.gitLoading && w.gitHistoryLoaded && !w.gitHistoryLoading })
	previous := w.gitHistory[0].Subject
	request := w.gitHistoryRequest

	a.refreshGit(w)
	waitGitHistory(t, tester, q, func() bool { return !w.gitLoading })
	if w.gitHistoryRequest != request {
		t.Fatal("unchanged status reloaded history")
	}
	if err := os.WriteFile(filepath.Join(root, "draft.txt"), []byte("untracked"), 0600); err != nil {
		t.Fatal(err)
	}
	a.refreshGit(w)
	waitGitHistory(t, tester, q, func() bool { return !w.gitLoading })
	if len(w.git.Entries) == 0 || w.gitHistoryRequest != request {
		t.Fatal("worktree changes should refresh status only")
	}

	git("commit", "--allow-empty", "-m", "background refresh commit")
	a.refreshGit(w)
	waitGitHistory(t, tester, q, func() bool { return !w.gitLoading })
	if !w.gitHistoryLoading {
		t.Fatal("background refresh did not start")
	}
	if tester.HasText("正在读取提交…") || len(a.gitCommitsRows(w)) != len(w.gitHistory)+1 {
		t.Fatal("background refresh added a loading row")
	}
	if !tester.HasText(previous) {
		t.Fatal("background refresh hid existing commits")
	}

	waitGitHistory(t, tester, q, func() bool { return !w.gitHistoryLoading })
	if !tester.HasText("background refresh commit") {
		t.Fatal("background refresh did not show the new commit")
	}

	head := w.git.Head
	request = w.gitHistoryRequest
	git("tag", "refresh-test")
	a.refreshGit(w)
	waitGitHistory(t, tester, q, func() bool { return !w.gitLoading && w.gitHistoryLoaded && !w.gitHistoryLoading })
	if w.git.Head != head || w.gitHistoryRequest != request+1 {
		t.Fatal("tag change should reload history with HEAD unchanged")
	}
	for _, ref := range w.gitHistory[0].Refs {
		if ref.Name == "refresh-test" {
			return
		}
	}
	t.Fatal("reloaded history did not contain the new tag")
}

func TestGitCommitsAutoPaginationAndStablePrefix(t *testing.T) {
	a, q, root, g := gitWorkflowApp(t)
	for i := range 104 {
		g("commit", "--allow-empty", "-m", fmt.Sprintf("history %03d", i))
	}
	w := a.workspace(root)
	tt := gitHistoryTester(a, w)
	waitGitHistory(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 50 })
	advanceGitHistory(tt)
	if w.gitHistoryLoading || len(w.gitHistory) != 50 {
		t.Fatal("unscrolled list auto-loaded history")
	}
	list, _ := tt.Find("COMMITS 列表")
	w.gitBusy = "test"
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
	advanceGitHistory(tt)
	if w.gitHistoryLoading {
		t.Fatal("busy repository auto-loaded history")
	}
	w.gitBusy = ""
	waitGitHistory(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 100 })
	for range 5 {
		advanceGitHistory(tt)
	}
	if w.gitHistoryLoading || len(w.gitHistory) != 100 {
		t.Fatal("new page cascaded into another request")
	}
	if _, ok := tt.Find("显示更多提交"); ok {
		t.Fatal("manual pagination button remains")
	}
	// A new HEAD between pages must replace the prefix, not append offset pages.
	g("commit", "--allow-empty", "-m", "new HEAD")
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
	waitGitHistory(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 106 })
	want := strings.Fields(g("log", "--date-order", "--format=%H"))
	for i, cm := range w.gitHistory {
		if cm.Hash != want[i] {
			t.Fatalf("duplicate or skipped %d", i)
		}
	}
	if w.gitHistoryMore {
		t.Fatal("exhausted history offers another page")
	}
	a.gitLoadHistory(w, false)
	waitGitHistory(t, tt, q, func() bool { return !w.gitHistoryLoading })
	if len(w.gitHistory) != 106 {
		t.Fatal("refresh reset the loaded prefix", len(w.gitHistory))
	}
}
func TestGitCommitsFailurePreservesSnapshotAndManualRetry(t *testing.T) {
	a, q, root, g := gitWorkflowApp(t)
	for i := range 54 {
		g("commit", "--allow-empty", "-m", fmt.Sprintf("history %03d", i))
	}
	w := a.workspace(root)
	tt := gitHistoryTester(a, w)
	waitGitHistory(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 50 })
	old := w.gitHistory[0].Hash
	w.root = filepath.Join(root, "missing")
	list, _ := tt.Find("COMMITS 列表")
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
	waitGitHistory(t, tt, q, func() bool { return !w.gitHistoryLoading && w.gitHistoryError != "" })
	if len(w.gitHistory) != 50 || w.gitHistory[0].Hash != old {
		t.Fatal("failure discarded snapshot")
	}
	for range 5 {
		advanceGitHistory(tt)
	}
	if w.gitHistoryLoading || len(q) != 0 {
		t.Fatal("failure retried automatically")
	}
	w.root = root
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
	if err := tt.Click("重试读取提交"); err != nil {
		t.Fatal(err)
	}
	waitGitHistory(t, tt, q, func() bool { return !w.gitHistoryLoading && w.gitHistoryError == "" })
}

func TestGitCommitsAutoPaginationRequiresScrollableOpenList(t *testing.T) {
	a, q, root, _ := gitWorkflowApp(t)
	w := a.workspace(root)
	tt := gitHistoryTester(a, w)
	waitGitHistory(t, tt, q, func() bool { return w.gitHistoryLoaded && !w.gitHistoryLoading })
	// Even when the backend offers another page, fitting content alone must
	// not request it. The user needs to scroll through the loaded rows.
	w.gitHistoryMore = true
	for range 5 {
		advanceGitHistory(tt)
	}
	if w.gitHistoryLoading || len(q) != 0 {
		t.Fatal("fitting content requested another page")
	}
	if err := tt.Click("切换 COMMITS"); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		advanceGitHistory(tt)
	}
	if commitsOpen(w) || w.gitHistoryLoading || len(q) != 0 {
		t.Fatal("collapsed history requested another page")
	}
}
