package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

func TestGitCommitsNativeScopeCompareAndPreview(t *testing.T) {
	a, q, root, g := gitWorkflowApp(t)
	g("branch", "sr-next")
	g("switch", "-c", "feature")
	os.WriteFile(filepath.Join(root, "README.md"), []byte("feature reminder\n"), 0600)
	g("commit", "-am", "feature reminder\n\nCo-authored-by: Claude <noreply@anthropic.com>")
	feature := strings.TrimSpace(g("rev-parse", "HEAD"))
	g("switch", "sr-next")
	os.WriteFile(filepath.Join(root, "queue.js"), []byte("upstream queue\n"), 0600)
	g("add", ".")
	g("commit", "-m", "sr-next queue")
	upstream := strings.TrimSpace(g("rev-parse", "HEAD"))
	g("switch", "feature")
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1600, 1100)
	tt.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	p4Wait(t, tt, q, func() bool {
		return !w.gitLoading && w.gitFingerprint != "" && w.gitHistoryLoaded && len(w.gitHistory) == 2
	})
	header, ok := tt.Find("切换 COMMITS")
	if !ok {
		t.Fatal("missing COMMITS header")
	}
	tt.Move(header.X+20, header.Y+10)
	if err := tt.Click("显示全部分支"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 3 })
	if !a.preferences.CommitsAllBranches {
		t.Fatal("scope not saved")
	}
	commitRow, _ := tt.Find("提交 " + feature)
	tt.Move(commitRow.X+commitRow.W/2, commitRow.Y+commitRow.H/2)
	tt.Advance(time.Second)
	p4Wait(t, tt, q, func() bool {
		stat := w.gitCommitStats[feature]
		return stat != nil && !stat.loading
	})
	if r, ok := tt.Find("提交时间"); !ok || r.W <= 0 || r.X < 0 || r.X+r.W > 1600 {
		t.Fatal("hover card outside window", r)
	}
	if !tt.HasText("共同作者：Claude") {
		t.Fatal("co-author missing from hover card")
	}
	openMenu := func(hash string) {
		t.Helper()
		if err := tt.RightClick("提交 " + hash); err != nil {
			t.Fatal(err)
		}
	}
	openMenu(upstream)
	for _, label := range []string{"打开更改", "检出…", "合并到 feature…", "将 feature 变基到此提交…", "在此创建标签…", "在此创建分支…", "将 feature 重置到此提交…", "还原此提交…", "拣选此提交", "创建补丁…", "复制更改（补丁）", "从此处比较到 HEAD", "选择用于比较", "在远端打开提交"} {
		if !slices.Contains(tt.Menu(), label) {
			t.Errorf("missing commit menu action %q", label)
		}
	}
	if got := strings.TrimSpace(g("rev-parse", "HEAD")); got != feature {
		t.Fatal("opening menu modified HEAD")
	}
	if err := tt.ChooseMenuItem("从此处比较到 HEAD"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		d := a.currentFile()
		return d != nil && d.loaded && d.gitCommit == upstream+".."+feature
	})
	d := a.currentFile()
	if len(d.gitCommitFiles) != 2 {
		t.Fatal("comparison files", d.gitCommitFiles, d.err)
	}
	a.gitLoadCommit(d, d.gitCommitInfo, "queue.js")
	p4Wait(t, tt, q, func() bool { return !d.loading && d.gitReview != nil && d.gitReview.Entry.Path == "queue.js" })
	if !strings.Contains(d.gitReview.Patch, "-upstream queue") {
		t.Fatal("comparison direction lost", d.gitReview.Patch, d.err)
	}
	if path := os.Getenv("SASA_COMMITS_EVIDENCE"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	restored := &filePage{id: d.id, root: root, path: d.path, kind: "diff", compare: d.compare}
	a.cancelFileRead(d)
	a.files[d.id] = restored
	a.selectFile(restored)
	p4Wait(t, tt, q, func() bool { return restored.loaded && !restored.loading })
	if restored.gitCommit != upstream+".."+feature || len(restored.gitCommitFiles) != 2 ||
		restored.gitCommitInfo.Subject != upstream[:7]+" → "+feature[:7] {
		t.Fatal("comparison restore lost endpoints or title", restored.gitCommitInfo, restored.err)
	}
	openMenu(upstream)
	if err := tt.ChooseMenuItem("选择用于比较"); err != nil {
		t.Fatal(err)
	}
	openMenu(feature)
	if err := tt.ChooseMenuItem("与所选提交比较（" + upstream[:7] + "）"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().gitCommit == upstream+".."+feature })

	// A preview replaces only the previous preview, while an explicitly opened
	// commit stays pinned when another commit is previewed.
	cm := func(hash string) gitview.CommitInfo {
		for _, item := range w.gitHistory {
			if item.Hash == hash {
				return item
			}
		}
		t.Fatal("commit missing", hash)
		return gitview.CommitInfo{}
	}
	a.gitPreviewCommit(w, cm(feature), false)
	first := w.gitPreview
	a.gitPreviewCommit(w, cm(upstream), false)
	if first == "" || a.files[first] != nil {
		t.Fatal("preview not replaced")
	}
	pinned := w.gitPreview
	a.gitPreviewCommit(w, cm(upstream), true)
	a.gitPreviewCommit(w, cm(feature), false)
	if w.gitPreview == "" || a.files[pinned] == nil || pinned == w.gitPreview {
		t.Fatal("pinned tab was replaced")
	}
}

