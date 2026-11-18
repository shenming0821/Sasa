// Package git exposes bounded worktree state, patches and explicit Git actions.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const OutputLimit = 8 << 20

// MetadataDirectories includes both worktree-local index/HEAD and shared refs.
func MetadataDirectories(ctx context.Context, root string) ([]string, error) {
	var dirs []string
	seen := map[string]bool{}
	for _, arg := range []string{"--absolute-git-dir", "--git-common-dir"} {
		out, err := run(ctx, root, "rev-parse", arg)
		if err != nil {
			return nil, err
		}
		dir := strings.TrimSuffix(string(out), "\n")
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		dir = filepath.Clean(dir)
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
		err = filepath.WalkDir(filepath.Join(dir, "refs"), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if e.IsDir() && !seen[path] {
				if len(dirs) >= 256 {
					return fs.SkipDir
				}
				seen[path] = true
				dirs = append(dirs, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return dirs, nil
}

// CompareText uses the same read-only patch renderer for two immutable buffers.
func CompareText(ctx context.Context, local, disk string) (string, error) {
	if len(local)+len(disk) > 2*OutputLimit {
		return "", errors.New("比较内容超过容量限制")
	}
	dir, err := os.MkdirTemp("", "sasa-compare-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "local"), []byte(local), 0600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "disk"), []byte(disk), 0600); err != nil {
		return "", err
	}
	return noIndex(ctx, dir, "local", "disk")
}

type limited struct {
	bytes.Buffer
	overflow bool
}

func (b *limited) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > OutputLimit {
		b.overflow = true
		return 0, errors.New("Git 输出超过 8 MiB")
	}
	return b.Buffer.Write(p)
}
func run(ctx context.Context, root string, args ...string) ([]byte, error) {
	return runWithEnvironment(ctx, root, os.Environ(), args...)
}

func runWithEnvironment(ctx context.Context, root string, environment []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "--literal-pathspecs", "-C", root, "-c", "core.fsmonitor=false"}, args...)...)
	configureGitCommand(cmd)
	cmd.Env = append(environment, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_EXTERNAL_DIFF=", "GIT_PAGER=cat")
	var out, stderr limited
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if out.overflow || stderr.overflow {
		return nil, errors.New("Git 输出超过 8 MiB")
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}

type Entry struct {
	Path, Original, XY  string
	Conflict, Submodule bool
}

func (e Entry) Staged() bool {
	return !e.Conflict && len(e.XY) == 2 && e.XY[0] != '.' && e.XY[0] != '?'
}
func (e Entry) Unstaged() bool {
	return !e.Conflict && len(e.XY) == 2 && (e.XY[1] != '.' || e.XY == "??")
}

type Status struct {
	Branch, Head, Upstream string
	Ahead, Behind          int
	Entries                []Entry
}

func Read(ctx context.Context, root string) (Status, error) {
	raw, err := run(ctx, root, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return Status{}, err
	}
	return Parse(raw)
}
func Parse(raw []byte) (Status, error) {
	var s Status
	records := strings.Split(string(raw), "\x00")
	for i := 0; i < len(records); i++ {
		r := records[i]
		if r == "" {
			continue
		}
		if strings.HasPrefix(r, "# ") {
			key, value, _ := strings.Cut(r[2:], " ")
			switch key {
			case "branch.head":
				s.Branch = value
			case "branch.oid":
				s.Head = value
			case "branch.upstream":
				s.Upstream = value
			case "branch.ab":
				if _, err := fmt.Sscanf(value, "+%d -%d", &s.Ahead, &s.Behind); err != nil {
					return s, fmt.Errorf("Git ahead/behind 记录不完整：%w", err)
				}
			}
			continue
		}
		if strings.HasPrefix(r, "? ") {
			s.Entries = append(s.Entries, Entry{Path: r[2:], XY: "??"})
			continue
		}
		if strings.HasPrefix(r, "! ") {
			continue
		}
		n := 0
		switch r[0] {
		case '1':
			n = 8
		case '2':
			n = 9
		case 'u':
			n = 10
		default:
			return s, fmt.Errorf("未知 Git 状态记录")
		}
		fields := strings.SplitN(r, " ", n+1)
		if len(fields) != n+1 || len(fields[1]) != 2 {
			return s, fmt.Errorf("Git 状态记录不完整")
		}
		e := Entry{Path: fields[n], XY: fields[1], Conflict: r[0] == 'u', Submodule: strings.HasPrefix(fields[2], "S")}
		if r[0] == '2' {
			i++
			if i >= len(records) || records[i] == "" {
				return s, fmt.Errorf("Git 重命名源路径缺失")
			}
			e.Original = records[i]
		}
		s.Entries = append(s.Entries, e)
	}
	return s, nil
}
func Diff(ctx context.Context, root string, e Entry, staged bool) (string, error) {
	var previousStatus []byte
	var previousPatch string
	for attempt := 0; attempt < 3; attempt++ {
		before, err := run(ctx, root, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
		if err != nil {
			return "", err
		}
		patch, err := diff(ctx, root, e, staged)
		if err != nil {
			return "", err
		}
		if attempt > 0 && bytes.Equal(before, previousStatus) && patch == previousPatch {
			return patch, nil
		}
		previousStatus, previousPatch = before, patch
	}
	return "", errors.New("比较期间 Git 或文件持续变化，请刷新后重试")
}

func diff(ctx context.Context, root string, e Entry, staged bool) (string, error) {
	if e.Submodule {
		return "子模块更改（首版仅显示状态）", nil
	}
	if e.Conflict {
		out, err := run(ctx, root, "--no-pager", "diff", "--cc", "--no-ext-diff", "--no-textconv", "--no-color", "--", e.Path)
		return string(out), err
	}
	if e.XY == "??" {
		return noIndex(ctx, root, "/dev/null", e.Path)
	}
	args := []string{"--no-pager", "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-relative", "--find-renames", "--unified=3"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", e.Path)
	if e.Original != "" {
		args = append(args, e.Original)
	}
	out, err := run(ctx, root, args...)
	return string(out), err
}

func noIndex(ctx context.Context, root, old, next string) (string, error) {
	// --no-index also exits 1 for a successful nonempty diff.
	cmd := exec.CommandContext(ctx, "git", "--no-pager", "diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color", "--", old, next)
	configureGitCommand(cmd)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	var out, errout limited
	cmd.Stdout, cmd.Stderr = &out, &errout
	err := cmd.Run()
	if out.overflow || errout.overflow {
		return "", errors.New("差异超过 8 MiB")
	}
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return "", fmt.Errorf("读取差异：%v %s", err, errout.String())
	}
	return out.String(), nil
}
