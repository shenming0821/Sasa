package git

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type WorktreeSource struct {
	Ref, Label string
}

type WorktreeFieldError struct {
	Field, Message string
}

func (err *WorktreeFieldError) Error() string { return err.Message }

type WorktreeResult struct {
	Path                                               string
	Created, Registered, BranchExists, DirectoryExists bool
}

func worktreeRun(ctx context.Context, root string, args ...string) ([]byte, error) {
	var environment []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX":
			continue
		}
		environment = append(environment, entry)
	}
	return runWithEnvironment(ctx, root, environment, args...)
}

func WorktreeDirectory(dataDir, commonDir, folder string) string {
	if absolute, err := filepath.Abs(dataDir); err == nil {
		dataDir = absolute
	}
	if resolved, err := filepath.EvalSymlinks(dataDir); err == nil {
		dataDir = resolved
	}
	hash := sha256.Sum256([]byte(commonDir))
	return filepath.Join(dataDir, "worktrees", fmt.Sprintf("%x", hash[:12]), folder)
}

func WorktreeFolder(branch string) string {
	return strings.ReplaceAll(strings.TrimSpace(branch), "/", "-")
}

func ValidateWorktreeFolder(folder string) error {
	if folder == "" || folder == "." || folder == ".." || strings.ContainsAny(folder, "/\\:") || strings.ContainsFunc(folder, unicode.IsControl) {
		return &WorktreeFieldError{"folder", "请输入有效的单层文件夹名称，名称不能包含路径分隔符、冒号或控制字符"}
	}
	return nil
}

func WorktreeSources(ctx context.Context, root string) ([]WorktreeSource, string, error) {
	head, err := worktreeRun(ctx, root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, "", fmt.Errorf("无法读取当前提交，请确认仓库已有首次提交：%w", err)
	}
	branch, err := worktreeRun(ctx, root, "symbolic-ref", "-q", "HEAD")
	selected := strings.TrimSpace(string(branch))
	var sources []WorktreeSource
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return nil, "", fmt.Errorf("读取当前分支失败：%w", err)
		}
		selected = "HEAD"
		sources = append(sources, WorktreeSource{"HEAD", "当前提交 " + strings.TrimSpace(string(head))[:8]})
	}
	branches, err := RepositoryBranches(ctx, root)
	if err != nil {
		return nil, "", err
	}
	for _, branch := range branches {
		label := branch.Name
		if branch.Remote {
			label += "（远程）"
		}
		sources = append(sources, WorktreeSource{branch.Ref, label})
	}
	return sources, selected, nil
}

func ValidateNewWorktree(ctx context.Context, root, base, branch, target string) (string, error) {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return "", &WorktreeFieldError{"branch", "请输入有效的新分支名"}
	}
	if _, err := worktreeRun(ctx, root, "check-ref-format", "refs/heads/"+branch); err != nil {
		return "", &WorktreeFieldError{"branch", "分支名称无效：" + err.Error()}
	}
	if _, err := worktreeRun(ctx, root, "check-ref-format", "--branch", branch); err != nil {
		return "", &WorktreeFieldError{"branch", "分支名称无效：" + err.Error()}
	}
	if _, err := worktreeRun(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return "", &WorktreeFieldError{"branch", "本地分支已存在，请使用新的分支名"}
	} else {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return "", fmt.Errorf("检查分支失败：%w", err)
		}
	}
	if err := ValidateWorktreeFolder(filepath.Base(target)); err != nil {
		return "", err
	}
	if _, err := os.Lstat(target); err == nil {
		return "", &WorktreeFieldError{"folder", "目标路径已存在，请修改文件夹名称"}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", &WorktreeFieldError{"folder", "无法检查目标路径：" + err.Error()}
	}
	if base != "HEAD" && !strings.HasPrefix(base, "refs/heads/") && !strings.HasPrefix(base, "refs/remotes/") {
		return "", &WorktreeFieldError{"base", "请选择有效的基础分支"}
	}
	commit, err := worktreeRun(ctx, root, "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
	if err != nil {
		return "", &WorktreeFieldError{"base", "基础分支已失效或没有提交，请重新选择：" + err.Error()}
	}
	return strings.TrimSpace(string(commit)), nil
}

func CreateWorktree(ctx context.Context, root, base, branch, target string) (WorktreeResult, error) {
	result := WorktreeResult{Path: target}
	commit, err := ValidateNewWorktree(ctx, root, base, branch, target)
	if err != nil {
		return result, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return result, fmt.Errorf("创建存放目录失败：%w", err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		return result, &WorktreeFieldError{"folder", "无法创建文件夹：" + err.Error()}
	}
	_, createErr := worktreeRun(ctx, root, "worktree", "add", "-b", branch, "--", target, commit)
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	info, statErr := os.Stat(target)
	result.DirectoryExists = statErr == nil && info.IsDir()
	_, branchErr := worktreeRun(checkCtx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	result.BranchExists = branchErr == nil
	listing, listErr := worktreeRun(checkCtx, root, "worktree", "list", "--porcelain", "-z")
	canonicalTarget, canonicalErr := filepath.EvalSymlinks(target)
	if canonicalErr != nil {
		canonicalTarget = target
	}
	for _, record := range strings.Split(string(listing), "\x00\x00") {
		fields := strings.Split(record, "\x00")
		if len(fields) > 0 && strings.TrimPrefix(fields[0], "worktree ") == canonicalTarget {
			for _, field := range fields {
				if field == "branch refs/heads/"+branch {
					result.Registered = true
				}
			}
		}
	}
	if createErr == nil {
		result.Created = true
		return result, nil
	}
	return result, fmt.Errorf("创建未完成：%w；核对结果：目录存在=%t（%v），分支存在=%t（%v），工作树已登记=%t（%v）。已保留现有内容，路径：%s", createErr, result.DirectoryExists, statErr, result.BranchExists, branchErr, result.Registered, listErr, target)
}
