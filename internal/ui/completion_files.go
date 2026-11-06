package ui

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shenming0821/Fuyu/internal/workspace"
)

const completionFilesTTL = time.Minute
const completionFilePageSize = 30

type completionFile struct {
	rel, name string
	dir       bool
}

// loadCompletionFiles indexes the session workspace once, then serves the @
// picker from memory. Results stay usable while a refresh is in flight.
func (a *App) loadCompletionFiles(force bool) {
	c := &a.completion
	cwd := a.State.Cwd
	if cwd == "" || c.filesLoading || !a.canAsync() {
		return
	}
	if !force {
		fresh := c.filesCwd == cwd && c.filesError == "" && time.Since(c.filesChecked) < completionFilesTTL
		retryWait := c.filesError != "" && time.Since(c.filesChecked) < completionRetryDelay
		if fresh || retryWait {
			return
		}
	}
	c.filesLoading = true
	s := a.session
	ctx, cancel := a.projectContext(30 * time.Second)
	a.runBackgroundFileTaskContext(ctx, func() {
		defer cancel()
		result, err := workspace.Index(ctx, cwd)
		a.update(func() {
			cc := &s.completion
			cc.filesLoading = false
			if s.State.Cwd != cwd {
				return
			}
			cc.filesChecked = time.Now()
			if err != nil {
				cc.filesError = err.Error()
				return
			}
			cc.filesError = ""
			cc.filesCwd = cwd
			cc.filesVersion++
			cc.filesTruncated = result.Truncated
			cc.files = completionFiles(cwd, result.Entries)
		})
	})
}

func completionFiles(root string, entries []workspace.Entry) []completionFile {
	files := make([]completionFile, 0, len(entries))
	for _, e := range entries {
		rel, err := filepath.Rel(root, e.Path)
		if err != nil || rel == "." {
			continue
		}
		files = append(files, completionFile{rel: filepath.ToSlash(rel), name: e.Name, dir: e.Dir})
	}
	return files
}

func (a *App) completionFileItems(q completionQuery) []completionItem {
	c := &a.completion
	key := fmt.Sprintf("%d\x00%s", c.filesVersion, q.query)
	if c.fileFilterKey == key {
		return c.fileFiltered
	}
	var items []completionItem
	if c.filesCwd == a.State.Cwd {
		query := strings.ToLower(q.query)
		for _, f := range c.files {
			if query != "" && !strings.Contains(strings.ToLower(f.rel), query) {
				continue
			}
			description := ""
			if parent := path.Dir(f.rel); parent != "." {
				description = parent
			}
			items = append(items, completionItem{name: f.name, description: description, path: f.rel, dir: f.dir})
		}
		sort.SliceStable(items, func(i, j int) bool {
			ig, jg := fileMatchGroup(items[i], query), fileMatchGroup(items[j], query)
			if ig != jg {
				return ig < jg
			}
			id, jd := strings.Count(items[i].path, "/"), strings.Count(items[j].path, "/")
			if id != jd {
				return id < jd
			}
			if len(items[i].path) != len(items[j].path) {
				return len(items[i].path) < len(items[j].path)
			}
			return items[i].path < items[j].path
		})
	}
	c.fileFilterKey, c.fileFiltered = key, items
	return items
}

// fileMatchGroup ranks base-name hits ahead of parent-directory hits so short
// queries surface the file itself rather than every path containing the text.
func fileMatchGroup(item completionItem, query string) int {
	if query == "" || strings.Contains(strings.ToLower(item.name), query) {
		return 0
	}
	return 1
}
