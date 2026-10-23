package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ResolveCodex returns an absolute executable and the PATH its children need.
// Call once at desktop startup, before creating the version probe or App Server.
func ResolveCodex(executable string) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("locate Codex home: %w", err)
	}
	return resolveCodex(executable, os.Getenv("PATH"), loginShellPath(os.Getenv("SHELL")), home, os.Getenv("NVM_DIR"))
}

func resolveCodex(executable, inherited, login, home, nvmDir string) (string, string, error) {
	parts := []string{inherited, login}
	if strings.ContainsRune(executable, filepath.Separator) {
		if path, err := filepath.Abs(executable); err == nil {
			parts = append(parts, filepath.Dir(path))
		}
	}
	parts = append(parts, filepath.Join(home, ".bun/bin"), filepath.Join(home, ".local/bin"),
		filepath.Join(home, ".npm-global/bin"), filepath.Join(home, "Library/pnpm"), filepath.Join(home, ".local/share/pnpm"),
		filepath.Join(home, ".volta/bin"), filepath.Join(home, ".asdf/shims"), "/opt/homebrew/bin", "/usr/local/bin",
		"/usr/bin", "/bin", "/usr/sbin", "/sbin")
	for _, root := range []string{nvmDir, filepath.Join(home, ".nvm"), filepath.Join(home, ".local/share/nvm")} {
		if !filepath.IsAbs(root) {
			continue
		}
		versions, _ := filepath.Glob(filepath.Join(root, "versions/node/v*/bin"))
		// Some nvm installations store versions directly under their data directory.
		direct, _ := filepath.Glob(filepath.Join(root, "v*/bin"))
		versions = append(versions, direct...)
		slices.SortFunc(versions, newerNodeFirst)
		parts = append(parts, versions...)
	}
	var dirs []string
	seen := make(map[string]bool)
	for _, part := range parts {
		for _, dir := range filepath.SplitList(part) {
			// Never discover a runtime from the current project directory.
			if filepath.IsAbs(dir) && !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}
	searchPath := strings.Join(dirs, string(os.PathListSeparator))
	if strings.ContainsRune(executable, filepath.Separator) {
		path, err := filepath.Abs(executable)
		if err == nil && executableFile(path) {
			return checkedCodex(path, searchPath)
		}
	} else {
		for _, dir := range dirs {
			path := filepath.Join(dir, executable)
			if executableFile(path) {
				return checkedCodex(path, searchPath)
			}
		}
	}
	return "", searchPath, fmt.Errorf("未找到 Codex 可执行文件 %q；请检查安装，或通过 -codex 指定可执行文件的绝对路径", executable)
}

func checkedCodex(path, searchPath string) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", searchPath, fmt.Errorf("读取 Codex 可执行文件 %q: %w", path, err)
	}
	defer file.Close()
	header := make([]byte, 256)
	n, _ := file.Read(header)
	line, _, _ := strings.Cut(string(header[:n]), "\n")
	if strings.TrimSpace(line) == "#!/usr/bin/env node" {
		for _, dir := range filepath.SplitList(searchPath) {
			if executableFile(filepath.Join(dir, "node")) {
				return path, searchPath, nil
			}
		}
		return "", searchPath, fmt.Errorf("Codex %q 需要 Node.js，但启动环境中未找到 node；请检查 Node.js 安装与登录 shell 的 PATH", path)
	}
	return path, searchPath, nil
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

func newerNodeFirst(a, b string) int {
	version := func(path string) []string {
		return strings.Split(strings.TrimPrefix(filepath.Base(filepath.Dir(path)), "v"), ".")
	}
	av, bv := version(a), version(b)
	for i := 0; i < min(len(av), len(bv)); i++ {
		x, _ := strconv.Atoi(av[i])
		y, _ := strconv.Atoi(bv[i])
		if x != y {
			if x > y {
				return -1
			}
			return 1
		}
	}
	return strings.Compare(a, b)
}

func loginShellPath(shell string) string {
	if !filepath.IsAbs(shell) || !executableFile(shell) {
		shell = "/bin/zsh"
		if !executableFile(shell) {
			shell = "/bin/sh"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Use external printenv so fish and POSIX shells export PATH identically.
	cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", `/usr/bin/printf '\000FUYU_PATH\000'; /usr/bin/printenv PATH`)
	configureGroup(cmd)
	cmd.Cancel = func() error { killGroup(cmd); return nil }
	cmd.WaitDelay = 100 * time.Millisecond
	var output bytes.Buffer
	cmd.Stdout = &boundedOutput{buffer: &output, remaining: 64 * 1024}
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return ""
	}
	const marker = "\x00FUYU_PATH\x00"
	_, path, ok := strings.Cut(output.String(), marker)
	if !ok {
		return ""
	}
	return strings.TrimSuffix(path, "\n")
}

type boundedOutput struct {
	buffer    *bytes.Buffer
	remaining int
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, io.ErrShortWrite
	}
	w.remaining -= len(p)
	return w.buffer.Write(p)
}