func TestGitCommitsNestedDirectoryCollapseIsPerCommit(t *testing.T) {
	a, _, root, _ := gitWorkflowApp(t)
	w := a.workspace(root)
	a.preferences.CommitsTree = true
	entries := []gitview.Entry{{Path: "src/ui/main.go"}, {Path: "src/ui/panel.go"}, {Path: "README.md"}}
	rows := a.gitCommitFileRows(w, "one", 0, entries)
	if len(rows) != 5 || rows[1].entry.Path != "src/ui" || rows[2].depth != 2 {
		t.Fatal("nested tree lost hierarchy", rows)
	}
	w.gitCommitDirs = map[string]bool{"one:src/ui": true}
	rows = a.gitCommitFileRows(w, "one", 0, entries)
	if len(rows) != 3 {
		t.Fatal("directory did not collapse", rows)
	}
	if other := a.gitCommitFileRows(w, "two", 1, entries); len(other) != 5 {
		t.Fatal("collapse leaked across commits", other)
	}
}

func TestGitCommitsNativeSelectionAndKeyboard(t *testing.T) {
	a, q, root, g := gitWorkflowApp(t)
	for _, title := range []string{"second", "third"} {
		g("commit", "--allow-empty", "-m", title)
	}
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1400, 900)
	tt.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	p4Wait(t, tt, q, func() bool { return !w.gitLoading && w.gitHistoryLoaded && len(w.gitHistory) == 3 })
	first, second, third := w.gitHistory[0], w.gitHistory[1], w.gitHistory[2]
	if err := tt.Click("提交 " + first.Hash); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitPreview != "" && a.currentFile() != nil && a.currentFile().loaded })
	if err := tt.ClickWith(native.Cmd, "提交 "+second.Hash); err != nil {
		t.Fatal(err)
	}
	if !w.gitCommitSelection[first.Hash] || !w.gitCommitSelection[second.Hash] {
		t.Fatal("modifier click lost selection", w.gitCommitSelection)
	}
	if err := tt.ClickWith(native.Shift, "提交 "+third.Hash); err != nil {
		t.Fatal(err)
	}
	if !w.gitCommitSelection[second.Hash] || !w.gitCommitSelection[third.Hash] {
		t.Fatal("shift selection missing", w.gitCommitSelection)
	}
	tt.Click("提交 " + first.Hash)
	tt.Key(0, native.KeyDown)
	p4Wait(t, tt, q, func() bool {
		return a.currentFile() != nil && a.currentFile().gitCommit == second.Hash
	})
	tt.Key(0, native.KeyRight)
	p4Wait(t, tt, q, func() bool { return w.gitCommitTrees[second.Hash].expanded })
	tt.Key(0, native.KeyLeft)
	if w.gitCommitTrees[second.Hash].expanded {
		t.Fatal("left arrow did not collapse")
	}
	tt.Key(0, native.KeyEnter)
	if w.gitPreview != "" {
		t.Fatal("Enter did not pin preview")
	}
}

func TestGitCommitsCompareDoesNotOpenAfterLeavingWorkspace(t *testing.T) {
	a, q, root, _ := gitWorkflowApp(t)
	w := a.workspace(root)
	a.gitCompareCommits(w, "HEAD", "HEAD")
	a.selectWorkspace(t.TempDir())
	select {
	case fn := <-q:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("comparison did not finish")
	}
	if a.currentFile() != nil {
		t.Fatal("late comparison stole focus")
	}
}
