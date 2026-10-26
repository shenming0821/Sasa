package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func actionRepo(t *testing.T) (string, func(...string) string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	git("init", "-b", "main")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.invalid")
	git("config", "core.hooksPath", "/dev/null")
	git("config", "commit.gpgsign", "false")
	return root, git
}

func TestStageUnstageCommitAndHistoryKeepDiskAndOtherChanges(t *testing.T) {
	root, git := actionRepo(t)
	ctx := context.Background()
	path := "-带 空格\nfile.txt"
	os.WriteFile(filepath.Join(root, path), []byte("base\n"), 0600)
	if err := Stage(ctx, root, []string{path}); err != nil {
		t.Fatal(err)
	}
	if err := Unstage(ctx, root, []string{path}); err != nil {
		t.Fatal("unborn unstage", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(root, path)); string(raw) != "base\n" {
		t.Fatal("unstage changed disk")
	}
	Stage(ctx, root, []string{path})
	hash, err := Commit(ctx, root, "feat: 第一项\n\nbody")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, path), []byte("selected\n"), 0600)
	os.WriteFile(filepath.Join(root, "untouched"), []byte("other\n"), 0600)
	Stage(ctx, root, []string{path})
	os.WriteFile(filepath.Join(root, path), []byte("still on disk\n"), 0600)
	if _, err := Commit(ctx, root, "fix: staged only"); err != nil {
		t.Fatal(err)
	}
	if got := git("show", "HEAD:"+path); got != "selected\n" {
		t.Fatal("commit included unstaged bytes", got)
	}
	st, _ := Read(ctx, root)
	if len(st.Entries) != 2 {
		t.Fatal("commit changed other work", st)
	}
	log, err := History(ctx, root, 1, 1)
	if err != nil || len(log) != 1 || log[0].Hash != hash || log[0].Subject != "feat: 第一项" {
		t.Fatal("paged history", log, err)
	}
	files, err := CommitFiles(ctx, root, hash)
	if err != nil || len(files) != 1 || files[0].Path != path {
		t.Fatal("initial commit file names", files, err)
	}
	patch, err := CommitDiff(ctx, root, hash, files[0])
	if err != nil || !strings.Contains(patch, "+base") {
		t.Fatal("initial commit patch", patch, err)
	}
	for _, bad := range []string{"../outside", "/tmp/absolute", ".git/config", ".", ""} {
		if err := Stage(ctx, root, []string{bad}); err == nil {
			t.Fatal("accepted invalid path", bad)
		}
	}
}

func TestHunkStageUnstageAndStaleReview(t *testing.T) {
	root, git := actionRepo(t)
	ctx := context.Background()
	var original []string
	for i := range 30 {
		original = append(original, fmt.Sprintf("line %02d\n", i))
	}
	base := strings.Join(original, "")
	path := "example.txt"
	os.WriteFile(filepath.Join(root, path), []byte(base), 0600)
	git("add", ".")
	git("commit", "-m", "base")
	work := strings.Replace(strings.Replace(base, "line 02", "changed 02", 1), "line 25", "changed 25", 1)
	os.WriteFile(filepath.Join(root, path), []byte(work), 0600)
	review, err := ReadReview(ctx, root, Entry{Path: path, XY: ".M"}, false)
	if err != nil || len(review.Hunks) != 2 || review.Added != 2 || review.Deleted != 2 {
		t.Fatal(review, err)
	}
	if err = ApplyHunk(ctx, root, review, 0); err != nil {
		t.Fatal(err)
	}
	index := git("show", ":"+path)
	if !strings.Contains(index, "changed 02") || strings.Contains(index, "changed 25") {
		t.Fatal("block stage included other hunk", index)
	}
	if err = ApplyHunk(ctx, root, review, 1); err == nil {
		t.Fatal("stale index accepted")
	}
	staged, err := ReadReview(ctx, root, Entry{Path: path, XY: "MM"}, true)
	if err != nil || len(staged.Hunks) != 1 {
		t.Fatal(staged, err)
	}
	if err = ApplyHunk(ctx, root, staged, 0); err != nil || git("show", ":"+path) != base {
		t.Fatal("block unstage", err)
	}
	review, _ = ReadReview(ctx, root, Entry{Path: path, XY: ".M"}, false)
	os.WriteFile(filepath.Join(root, path), []byte(work+"late\n"), 0600)
	if err = ApplyHunk(ctx, root, review, 0); err == nil {
		t.Fatal("stale disk accepted")
	}
}

