// Package project reads local Git repository and worktree metadata without changing Git state.
package project

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Worktree struct {
	Path     string
	Branch   string
	Head     string
	Main     bool
	Bare     bool
	Detached bool
	Locked   bool
	Prunable bool
	Missing  bool
}

type Repository struct {
	CommonDir string
	Path      string
	Name      string
	Worktrees []Worktree
}

func Canonical(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved
	}
	return filepath.Clean(absolute)
}

func git(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	// Repository discovery follows the chosen directory, never inherited Git overrides.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(raw)))
	}
	return string(raw), nil
}

func Discover(ctx context.Context, path string) (Repository, error) {
	path = Canonical(path)
	raw, err := git(ctx, path, "rev-parse", "--git-common-dir")
	if err != nil {
		return Repository{}, err
	}
	common := strings.TrimSuffix(raw, "\n")
	if !filepath.IsAbs(common) {
		common = filepath.Join(path, common)
	}
	common = Canonical(common)
	raw, err = git(ctx, path, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return Repository{}, err
	}
	trees, err := parseWorktrees(raw)
	if err != nil {
		return Repository{}, err
	}
	for i := range trees {
		info, err := os.Stat(trees[i].Path)
		trees[i].Missing = err != nil || !info.IsDir()
	}
	root := trees[0].Path
	name := filepath.Base(root)
	if trees[0].Bare {
		name = strings.TrimSuffix(name, ".git")
	}
	return Repository{CommonDir: common, Path: root, Name: name, Worktrees: trees}, nil
}

func parseWorktrees(raw string) ([]Worktree, error) {
	var result []Worktree
	var current *Worktree
	for _, field := range strings.Split(raw, "\x00") {
		if field == "" {
			current = nil
			continue
		}
		key, value, _ := strings.Cut(field, " ")
		if key == "worktree" {
			if value == "" || !filepath.IsAbs(value) {
				return nil, fmt.Errorf("Git 工作区路径无效：%q", value)
			}
			result = append(result, Worktree{Path: Canonical(value), Main: len(result) == 0})
			current = &result[len(result)-1]
			continue
		}
		if current == nil {
			return nil, fmt.Errorf("Git 工作区记录缺少路径")
		}
		switch key {
		case "HEAD":
			current.Head = value
		case "branch":
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "bare":
			current.Bare = true
		case "detached":
			current.Detached = true
		case "locked":
			current.Locked = true
		case "prunable":
			current.Prunable = true
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("Git 未返回工作区列表")
	}
	return result, nil
}

func (w Worktree) Available() bool {
	return !w.Bare && !w.Prunable && !w.Missing
}

func (w Worktree) Label() string {
	if w.Branch != "" {
		return w.Branch
	}
	if w.Bare {
		return "裸仓库"
	}
	if w.Detached {
		return "detached · " + w.Head[:min(8, len(w.Head))]
	}
	return "未命名分支"
}
