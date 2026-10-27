package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ChangedFile describes the net edits recoverable from this turn's protocol patches.
// Unknown counts are kept separate from zero; no working-tree files are read.
type ChangedFile struct {
	Path, PreviousPath, Patch string
	Added, Deleted            int
	Exact                     bool
	DeletedFile               bool
}

type ChangeSummary struct {
	Files          []ChangedFile
	Added, Deleted int
	Exact          bool
}

type patchLine struct {
	original  int
	text      string
	known     bool
	noNewline bool
}
type patchFile struct {
	path, previous  string
	base, current   []patchLine
	next            int
	exact, complete bool
	deleted         bool
	raw             []string
}

var turnHunk = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func SummarizeChanges(cwd string, messages []*Message) ChangeSummary {
	var files []*patchFile
	byPath := map[string]*patchFile{}
	for _, m := range messages {
		if m.Tool == nil || m.Tool.Kind != "fileChange" || m.Tool.Status != "completed" {
			continue
		}
		for _, change := range m.Tool.Changes {
			path := filepath.Clean(change.Path)
			if !filepath.IsAbs(path) && filepath.IsAbs(cwd) {
				path = filepath.Join(cwd, path)
			}
			f := byPath[path]
			if f == nil {
				f = &patchFile{path: path, exact: true}
				files = append(files, f)
				byPath[path] = f
			}
			var kind struct {
				Type     string `json:"type"`
				MovePath string `json:"move_path"`
			}
			if json.Unmarshal(change.Kind, &kind) != nil {
				f.exact = false
			}
			f.deleted = kind.Type == "delete"
			if kind.MovePath != "" {
				target := filepath.Clean(kind.MovePath)
				if !filepath.IsAbs(target) && filepath.IsAbs(cwd) {
					target = filepath.Join(cwd, target)
				}
				if other := byPath[target]; other != nil && other != f {
					f.exact = false
					other.exact = false
				}
				if f.previous == "" {
					f.previous = path
				}
				f.path = target
				delete(byPath, path)
				byPath[target] = f
			}
			f.raw = append(f.raw, change.Diff)
			if m.Tool.Truncated || change.Truncated {
				f.exact = false
			}
			if f.exact {
				f.exact = f.apply(change.Diff, kind.Type)
			}
		}
	}
	result := ChangeSummary{Exact: true}
	for _, f := range files {
		item := ChangedFile{Path: f.path, PreviousPath: f.previous, Exact: f.exact, DeletedFile: f.deleted}
		if f.exact {
			item.Patch, item.Added, item.Deleted, item.Exact = f.netPatch()
		}
		if !item.Exact {
			item.Added, item.Deleted = 0, 0
			item.Patch = strings.Join(f.raw, "\n\n")
			result.Exact = false
		}
		result.Added += item.Added
		result.Deleted += item.Deleted
		result.Files = append(result.Files, item)
	}
	return result
}

func rawLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func (f *patchFile) ensure(n int) bool {
	if n < 0 || n > 100000 {
		return false
	}
	if f.complete && n > len(f.current) {
		return false
	}
	for len(f.current) < n {
		line := patchLine{original: f.next}
		f.next++
		f.base = append(f.base, line)
		f.current = append(f.current, line)
	}
	return true
}

func (f *patchFile) learn(line patchLine, text string, noNewline bool) bool {
	if line.known {
		return line.text == text && line.noNewline == noNewline
	}
	if line.original < 0 {
		return false
	}
	f.base[line.original].text = text
	f.base[line.original].known = true
	f.base[line.original].noNewline = noNewline
	return true
}

