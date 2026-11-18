package ui

import (
	"fmt"
	native "github.com/egoist/mygo/ui"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
func TestGitCommitsManualPaginationAndStablePrefix(t *testing.T) {
	a, q, root, g := gitWorkflowApp(t)
	for i := range 104 {
		g("commit", "--allow-empty", "-m", fmt.Sprintf("history %03d", i))
	}
	w := a.workspace(root)
	tt := gitHistoryTester(a, w)
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 50 })
	list, _ := tt.Find("COMMITS 列表")
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
	advanceGitHistory(tt)
	if w.gitHistoryLoading {
		t.Fatal("scroll auto-loaded history")
	}
	if err := tt.Click("显示更多提交"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 100 })
	// A new HEAD between pages must replace the prefix, not append offset pages.
	g("commit", "--allow-empty", "-m", "new HEAD")
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
	if err := tt.Click("显示更多提交"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 106 })
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
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading })
	if len(w.gitHistory) != 106 {
		t.Fatal("refresh reset the loaded prefix", len(w.gitHistory))
	}
}
func TestGitCommitsFailurePreservesSnapshotAndManualRetry(t *testing.T) {
	a, q, root, g := gitWorkflowApp(t)
	g("commit", "--allow-empty", "-m", "second")
	w := a.workspace(root)
	tt := gitHistoryTester(a, w)
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 2 })
	old := w.gitHistory[0].Hash
	w.root = filepath.Join(root, "missing")
	a.gitLoadHistory(w, false)
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && w.gitHistoryError != "" })
	if len(w.gitHistory) != 2 || w.gitHistory[0].Hash != old {
		t.Fatal("failure discarded snapshot")
	}
	for range 5 {
		advanceGitHistory(tt)
	}
	if w.gitHistoryLoading || len(q) != 0 {
		t.Fatal("failure retried automatically")
	}
	w.root = root
	list, _ := tt.Find("COMMITS 列表")
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
	if err := tt.Click("重试读取提交"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && w.gitHistoryError == "" })
}
