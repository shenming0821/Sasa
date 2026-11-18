package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitsBranchGraphAndComparisonDirection(t *testing.T) {
	root, g := actionRepo(t)
	ctx := context.Background()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("reception.js", "base\n")
	g("add", ".")
	g("commit", "-m", "base")
	g("branch", "sr-next")
	g("switch", "-c", "feature")
	write("reception.js", "feature\n")
	g("commit", "-am", "feature\n\nCo-authored-by: Helper <helper@example.invalid>")
	feature := strings.TrimSpace(g("rev-parse", "HEAD"))
	g("switch", "sr-next")
	write("queue.js", "upstream\n")
	g("add", ".")
	g("commit", "-m", "upstream")
	upstream := strings.TrimSpace(g("rev-parse", "HEAD"))
	g("tag", "v1")
	g("switch", "feature")
	current, err := ReadHistory(ctx, root, 50, false)
	if err != nil || len(current) != 2 {
		t.Fatal(current, err)
	}
	all, err := ReadHistory(ctx, root, 50, true)
	if err != nil || len(all) != 3 {
		t.Fatal(all, err)
	}
	found := false
	for _, cm := range all {
		if cm.Hash == feature {
			if len(cm.Parents) != 1 || len(cm.CoAuthors) != 1 || cm.Email == "" {
				t.Fatal("missing metadata", cm)
			}
		}
		if cm.Hash == upstream {
			found = len(cm.Refs) == 2
		}
	}
	if !found {
		t.Fatal("branch/tag refs missing", all)
	}
	files, err := CompareFiles(ctx, root, upstream, feature)
	if err != nil || len(files) != 2 {
		t.Fatal(files, err)
	}
	for _, e := range files {
		patch, err := CompareDiff(ctx, root, upstream, feature, e)
		if err != nil {
			t.Fatal(err)
		}
		if e.Path == "queue.js" && (!strings.HasPrefix(e.XY, "D") || !strings.Contains(patch, "-upstream")) {
			t.Fatal("wrong comparison direction", e, patch)
		}
	}
	// The two immutable IDs keep a comparison stable when the branch advances.
	write("new.txt", "new\n")
	g("add", ".")
	g("commit", "-m", "later")
	same, err := CommitFiles(ctx, root, upstream+".."+feature)
	if err != nil || len(same) != 2 {
		t.Fatal(same, err)
	}
	empty, err := CompareFiles(ctx, root, feature, feature)
	if err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
}
func TestCommitsOperationsAndPatch(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"merge", "rebase", "cherry-pick", "revert", "soft", "mixed", "hard", "undo", "checkout"} {
		t.Run(op, func(t *testing.T) {
			root, g := actionRepo(t)
			os.WriteFile(filepath.Join(root, "base"), []byte("base\n"), 0600)
			g("add", ".")
			g("commit", "-m", "base")
			base := strings.TrimSpace(g("rev-parse", "HEAD"))
			g("switch", "-c", "other")
			os.WriteFile(filepath.Join(root, "other"), []byte("other\n"), 0600)
			g("add", ".")
			g("commit", "-m", "other")
			tip := strings.TrimSpace(g("rev-parse", "HEAD"))
			target := tip
			if op == "merge" || op == "rebase" || op == "cherry-pick" {
				g("switch", "main")
			} else if op == "soft" || op == "mixed" || op == "hard" || op == "checkout" {
				target = base
			}
			if err := CommitOperation(ctx, root, op, target, false); err != nil {
				t.Fatal(err)
			}
			switch op {
			case "revert":
				if _, err := os.Stat(filepath.Join(root, "other")); !os.IsNotExist(err) {
					t.Fatal("revert did not remove file")
				}
			case "soft", "mixed", "hard", "undo":
				if got := strings.TrimSpace(g("rev-parse", "HEAD")); got != base {
					t.Fatal(got)
				}
				if op == "hard" {
					if _, err := os.Stat(filepath.Join(root, "other")); !os.IsNotExist(err) {
						t.Fatal("hard left file")
					}
				}
			case "checkout":
				if got := strings.TrimSpace(g("rev-parse", "HEAD")); got != base {
					t.Fatal(got)
				}
			default:
				if _, err := os.Stat(filepath.Join(root, "other")); err != nil {
					t.Fatal("operation lost incoming file", err)
				}
			}
			patch, err := CommitPatch(ctx, root, tip)
			if err != nil || !strings.Contains(patch, "+other") {
				t.Fatal(patch, err)
			}
			if err := CreateRef(ctx, root, "from-commit", base, false); err != nil {
				t.Fatal(err)
			}
			if err := CreateRef(ctx, root, "at-commit", base, true); err != nil {
				t.Fatal(err)
			}
			for _, ref := range []string{"refs/heads/from-commit", "refs/tags/at-commit"} {
				if got := strings.TrimSpace(g("rev-parse", ref)); got != base {
					t.Fatal(got)
				}
			}
		})
	}
}
func TestCommitsEmptyDetachedAndInvalidInputs(t *testing.T) {
	root, g := actionRepo(t)
	ctx := context.Background()
	cs, err := ReadHistory(ctx, root, 50, true)
	if err != nil || len(cs) != 0 {
		t.Fatal(cs, err)
	}
	g("commit", "--allow-empty", "-m", "root")
	hash := strings.TrimSpace(g("rev-parse", "HEAD"))
	g("switch", "--detach", hash)
	cs, err = ReadHistory(ctx, root, 50, false)
	if err != nil || len(cs) != 1 || len(cs[0].Refs) == 0 {
		t.Fatal(cs, err)
	}
	for _, bad := range []string{"--help", "HEAD", "abc", "a..b"} {
		if err := CommitOperation(ctx, root, "hard", bad, false); err == nil {
			t.Fatal("accepted invalid hash", bad)
		}
	}
	if _, err := ResolveCommit(ctx, root, "--help"); err == nil {
		t.Fatal("accepted option as ref")
	}
	if err := CreateRef(ctx, root, "--help", hash, true); err == nil {
		t.Fatal("accepted invalid name")
	}
}

