package ui

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/workspace"
)

const quickOpenRecentLimit = 10
const quickOpenRowHeight = 30

// quickOpenState drives the Cmd+P file picker. The modal shares the
// @-completion indexing pattern: workspace.Index runs in the background and
// keystrokes filter the cached result, with the file page's pendingLocation
// carrying a ":line" jump into the editor.
type quickOpenState struct {
	open           bool
	query          string
	root           string
	selected       int
	reveal         bool
	files          []completionFile
	filesRoot      string
	filesChecked   time.Time
	filesLoading   bool
	filesError     string
	filesTruncated bool
	filesVersion   uint64
	filtered       []quickOpenItem
	filterKey      string
	list           native.ListState
}

type quickOpenItem struct {
	rel, name, parent, path string
}

type quickOpenQuery struct {
	name         string
	line, column int
	symbol       string
}

func (a *App) openQuickOpen() {
	if a.quitting || a.quitDialog || a.settingsOpen || a.sessionRenameOpen ||
		a.worktreeRenameOpen || a.worktreeCreate != nil || a.fileAction.open || a.viewerOpen {
		return
	}
	root := a.application()
	q := &root.quickOpen
	q.root = a.workspaceCwd()
	q.open = true
	q.query = ""
	q.selected = 0
	if q.filesRoot != q.root {
		q.files, q.filtered, q.filterKey = nil, nil, ""
	}
	root.loadQuickOpenFiles(false)
}

// loadQuickOpenFiles indexes the picked workspace once, then serves the picker
// from memory. Results stay usable while a refresh is in flight.
func (a *App) loadQuickOpenFiles(force bool) {
	root := a.application()
	q := &root.quickOpen
	dir := q.root
	if dir == "" || q.filesLoading || !a.canAsync() {
		return
	}
	if !force {
		fresh := q.filesRoot == dir && q.filesError == "" && time.Since(q.filesChecked) < completionFilesTTL
		retryWait := q.filesError != "" && time.Since(q.filesChecked) < completionRetryDelay
		if fresh || retryWait {
			return
		}
	}
	q.filesLoading = true
	ctx, cancel := a.projectContext(30 * time.Second)
	a.runBackgroundFileTaskContext(ctx, func() {
		defer cancel()
		result, err := workspace.Index(ctx, dir)
		a.update(func() {
			qq := &root.quickOpen
			qq.filesLoading = false
			if qq.root != dir {
				return
			}
			qq.filesChecked = time.Now()
			if err != nil {
				qq.filesError = err.Error()
				return
			}
			qq.filesError = ""
			qq.filesRoot = dir
			qq.filesVersion++
			qq.filesTruncated = result.Truncated
			qq.files = completionFiles(dir, result.Entries)
		})
	})
}

// parseQuickQuery splits a quick-open query into its file-name filter and a
// trailing location, as in "foo.go:12:3". A ":line" or ":line:column" tail
// (and a bare trailing colon while one is being typed) comes off the filter;
// anything else stays part of the name. An "@symbol" tail parses out too; the
// picker's symbol mode is not implemented yet, so the name before it is what
// still filters files.
func parseQuickQuery(text string) quickOpenQuery {
	q := quickOpenQuery{name: text}
	if i := strings.LastIndexByte(q.name, ':'); i >= 0 {
		before, tail := q.name[:i], q.name[i+1:]
		switch n, ok := quickOpenNumber(tail); {
		case tail == "":
			q.name = before
		case ok:
			q.name = before
			if j := strings.LastIndexByte(before, ':'); j >= 0 {
				if m, ok := quickOpenNumber(before[j+1:]); ok {
					q.name, q.line, q.column = before[:j], m, n
					break
				}
			}
			q.line = n
		}
	}
	if i := strings.IndexByte(q.name, '@'); i >= 0 {
		q.name, q.symbol = q.name[:i], q.name[i+1:]
	}
	return q
}

func quickOpenNumber(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, _ := strconv.Atoi(s)
	return n, true
}