func TestRemoteActionsAndWorktreeBranches(t *testing.T) {
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
	if err := RemoteAction(ctx, root, "fetch", ""); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "file"), []byte("next\n"), 0600)
	git("commit", "-am", "next")
	st, err := Read(ctx, root)
	if err != nil || st.Ahead != 1 || st.Behind != 0 {
		t.Fatal("ahead count", st, err)
	}
	if err = RemoteAction(ctx, root, "push", ""); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	git("worktree", "add", "-b", "other", other)
	branches, err := Branches(ctx, root)
	if err != nil || len(branches) != 2 || branches[1].Worktree == "" {
		t.Fatal(branches, err)
	}
	if err = Switch(ctx, root, "other", false); err == nil {
		t.Fatal("switched to branch held by another worktree")
	}
	if err = Switch(ctx, root, "new", true); err != nil {
		t.Fatal(err)
	}
	if err = Stage(ctx, other, []string{"../file"}); err == nil {
		t.Fatal("escaped worktree")
	}
	if err = RemoteAction(ctx, root, "publish", "--all"); err == nil {
		t.Fatal("remote option injection")
	}
}

func TestDiscardAndConflictOperations(t *testing.T) {
	root, git := actionRepo(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(root, "file"), []byte("base\n"), 0600)
	git("add", ".")
	git("commit", "-m", "base")
	os.WriteFile(filepath.Join(root, "file"), []byte("disk\n"), 0600)
	os.WriteFile(filepath.Join(root, "new"), []byte("recover me\n"), 0600)
	recovery := filepath.Join(t.TempDir(), "recovery")
	if err := Discard(ctx, root, []Entry{{Path: "file", XY: ".M"}, {Path: "new", XY: "??"}}, recovery); err != nil {
		t.Fatal(err)
	}
	dirs, _ := os.ReadDir(recovery)
	data, _ := os.ReadFile(filepath.Join(recovery, dirs[0].Name(), "content"))
	if string(data) != "recover me\n" {
		t.Fatal("untracked data lost")
	}
	base, _ := os.ReadFile(filepath.Join(root, "file"))
	if string(base) != "base\n" {
		t.Fatal("tracked discard failed")
	}
	git("switch", "-c", "other")
	os.WriteFile(filepath.Join(root, "file"), []byte("incoming\n"), 0600)
	git("commit", "-am", "incoming")
	git("switch", "main")
	os.WriteFile(filepath.Join(root, "file"), []byte("current\n"), 0600)
	git("commit", "-am", "current")
	if out, err := exec.Command("git", "-C", root, "merge", "other").CombinedOutput(); err == nil {
		t.Fatal("fixture did not conflict", string(out))
	}
	sides, err := ConflictText(ctx, root, "file")
	if err != nil || sides.Base != "base\n" || sides.Current != "current\n" || sides.Incoming != "incoming\n" {
		t.Fatal(sides, err)
	}
	if InProgress(ctx, root) != "merge" {
		t.Fatal("missing merge operation")
	}
	if err = ContinueOperation(ctx, root, "rebase", true); err == nil {
		t.Fatal("accepted incorrect operation")
	}
	if err = ContinueOperation(ctx, root, "merge", true); err != nil {
		t.Fatal(err)
	}
	if InProgress(ctx, root) != "" {
		t.Fatal("merge not aborted")
	}
}
