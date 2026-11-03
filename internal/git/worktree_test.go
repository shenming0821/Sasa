package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorktreeCreatePreservesSourceAndUsesSelectedCommit(t *testing.T) {
	root, git := actionRepo(t)
	ctx := context.Background()
	file := filepath.Join(root, "tracked.txt")
	os.WriteFile(file, []byte("base"), 0600)
	git("add", "tracked.txt")
	git("commit", "-m", "base")
	base := strings.TrimSpace(git("rev-parse", "HEAD"))
	git("update-ref", "refs/remotes/origin/start", base)
	os.WriteFile(file, []byte("later"), 0600)
	git("add", "tracked.txt")
	git("commit", "-m", "later")
	os.WriteFile(file, []byte("staged"), 0600)
	git("add", "tracked.txt")
	os.WriteFile(file, []byte("working"), 0600)
	os.WriteFile(filepath.Join(root, ".env"), []byte("local"), 0600)
	before := git("status", "--porcelain=v1", "-z") + git("diff", "--cached") + git("diff") + git("rev-parse", "HEAD")
	common := strings.TrimSpace(git("rev-parse", "--absolute-git-dir"))
	target := WorktreeDirectory(t.TempDir(), common, "自定义 空格")
	result, err := CreateWorktree(ctx, root, "refs/remotes/origin/start", "feature/new", target)
	if err != nil || !result.Created || !result.BranchExists || !result.DirectoryExists {
		t.Fatal(result, err)
	}
	content, err := os.ReadFile(filepath.Join(target, "tracked.txt"))
	if err != nil || string(content) != "base" {
		t.Fatal("did not use selected commit", string(content), err)
	}
	if _, err := os.Stat(filepath.Join(target, ".env")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("untracked file copied", err)
	}
	after := git("status", "--porcelain=v1", "-z") + git("diff", "--cached") + git("diff") + git("rev-parse", "HEAD")
	if before != after || strings.TrimSpace(git("branch", "--show-current")) != "main" {
		t.Fatal("source worktree or index changed")
	}
	if WorktreeDirectory("/data", common, "same") == WorktreeDirectory("/data", common+"other", "same") {
		t.Fatal("repositories share a directory")
	}
}

func TestWorktreeValidationAndSources(t *testing.T) {
	root, git := actionRepo(t)
	ctx := context.Background()
	if _, _, err := WorktreeSources(ctx, root); err == nil {
		t.Fatal("accepted unborn repository")
	}
	git("commit", "--allow-empty", "-m", "base")
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	sources, selected, err := WorktreeSources(ctx, root)
	if err != nil || selected != "refs/heads/main" || len(sources) != 2 {
		t.Fatal(sources, selected, err)
	}
	git("checkout", "--detach")
	sources, selected, err = WorktreeSources(ctx, root)
	if err != nil || selected != "HEAD" || !strings.HasPrefix(sources[0].Label, "当前提交 ") {
		t.Fatal(sources, selected, err)
	}
	for _, invalid := range []string{"", ".", "..", "a/b", "a\\b", "a:b", "a\nb"} {
		if ValidateWorktreeFolder(invalid) == nil {
			t.Fatal("accepted invalid folder", invalid)
		}
	}
	for _, branch := range []string{"", "main", "HEAD", "bad..branch", "-option", "@{-1}"} {
		_, err := CreateWorktree(ctx, root, "HEAD", branch, filepath.Join(t.TempDir(), "target"))
		var field *WorktreeFieldError
		if !errors.As(err, &field) || field.Field != "branch" {
			t.Fatal(branch, err)
		}
	}
	_, err = CreateWorktree(ctx, root, "HEAD", "new", root)
	var field *WorktreeFieldError
	if !errors.As(err, &field) || field.Field != "folder" {
		t.Fatal("existing directory accepted", err)
	}
	_, err = CreateWorktree(ctx, root, "refs/heads/missing", "new", filepath.Join(t.TempDir(), "target"))
	if !errors.As(err, &field) || field.Field != "base" {
		t.Fatal("missing source accepted", err)
	}
}

func TestWorktreeCommandsIgnoreInheritedRepositoryOverrides(t *testing.T) {
	root, git := actionRepo(t)
	git("commit", "--allow-empty", "-m", "base")
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE"} {
		t.Setenv(name, filepath.Join(t.TempDir(), "invalid"))
	}
	sources, selected, err := WorktreeSources(context.Background(), root)
	if err != nil || selected != "refs/heads/main" || len(sources) != 1 {
		t.Fatal("inherited Git environment redirected source", sources, selected, err)
	}
	result, err := CreateWorktree(context.Background(), root, selected, "isolated", filepath.Join(t.TempDir(), "isolated"))
	if err != nil || !result.Created {
		t.Fatal("inherited Git environment redirected mutation", result, err)
	}
}

func TestWorktreeInterruptedCheckoutRetainsAndReportsActualState(t *testing.T) {
	root, git := actionRepo(t)
	git("commit", "--allow-empty", "-m", "base")
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte("#!/bin/sh\nwhile :; do sleep 1; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	git("config", "core.hooksPath", hooks)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	target := filepath.Join(t.TempDir(), "interrupted")
	result, err := CreateWorktree(ctx, root, "HEAD", "interrupted", target)
	if err == nil || result.Created || !result.DirectoryExists || !result.BranchExists || !strings.Contains(err.Error(), target) {
		t.Fatal("interruption lost actual state", result, err)
	}
	if !strings.Contains(git("worktree", "list", "--porcelain"), "interrupted") {
		t.Fatal("interrupted worktree silently removed")
	}
}
