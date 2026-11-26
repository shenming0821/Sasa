package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Root resolves the actual worktree, including linked worktrees.
func Root(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	return strings.TrimSuffix(string(out), "\n"), err
}

func validPaths(paths []string) error {
	if len(paths) == 0 {
		return errors.New("请选择文件")
	}
	for _, path := range paths {
		clean := filepath.Clean(path)
		if path == "" || strings.ContainsRune(path, 0) || filepath.IsAbs(path) ||
			clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
			strings.EqualFold(strings.Split(filepath.ToSlash(clean), "/")[0], ".git") {
			return fmt.Errorf("文件路径不在工作区范围内：%q", path)
		}
	}
	return nil
}

func Stage(ctx context.Context, root string, paths []string) error {
	if err := validPaths(paths); err != nil {
		return err
	}
	_, err := run(ctx, root, append([]string{"add", "-A", "--"}, paths...)...)
	return err
}

func Unstage(ctx context.Context, root string, paths []string) error {
	if err := validPaths(paths); err != nil {
		return err
	}
	_, headErr := run(ctx, root, "rev-parse", "--verify", "HEAD")
	args := []string{"restore", "--staged", "--source=HEAD", "--"}
	if headErr != nil {
		status, err := Read(ctx, root)
		if err != nil || status.Head != "(initial)" {
			return fmt.Errorf("读取 HEAD 失败：%w", headErr)
		}
		args = []string{"rm", "--cached", "-r", "--ignore-unmatch", "--"}
	}
	_, err := run(ctx, root, append(args, paths...)...)
	return err
}

func Commit(ctx context.Context, root, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("请输入提交信息")
	}
	if strings.ContainsRune(message, 0) {
		return "", errors.New("提交信息包含无效字符")
	}
	_, err := run(ctx, root, "commit", "-m", message)
	if err != nil {
		return "", err
	}
	out, err := run(ctx, root, "rev-parse", "HEAD")
	return strings.TrimSpace(string(out)), err
}

type Branch struct {
	Name, Hash, Upstream, Worktree string
	Current                        bool
	Ref                            string
	Remote                         bool
	Author, Subject                string
	CommitTime                     time.Time
}

func Branches(ctx context.Context, root string) ([]Branch, error) {
	branches, err := RepositoryBranches(ctx, root)
	var local []Branch
	for _, branch := range branches {
		if !branch.Remote {
			local = append(local, branch)
		}
	}
	return local, err
}

