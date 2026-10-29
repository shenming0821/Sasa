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
