package codex

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	process "github.com/shenming0821/Fuyu/internal/runtime"
)

func messageClient(t *testing.T, script string) *Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex fixture")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	return &Client{process: &process.Process{Cmd: &exec.Cmd{Path: path}}}
}

func TestGenerateMessageUsesReadOnlyEphemeralCLIAndStdin(t *testing.T) {
	c := messageClient(t, `
output=""
all="$*"
case "$all" in *"--ephemeral --sandbox read-only --skip-git-repo-check"*) ;; *) exit 10 ;; esac
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then shift; output="$1"; fi
  shift
done
input=$(cat)
[ "$input" = 'diff with $literal and spaces' ] || exit 11
printf 'fix: generated message\n' > "$output"
`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	message, err := c.GenerateCommitMessage(ctx, "diff with $literal and spaces")
	if err != nil || message != "fix: generated message" {
		t.Fatal(message, err)
	}
}

func TestGenerateMessageCancellationBoundsBlockedInput(t *testing.T) {
	c := messageClient(t, "sleep 30 &\nwait\n")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.GenerateCommitMessage(ctx, strings.Repeat("x", 256<<10))
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 4*time.Second {
		t.Fatal("generation cancellation failed", time.Since(start), err)
	}
}

func TestGenerateMessageFailureIsNotAnEmptySuccess(t *testing.T) {
	c := messageClient(t, "cat >/dev/null\nexit 12\n")
	if message, err := c.GenerateCommitMessage(context.Background(), "diff"); err == nil || message != "" {
		t.Fatal(message, err)
	}
}

func TestLiveGitCommitMessage(t *testing.T) {
	if os.Getenv("FUYU_LIVE") != "1" {
		t.Skip("set FUYU_LIVE=1 to use the existing Codex login")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := Start(ctx, "codex", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	message, err := c.GenerateCommitMessage(ctx, "只返回一行中文 Git 提交信息，格式 fix: 具体变化。不要执行工具。近期提交：fix: 修正编辑器滚动。差异：\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-timeout := 1\n+timeout := 5\n")
	if err != nil || !strings.HasPrefix(message, "fix: ") || strings.Contains(message, "```") {
		t.Fatal(message, err)
	}
	t.Logf("ephemeral read-only generation: %s", message)
}
