// Package workspace provides bounded, cancellable local directory discovery.
package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
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

var searchSkipDirs = map[string]bool{"node_modules": true, ".next": true, ".cache": true, ".venv": true, "target": true, "__pycache__": true, ".git": true, "bin": true}

var errSearchLimit = errors.New("workspace search result limit")

// walkEntries visits the entries under dir in tree order, marking ignore state
// and applying the shared traversal bounds. visit reports whether a directory
// should be descended into; returning errSearchLimit stops the walk and marks
// the result truncated.
func walkEntries(ctx context.Context, root, dir string, visit func(Entry) (bool, error)) (bool, error) {
	count, bytes := 0, 0
	truncated := false
	var walk func(string) error
	walk = func(path string) error {
		entries, err := ReadDir(ctx, root, path)
		if err != nil {
			return err
		}
		MarkIgnored(ctx, root, entries)
		for _, v := range entries {
			count++
			bytes += len(v.Path)
			if count > 100000 || bytes > 16<<20 {
				truncated = true
				return nil
			}
			if v.Ignored || v.Link {
				continue
			}
			descend, err := visit(v)
			if err != nil {
				if errors.Is(err, errSearchLimit) {
					truncated = true
					return nil
				}
				return err
			}
			if v.Dir && descend && !searchSkipDirs[v.Name] {
				if e := walk(v.Path); e != nil {
					return e
				}
				if truncated {
					return nil
				}
			}
		}
		return nil
	}
	err := walk(dir)
	return truncated, err
}

func Search(ctx context.Context, root, query string) (SearchResult, error) {
	var result SearchResult
	query = strings.ToLower(query)
	truncated, err := walkEntries(ctx, root, root, func(v Entry) (bool, error) {
		if v.Dir {
			return true, nil
		}
		if len(result.Entries) >= 200 {
			return false, errSearchLimit
		}
		rel, _ := filepath.Rel(root, v.Path)
		if strings.Contains(strings.ToLower(rel), query) {
			result.Entries = append(result.Entries, v)
		}
		return false, nil
	})
	result.Truncated = truncated
	return result, err
}

const (
	contentSearchMaxFileBytes  = 4 << 20
	contentSearchMaxMatches    = 200
	contentSearchMaxFileMatch  = 50
	contentSearchPreviewRunes  = 240
	contentSearchPreviewLeadIn = 60
)

type ContentMatch struct {
	Path      string
	Line, Col int
	Text      string
}

type ContentSearchResult struct {
	Matches   []ContentMatch
	Truncated bool
}

// SearchContent scans regular text files under dir for lines containing query
// (case-insensitive). dir must stay inside root so ignore rules and the tree
// boundary still apply. Binary files and unsupported encodings are skipped the
// same way the editor refuses them; oversized or unreadable files mark the
// result truncated.
func SearchContent(ctx context.Context, root, dir, query string) (ContentSearchResult, error) {
	var result ContentSearchResult
	if !Contains(root, dir) {
		return result, fmt.Errorf("目录不属于当前工作区")
	}
	needle := strings.ToLower(query)
	truncated, err := walkEntries(ctx, root, dir, func(v Entry) (bool, error) {
		if v.Dir {
			return true, nil
		}
		if len(result.Matches) >= contentSearchMaxMatches {
			return false, errSearchLimit
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		matches, incomplete := searchFileContent(v.Path, needle, min(contentSearchMaxFileMatch, contentSearchMaxMatches-len(result.Matches)))
		result.Matches = append(result.Matches, matches...)
		if incomplete {
			result.Truncated = true
		}
		return false, nil
	})
	result.Truncated = result.Truncated || truncated
	return result, err
}

func searchFileContent(path, needle string, limit int) ([]ContentMatch, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, err != nil
	}
	if info.Size() > contentSearchMaxFileBytes {
		return nil, true
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > contentSearchMaxFileBytes {
		return nil, true
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, false
	}
	var matches []ContentMatch
	truncated := false
	lines := strings.Split(string(data), "\n")
	lowered := strings.Split(strings.ToLower(string(data)), "\n")
	for i, raw := range lines {
		offset := strings.Index(lowered[i], needle)
		if offset < 0 {
			continue
		}
		if len(matches) >= limit {
			truncated = true
			break
		}
		col := utf8.RuneCountInString(lowered[i][:offset]) + 1
		matches = append(matches, ContentMatch{Path: path, Line: i + 1, Col: col, Text: matchPreview(raw, col)})
	}
	return matches, truncated
}

// matchPreview keeps the matched line readable: trimmed, capped, and centered
// on the match when the line is longer than the preview budget.
func matchPreview(raw string, col int) string {
	runes := []rune(strings.TrimSpace(raw))
	if len(runes) <= contentSearchPreviewRunes {
		return string(runes)
	}
	lead := len([]rune(raw)) - len([]rune(strings.TrimLeft(raw, " \t")))
	match := max(0, col-1-lead)
	start := max(0, match-contentSearchPreviewLeadIn)
	if start+contentSearchPreviewRunes > len(runes) {
		start = len(runes) - contentSearchPreviewRunes
	}
	end := min(len(runes), start+contentSearchPreviewRunes)
	text := string(runes[start:end])
	if start > 0 {
		text = "…" + text
	}
	if end < len(runes) {
		text += "…"
	}
	return text
}
