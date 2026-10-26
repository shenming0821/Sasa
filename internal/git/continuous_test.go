package git

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiffDuringContinuousExternalReplacement(t *testing.T) {
	root := t.TempDir()
	command := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	command("init", "-b", "main")
	path := filepath.Join(root, "changing.txt")
	if err := os.WriteFile(path, []byte("baseline\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command("add", "--", "changing.txt")
	command("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-m", "baseline")
	indexPath := filepath.Join(root, ".git", "index")
	before, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	started, stop, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		temporary := filepath.Join(root, ".external-replacement")
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		defer os.Remove(temporary)
		for i := 0; ; i++ {
			version := "version A\n"
			if i%2 != 0 {
				version = "version B\n"
			}
			if err := os.WriteFile(temporary, []byte(version), 0600); err != nil {
				finished <- err
				return
			}
			if err := os.Rename(temporary, path); err != nil {
				finished <- err
				return
			}
			if i == 0 {
				close(started)
			}
			select {
			case <-stop:
				finished <- nil
				return
			case <-tick.C:
			}
		}
	}()
	// Always stop and join the writer, including assertion failures.
	t.Cleanup(func() {
		close(stop)
		if err := <-finished; err != nil {
			t.Error("external writer", err)
		}
	})
	select {
	case <-started:
	case err := <-finished:
		finished <- err
		t.Fatal("writer did not start", err)
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entry := Entry{Path: "changing.txt", XY: ".M"}
	for range 6 {
		patch, err := Diff(ctx, root, entry, false)
		if err != nil {
			if !strings.Contains(err.Error(), "持续变化") {
				t.Fatal("unexpected error during external replacement", err)
			}
			continue
		}
		a, b := strings.Contains(patch, "+version A\n"), strings.Contains(patch, "+version B\n")
		if a == b || !strings.Contains(patch, "-baseline\n") {
			t.Fatal("diff did not describe one complete observed version", patch)
		}
	}
	after, err := os.ReadFile(indexPath)
	if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("continuous inspection changed index", err)
	}
}
