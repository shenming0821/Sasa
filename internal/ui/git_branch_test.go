package ui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

func TestGitBranchPickerLayoutAndMotion(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{240, 320, 520} {
			t.Run(fmt.Sprintf("dark=%t-width=%d", dark, width), func(t *testing.T) {
				a, _, root := p4App(t)
				w := a.workspace(root)
				w.gitTime, w.gitFingerprint = time.Now(), "fixture"
				w.git = gitview.Status{Branch: "main", Upstream: "origin/main"}
				w.gitRemotes = []string{"origin"}
				w.gitBranches = []gitview.Branch{{Name: "fe"}, {Name: "feature/" + strings.Repeat("long-name-", 8)}, {Name: "main", Current: true}}
				tester := native.NewTester(func(c *native.Context) {
					native.Column(c).Fill().Background(c.Theme().Surface).Children(func() { a.gitPanel(c, w) })
				}, width, 650)
				tester.SetDark(dark)
				position := func() float32 {
					t.Helper()
					bounds, found := tester.Find("Git 提交信息")
					if !found {
						t.Fatal("missing commit input")
					}
					return bounds.Y
				}
				closed := position()
				if err := tester.Click("选择 Git 分支"); err != nil {
					t.Fatal(err)
				}
				tester.Advance(60 * time.Millisecond)
				opening := position()
				tester.Advance(250 * time.Millisecond)
				opened := position()
				if opening <= closed || opening >= opened || w.gitBranchBase != "main" {
					t.Fatal("opening did not animate or default to current branch", closed, opening, opened, w.gitBranchBase)
				}
				for _, label := range []string{"Git 分支面板", "新分支基于分支", "新分支名称", "新建", "切换分支 main", "切换分支 " + w.gitBranches[1].Name} {
					bounds, found := tester.Find(label)
					if !found || bounds.W <= 0 || bounds.X < 12 || bounds.X+bounds.W > float32(width)-12 {
						t.Fatal("branch control exceeds inset panel", label, bounds)
					}
				}
				current, _ := tester.Find("切换分支 main")
				first, _ := tester.Find("切换分支 fe")
				second, _ := tester.Find("切换分支 " + w.gitBranches[1].Name)
				if current.Y >= first.Y || first.Y >= second.Y {
					t.Fatal("current branch is not first or other branches changed order")
				}
				if dir := os.Getenv("SASA_UI_EVIDENCE"); dir != "" {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					file, err := os.Create(filepath.Join(dir, fmt.Sprintf("git-branches-dark-%t-width-%d.png", dark, width)))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(file, tester.Image())
					file.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := tester.Click("新分支基于分支"); err != nil {
					t.Fatal(err)
				}
				current, _ = tester.Find("来源分支 main")
				first, _ = tester.Find("来源分支 fe")
				second, _ = tester.Find("来源分支 " + w.gitBranches[1].Name)
				if current.Y >= first.Y || first.Y >= second.Y {
					t.Fatal("base dropdown does not put current branch first")
				}
				if err := tester.Click("来源分支 " + w.gitBranches[1].Name); err != nil {
					t.Fatal(err)
				}
				if w.gitBranchBase != w.gitBranches[1].Name {
					t.Fatal("dropdown did not select the requested base")
				}
				tester.Click("选择 Git 分支")
				tester.Advance(60 * time.Millisecond)
				closing := position()
				if closing <= closed || closing >= opened {
					t.Fatal("collapse skipped intermediate height", closing)
				}
				tester.Click("选择 Git 分支")
				tester.Advance(250 * time.Millisecond)
				if position() != opened {
					t.Fatal("reversing collapse did not settle open")
				}
				tester.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
				tester.Click("选择 Git 分支")
				if position() != closed {
					t.Fatal("reduced motion did not collapse immediately")
				}
				tester.Click("选择 Git 分支")
				if position() != opened || w.gitBranchBase != "main" {
					t.Fatal("reduced motion did not open immediately with current base")
				}
			})
		}
	}
}

func TestGitBranchCreateFromSelectedBase(t *testing.T) {
	a, queue, root, git := gitWorkflowApp(t)
	git("branch", "base")
	base := strings.TrimSpace(git("rev-parse", "base"))
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("new main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("commit", "-am", "advance main")
	w := a.workspace(root)
	tester := native.NewTester(a.view, 1280, 800)
	p4Wait(t, tester, queue, func() bool { return !w.gitLoading && len(w.gitBranches) == 2 })
	tester.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	tester.Click("选择 Git 分支")
	if w.gitBranchBase != "main" {
		t.Fatal("current branch was not the default")
	}
	tester.Click("新分支基于分支")
	if err := tester.Click("来源分支 base"); err != nil {
		t.Fatal(err)
	}
	tester.Click("新分支名称")
	tester.Type("feature/from-base")
	if err := tester.Click("新建"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tester, queue, func() bool { return w.gitBusy == "" && w.git.Branch == "feature/from-base" })
	if got := strings.TrimSpace(git("rev-parse", "HEAD")); got != base {
		t.Fatal("new branch did not start at the selected base", got, base)
	}
	if w.gitBranchPicker || w.gitBranchName != "" || w.gitBranchBase != "" {
		t.Fatal("successful creation did not reset the form")
	}
}
