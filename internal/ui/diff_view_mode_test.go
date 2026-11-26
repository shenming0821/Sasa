package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
	gitview "github.com/shenming0821/Sasa/internal/git"
	"github.com/shenming0821/Sasa/internal/storage"
)

func TestDiffViewModeDefaultsAndNormalization(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	if a.preferences.DiffViewMode != diffViewModeInline || a.diffViewSplit() {
		t.Fatalf("unexpected default diff mode: %q", a.preferences.DiffViewMode)
	}
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}

	for _, raw := range []string{"", "unknown", diffViewModeInline} {
		snapshot, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		prefs := defaultPreferences()
		prefs.DiffViewMode = raw
		snapshot.Preferences = &prefs
		if err := store.Save(snapshot); err != nil {
			t.Fatal(err)
		}
		restored := New(stubBackend{}, "test", "/workspace", nil)
		if err := restored.AttachStore(store); err != nil {
			t.Fatal(err)
		}
		if restored.preferences.DiffViewMode != diffViewModeInline || restored.diffViewSplit() {
			t.Fatalf("raw %q normalized to %q, want %q", raw, restored.preferences.DiffViewMode, diffViewModeInline)
		}
	}

	snapshot, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	prefs := defaultPreferences()
	prefs.DiffViewMode = diffViewModeSplit
	snapshot.Preferences = &prefs
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "/workspace", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.preferences.DiffViewMode != diffViewModeSplit || !restored.diffViewSplit() {
		t.Fatalf("restored diff mode = %q, want %q", restored.preferences.DiffViewMode, diffViewModeSplit)
	}
}

