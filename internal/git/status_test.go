package git

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusAndDiffReadIndexAndDiskSeparately(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %v %s", args, e, out)
		}
	}
	run("init", "-b", "main")
	path := filepath.Join(root, "带 空格.txt")
	os.WriteFile(path, []byte("base\n"), 0600)
	run("add", "--", "带 空格.txt")
	run("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-m", "base")
	os.WriteFile(path, []byte("staged\n"), 0600)
	run("add", "--", "带 空格.txt")
	os.WriteFile(path, []byte("disk\n"), 0600)
	os.WriteFile(filepath.Join(root, "-new\nfile"), []byte("new\n"), 0600)
	index := filepath.Join(root, ".git", "index")
	before, _ := os.ReadFile(index)
	state, e := Read(context.Background(), root)
	if e != nil || len(state.Entries) != 2 {
		t.Fatal(state, e)
	}
	var entry Entry
	for _, v := range state.Entries {
		if v.Path == "带 空格.txt" {
			entry = v
		}
	}
	if !entry.Staged() || !entry.Unstaged() {
		t.Fatal(entry)
	}
	staged, e := Diff(context.Background(), root, entry, true)
	if e != nil || !strings.Contains(staged, "+staged") || strings.Contains(staged, "+disk") {
		t.Fatal(staged, e)
	}
	unstaged, e := Diff(context.Background(), root, entry, false)
	if e != nil || !strings.Contains(unstaged, "-staged") || !strings.Contains(unstaged, "+disk") {
		t.Fatal(unstaged, e)
	}
	after, _ := os.ReadFile(index)
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("read-only UI mutated index")
	}
}
func TestParseRenameAndConflict(t *testing.T) {
	s, e := Parse([]byte("# branch.head main\x002 R. N... 100644 100644 100644 abc def R100 new name\x00old\nname\x00u UU N... 100644 100644 100644 100644 a b c conflict\x00"))
	if e != nil || len(s.Entries) != 2 || s.Entries[0].Original != "old\nname" || !s.Entries[1].Conflict {
		t.Fatal(s, e)
	}
}

func TestConflictPatchAndWorktreeMetadataAreReadOnly(t *testing.T) {
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	command := func(dir string, args ...string) ([]byte, error) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		return cmd.CombinedOutput()
	}
	must := func(dir string, args ...string) {
		t.Helper()
		if out, err := command(dir, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
	}
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "conflict.txt"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	must(root, "init", "-b", "main")
	write("base\n")
	must(root, "add", ".")
	must(root, "commit", "-m", "base")
	must(root, "checkout", "-b", "other")
	write("other side\n")
	must(root, "commit", "-am", "other")
	must(root, "checkout", "main")
	write("main side\n")
	must(root, "commit", "-am", "main")
	if _, err := command(root, "merge", "other"); err == nil {
		t.Fatal("expected genuine merge conflict")
	}
	index, _ := os.ReadFile(filepath.Join(root, ".git", "index"))
	head, _ := os.ReadFile(filepath.Join(root, ".git", "HEAD"))
	s, err := Read(context.Background(), root)
	if err != nil || len(s.Entries) != 1 || !s.Entries[0].Conflict {
		t.Fatal(s, err)
	}
	patch, err := Diff(context.Background(), root, s.Entries[0], false)
	if err != nil || !strings.Contains(patch, "<<<<<<<") || !strings.Contains(patch, "main side") || !strings.Contains(patch, "other side") {
		t.Fatal(patch, err)
	}
	afterIndex, _ := os.ReadFile(filepath.Join(root, ".git", "index"))
	afterHead, _ := os.ReadFile(filepath.Join(root, ".git", "HEAD"))
	if sha256.Sum256(index) != sha256.Sum256(afterIndex) || string(head) != string(afterHead) {
		t.Fatal("conflict inspection changed Git metadata")
	}
	worktree := filepath.Join(t.TempDir(), "linked")
	must(root, "worktree", "add", "-b", "linked", worktree)
	dirs, err := MetadataDirectories(context.Background(), worktree)
	if err != nil {
		t.Fatal(err)
	}
	var local, shared bool
	for _, dir := range dirs {
		local = local || dir == filepath.Join(root, ".git", "worktrees", "linked")
		shared = shared || dir == filepath.Join(root, ".git")
	}
	if !local || !shared {
		t.Fatalf("missing local index/HEAD or shared refs: %v", dirs)
	}
}

func TestUnbornRenameModeDeleteAndWorktreeIsolation(t *testing.T) {
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run(root, "init", "-b", "main")
	os.WriteFile(filepath.Join(root, "script.sh"), []byte("echo fixture\n"), 0644)
	run(root, "add", "--", "script.sh")
	s, err := Read(context.Background(), root)
	if err != nil || len(s.Entries) != 1 || s.Head != "(initial)" {
		t.Fatal("unborn status", s, err)
	}
	patch, err := Diff(context.Background(), root, s.Entries[0], true)
	if err != nil || !strings.Contains(patch, "+echo fixture") {
		t.Fatal("unborn diff", patch, err)
	}
	run(root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-m", "fixture")
	worktree := filepath.Join(t.TempDir(), "other")
	run(root, "-c", "core.hooksPath=/dev/null", "worktree", "add", "-b", "other", worktree)
	run(root, "mv", "--", "script.sh", "renamed.sh")
	s, err = Read(context.Background(), root)
	if err != nil || len(s.Entries) != 1 || s.Entries[0].Original != "script.sh" {
		t.Fatal("rename status", s, err)
	}
	patch, err = Diff(context.Background(), root, s.Entries[0], true)
	if err != nil || !strings.Contains(patch, "rename from script.sh") || !strings.Contains(patch, "rename to renamed.sh") {
		t.Fatal("rename diff", patch, err)
	}
	os.Chmod(filepath.Join(worktree, "script.sh"), 0755)
	s, err = Read(context.Background(), worktree)
	if err != nil || s.Branch != "other" || len(s.Entries) != 1 {
		t.Fatal("worktree status", s, err)
	}
	patch, err = Diff(context.Background(), worktree, s.Entries[0], false)
	if err != nil || !strings.Contains(patch, "old mode 100644") || !strings.Contains(patch, "new mode 100755") {
		t.Fatal("mode diff", patch, err)
	}
	os.Remove(filepath.Join(worktree, "script.sh"))
	s, err = Read(context.Background(), worktree)
	if err != nil || len(s.Entries) != 1 {
		t.Fatal("deleted status", s, err)
	}
	patch, err = Diff(context.Background(), worktree, s.Entries[0], false)
	if err != nil || !strings.Contains(patch, "deleted file mode") || !strings.Contains(patch, "-echo fixture") {
		t.Fatal("delete diff", patch, err)
	}
	s, err = Read(context.Background(), root)
	if err != nil || len(s.Entries) != 1 || s.Entries[0].Original != "script.sh" {
		t.Fatal("worktree change leaked to main", s, err)
	}
}
