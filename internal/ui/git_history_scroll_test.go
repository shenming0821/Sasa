package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
)

func gitHistoryTester(a *App, w *workspacePage) *native.Tester {
	return native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Children(func() { a.gitHistoryView(c, w, false, 650) })
	}, 360, 650)
}

func advanceGitHistory(tt *native.Tester) {
	// Pagination deliberately waits for a different frame time, including
	// when an input causes multiple build passes within one frame.
	for range 3 {
		tt.Advance(time.Millisecond)
	}
}

func TestGitHistoryAutoPagination(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	for i := range 44 {
		git("commit", "--allow-empty", "-m", fmt.Sprintf("history %02d", i))
	}
	expected := strings.Fields(git("log", "--format=%H"))
	w := a.workspace(root)
	w.collapsed["最近提交已展开"] = true
	tt := gitHistoryTester(a, w)
	tt.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 20 })
	advanceGitHistory(tt)
	if w.gitHistoryLoading || !w.gitHistoryMore || w.gitHistoryScroll.MaxY <= 80 {
		t.Fatal("first page should wait for scrolling", w.gitHistoryScroll)
	}
	list, _ := tt.Find("最近提交列表")
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, w.gitHistoryScroll.MaxY-81)
	advanceGitHistory(tt)
	if w.gitHistoryLoading {
		t.Fatal("pagination started outside the 80 DIP threshold")
	}
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, 1)
	advanceGitHistory(tt)
	if !w.gitHistoryLoading {
		t.Fatal("pagination did not start within the 80 DIP threshold")
	}
	if !tt.HasText("正在读取提交…") {
		t.Fatal("automatic pagination did not display loading feedback")
	}
	var complete func()
	select {
	case complete = <-q:
	case <-time.After(5 * time.Second):
		t.Fatal("second page did not complete")
	}
	for range 5 {
		advanceGitHistory(tt)
	}
	if !w.gitHistoryLoading || len(q) != 0 || len(w.gitHistory) != 20 {
		t.Fatal("rendering started another request while a page was in flight")
	}
	complete()
	tt.Frame()
	if w.gitHistoryLoading || len(w.gitHistory) != 40 {
		t.Fatal("old scroll bounds triggered a request before appended rows were measured")
	}
	advanceGitHistory(tt)
	if w.gitHistoryLoading || len(w.gitHistory) != 40 {
		t.Fatal("appending a full page should move the bottom away from the viewport")
	}
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
	advanceGitHistory(tt)
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 45 })
	for range 5 {
		advanceGitHistory(tt)
		tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
	}
	if w.gitHistoryMore || w.gitHistoryLoading || len(q) != 0 {
		t.Fatal("exhausted history kept loading")
	}
	for i, info := range w.gitHistory {
		if info.Hash != expected[i] {
			t.Fatalf("pagination duplicated or skipped commit %d: %s != %s", i, info.Hash, expected[i])
		}
	}
}

func TestGitHistoryAutoPaginationLayoutGuards(t *testing.T) {
	for _, scenario := range []string{"first-layout", "no-overflow", "opening", "closing", "commit-opening", "commit-closing", "busy", "error", "no-async"} {
		t.Run(scenario, func(t *testing.T) {
			a, _, root := p4App(t)
			w := a.workspace(root)
			w.gitHistoryLoaded, w.gitHistoryMore = true, true
			w.collapsed["最近提交已展开"] = true
			count := 20
			if scenario == "no-overflow" {
				count = 1
			}
			for i := range count {
				w.gitHistory = append(w.gitHistory, gitview.CommitInfo{Hash: fmt.Sprintf("%040d", i), Subject: "commit"})
			}
			if scenario == "opening" {
				w.collapsed["最近提交已展开"] = false
			}
			if scenario == "busy" {
				w.gitBusy = "提交"
			}
			if scenario == "error" {
				w.gitHistoryError = "读取失败"
			}
			if scenario == "no-async" {
				a.dispatch = nil
			}
			if strings.HasPrefix(scenario, "commit-") {
				tree := &gitCommitTree{loaded: true, expanded: scenario == "commit-closing"}
				for i := range 10 {
					tree.entries = append(tree.entries, gitview.Entry{Path: fmt.Sprintf("file-%d", i)})
				}
				w.gitCommitTrees = map[string]*gitCommitTree{w.gitHistory[0].Hash: tree}
			}
			tt := gitHistoryTester(a, w)
			if w.gitHistoryLoading {
				t.Fatal("first layout started pagination")
			}
			if scenario == "first-layout" {
				return
			}
			advanceGitHistory(tt)
			if scenario == "opening" || scenario == "closing" {
				if err := tt.Click("切换最近提交"); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(scenario, "commit-") {
				if err := tt.Click("提交 " + w.gitHistory[0].Hash); err != nil {
					t.Fatal(err)
				}
			}
			list, _ := tt.Find("最近提交列表")
			tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
			tt.Advance(30 * time.Millisecond)
			if w.gitHistoryLoading {
				t.Fatal("unstable, non-scrollable or blocked list started pagination")
			}
			if scenario == "no-overflow" && w.gitHistoryScroll.MaxY != 0 {
				t.Fatal("short list unexpectedly scrolls", w.gitHistoryScroll)
			}
			if scenario == "no-overflow" || scenario == "error" {
				if _, ok := tt.Find("加载更多提交"); !ok {
					t.Fatal("fallback button missing")
				}
			}
		})
	}
}

func TestGitHistoryAutoPaginationFailureNeedsManualRetry(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	for i := range 20 {
		git("commit", "--allow-empty", "-m", fmt.Sprintf("history %02d", i))
	}
	w := a.workspace(root)
	w.collapsed["最近提交已展开"] = true
	tt := gitHistoryTester(a, w)
	tt.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 20 })
	advanceGitHistory(tt)
	// Fail log in the isolated repository without replacing the system git.
	w.root = filepath.Join(root, "missing")
	list, _ := tt.Find("最近提交列表")
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
	advanceGitHistory(tt)
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && w.gitHistoryError != "" })
	for range 5 {
		advanceGitHistory(tt)
	}
	if w.gitHistoryLoading || len(q) != 0 || len(w.gitHistory) != 20 {
		t.Fatal("failed pagination retried automatically or discarded history")
	}
	w.root = root
	tt.Scroll(list.X+20, list.Y+list.H/2, 0, 100000)
	if err := tt.Click("加载更多提交"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 21 })
	if w.gitHistoryError != "" || w.gitHistoryMore {
		t.Fatal("manual retry did not finish pagination", w.gitHistoryError)
	}
}
