package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateBranchUsesSelectedLocalBase(t *testing.T) {
	root, git := actionRepo(t)
	ctx := context.Background()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "base")
	git("branch", "base")
	base := strings.TrimSpace(git("rev-parse", "HEAD"))
	if err := os.WriteFile(path, []byte("current\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("commit", "-am", "advance main")
	if err := CreateBranch(ctx, root, "feature/from-base", "base"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(git("rev-parse", "HEAD")); got != base {
		t.Fatal("created from current branch instead of selected base", got, base)
	}
	for _, invalid := range []struct{ branch, base string }{
		{"feature/from-base", "main"}, {"bad name", "main"}, {"missing-base", "missing"}, {"bad-base", "main~1"}, {"option-base", "--orphan"},
	} {
		if err := CreateBranch(ctx, root, invalid.branch, invalid.base); err == nil {
			t.Fatal("accepted invalid or existing branch", invalid)
		}
		if got := strings.TrimSpace(git("branch", "--show-current")); got != "feature/from-base" {
			t.Fatal("failed creation changed current branch", got)
		}
	}
	git("switch", "--detach", "main")
	head := strings.TrimSpace(git("rev-parse", "HEAD"))
	if err := CreateBranch(ctx, root, "from-detached", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(git("rev-parse", "HEAD")); got != head {
		t.Fatal("detached default did not preserve HEAD", got)
	}
}
