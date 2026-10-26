// Package workspace provides bounded, cancellable local directory discovery.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Entry struct {
	Path, Name         string
	Dir, Link, Ignored bool
}

func Contains(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func hidden(name string) bool { return name == ".git" || name == ".DS_Store" }
func ReadDir(ctx context.Context, root, path string) ([]Entry, error) {
	if !Contains(root, path) {
		return nil, fmt.Errorf("目录不属于当前工作区")
	}
	for parent := filepath.Dir(path); parent != root && Contains(root, parent); parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("不递归展开符号链接")
		}
	}
	info, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("不递归展开符号链接")
	}
	fd, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer fd.Close()
	var result []Entry
	for {
		entries, err := fd.ReadDir(256)
		for _, v := range entries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			if hidden(v.Name()) {
				continue
			}
			result = append(result, Entry{Path: filepath.Join(path, v.Name()), Name: v.Name(), Dir: v.IsDir(), Link: v.Type()&os.ModeSymlink != 0})
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, err
			}
			break
		}
		if len(result) > 100000 {
			return nil, fmt.Errorf("目录超过 100,000 项")
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Dir != result[j].Dir {
			return result[i].Dir
		}
		return naturalLess(result[i].Name, result[j].Name)
	})
	return result, nil
}

func naturalLess(a, b string) bool {
	x, y := strings.ToLower(a), strings.ToLower(b)
	for i, j := 0, 0; i < len(x) && j < len(y); {
		if x[i] >= '0' && x[i] <= '9' && y[j] >= '0' && y[j] <= '9' {
			ix, jy := i, j
			for i < len(x) && x[i] >= '0' && x[i] <= '9' {
				i++
			}
			for j < len(y) && y[j] >= '0' && y[j] <= '9' {
				j++
			}
			nx, ny := strings.TrimLeft(x[ix:i], "0"), strings.TrimLeft(y[jy:j], "0")
			if len(nx) != len(ny) {
				return len(nx) < len(ny)
			}
			if nx != ny {
				return nx < ny
			}
			if i-ix != j-jy {
				return i-ix < j-jy
			}
			continue
		}
		if x[i] != y[j] {
			return x[i] < y[j]
		}
		i++
		j++
	}
	if x != y {
		return x < y
	}
	return a < b
}
func MarkIgnored(ctx context.Context, root string, entries []Entry) {
	var b strings.Builder
	for _, v := range entries {
		b.WriteString(v.Path)
		b.WriteByte(0)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "check-ignore", "-z", "--stdin")
	cmd.Stdin = strings.NewReader(b.String())
	out, _ := cmd.Output()
	ignored := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		ignored[p] = true
	}
	for i := range entries {
		entries[i].Ignored = ignored[entries[i].Path]
	}
}

type SearchResult struct {
	Entries   []Entry
	Truncated bool
}

func Search(ctx context.Context, root, query string) (SearchResult, error) {
	var result SearchResult
	query = strings.ToLower(query)
	count, bytes := 0, 0
	skip := map[string]bool{"node_modules": true, ".next": true, ".cache": true, ".venv": true, "target": true, "__pycache__": true, ".git": true, "bin": true}
	var visit func(string) error
	visit = func(path string) error {
		entries, err := ReadDir(ctx, root, path)
		if err != nil {
			return err
		}
		MarkIgnored(ctx, root, entries)
		for _, v := range entries {
			count++
			bytes += len(v.Path)
			if count > 100000 || bytes > 16<<20 || len(result.Entries) >= 200 {
				result.Truncated = true
				return nil
			}
			if v.Ignored || v.Link {
				continue
			}
			if v.Dir {
				if !skip[v.Name] {
					if e := visit(v.Path); e != nil {
						return e
					}
					if result.Truncated {
						return nil
					}
				}
			} else {
				rel, _ := filepath.Rel(root, v.Path)
				if strings.Contains(strings.ToLower(rel), query) {
					result.Entries = append(result.Entries, v)
				}
			}
		}
		return nil
	}
	err := visit(root)
	return result, err
}