func (f *patchFile) apply(patch, kind string) bool {
	lines := rawLines(patch)
	hasHunk := slices.ContainsFunc(lines, func(s string) bool { return turnHunk.MatchString(s) })
	if !hasHunk {
		switch kind {
		case "add":
			if len(f.current) > 0 || (len(f.base) > 0 && !f.complete) {
				return false
			}
			for i, s := range lines {
				f.current = append(f.current, patchLine{original: -1, text: s, known: true, noNewline: i == len(lines)-1 && !strings.HasSuffix(patch, "\n")})
			}
			f.complete = true
			return true
		case "delete":
			if !f.ensure(len(lines)) || len(f.current) != len(lines) {
				return false
			}
			for i, s := range lines {
				if !f.learn(f.current[i], s, i == len(lines)-1 && !strings.HasSuffix(patch, "\n")) {
					return false
				}
			}
			f.current = nil
			f.complete = true
			return true
		default:
			return patch == "" && f.previous != ""
		}
	}
	// Hunk new positions account for prior hunks in the same patch.
	delta := 0
	for i := 0; i < len(lines); {
		match := turnHunk.FindStringSubmatch(lines[i])
		if match == nil {
			i++
			continue
		}
		oldStart, _ := strconv.Atoi(match[1])
		newStart, _ := strconv.Atoi(match[3])
		oldCount, newCount := 1, 1
		if match[2] != "" {
			oldCount, _ = strconv.Atoi(match[2])
		}
		if match[4] != "" {
			newCount, _ = strconv.Atoi(match[4])
		}
		at := newStart - 1
		if newCount == 0 {
			at = newStart
		}
		oldAt := oldStart - 1
		if oldCount == 0 {
			oldAt = oldStart
		}
		if at < 0 || at != oldAt+delta {
			return false
		}
		delta += newCount - oldCount
		if !f.ensure(at + oldCount) {
			return false
		}
		var inserted []patchLine
		consumed, produced := 0, 0
		i++
		for i < len(lines) && turnHunk.FindStringSubmatch(lines[i]) == nil {
			text := lines[i]
			i++
			if strings.HasPrefix(text, "\\ No newline") {
				continue
			}
			if consumed == oldCount && produced == newCount {
				if text != "" {
					return false
				}
				continue
			}
			if len(text) == 0 {
				return false
			}
			noNewline := i < len(lines) && strings.HasPrefix(lines[i], "\\ No newline")
			switch text[0] {
			case ' ', '-':
				if consumed >= oldCount {
					return false
				}
				line := f.current[at+consumed]
				if !f.learn(line, text[1:], noNewline) {
					return false
				}
				line.text, line.known, line.noNewline = text[1:], true, noNewline
				consumed++
				if text[0] == ' ' {
					inserted = append(inserted, line)
					produced++
				}
			case '+':
				inserted = append(inserted, patchLine{original: -1, text: text[1:], known: true, noNewline: noNewline})
				produced++
			default:
				return false
			}
		}
		if consumed != oldCount || produced != newCount {
			return false
		}
		f.current = slices.Replace(f.current, at, at+oldCount, inserted...)
	}
	if kind == "add" || kind == "delete" {
		f.complete = true
	}
	return true
}

// Unknown unchanged lines anchor sparse regions, so untouched file contents
// need not be present. Known context stays in LCS to avoid counting moved text twice.
func (f *patchFile) netPatch() (string, int, int, bool) {
	var out strings.Builder
	beforePath := f.path
	if f.previous != "" {
		beforePath = f.previous
	}
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n", beforePath, f.path)
	oldAt, newAt, added, deleted := 0, 0, 0, 0
	flush := func(oldEnd, newEnd int) bool {
		old, new := f.base[oldAt:oldEnd], f.current[newAt:newEnd]
		for len(old) > 0 && len(new) > 0 && old[0].known && new[0].known && samePatchLine(old[0], new[0]) {
			old = old[1:]
			new = new[1:]
			oldAt++
			newAt++
		}
		for len(old) > 0 && len(new) > 0 && old[len(old)-1].known && new[len(new)-1].known && samePatchLine(old[len(old)-1], new[len(new)-1]) {
			old = old[:len(old)-1]
			new = new[:len(new)-1]
		}
		if len(old) == 0 && len(new) == 0 {
			return true
		}
		for _, line := range append(slices.Clone(old), new...) {
			if !line.known {
				return false
			}
		}
		if (len(old)+1)*(len(new)+1) > 1000000 {
			return false
		}
		cols := len(new) + 1
		dp := make([]int, (len(old)+1)*cols)
		for i := len(old) - 1; i >= 0; i-- {
			for j := len(new) - 1; j >= 0; j-- {
				if samePatchLine(old[i], new[j]) {
					dp[i*cols+j] = dp[(i+1)*cols+j+1] + 1
				} else {
					dp[i*cols+j] = max(dp[(i+1)*cols+j], dp[i*cols+j+1])
				}
			}
		}
		startOld, startNew := oldAt+1, newAt+1
		if len(old) == 0 {
			startOld = oldAt
		}
		if len(new) == 0 {
			startNew = newAt
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", startOld, len(old), startNew, len(new))
		for i, j := 0, 0; i < len(old) || j < len(new); {
			if i < len(old) && j < len(new) && samePatchLine(old[i], new[j]) {
				writePatchLine(&out, " ", old[i])
				i++
				j++
			} else if j < len(new) && (i == len(old) || dp[i*cols+j+1] >= dp[(i+1)*cols+j]) {
				writePatchLine(&out, "+", new[j])
				j++
				added++
			} else {
				writePatchLine(&out, "-", old[i])
				i++
				deleted++
			}
		}
		return true
	}
	for i, line := range f.current {
		if line.original >= 0 && !f.base[line.original].known {
			if !flush(line.original, i) {
				return "", 0, 0, false
			}
			oldAt, newAt = line.original+1, i+1
		}
	}
	if !flush(len(f.base), len(f.current)) {
		return "", 0, 0, false
	}
	if added == 0 && deleted == 0 {
		return "", 0, 0, true
	}
	return out.String(), added, deleted, true
}

func samePatchLine(a, b patchLine) bool { return a.text == b.text && a.noNewline == b.noNewline }
func writePatchLine(out *strings.Builder, prefix string, line patchLine) {
	out.WriteString(prefix + line.text + "\n")
	if line.noNewline {
		out.WriteString("\\ No newline at end of file\n")
	}
}
