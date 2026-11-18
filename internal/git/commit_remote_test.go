package git

import (
	"context"
	"strings"
	"testing"
)

func TestCommitsRemoteSelectionAndCredentialFreeLinks(t *testing.T) {
	root, g := actionRepo(t)
	g("commit", "--allow-empty", "-m", "base")
	hash := strings.TrimSpace(g("rev-parse", "HEAD"))
	g("remote", "add", "origin", "https://token:secret@github.com/owner/repo.git?secret=hidden")
	g("remote", "add", "upstream", "git@gitlab.com:group/sub/repo.git")
	g("update-ref", "refs/remotes/upstream/main", hash)
	g("branch", "--set-upstream-to=upstream/main", "main")
	cm := CommitInfo{Hash: hash}
	ctx := context.Background()
	check := func(want string) {
		t.Helper()
		got, err := CommitRemoteURL(ctx, root, cm)
		if err != nil || got != want+hash {
			t.Fatalf("remote URL = %q, %v; want %q", got, err, want+hash)
		}
	}
	check("https://gitlab.com/group/sub/repo/-/commit/")
	cm.Refs = []Ref{{Kind: RefRemote, Name: "origin/main"}}
	check("https://github.com/owner/repo/commit/")
	cm.Refs = append(cm.Refs, Ref{Kind: RefRemote, Name: "upstream/main"})
	check("https://gitlab.com/group/sub/repo/-/commit/")
	g("branch", "--unset-upstream")
	cm.Refs = nil
	check("https://github.com/owner/repo/commit/")
	for _, raw := range []string{"http://github.com/a/b", "https://unknown.invalid/a/b", "file:///tmp/repo", "ssh://git@github.com/a/../b"} {
		if _, err := RemoteWebURL(raw, hash); err == nil {
			t.Fatal("accepted unsupported URL", raw)
		}
	}
	got, err := RemoteWebURL("ssh://git@bitbucket.org/team/repo.git", hash)
	if err != nil || got != "https://bitbucket.org/team/repo/commits/"+hash {
		t.Fatal(got, err)
	}
}