func RepositoryBranches(ctx context.Context, root string) ([]Branch, error) {
	out, err := worktreeRun(ctx, root, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(upstream:short)%00%(worktreepath)%00%(HEAD)%00%(symref)%00%(committerdate:unix)%00%(authorname)%00%(subject)%00", "refs/heads/", "refs/remotes/")
	if err != nil {
		return nil, err
	}
	var result []Branch
	fields := strings.Split(string(out), "\x00")
	for len(fields) >= 9 {
		ref := strings.TrimPrefix(fields[0], "\n")
		if fields[5] == "" {
			branch := Branch{Name: strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/remotes/"), Hash: fields[1], Upstream: fields[2], Worktree: fields[3], Current: fields[4] == "*", Ref: ref, Remote: strings.HasPrefix(ref, "refs/remotes/"), Author: fields[7], Subject: fields[8]}
			if stamp, err := strconv.ParseInt(fields[6], 10, 64); err == nil {
				branch.CommitTime = time.Unix(stamp, 0)
			}
			result = append(result, branch)
		}
		fields = fields[9:]
	}
	return result, nil
}

func Switch(ctx context.Context, root, branch string, create bool) error {
	if create {
		return CreateBranch(ctx, root, branch, "")
	}
	if _, err := run(ctx, root, "check-ref-format", "--branch", branch); err != nil {
		return fmt.Errorf("分支名称无效：%w", err)
	}
	_, err := run(ctx, root, "switch", branch)
	return err
}

func CreateBranch(ctx context.Context, root, branch, base string) error {
	if base != "" && base != "HEAD" {
		base = "refs/heads/" + base
	}
	return CreateBranchFromRef(ctx, root, branch, base)
}

func CreateBranchFromRef(ctx context.Context, root, branch, base string) error {
	if _, err := run(ctx, root, "check-ref-format", "--branch", branch); err != nil {
		return fmt.Errorf("分支名称无效：%w", err)
	}
	args := []string{"switch", "-c", branch}
	if base != "" && base != "HEAD" {
		if !strings.HasPrefix(base, "refs/heads/") && !strings.HasPrefix(base, "refs/remotes/") {
			return fmt.Errorf("来源分支无效：%s", base)
		}
		if _, err := run(ctx, root, "check-ref-format", base); err != nil {
			return fmt.Errorf("来源分支无效：%w", err)
		}
		if strings.HasPrefix(base, "refs/remotes/") {
			args = append(args, "--no-track")
		}
	}
	if base != "" {
		args = append(args, base)
	}
	_, err := run(ctx, root, args...)
	return err
}

func Remotes(ctx context.Context, root string) ([]string, error) {
	out, err := run(ctx, root, "remote")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// RemoteAction never rebases, force-pushes or stashes implicitly.
func RemoteAction(ctx context.Context, root, action, remote string) error {
	var args []string
	switch action {
	case "fetch":
		args = []string{"fetch", "--all"}
	case "pull":
		args = []string{"pull", "--ff-only", "--no-rebase"}
	case "push":
		args = []string{"push"}
	case "publish":
		status, err := Read(ctx, root)
		if err != nil {
			return err
		}
		remotes, err := Remotes(ctx, root)
		if err != nil {
			return err
		}
		found := false
		for _, name := range remotes {
			found = found || name == remote
		}
		if !found || status.Branch == "" || status.Branch == "(detached)" || status.Head == "(initial)" {
			return errors.New("请选择有效远程，并先创建分支提交")
		}
		args = []string{"push", "--set-upstream", remote, status.Branch}
	default:
		return errors.New("未知远程操作")
	}
	_, err := run(ctx, root, args...)
	return err
}

// Discard preserves untracked files in an application-owned recovery directory.
// Callers confirm the scope and handle open, unsaved editors before invoking it.
func Discard(ctx context.Context, root string, entries []Entry, recovery string) error {
	var tracked []string
	for _, e := range entries {
		if err := validPaths([]string{e.Path}); err != nil {
			return err
		}
		if e.Conflict || e.Submodule {
			return errors.New("冲突和子模块请单独处理")
		}
	}
	for _, e := range entries {
		if e.XY != "??" {
			tracked = append(tracked, e.Path)
			continue
		}
		if recovery == "" {
			return errors.New("无法确定未跟踪文件的回收位置")
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(filepath.Join(root, e.Path)))
		base, baseErr := filepath.EvalSymlinks(root)
		rel, relErr := filepath.Rel(base, parent)
		if err != nil || baseErr != nil || relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("回收文件的父目录已移出工作区，请刷新")
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		dir, err := os.MkdirTemp(recovery, "discard-")
		if os.IsNotExist(err) {
			if err = os.MkdirAll(recovery, 0700); err == nil {
				dir, err = os.MkdirTemp(recovery, "discard-")
			}
		}
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, "original-path"), []byte(filepath.Join(root, e.Path)), 0600); err != nil {
			return err
		}
		if err = os.Rename(filepath.Join(root, e.Path), filepath.Join(dir, "content")); err != nil {
			return fmt.Errorf("回收 %s：%w", e.Path, err)
		}
	}
	if len(tracked) == 0 {
		return nil
	}
	_, err := run(ctx, root, append([]string{"restore", "--worktree", "--"}, tracked...)...)
	return err
}

type ConflictSides struct{ Base, Current, Incoming string }

func ConflictText(ctx context.Context, root, path string) (ConflictSides, error) {
	if err := validPaths([]string{path}); err != nil {
		return ConflictSides{}, err
	}
	var result ConflictSides
	for i, target := range []*string{&result.Base, &result.Current, &result.Incoming} {
		stage := fmt.Sprintf(":%d:%s", i+1, filepath.ToSlash(path))
		out, err := run(ctx, root, "show", stage)
		if err != nil {
			// Add/delete conflicts legitimately lack one of the three stages.
			found, check := run(ctx, root, "ls-files", "--unmerged", "-z", "--", path)
			if check != nil || len(found) == 0 {
				return result, fmt.Errorf("文件已不处于冲突状态：%s", path)
			}
			if strings.Contains(string(found), fmt.Sprintf(" %d\t", i+1)) {
				return result, err
			}
		}
		*target = string(out)
	}
	return result, nil
}

func InProgress(ctx context.Context, root string) string {
	for _, item := range []struct{ marker, name string }{
		{"rebase-merge", "rebase"}, {"rebase-apply", "rebase"},
		{"MERGE_HEAD", "merge"}, {"CHERRY_PICK_HEAD", "cherry-pick"}, {"REVERT_HEAD", "revert"},
	} {
		out, err := run(ctx, root, "rev-parse", "--git-path", item.marker)
		if err != nil {
			continue
		}
		path := strings.TrimSuffix(string(out), "\n")
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if _, err := os.Stat(path); err == nil {
			return item.name
		}
	}
	return ""
}

func ContinueOperation(ctx context.Context, root, op string, abort bool) error {
	if actual := InProgress(ctx, root); actual == "" || actual != op {
		return errors.New("Git 操作状态已变化，请刷新")
	}
	mode := "--continue"
	if abort {
		mode = "--abort"
	}
	_, err := run(ctx, root, "-c", "core.editor=true", "-c", "sequence.editor=true", op, mode)
	return err
}

// fingerprint binds block actions to the exact index and file bytes reviewed.
func fingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// commandInput uses stdin for patches; no shell parses filenames or messages.
func commandInput(ctx context.Context, root string, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--literal-pathspecs", "-C", root, "-c", "core.fsmonitor=false"}, args...)...)
	configureGitCommand(cmd)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat")
	cmd.Stdin = strings.NewReader(string(input))
	var out, stderr limited
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.String())
	}
	if out.overflow || stderr.overflow {
		return nil, errors.New("Git 输出超过容量限制")
	}
	return out.Bytes(), nil
}

