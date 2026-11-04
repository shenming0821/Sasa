package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
)

func TestGitIndexActionsKeepHeaderPixelsSteady(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, message := range []string{"", "fix: draft"} {
			t.Run(fmt.Sprintf("dark=%t/message=%q", dark, message), func(t *testing.T) {
				a := New(nil, "test", "/repo", nil)
				w := a.workspace("/repo")
				w.git = gitview.Status{Branch: "main", Upstream: "origin/main", Ahead: 1, Entries: []gitview.Entry{{Path: "file.go", XY: ".M"}}}
				w.gitRemotes = []string{"origin"}
				w.gitMessage = message
				w.gitTime = time.Now()
				w.gitFingerprint = fmt.Sprintf("%+v", w.git)
				tester := native.NewTester(func(c *native.Context) {
					native.Column(c).Fill().Background(c.Theme().Surface).Children(func() { a.gitPanel(c, w) })
				}, 600, 650)
				tester.SetDark(dark)
				tester.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
				button, found := tester.Find("全部暂存并提交")
				if !found {
					t.Fatal("missing commit button")
				}
				header := image.Rect(0, 0, 600, int(button.Y+button.H))
				pixels := func() []byte {
					region := image.NewRGBA(header)
					draw.Draw(region, header, tester.Image(), header.Min, draw.Src)
					return region.Pix
				}
				before := pixels()
				for _, action := range []string{"暂存", "", "取消暂存", ""} {
					w.gitBusy = action
					tester.Frame()
					if !bytes.Equal(before, pixels()) {
						t.Fatalf("Git header flashed during %q", action)
					}
				}
				w.gitBusy = "暂存"
				tester.Frame()
				if err := tester.Click("选择 Git 分支"); err != nil {
					t.Fatal(err)
				}
				if w.gitBranchPicker {
					t.Fatal("branch picker opened during index write")
				}
				if err := tester.Click("Git 提交信息"); err != nil {
					t.Fatal(err)
				}
				tester.Type("edited")
				if !strings.Contains(w.gitMessage, "edited") {
					t.Fatal("index action prevented editing commit message")
				}
			})
		}
	}
}

func TestGitIndexActionsKeepListSteady(t *testing.T) {
	a := New(nil, "test", "/repo", nil)
	a.historyOpen, a.rightTab = true, "git"
	w := a.workspace("/repo")
	w.git = gitview.Status{Branch: "main", Entries: []gitview.Entry{{Path: "file.go", XY: ".M"}}}
	w.gitTime = time.Now()
	w.gitFingerprint = fmt.Sprintf("%+v", w.git)
	tester := native.NewTester(a.view, 1280, 800)
	row, found := tester.Find("Git 更改 file.go")
	if !found {
		t.Fatal("missing Git row")
	}
	for _, action := range []string{"暂存", "", "取消暂存", ""} {
		w.gitBusy = action
		tester.Frame()
		if tester.HasText("正在暂存…") || tester.HasText("正在取消暂存…") {
			t.Fatal("index action inserted a progress row")
		}
		current, found := tester.Find("Git 更改 file.go")
		if !found || current != row {
			t.Fatalf("index action moved content: before=%+v after=%+v", row, current)
		}
	}
	w.gitActionError = "暂存失败：index locked"
	tester.Frame()
	if !tester.HasText(w.gitActionError) {
		t.Fatal("index action error was hidden")
	}
}