func TestDiffViewModeRemembersAcrossDiffTypesAndRestart(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	a.State.Cwd = root
	a.historyOpen, a.rightTab = true, "git"

	readme := filepath.Join(root, "README.md")
	mainFile := filepath.Join(root, "internal/main.go")
	if err := os.WriteFile(readme, []byte("updated readme\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainFile, []byte("package main\n// changed\n"), 0600); err != nil {
		t.Fatal(err)
	}

	w := a.workspace(root)
	tt := native.NewTester(a.view, 1400, 820)
	tt.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	p4Wait(t, tt, q, func() bool { return !w.gitLoading && len(gitEntries(w, "更改")) == 2 })

	if err := tt.Click("Git 更改 README.md"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		return a.currentFile() != nil && a.currentFile().loaded && a.currentFile().gitReview != nil
	})
	firstDiff := a.currentFile()
	if firstDiff.gitSplit {
		t.Fatal("first diff should start in inline mode")
	}
	tt.Frame()
	if _, found := tt.Find("差异 原版本"); found {
		t.Fatal("inline mode should not render split panes")
	}

	if err := tt.Click("并排"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !firstDiff.gitSplit || a.preferences.DiffViewMode != diffViewModeSplit {
		t.Fatalf("split toggle did not update state: tab=%v pref=%q", firstDiff.gitSplit, a.preferences.DiffViewMode)
	}
	if _, found := tt.Find("差异 原版本"); !found {
		t.Fatal("split mode did not render left pane")
	}
	if _, found := tt.Find("差异 新版本"); !found {
		t.Fatal("split mode did not render right pane")
	}
	saved, err := store.Load()
	if err != nil || saved.Preferences == nil || saved.Preferences.DiffViewMode != diffViewModeSplit {
		t.Fatalf("split mode was not persisted: %+v %v", saved.Preferences, err)
	}

	// Narrow window falls back to inline visually without overwriting the remembered split mode.
	narrow := native.NewTester(a.view, 500, 820)
	narrow.Frame()
	if _, found := narrow.Find("差异 原版本"); found {
		t.Fatal("narrow viewport should fall back to inline view")
	}
	if !firstDiff.gitSplit || a.preferences.DiffViewMode != diffViewModeSplit {
		t.Fatalf("narrow viewport overwrote remembered split mode: tab=%v pref=%q", firstDiff.gitSplit, a.preferences.DiffViewMode)
	}

	// New Git diff defaults to remembered split mode.
	if err := tt.Click("Git 更改 internal/main.go"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile() != firstDiff && a.currentFile().loaded })
	secondDiff := a.currentFile()
	if !secondDiff.gitSplit {
		t.Fatal("new Git diff did not default to remembered split mode")
	}

	// Commit diff defaults to remembered split mode.
	hash := strings.TrimSpace(git("rev-parse", "HEAD"))
	a.gitOpenCommit(w, gitview.CommitInfo{Hash: hash, Subject: "feat: baseline"})
	p4Wait(t, tt, q, func() bool {
		return a.currentFile() != nil && a.currentFile().gitCommit == hash && a.currentFile().loaded
	})
	if !a.currentFile().gitSplit {
		t.Fatal("commit diff did not default to remembered split mode")
	}

	// Turn review diff defaults to remembered split mode.
	summary := app.ChangeSummary{
		Files: []app.ChangedFile{{
			Path:  readme,
			Patch: "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n",
			Exact: true,
		}},
	}
	a.openTurnReview([]*app.Message{{ID: "turn-1"}}, summary, 0)
	if !a.currentFile().gitSplit {
		t.Fatal("turn review diff did not default to remembered split mode")
	}

	// External disk comparison diff defaults to remembered split mode and exposes layout toggle.
	a.openFile(root, readme)
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().kind == "file" && a.currentFile().loaded })
	source := a.currentFile()
	source.buffer.Set("local edit\n")
	source.observe()
	a.showDiskComparison(source)
	p4Wait(t, tt, q, func() bool {
		return a.currentFile() != nil && a.currentFile().compare == "外部更改" && a.currentFile().loaded
	})
	externalDiff := a.currentFile()
	if !externalDiff.gitSplit {
		t.Fatal("external comparison diff did not default to remembered split mode")
	}
	tt.Frame()
	if _, found := tt.Find("差异 本地编辑"); !found {
		t.Fatal("external comparison split view missing local pane")
	}
	if _, found := tt.Find("差异 磁盘版本"); !found {
		t.Fatal("external comparison split view missing disk pane")
	}

	// Persist current tabs and restart: restored diff tabs and new diff tabs remember split mode.
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", root, nil)
	restoredQueue := make(chan func(), 128)
	restored.dispatch = func(fn func()) { restoredQueue <- fn }
	t.Cleanup(restored.closeWorkspaceResources)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.preferences.DiffViewMode != diffViewModeSplit || !restored.diffViewSplit() {
		t.Fatalf("restarted app lost split preference: %q", restored.preferences.DiffViewMode)
	}
	restoredFirst := restored.files[firstDiff.id]
	if restoredFirst == nil || !restoredFirst.gitSplit {
		t.Fatalf("restored diff tab did not keep split mode: %+v", restoredFirst)
	}
	restored.selectFile(restoredFirst)
	restoredTester := native.NewTester(restored.view, 1400, 820)
	restoredTester.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	p4Wait(t, restoredTester, restoredQueue, func() bool { return restoredFirst.loaded && restoredFirst.gitReview != nil })
	restoredTester.Frame()
	if _, found := restoredTester.Find("差异 原版本"); !found {
		t.Fatal("restored diff tab did not render split view")
	}

	// Switching back to inline persists and applies to subsequent diff tabs and restarts.
	if err := restoredTester.Click("行内"); err != nil {
		t.Fatal(err)
	}
	restoredTester.Frame()
	if restoredFirst.gitSplit || restored.preferences.DiffViewMode != diffViewModeInline {
		t.Fatalf("inline toggle did not update state: tab=%v pref=%q", restoredFirst.gitSplit, restored.preferences.DiffViewMode)
	}
	if _, found := restoredTester.Find("差异 原版本"); found {
		t.Fatal("inline toggle kept split pane visible")
	}
	restored.openGitDiff(restored.workspace(root), gitview.Entry{Path: "README.md", XY: " M"}, true)
	if restored.currentFile().gitSplit {
		t.Fatal("newly opened diff after switching to inline should default to inline")
	}
	restartedInline := New(stubBackend{}, "test", root, nil)
	if err := restartedInline.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restartedInline.preferences.DiffViewMode != diffViewModeInline || restartedInline.diffViewSplit() {
		t.Fatalf("restarted app did not preserve inline mode: %q", restartedInline.preferences.DiffViewMode)
	}
	if restoredTab := restartedInline.files[firstDiff.id]; restoredTab == nil || restoredTab.gitSplit {
		t.Fatalf("restored diff tab after inline switch should be inline: %+v", restoredTab)
	}
}