// History returns current-branch history; COMMITS uses ReadHistory for all refs.
func History(ctx context.Context, root string, limit, skip int) ([]CommitInfo, error) {
	return readCommitLog(ctx, root, limit, skip, []string{"HEAD"})
}

func validHash(hash string) bool {
	_, err := hex.DecodeString(hash)
	return err == nil && (len(hash) == 40 || len(hash) == 64)
}

func CommitFiles(ctx context.Context, root, hash string) ([]Entry, error) {
	if from, to, ok := comparisonEnds(hash); ok {
		return CompareFiles(ctx, root, from, to)
	}
	if !validHash(hash) {
		return nil, errors.New("提交 ID 无效")
	}
	out, err := run(ctx, root, "diff-tree", "--root", "--no-commit-id", "-r", "-m", "--first-parent", "--name-status", "-z", "--find-renames", hash)
	if err != nil {
		return nil, err
	}
	return parseCommitFiles(string(out))
}

func parseCommitFiles(out string) ([]Entry, error) {
	fields := strings.Split(out, "\x00")
	var result []Entry
	for i := 0; i+1 < len(fields); {
		state := fields[i]
		path := fields[i+1]
		i += 2
		e := Entry{Path: path, XY: state[:min(1, len(state))] + "."}
		if strings.HasPrefix(state, "R") || strings.HasPrefix(state, "C") {
			if i >= len(fields) {
				return nil, errors.New("提交重命名记录不完整")
			}
			e.Original, e.Path = path, fields[i]
			i++
		}
		if e.Path != "" {
			result = append(result, e)
		}
	}
	return result, nil
}

func CommitDiff(ctx context.Context, root, hash string, entry Entry) (string, error) {
	if from, to, ok := comparisonEnds(hash); ok {
		return CompareDiff(ctx, root, from, to, entry)
	}
	if !validHash(hash) {
		return "", errors.New("提交 ID 无效")
	}
	if err := validPaths([]string{entry.Path}); err != nil {
		return "", err
	}
	args := []string{"show", "--format=", "--first-parent", "--root", "--no-ext-diff", "--no-textconv", "--no-color", "--find-renames", hash, "--", entry.Path}
	if entry.Original != "" {
		args = append(args, entry.Original)
	}
	out, err := run(ctx, root, args...)
	return string(out), err
}