// quickOpenRecent lists the root's open file pages, most recently visited
// first. Only tabs still open count; closed files leave no trace here.
func (a *App) quickOpenRecent() []*filePage {
	q := &a.quickOpen
	var recent []*filePage
	for _, d := range a.files {
		if d.kind == "file" && d.root == q.root {
			recent = append(recent, d)
		}
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].lastVisited.After(recent[j].lastVisited) })
	return recent[:min(len(recent), quickOpenRecentLimit)]
}

func quickOpenItemFor(root, abs, rel string) quickOpenItem {
	if rel == "" {
		if r, err := filepath.Rel(root, abs); err == nil {
			rel = filepath.ToSlash(r)
		}
	}
	parent := path.Dir(rel)
	if parent == "." || parent == "/" {
		parent = ""
	}
	return quickOpenItem{rel: rel, name: path.Base(rel), parent: parent, path: abs}
}

// quickOpenItems answers the row list: the MRU section while the name filter
// is empty, else the index filtered per keystroke with MRU entries boosted.
func (a *App) quickOpenItems(query quickOpenQuery) []quickOpenItem {
	q := &a.quickOpen
	recent := a.quickOpenRecent()
	if strings.TrimSpace(query.name) == "" {
		items := make([]quickOpenItem, 0, len(recent))
		for _, d := range recent {
			items = append(items, quickOpenItemFor(q.root, d.path, ""))
		}
		return items
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d\x00%s\x00%s\x00", q.filesVersion, q.filesRoot, strings.ToLower(query.name))
	for _, d := range recent {
		b.WriteString(d.path)
		b.WriteByte(0)
	}
	if q.filterKey == b.String() {
		return q.filtered
	}
	rank := make(map[string]int, len(recent))
	for i, d := range recent {
		rank[d.path] = i
	}
	var items []quickOpenItem
	if q.filesRoot == q.root {
		name := strings.ToLower(query.name)
		for _, f := range q.files {
			if f.dir || !strings.Contains(strings.ToLower(f.rel), name) {
				continue
			}
			items = append(items, quickOpenItemFor(q.root, filepath.Join(q.root, filepath.FromSlash(f.rel)), f.rel))
		}
		sort.SliceStable(items, func(i, j int) bool {
			ri, iok := rank[items[i].path]
			rj, jok := rank[items[j].path]
			if iok != jok {
				return iok
			}
			if iok && ri != rj {
				return ri < rj
			}
			ig, jg := quickOpenGroup(items[i], name), quickOpenGroup(items[j], name)
			if ig != jg {
				return ig < jg
			}
			id, jd := strings.Count(items[i].rel, "/"), strings.Count(items[j].rel, "/")
			if id != jd {
				return id < jd
			}
			if len(items[i].rel) != len(items[j].rel) {
				return len(items[i].rel) < len(items[j].rel)
			}
			return items[i].rel < items[j].rel
		})
	}
	q.filterKey, q.filtered = b.String(), items
	return items
}

// quickOpenGroup ranks base-name hits ahead of parent-directory hits, matching
// the @-completion ordering.
func quickOpenGroup(item quickOpenItem, name string) int {
	if strings.Contains(strings.ToLower(item.name), name) {
		return 0
	}
	return 1
}

func (a *App) acceptQuickOpen(item quickOpenItem, query quickOpenQuery) {
	q := &a.quickOpen
	q.open = false
	a.openFile(q.root, item.path)
	if query.line > 0 {
		if d := a.currentFile(); d != nil && d.root == q.root && d.path == item.path {
			d.pendingLocation = &fileLocation{line: query.line, column: max(query.column, 1)}
			d.pinFrame = 0
		}
	}
}

// The picker's panel pins to the top rather than Modal's center, so the
// input holds still while the result list grows and shrinks under it.
func (a *App) quickOpenView(c *native.Context) {
	q := &a.quickOpen
	native.DialogBase(c.Key("quick-open"), &q.open, func(backdrop, panel native.Element) {
		if c.Shortcut(native.Cmd, native.KeyW) {
			q.open = false
		}
		width, height := c.Size()
		t := c.Theme()
		top := min(140, max(48, height*0.12))
		backdrop.Background(native.RGBA(0, 0, 0, 0.4)).Justify(native.Start).AlignItems(native.Center)
		panel.Margin(top, 0, 0, 0).Padding(10).Gap(8).Radius(10).Background(t.Background).
			Border(1, t.Border.Alpha(.7)).Shadow(0, 10, 30, 0, native.RGBA(0, 0, 0, 0.3)).
			MaxHeight(height - top - 32)
		a.loadQuickOpenFiles(false)
		query := parseQuickQuery(q.query)
		items := a.quickOpenItems(query)
		q.selected = min(max(q.selected, 0), max(len(items)-1, 0))
		native.Column(c).Width(min(width-64, 560)).Gap(8).Children(func() {
			input := native.TextInput(c, &q.query).Label("快速打开文件").Placeholder("按文件名搜索（:行号 跳转）").AutoFocus().FillWidth()
			composing := input.Composing()
			input.OnTextKey(func(k native.TextKey) bool {
				if composing {
					return false
				}
				switch k.Key {
				case native.KeyDown, native.KeyUp:
					if len(items) > 0 {
						delta := 1
						if k.Key == native.KeyUp {
							delta = -1
						}
						q.selected = (q.selected + delta + len(items)) % len(items)
						q.reveal = true
					}
					return true
				case native.KeyEnter, native.KeyTab:
					if len(items) > 0 {
						a.acceptQuickOpen(items[q.selected], query)
					}
					return true
				}
				return false
			})
			if input.Submitted() && len(items) > 0 {
				a.acceptQuickOpen(items[q.selected], query)
			}
			a.quickOpenStatus(c, query, len(items))
			if len(items) > 0 {
				if strings.TrimSpace(query.name) == "" {
					native.Text(c, "recently opened").FontSize(11).TextColor(t.TextMuted).Padding(0, 4)
				}
				q.list.Key = func(i int) any { return items[i].path }
				list := native.List(c, &q.list, len(items), func(i int) {
					a.quickOpenRow(c, items[i], i == q.selected, query)
				}).FillWidth()
				list.Height(min(height-top-140, float32(len(items))*quickOpenRowHeight+4))
				if q.reveal {
					q.list.ScrollIntoView(q.selected)
				}
			}
			if q.filesTruncated && strings.TrimSpace(query.name) != "" {
				native.Text(c, "结果不完整，请缩小搜索范围").FontSize(10).TextColor(t.TextMuted)
			}
		})
		q.reveal = false
	})
}

func (a *App) quickOpenStatus(c *native.Context, query quickOpenQuery, count int) {
	q := &a.quickOpen
	t := c.Theme()
	switch {
	case q.root == "":
		native.Text(c, "没有可搜索的工作区").FontSize(12).TextColor(t.TextMuted)
	case q.filesError != "":
		native.Row(c).FillWidth().Gap(8).AlignItems(native.Center).Children(func() {
			native.Text(c, q.filesError).FontSize(12).Grow(1).MinWidth(0).TextColor(t.TextMuted)
			if quietButton(c, "重新索引文件").KeepFocus().Children(func() { native.Text(c, "重试").FontSize(12) }).Clicked() {
				a.loadQuickOpenFiles(true)
			}
		})
	case count == 0 && strings.TrimSpace(query.name) == "":
		native.Text(c, "没有最近打开的文件，输入以搜索").FontSize(12).TextColor(t.TextMuted)
	case count == 0 && q.filesLoading:
		native.Text(c, "正在索引工作区文件…").FontSize(12).TextColor(t.TextMuted)
	case count == 0:
		native.Text(c, "没有匹配的文件").FontSize(12).TextColor(t.TextMuted)
	}
}

func (a *App) quickOpenRow(c *native.Context, item quickOpenItem, selected bool, query quickOpenQuery) {
	t := c.Theme()
	rc := c.Key(item.path)
	row := native.ButtonBase(rc).Label("打开 "+item.rel).FillWidth().Radius(6).Padding(6, 8).KeepFocus()
	if selected || row.Hovered() {
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		native.Row(rc).FillWidth().AlignItems(native.Center).Gap(8).Children(func() {
			native.Icon(rc, fileIcon).Size(13, 13).Shrink(0).TextColor(t.TextMuted)
			native.Text(rc, item.name).FontSize(13).SingleLine().Shrink(0)
			native.Text(rc, item.parent).FontSize(11).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
		})
	})
	if row.Clicked() {
		a.acceptQuickOpen(item, query)
	}
}
