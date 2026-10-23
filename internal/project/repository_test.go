package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, path string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
	// Hermetic identity and hooks/config, independent of user credentials.
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, raw)
	}
	return string(raw)
}

func TestDiscoverLinkedWorktreesAndIdentity(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "repo 空格")
	if err := os.Mkdir(main, 0700); err != nil {
		t.Fatal(err)
	}
	runGit(t, main, "init", "-b", "main")
	runGit(t, main, "-c", "core.hooksPath=/dev/null", "commit", "--allow-empty", "-m", "initial")
	linked := filepath.Join(root, "feature worktree")
	detached := filepath.Join(root, "detached\nworkspace")
	runGit(t, main, "worktree", "add", "-b", "feature/input", linked)
	runGit(t, main, "worktree", "add", "--detach", detached)
	runGit(t, main, "worktree", "lock", "--reason", "fixture", linked)
	userFile := filepath.Join(main, "user-change.txt")
	if err := os.WriteFile(userFile, []byte("staged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit(t, main, "add", "--", "user-change.txt")
	if err := os.WriteFile(userFile, []byte("unstaged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := runGit(t, main, "status", "--porcelain=v1")
	repo, err := Discover(context.Background(), linked)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Path != Canonical(main) || len(repo.Worktrees) != 3 {
		t.Fatalf("wrong repository: %+v", repo)
	}
	if !repo.Worktrees[0].Main || repo.Worktrees[0].Branch != "main" {
		t.Fatal("primary worktree missing")
	}
	var linkedTree, detachedTree Worktree
	for _, w := range repo.Worktrees {
		if w.Path == Canonical(linked) {
			linkedTree = w
		}
		if w.Path == Canonical(detached) {
			detachedTree = w
		}
	}
	if linkedTree.Branch != "feature/input" || !linkedTree.Locked || !linkedTree.Available() {
		t.Fatal("locked worktree incorrectly disabled")
	}
	if !detachedTree.Detached || detachedTree.Path != Canonical(detached) || !strings.HasPrefix(detachedTree.Label(), "detached") {
		t.Fatal("NUL-delimited path or detached branch lost")
	}
	fromMain, err := Discover(context.Background(), main)
	if err != nil || fromMain.CommonDir != repo.CommonDir {
		t.Fatal("linked import did not deduplicate repository identity")
	}
	symlink := filepath.Join(root, "alias")
	if err := os.Symlink(main, symlink); err != nil {
		t.Fatal(err)
	}
	alias, err := Discover(context.Background(), symlink)
	if err != nil || alias.CommonDir != repo.CommonDir {
		t.Fatal("symlink import changed identity")
	}
	if got := runGit(t, main, "status", "--porcelain=v1"); got != before {
		t.Fatal("discovery changed worktree state")
	}
	runGit(t, main, "worktree", "unlock", linked)
	linkedPath := Canonical(linked)
	if err := os.RemoveAll(linked); err != nil {
		t.Fatal(err)
	}
	updated, err := Discover(context.Background(), main)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range updated.Worktrees {
		if w.Path == linkedPath {
			linkedTree = w
		}
	}
	if linkedTree.Available() || !linkedTree.Missing {
		t.Fatal("missing worktree presented as usable")
	}
}

func TestDiscoverBareInvalidAndCancelled(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "bare.git")
	if err := os.Mkdir(bare, 0700); err != nil {
		t.Fatal(err)
	}
	runGit(t, bare, "init", "--bare")
	repo, err := Discover(context.Background(), bare)
	if err != nil || len(repo.Worktrees) != 1 || !repo.Worktrees[0].Bare || repo.Worktrees[0].Available() {
		t.Fatalf("bare repository misrepresented: %+v %v", repo, err)
	}
	if _, err := Discover(context.Background(), t.TempDir()); err == nil {
		t.Fatal("non-Git folder imported as repository")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Discover(ctx, bare); err == nil {
		t.Fatal("cancelled discovery succeeded")
	}
	if _, err := parseWorktrees(""); err == nil {
		t.Fatal("empty scan accepted as authoritative")
	}
}
