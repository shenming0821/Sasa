package codex

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	process "github.com/shenming0821/Sasa/internal/runtime"
)

// GenerateCommitMessage uses the same resolved executable and user model
// configuration as the App Server, in an ephemeral read-only working directory.
func (c *Client) GenerateCommitMessage(ctx context.Context, prompt string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "sasa-commit-message-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	output := filepath.Join(dir, "message.txt")
	p, err := process.Start(c.process.Cmd.Path, []string{
		"exec", "--ephemeral", "--sandbox", "read-only", "--skip-git-repo-check",
		"-c", "approval_policy=\"never\"", "--color", "never", "--output-last-message", output, "-",
	}, dir, io.Discard)
	if err != nil {
		return "", err
	}
	done := make(chan error, 1)
	written := make(chan error, 1)
	go func() {
		io.Copy(io.Discard, p.Stdout)
		done <- p.Wait()
	}()
	go func() {
		_, err := io.WriteString(p.Stdin, prompt)
		closeErr := p.Stdin.Close()
		if err == nil {
			err = closeErr
		}
		written <- err
	}()
	select {
	case err = <-done:
	case <-ctx.Done():
		p.Close()
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("Codex 生成提交信息失败：%w", err)
	}
	if err = <-written; err != nil {
		return "", fmt.Errorf("发送提交差异失败：%w", err)
	}
	info, err := os.Stat(output)
	if err != nil {
		return "", err
	}
	if info.Size() > 64<<10 {
		return "", fmt.Errorf("提交信息超过 64 KiB")
	}
	data, err := os.ReadFile(output)
	text := strings.TrimSpace(string(data))
	if text == "" && err == nil {
		err = fmt.Errorf("Codex 未返回提交信息")
	}
	return text, err
}