func TestGitBulkStageAlternatesWithReadySnapshot(t *testing.T) {
	a, queue, root, git := gitWorkflowApp(t)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("bulk stage\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/main.go"), []byte("package changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := a.workspace(root)
	tester := native.NewTester(a.view, 1280, 800)
	p4Wait(t, tester, queue, func() bool { return !w.gitLoading && len(w.git.Entries) == 2 })
	w.gitHistoryLoaded = true
	for iteration := range 6 {
		group, label, target := "更改", "全部暂存", "已暂存的更改"
		if iteration%2 != 0 {
			group, label, target = target, "全部取消暂存", group
		}
		header, found := tester.Find("折叠 " + group)
		if !found {
			t.Fatal("missing group", group)
		}
		tester.Move(header.X+10, header.Y+10)
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
		if w.gitBusy == "" {
			t.Fatal("bulk action did not start")
		}
		p4Wait(t, tester, queue, func() bool { return w.gitBusy == "" })
		if len(gitEntries(w, target)) != 2 || len(gitEntries(w, group)) != 0 {
			t.Fatalf("action became available before result: %+v", w.git.Entries)
		}
		if w.gitLoading || w.gitNotice != "" || !w.gitHistoryLoaded || w.gitActionError != "" {
			t.Fatalf("unnecessary refresh or notice: loading=%t history=%t notice=%q error=%q", w.gitLoading, w.gitHistoryLoaded, w.gitNotice, w.gitActionError)
		}
		staged := strings.TrimSpace(git("diff", "--cached", "--name-only"))
		if (staged != "") != (iteration%2 == 0) {
			t.Fatal("snapshot does not match index", staged)
		}
	}
}

func TestGitMoveStatsCarriesOnlyUnchangedDiffs(t *testing.T) {
	stats := map[string]gitview.Review{
		gitKey("false", "modified.go"):  {Added: 3, Deleted: 1},
		gitKey("false", "untracked.go"): {Added: 5},
		gitKey("false", "partial.go"):   {Added: 2},
		gitKey("true", "partial.go"):    {Added: 1},
		gitKey("true", "staged.go"):     {Added: 4, Deleted: 2},
		gitKey("true", "rename.go"):     {Added: 7},
	}
	gitMoveStats(stats, []gitview.Entry{
		{Path: "modified.go", XY: ".M"},
		{Path: "untracked.go", XY: "??"},
		{Path: "partial.go", XY: "MM"},
	}, false)
	if got := stats[gitKey("true", "modified.go")]; got.Added != 3 || got.Deleted != 1 {
		t.Fatal("staging did not carry the worktree count", got)
	}
	if got := stats[gitKey("true", "untracked.go")]; got.Added != 5 {
		t.Fatal("staging an untracked file did not carry its count", got)
	}
	if _, ok := stats[gitKey("false", "modified.go")]; ok {
		t.Fatal("staged file kept a worktree count")
	}
	if _, ok := stats[gitKey("true", "partial.go")]; ok || stats[gitKey("false", "partial.go")].Added != 0 {
		t.Fatal("partially staged file kept stale counts")
	}
	gitMoveStats(stats, []gitview.Entry{
		{Path: "staged.go", XY: "M."},
		{Path: "rename.go", XY: "R.", Original: "old.go"},
	}, true)
	if got := stats[gitKey("false", "staged.go")]; got.Added != 4 || got.Deleted != 2 {
		t.Fatal("unstaging did not carry the index count", got)
	}
	if _, ok := stats[gitKey("false", "rename.go")]; ok {
		t.Fatal("unstaging a rename kept a stale count")
	}
}

func TestGitStageKeepsLineCountsVisible(t *testing.T) {
	a, q, root, _ := gitWorkflowApp(t)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("extra line\n"), 0600)
	w := a.workspace(root)
	tester := native.NewTester(a.view, 1280, 800)
	p4Wait(t, tester, q, func() bool { return !w.gitLoading && len(w.git.Entries) == 1 })
	source := gitKey("false", "README.md")
	p4Wait(t, tester, q, func() bool {
		_, ok := w.gitStats[source]
		return ok
	})
	before := w.gitStats[source]
	a.gitStage(w, gitEntries(w, "更改"), false)
	p4Wait(t, tester, q, func() bool { return w.gitBusy == "" && len(gitEntries(w, "已暂存的更改")) == 1 })
	moved, ok := w.gitStats[gitKey("true", "README.md")]
	if !ok || moved.Added != before.Added || moved.Deleted != before.Deleted {
		t.Fatal("staged row lost its line counts", w.gitStats)
	}
	a.gitStage(w, gitEntries(w, "已暂存的更改"), true)
	p4Wait(t, tester, q, func() bool { return w.gitBusy == "" && len(gitEntries(w, "更改")) == 1 })
	if _, ok := w.gitStats[source]; !ok {
		t.Fatal("unstaged row lost its line counts", w.gitStats)
	}
}
