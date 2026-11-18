package ui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/project"
)

func TestWorktreeRenameMenuSaveCancelAndNavigation(t *testing.T) {
	repo := projectFixture(t)
	a := New(stubBackend{}, "test", repo.Path, nil)
	a.repositories = []project.Repository{repo}
	a.State.Draft = "keep draft"
	originalSession := a.session
	tt := native.NewTester(a.view, 1000, 760)
	openRename := func(path string) {
		t.Helper()
		if err := tt.RightClick("打开工作区 " + path); err != nil {
			t.Fatal(err)
		}
		if got := tt.Menu(); !slices.Equal(got, []string{"在Finder中打开", "重命名"}) {
			t.Fatalf("worktree menu = %v", got)
		}
		if err := tt.ChooseMenuItem("重命名"); err != nil {
			t.Fatal(err)
		}
		if !a.worktreeRenameOpen || a.worktreeRenameDraft != a.worktreeName(path) {
			t.Fatal("rename did not open with the current name")
		}
	}
	typeName := func(name string) {
		t.Helper()
		if err := tt.Click("工作区名称"); err != nil {
			t.Fatal(err)
		}
		tt.Key(native.Cmd, native.KeyA)
		tt.Type(name)
	}
	openRename(repo.Path)
	typeName("cancelled name")
	if err := tt.Click("取消重命名"); err != nil {
		t.Fatal(err)
	}
	if a.worktreeRenameOpen || a.worktreeName(repo.Path) != filepath.Base(repo.Path) {
		t.Fatal("cancel changed the title")
	}
	openRename(repo.Path)
	typeName("  主工作区  ")
	tt.Key(0, native.KeyEnter)
	tt.Frame()
	if a.worktreeRenameOpen || a.worktreeName(repo.Path) != "主工作区" || !tt.HasText("主工作区") {
		t.Fatal("Enter did not save the trimmed name", a.worktreeRenameDraft, a.worktreeRenameError)
	}
	for _, w := range repo.Worktrees {
		if !tt.HasText("打开工作区 "+w.Path) || !tt.HasText(w.Branch) {
			t.Fatal("rename hid a worktree or its branch", w.Path)
		}
	}
	openRename(repo.Path)
	typeName("cancel by shortcut")
	tt.Key(native.Cmd, native.KeyW)
	if a.worktreeRenameOpen || !originalSession.open || a.worktreeName(repo.Path) != "主工作区" {
		t.Fatal("close shortcut changed the session or name")
	}
	openRename(repo.Path)
	typeName("cancel by Escape")
	tt.Key(0, native.KeyEscape)
	if a.worktreeRenameOpen || a.worktreeName(repo.Path) != "主工作区" {
		t.Fatal("Escape did not cancel rename")
	}
	if err := tt.Click("打开工作区 " + repo.Path); err != nil {
		t.Fatal(err)
	}
	if a.session != originalSession || a.State.Cwd != repo.Path || a.State.Draft != "keep draft" || len(a.sessions) != 1 {
		t.Fatal("renaming changed worktree navigation or session identity")
	}
	openRename(repo.Path)
	typeName(" ")
	if err := tt.Click("保存名称"); err != nil {
		t.Fatal(err)
	}
	if a.worktreeName(repo.Path) != filepath.Base(repo.Path) || len(a.worktreeNames) != 0 {
		t.Fatal("empty name did not restore folder name")
	}
}

func TestWorktreeRenamePersistsAcrossRefreshAndRestart(t *testing.T) {
	repo := projectFixture(t)
	a := New(stubBackend{}, "test", repo.Path, nil)
	store := imageStore(t, a)
	a.repositories = []project.Repository{repo}
	a.State.Draft = "retained draft"
	for _, worktree := range repo.Worktrees {
		if err := a.saveWorktreeName(worktree.Path, "同名工作区"); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := a.snapshot()
	snapshot.WorktreeNames[repo.Path] = "isolated snapshot"
	if a.worktreeName(repo.Path) != "同名工作区" {
		t.Fatal("snapshot aliases share mutable state")
	}
	queue := make(chan func(), 10)
	a.dispatch = func(fn func()) { queue <- fn }
	a.refreshProjects()
	pumpUpdate(t, queue)
	for _, worktree := range repo.Worktrees {
		if a.worktreeName(worktree.Path) != "同名工作区" {
			t.Fatal("refresh lost name", worktree.Path)
		}
	}
	restored := New(stubBackend{}, "test", repo.Path, nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.worktreeNames, a.worktreeNames) || restored.State.Draft != "retained draft" {
		t.Fatal("restart lost aliases or draft")
	}
	if err := restored.saveWorktreeName(repo.Path, ""); err != nil {
		t.Fatal(err)
	}
	if restored.worktreeName(repo.Worktrees[1].Path) != "同名工作区" {
		t.Fatal("reset changed another worktree with the same name")
	}
	if _, err := os.Stat(repo.Worktrees[1].Path); err != nil {
		t.Fatal("rename changed directory", err)
	}
	after, err := project.Discover(context.Background(), repo.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, repo) {
		t.Fatal("rename changed Git worktree metadata")
	}
	final := New(stubBackend{}, "test", repo.Path, nil)
	if err := final.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if final.worktreeName(repo.Path) != filepath.Base(repo.Path) || final.worktreeName(repo.Worktrees[1].Path) != "同名工作区" {
		t.Fatal("reset did not persist independently")
	}
}

func TestWorktreeRenameFailureKeepsNameAndDialog(t *testing.T) {
	a := projectOrderFixture()
	store := imageStore(t, a)
	a.repositories = []project.Repository{{CommonDir: "/repo/.git", Path: "/repo", Name: "repo", Worktrees: []project.Worktree{{Path: "/repo", Branch: "main"}}}}
	if err := a.saveWorktreeName("/repo", "original name"); err != nil {
		t.Fatal(err)
	}
	if err := a.saveWorktreeName("/repo", "invalid\nname"); err == nil || a.worktreeName("/repo") != "original name" {
		t.Fatal("multiline name was accepted")
	}
	if err := a.saveWorktreeName("/missing", "missing worktree"); err == nil {
		t.Fatal("missing worktree was renamed")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	a.beginWorktreeRename("/repo")
	a.worktreeRenameDraft = "unsaved name"
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.Click("保存名称"); err != nil {
		t.Fatal(err)
	}
	if !a.worktreeRenameOpen || a.worktreeRenameError == "" || a.worktreeRenameDraft != "unsaved name" || a.worktreeName("/repo") != "original name" {
		t.Fatal("failed persistence changed the name or discarded the edit")
	}
}