func TestCommitsUndoRootAndRejectStaleHeadOrShallowBoundary(t *testing.T) {
	ctx := context.Background()
	root, g := actionRepo(t)
	os.WriteFile(filepath.Join(root, "file"), []byte("root\n"), 0600)
	g("add", ".")
	g("commit", "-m", "root")
	hash := strings.TrimSpace(g("rev-parse", "HEAD"))
	shallow := filepath.Join(t.TempDir(), "shallow")
	cmd := exec.Command("git", "clone", "--depth=1", "file://"+root, shallow)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if err := CommitOperation(ctx, shallow, "undo", hash, false); err == nil {
		t.Fatal("undid shallow boundary")
	}
	if got, err := ResolveCommit(ctx, shallow, "HEAD"); err != nil || got != hash {
		t.Fatal("shallow HEAD modified", got, err)
	}
	if err := CommitOperation(ctx, root, "undo", hash, false); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveCommit(ctx, root, "HEAD"); err == nil {
		t.Fatal("root still committed")
	}
	if got := g("diff", "--cached", "--name-only"); strings.TrimSpace(got) != "file" {
		t.Fatal("root undo lost staged changes", got)
	}
	g("commit", "-m", "root again")
	previous := strings.TrimSpace(g("rev-parse", "HEAD"))
	g("commit", "--allow-empty", "-m", "new head")
	if err := CommitOperation(ctx, root, "undo", previous, false); err == nil {
		t.Fatal("stale undo changed HEAD")
	}
}

func TestCommitsConflictingOperationsPreserveAbortState(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"merge", "rebase", "cherry-pick", "revert"} {
		t.Run(op, func(t *testing.T) {
			root, g := actionRepo(t)
			write := func(text string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, "file"), []byte(text+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("base")
			g("add", ".")
			g("commit", "-m", "base")
			g("switch", "-c", "other")
			write("other")
			g("commit", "-am", "other")
			target := strings.TrimSpace(g("rev-parse", "HEAD"))
			if op != "revert" {
				g("switch", "main")
			}
			write("current")
			g("commit", "-am", "current")
			before := strings.TrimSpace(g("rev-parse", "HEAD"))
			if err := CommitOperation(ctx, root, op, target, false); err == nil {
				t.Fatal("conflict unexpectedly succeeded")
			}
			if got := InProgress(ctx, root); got != op {
				t.Fatal("stopped operation not discoverable", got)
			}
			if err := ContinueOperation(ctx, root, op, true); err != nil {
				t.Fatal("abort failed", err)
			}
			if got := strings.TrimSpace(g("rev-parse", "HEAD")); got != before {
				t.Fatal("abort did not restore HEAD", got, before)
			}
			if got := strings.TrimSpace(g("status", "--porcelain")); got != "" {
				t.Fatal("abort left conflicts", got)
			}
		})
	}
}
