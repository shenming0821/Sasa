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

func TestGitBranchPickerOverlayLayout(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{240, 520, 1000} {
			t.Run(fmt.Sprintf("dark=%t-width=%d", dark, width), func(t *testing.T) {
				a, _, root := p4App(t)
				w := a.workspace(root)
				w.gitTime, w.gitFingerprint = time.Now(), "fixture"
				w.git = gitview.Status{Branch: "main", Upstream: "origin/main"}
				w.gitRemotes = []string{"origin"}
				catalog := a.branchCatalog(root)
				catalog.loaded, catalog.checked = true, time.Now()
				catalog.branches = []gitview.Branch{
					{Name: "fe", CommitTime: catalog.checked, Author: "Anton", Subject: "Latest change"},
					{Name: "feature/" + strings.Repeat("long-name-", 8)},
					{Name: "main"},
					{Name: "origin/main", Remote: true},
					{Name: "origin/remote-only", Remote: true},
				}
				tester := native.NewTester(func(c *native.Context) {
					native.Column(c).Fill().Background(c.Theme().Surface).Children(func() { a.gitPanel(c, w) })
					a.gitBranchPickerView(c)
				}, width, 650)
				tester.SetDark(dark)
				before, found := tester.Find("Git 提交信息")
				if !found {
					t.Fatal("missing commit input")
				}
				if err := tester.Click("选择 Git 分支"); err != nil {
					t.Fatal(err)
				}
				tester.Advance(200 * time.Millisecond)
				dialog, found := tester.Find("切换分支")
				if !found || dialog.W <= 0 || dialog.W > 560 || dialog.W > float32(width)*.92+1 ||
					dialog.Y < 45 || dialog.Y > 47 || absFloat32(dialog.X+dialog.W/2-float32(width)/2) > 1 {
					t.Fatal("branch picker is outside its centered bounds", dialog)
				}
				current, hasCurrent := tester.Find("切换分支 main")
				local, hasLocal := tester.Find("切换分支 fe")
				remote, hasRemote := tester.Find("切换分支 origin/remote-only")
				_, hasDuplicate := tester.Find("切换分支 origin/main")
				if !hasCurrent || !hasLocal || !hasRemote || current.Y >= local.Y || local.Y >= remote.Y ||
					hasDuplicate || !tester.HasText("Anton • now • Latest change") {
					t.Fatal("missing group order, deduplication, or commit metadata", current, local, remote, hasDuplicate, tester.HasText("Anton • now • Latest change"))
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
				tester.Key(0, native.KeyEscape)
				tester.Frame()
				after, found := tester.Find("Git 提交信息")
				if a.branchPicker != nil || !found || after != before {
					t.Fatal("Escape did not close the overlay or the commit input moved", before, after)
				}
				if err := tester.Click("选择 Git 分支"); err != nil {
					t.Fatal(err)
				}
				tester.Advance(200 * time.Millisecond)
				tester.ClickAt(float32(width)/2, 10)
				if a.branchPicker != nil {
					t.Fatal("outside click did not close the overlay")
				}
			})
		}
	}
}

func TestGitBranchPickerSearchSwitchAndCreate(t *testing.T) {
	a, queue, root, git := gitWorkflowApp(t)
	git("branch", "feature/older")
	git("branch", "feature/target")
	git("update-ref", "refs/remotes/origin/remote-only", "HEAD")
	git("remote", "add", "origin", root)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("new main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("commit", "-am", "advance main")
	head := strings.TrimSpace(git("rev-parse", "HEAD"))
	w := a.workspace(root)
	tester := native.NewTester(a.view, 1280, 800)
	p4Wait(t, tester, queue, func() bool { return !w.gitLoading && a.branchCatalog(root).loaded })
	tester.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	open := func() {
		t.Helper()
		if err := tester.Click("选择 Git 分支"); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
	}
	open()
	tester.Type("ftrg")
	if !tester.HasText("创建分支 ftrg") {
		t.Fatal("missing explicit create choice beside fuzzy match")
	}
	tester.Key(0, native.KeyEnter)
	p4Wait(t, tester, queue, func() bool { return w.gitBusy == "" && w.git.Branch == "feature/target" })
	open()
	tester.Type("remote-only")
	if tester.HasText("创建分支 remote-only") {
		t.Fatal("a remote branch's short name was offered for creation")
	}
	tester.Key(0, native.KeyEnter)
	p4Wait(t, tester, queue, func() bool { return w.gitBusy == "" && w.git.Branch == "remote-only" })
	if got := strings.TrimSpace(git("rev-parse", "--abbrev-ref", "@{upstream}")); got != "origin/remote-only" {
		t.Fatal("remote branch was not tracked", got)
	}
	git("switch", "main")
	a.refreshGit(w)
	p4Wait(t, tester, queue, func() bool { return !w.gitLoading && w.git.Branch == "main" })
	open()
	tester.Key(0, native.KeyUp)
	tester.Key(0, native.KeyEnter)
	tester.Frame()
	if _, found := tester.Find("新分支名称"); !found {
		t.Fatal("create row did not open name prompt")
	}
	tester.Type("feature/new")
	tester.Key(0, native.KeyEnter)
	p4Wait(t, tester, queue, func() bool { return w.gitBusy == "" && w.git.Branch == "feature/new" })
	if got := strings.TrimSpace(git("rev-parse", "HEAD")); got != head || a.branchPicker != nil {
		t.Fatal("creation did not start from current HEAD and close the picker", got)
	}
	open()
	tester.Type("unique-new")
	tester.Key(0, native.KeyEnter)
	p4Wait(t, tester, queue, func() bool { return w.gitBusy == "" && w.git.Branch == "unique-new" })
	for _, source := range []struct{ label, ref, name string }{
		{"feature/older", "refs/heads/feature/older", "qa/from-local"},
		{"origin/remote-only", "refs/remotes/origin/remote-only", "qa/from-remote"},
	} {
		if strings.TrimSpace(git("rev-parse", "HEAD")) != head {
			t.Fatal("source test requires a distinct current HEAD")
		}
		open()
		if err := tester.Click("创建分支…"); err != nil {
			t.Fatal(err)
		}
		if err := tester.Click("新分支基于分支"); err != nil {
			t.Fatal(err)
		}
		if source.ref == "refs/remotes/origin/remote-only" {
			first, found := tester.Find("来源分支 main（当前）")
			if !found {
				t.Fatal("source dropdown did not open")
			}
			tester.Scroll(first.X+first.W/2, first.Y+first.H/2, 0, 500)
			tester.Advance(200 * time.Millisecond)
		}
		if err := tester.Click("来源分支 " + source.label); err != nil {
			t.Fatal(err)
		}
		if a.branchPicker == nil || a.branchPicker.base != source.ref {
			t.Fatal("source selection closed the dialog or selected the wrong ref", source)
		}
		if err := tester.Click("新分支名称"); err != nil {
			t.Fatal(err)
		}
		tester.Type(source.name)
		tester.Key(0, native.KeyEnter)
		p4Wait(t, tester, queue, func() bool { return w.gitBusy == "" && w.git.Branch == source.name })
		if got, want := strings.TrimSpace(git("rev-parse", "HEAD")), strings.TrimSpace(git("rev-parse", source.ref)); got != want || got == head {
			t.Fatal("branch was created from the wrong commit", source, got, want)
		}
		git("switch", "main")
		a.refreshGit(w)
		p4Wait(t, tester, queue, func() bool { return !w.gitLoading && w.git.Branch == "main" })
	}
}

func absFloat32(value float32) float32 {
	return max(value, -value)
}
