package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPullFastForwardAndDivergencePreserveLocalWork(t *testing.T) {
	root, git := actionRepo(t)
	ctx := context.Background()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "--initial-branch=main", remote).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	os.WriteFile(filepath.Join(root, "file"), []byte("base\n"), 0600)
	git("add", ".")
	git("commit", "-m", "base")
	git("remote", "add", "origin", remote)
	if err := RemoteAction(ctx, root, "publish", "origin"); err != nil {
		t.Fatal(err)
	}
	peer := filepath.Join(t.TempDir(), "peer")
	git("clone", remote, peer)
	peerGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", peer, "-c", "user.name=Peer", "-c", "user.email=peer@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(args, err, string(out))
		}
	}
	os.WriteFile(filepath.Join(peer, "remote"), []byte("remote\n"), 0600)
	peerGit("add", ".")
	peerGit("commit", "-m", "remote")
	peerGit("push")
	if err := RemoteAction(ctx, root, "fetch", ""); err != nil {
		t.Fatal(err)
	}
	st, err := Read(ctx, root)
	if err != nil || st.Behind != 1 || st.Ahead != 0 {
		t.Fatal(st, err)
	}
	if err = RemoteAction(ctx, root, "pull", ""); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "local"), []byte("local\n"), 0600)
	git("add", "local")
	git("commit", "-m", "local")
	localHead := git("rev-parse", "HEAD")
	os.WriteFile(filepath.Join(peer, "remote"), []byte("remote next\n"), 0600)
	peerGit("commit", "-am", "remote next")
	peerGit("push")
	if err = RemoteAction(ctx, root, "pull", ""); err == nil {
		t.Fatal("divergence was silently merged or rebased")
	}
	if git("rev-parse", "HEAD") != localHead || InProgress(ctx, root) != "" {
		t.Fatal("failed pull changed local history")
	}
}

func TestNewDeletedAndMissingNewlineHunks(t *testing.T) {
	for _, kind := range []string{"new", "deleted", "no newline"} {
		t.Run(kind, func(t *testing.T) {
			root, git := actionRepo(t)
			ctx := context.Background()
			os.WriteFile(filepath.Join(root, "keep"), []byte("keep\n"), 0600)
			path := filepath.Join(root, "file")
			if kind != "new" {
				os.WriteFile(path, []byte("base"), 0600)
			}
			git("add", ".")
			git("commit", "-m", "base")
			state := ".M"
			switch kind {
			case "new":
				os.WriteFile(path, []byte("new"), 0600)
				state = "??"
			case "deleted":
				os.Remove(path)
				state = ".D"
			default:
				os.WriteFile(path, []byte("changed"), 0600)
			}
			review, err := ReadReview(ctx, root, Entry{Path: "file", XY: state}, false)
			if err != nil || len(review.Hunks) != 1 {
				t.Fatal(review, err)
			}
			if err = ApplyHunk(ctx, root, review, 0); err != nil {
				t.Fatal(err)
			}
			st, _ := Read(ctx, root)
			staged, err := ReadReview(ctx, root, st.Entries[0], true)
			if err != nil {
				t.Fatal(err)
			}
			if err = ApplyHunk(ctx, root, staged, 0); err != nil {
				t.Fatal("reverse hunk", err)
			}
			if git("diff", "--cached") != "" {
				t.Fatal("unstaging left staged changes")
			}
		})
	}
}

func TestRenameUnstagePreservesMovedFile(t *testing.T) {
	root, git := actionRepo(t)
	os.WriteFile(filepath.Join(root, "old name"), []byte("base\n"), 0600)
	git("add", ".")
	git("commit", "-m", "base")
	git("mv", "old name", "new name")
	ctx := context.Background()
	st, _ := Read(ctx, root)
	if len(st.Entries) != 1 || st.Entries[0].Original != "old name" {
		t.Fatal(st)
	}
	review, err := ReadReview(ctx, root, st.Entries[0], true)
	if err != nil || review.Blockable {
		t.Fatal("rename should require whole file operation", review, err)
	}
	if err = Unstage(ctx, root, []string{"old name", "new name"}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, "new name")); string(raw) != "base\n" || git("diff", "--cached") != "" {
		t.Fatal("unstage changed moved file or index")
	}
}

func TestConflictBlocksAndResolutionContinuation(t *testing.T) {
	text := "before\r\n<<<<<<< HEAD\r\n当前\r\n||||||| base\r\n祖先\r\n=======\r\n传入\r\n>>>>>>> other\r\nafter"
	blocks := ConflictBlocks(text)
	if len(blocks) != 1 || blocks[0].Current != "当前\r\n" || blocks[0].Base != "祖先\r\n" || blocks[0].Incoming != "传入\r\n" ||
		text[:blocks[0].Start] != "before\r\n" || text[blocks[0].End:] != "after" {
		t.Fatal(blocks)
	}
	for _, op := range []string{"merge", "rebase", "cherry-pick"} {
		t.Run(op, func(t *testing.T) {
			root, git := actionRepo(t)
			ctx := context.Background()
			path := filepath.Join(root, "file")
			os.WriteFile(path, []byte("base\n"), 0600)
			git("add", ".")
			git("commit", "-m", "base")
			git("switch", "-c", "other")
			os.WriteFile(path, []byte("incoming\n"), 0600)
			git("commit", "-am", "incoming")
			other := strings.TrimSpace(git("rev-parse", "HEAD"))
			git("switch", "main")
			os.WriteFile(path, []byte("current\n"), 0600)
			git("commit", "-am", "current")
			arg := "other"
			if op == "cherry-pick" {
				arg = other
			}
			if out, err := exec.Command("git", "-C", root, op, arg).CombinedOutput(); err == nil {
				t.Fatal("expected conflict", string(out))
			}
			if InProgress(ctx, root) != op {
				t.Fatal("wrong operation", InProgress(ctx, root))
			}
			if err := MarkResolved(ctx, root, "file"); err == nil {
				t.Fatal("unresolved markers accepted")
			}
			os.WriteFile(path, []byte("<<<<<<< incomplete\nmanual\n"), 0600)
			if err := MarkResolved(ctx, root, "file"); err == nil {
				t.Fatal("incomplete markers accepted")
			}
			os.WriteFile(path, []byte("resolved\n"), 0600)
			if err := MarkResolved(ctx, root, "file"); err != nil {
				t.Fatal(err)
			}
			if err := ContinueOperation(ctx, root, op, false); err != nil {
				t.Fatal(err)
			}
			if InProgress(ctx, root) != "" || git("show", "HEAD:file") != "resolved\n" {
				t.Fatal("resolution not committed")
			}
		})
	}
}

func TestCancelGitHookReturnsWithoutWaitingForDescendant(t *testing.T) {
	root, git := actionRepo(t)
	os.WriteFile(filepath.Join(root, "file"), []byte("base\n"), 0600)
	git("add", ".")
	hooks := filepath.Join(t.TempDir(), "hooks")
	os.MkdirAll(hooks, 0700)
	os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nsleep 30 &\nwait\n"), 0700)
	git("config", "core.hooksPath", hooks)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Commit(ctx, root, "should cancel"); err == nil || ctx.Err() == nil {
		t.Fatal("hook cancellation not reported", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancellation waited for descendant")
	}
	st, err := Read(context.Background(), root)
	if err != nil || len(st.Entries) != 1 || !st.Entries[0].Staged() {
		t.Fatal("canceled commit lost staged content", st, err)
	}
}
