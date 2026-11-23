package ui

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/editor"
	gitview "github.com/shenming0821/Sasa/internal/git"
	"github.com/shenming0821/Sasa/internal/storage"
	"github.com/shenming0821/Sasa/internal/workspace"
)

var fileIcon = icon(`<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8zM14 2v6h6M8 13h8M8 17h6"/>`)
var gitIcon = icon(`<circle cx="6" cy="5" r="3"/><circle cx="6" cy="19" r="3"/><circle cx="18" cy="5" r="3"/><path d="M6 8v8m12-8c0 6-12 1-12 8"/>`)
var historyTabIcon = icon(`<path d="M3 11a9 9 0 1 1 2.6 7.4M3 4v7h7M12 7v5l3 2"/>`)

func (a *App) rightPanel(c *native.Context) {
	t := c.Theme()
	native.Column(c.Key("workspace-panel")).Fill().MinWidth(0).Background(t.Surface).Children(func() {
		native.Row(c).Height(46).FillWidth().Padding(0, 12).Gap(4).AlignItems(native.Center).Children(func() {
			for _, tab := range []struct {
				id, label string
				glyph     *native.SVG
			}{{"files", "文件", fileIcon}, {"history", "会话历史", historyTabIcon}, {"git", "Git", gitIcon}} {
				b := panelTabButton(c.Key(tab.id), tab.label, a.rightTab == tab.id)
				b.Children(func() { native.Icon(c, tab.glyph).Size(18, 18) })
				if b.Clicked() {
					a.rightTab = tab.id
					if root := a.activeWorkspace; root != "" {
						w := a.workspace(root)
						if !a.gitWorkspaceVisible(w) {
							a.cancelGitRefresh(w)
						}
					}
					if tab.id == "history" {
						a.openThreadList()
					}
				}
			}
			native.Spacer(c)
			a.sidebarToggle(c, false)
		})
		if a.rightTab == "history" {
			native.Column(c).FillWidth().Padding(12).Gap(12).Children(func() {
				native.Text(c, "会话历史").FontSize(13).Bold()
				native.TextInput(c, &a.historyQuery).Label("搜索历史").Placeholder("搜索已加载的历史").FillWidth().FontSize(12)
				a.historyScopeControl(c)
			})
			a.historyBrowser(c)
			return
		}
		root := a.workspaceRoot(a.workspaceCwd())
		if root == "" || root == "." {
			native.Text(c, "选择项目以浏览文件").FontSize(12).TextColor(t.TextMuted).Padding(16)
			return
		}
		w := a.workspace(root)
		if a.rightTab == "git" {
			a.gitPanel(c, w)
		} else {
			a.filesPanel(c, w)
		}
	})
}
func (a *App) filesPanel(c *native.Context, w *workspacePage) {
	t := c.Theme()
	a.startWatcher(w)
	if _, ok := w.children[w.root]; !ok && !w.loading[w.root] && w.error == "" {
		a.loadDirectory(w, w.root)
	}
	for path, expanded := range w.expanded {
		if _, loaded := w.children[path]; expanded && !loaded && !w.loading[path] && w.error == "" {
			a.loadDirectory(w, path)
		}
	}
	native.Row(c).Height(40).FillWidth().Padding(0, 14).Gap(6).AlignItems(native.Center).Children(func() {
		native.Text(c, projectName(w.root)).FontSize(13).Bold().SingleLine().Shrink(1).MinWidth(0)
		branch := w.git.Branch
		if worktree := a.worktreeFor(w.root); branch == "" && worktree != nil {
			branch = worktree.Label()
		}
		if branch != "" {
			native.Text(c, branch).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		}
		native.Spacer(c)
		b := quietIconButton(c, "全部折叠").Size(26, 26)
		b.Children(func() { native.Icon(c, collapseAllIcon).Size(14, 14) })
		if b.Clicked() {
			w.expanded = map[string]bool{w.root: true}
			if filepath.Dir(w.selected) != w.root {
				w.selected = ""
			}
		}
		b = quietIconButton(c, "刷新文件").Size(26, 26).Disabled(w.loading[w.root])
		b.Children(func() { native.Icon(c, refreshProjectIcon).Size(14, 14) })
		if b.Clicked() {
			w.error = ""
			for path := range w.expanded {
				a.loadDirectory(w, path)
			}
		}
	})
	native.Column(c).FillWidth().Padding(4, 12, 10, 12).Gap(6).Children(func() {
		native.Row(c).FillWidth().Gap(4).AlignItems(native.Center).Children(func() {
			searchField(c).Grow(1).Children(func() {
				input := native.TextInputBase(c, &w.query).Label("搜索文件内容").Placeholder("搜索文件内容").Grow(1).MinWidth(0).FontSize(12).Padding(4)
				if w.searchFocus {
					input.Focus()
					input.SetTextSelection(0, len([]rune(w.query)))
					w.searchFocus = false
				}
				composing := input.Composing()
				input.OnTextKey(func(k native.TextKey) bool {
					if composing {
						return false
					}
					if k.Key == native.KeyEscape {
						w.query = ""
						k.SetText("")
						return true
					}
					count := len(w.contentSearch.Matches)
					if k.Key == native.KeyEnter && !w.searchLoading && w.searchKey() == w.searchQuery && count > 0 {
						a.openSearchMatch(w, w.contentSearch.Matches[min(w.searchIndex, count-1)])
						return true
					}
					if count > 0 && (k.Key == native.KeyDown || k.Key == native.KeyUp) {
						delta := 1
						if k.Key == native.KeyUp {
							delta = -1
						}
						w.searchIndex = (w.searchIndex + delta + count) % count
						revealSearchMatch(w, w.searchIndex)
						return true
					}
					return false
				})
				searchClearButton(c, w)
				searchOptionButton(c, "区分大小写", "Aa", &w.searchCase)
				searchOptionButton(c, "全字匹配", "ab", &w.searchWord)
				searchOptionButton(c, "使用正则表达式", ".*", &w.searchRegex)
			})
			label := "展开搜索筛选"
			if w.searchDetails {
				label = "收起搜索筛选"
			}
			b := quietIconButton(c.Key("toggle-search-details"), label).Size(24, 26)
			if b.Clicked() {
				w.searchDetails = !w.searchDetails
				if w.searchDetails {
					label = "收起搜索筛选"
				} else {
					label = "展开搜索筛选"
				}
				b.Label(label)
			}
			if !w.searchDetails && (strings.TrimSpace(w.searchInclude) != "" || strings.TrimSpace(w.searchExclude) != "" || w.searchIgnored) {
				b.TextColor(t.Accent)
			}
			filterProgress := animateDisclosure(b, "filters-open", w.searchDetails)
			b.Children(func() {
				native.Icon(c, chevron).Size(14, 14).Rotate(-90 * (1 - filterProgress))
			})
		})
		collapsibleSection(c, "search-filters", w.searchDetails, func() {
			native.Column(c).FillWidth().Gap(6).Children(func() {
				searchField(c).Children(func() {
					native.TextInputBase(c, &w.searchInclude).Label("包含的文件").Placeholder("包含的文件").
						Grow(1).MinWidth(0).FontSize(12).Padding(4)
				})
				searchField(c).Children(func() {
					native.TextInputBase(c, &w.searchExclude).Label("排除的文件").Placeholder("排除的文件").
						Grow(1).MinWidth(0).FontSize(12).Padding(4)
					label := "使用排除设置和忽略文件"
					b := native.ButtonBase(c).Label(label).Tooltip(label).Size(20, 20).Radius(4).TextColor(t.TextMuted)
					selectedButtonFace(c, b, !w.searchIgnored, 4, 0)
					b.Children(func() { native.Icon(c, searchIgnoreIcon).Size(13, 13) })
					if b.Clicked() {
						w.searchIgnored = !w.searchIgnored
					}
				})
			})
		})
	})
	a.searchFiles(w)
	if w.error != "" {
		native.Text(c, w.error).FontSize(11).TextColor(t.Danger).MaxLines(3).Padding(8, 12)
	}
	if w.directoryProgress == nil {
		w.directoryProgress = map[string]float32{}
	}
	for path := range w.directoryProgress {
		if _, exists := w.expanded[path]; !exists {
			w.expanded[path] = false
		}
	}
	directoryMotions := make(map[string]native.Element, len(w.expanded))
	for path := range w.expanded {
		target := float32(0)
		if w.expanded[path] && len(w.children[path]) > 0 {
			target = 1
		}
		if treeAnimationSize(w, path) > 200 {
			w.directoryProgress[path] = target
			continue
		}
		motion := native.Box(c.Key("directory-motion:"+path)).Size(0, 0)
		directoryMotions[path] = motion
		w.directoryProgress[path] = motion.AnimateTo("open", target, disclosureAnimation)
	}
	rows := a.flattenTree(w)
	var matches []workspace.ContentMatch
	var resultRows []searchRow
	searchMotions := make(map[string]native.Element)
	searchProgress := make(map[string]float32)
	list := &w.list
	listContext := c.Key("file-tree-list")
	searching := strings.TrimSpace(w.query) != ""
	if searching {
		rows = nil
		list = &w.contentList
		listContext = c.Key("file-content-search-list")
		matches = w.contentSearch.Matches
		if !w.searchLoading && len(matches) == 0 {
			native.Text(c, "没有匹配的内容").FontSize(12).TextColor(t.TextMuted).Padding(16)
		} else if len(matches) > 0 {
			contentSearchSummary(c, w, matches)
		}
		for _, match := range matches {
			if _, exists := searchProgress[match.Path]; exists {
				continue
			}
			motion := native.Box(c.Key("search-motion:"+match.Path)).Size(0, 0)
			searchMotions[match.Path] = motion
			searchProgress[match.Path] = animateDisclosure(motion, "open", !w.searchCollapsed[match.Path])
		}
		resultRows = contentSearchRows(matches, w.searchCollapsed, searchProgress)
		if w.contentSearch.Truncated {
			native.Text(c, "结果不完整，请缩小搜索范围").FontSize(11).TextColor(t.TextMuted).Padding(8, 12)
		}
	}
	w.entries = rows
	if w.reveal && w.query == "" {
		for i, row := range rows {
			if row.Path == w.selected {
				w.list.ScrollTo(i, native.Center)
				w.reveal = false
				break
			}
		}
	}
	if w.rowHandles == nil {
		w.rowHandles = map[string]*native.Handle{}
	}
	count := len(rows)
	if searching {
		count = len(resultRows)
	}
	list.Key = func(i int) any {
		if searching {
			r := resultRows[i]
			if r.match < 0 {
				return "file\x00" + r.path
			}
			m := matches[r.match]
			return m.Path + "\x00" + fmt.Sprint(m.Line)
		}
		return rows[i].Path
	}
	native.List(listContext, list, count, func(i int) {
		if searching {
			if r := resultRows[i]; r.match < 0 {
				a.searchFileRow(c, w, r, searchMotions[r.path])
			} else {
				row := a.contentMatchRow(c, w, matches[r.match], r.match)
				if progress := searchProgress[r.path]; progress < 1 {
					row.Height(24 * max(0, progress)).ClipY().Opacity(progress)
				}
			}
			return
		}
		row := rows[i]
		rc := c.Key(row.Path)
		b := treeButton(rc, "文件树 "+row.Path).FillWidth().Height(32*row.Scale).Radius(5).Padding(0, 10, 0, float32(12+row.Depth*14))
		dragFiles(b, []string{row.Path})
		if row.Scale < 1 {
			b.ClipY().Opacity(row.Scale)
		}
		if w.rowHandles[row.Path] == nil {
			w.rowHandles[row.Path] = &native.Handle{}
		}
		b.Bind(w.rowHandles[row.Path])
		b.ContextMenu(a.fileMenu(w.root, row.Path, false, row.Dir && !row.Link))
		if w.focusPath == row.Path {
			b.Focus()
			w.focusPath = ""
		}
		b.HandleInput(func(ev native.InputEvent) bool {
			if ev.Kind != native.InputKeyDown {
				return false
			}
			next := i
			switch ev.Key {
			case native.KeyDown:
				next = min(len(rows)-1, i+1)
			case native.KeyUp:
				next = max(0, i-1)
			case native.KeyRight:
				if row.Dir && !w.expanded[row.Path] {
					a.toggleFileDirectory(w, row.Path)
					return true
				}
				if row.Dir && i+1 < len(rows) && rows[i+1].Depth > row.Depth {
					next = i + 1
				}
			case native.KeyLeft:
				if row.Dir && w.expanded[row.Path] {
					a.toggleFileDirectory(w, row.Path)
					return true
				}
				for j := i - 1; j >= 0; j-- {
					if rows[j].Depth < row.Depth {
						next = j
						break
					}
				}
			default:
				return false
			}
			w.selected, w.focusPath = rows[next].Path, rows[next].Path
			list.ScrollTo(next, native.Center)
			return true
		})
		if w.selected == row.Path {
			b.Background(cmp.Or(a.activePalette(t).pickerSelected, t.SurfacePressed))
		}
		if b.Clicked() {
			w.selected = row.Path
			if row.Dir {
				a.toggleFileDirectory(w, row.Path)
				if motion := directoryMotions[row.Path]; motion.Valid() {
					w.directoryProgress[row.Path] = animateDisclosure(motion, "open", w.expanded[row.Path] && len(w.children[row.Path]) > 0)
				}
				c.AnimationFrame()
			} else {
				a.openFile(w.root, row.Path)
				if d := a.currentFile(); d != nil {
					d.focusOwner = w.rowHandles[row.Path]
				}
			}
		}
		b.Children(func() {
			native.Row(rc).Fill().Gap(6).AlignItems(native.Center).Children(func() {
				if row.Dir {
					g := native.Icon(rc, chevron).Size(12, 12)
					progress := animateDisclosure(g, "directory-open", w.expanded[row.Path])
					g.Rotate(-90 * (1 - progress))
					native.Icon(rc, folderIcon).Size(14, 14)
				} else {
					native.Box(rc).Width(12)
					native.Icon(rc, fileIcon).Size(14, 14).TextColor(t.TextMuted)
				}
				col := t.Text
				if row.Ignored {
					col = t.TextMuted
				}
				native.Text(rc, row.Name).FontSize(12).TextColor(col).SingleLine().Shrink(1).MinWidth(0)
				if row.Link {
					native.Text(rc, "↗").FontSize(11).TextColor(t.TextMuted)
				}
			})
		})

	}).FillWidth().Grow(1).MinHeight(0)
}
func (a *App) contentMatchRow(c *native.Context, w *workspacePage, m workspace.ContentMatch, i int) native.Element {
	t := c.Theme()
	key := m.Path + "\x00" + fmt.Sprint(m.Line)
	rc := c.Key(key)
	b := treeButton(rc, fmt.Sprintf("搜索结果 %s:%d", m.Path, m.Line)).FillWidth().Height(24).Radius(5).Padding(0, 10, 0, 34)
	if w.rowHandles[key] == nil {
		w.rowHandles[key] = &native.Handle{}
	}
	b.Bind(w.rowHandles[key])
	b.ContextMenu(a.fileMenu(w.root, m.Path, false, false))
	b.HandleInput(func(ev native.InputEvent) bool {
		if ev.Kind != native.InputKeyDown {
			return false
		}
		next := i
		switch ev.Key {
		case native.KeyDown:
			next = min(len(w.contentSearch.Matches)-1, i+1)
		case native.KeyUp:
			next = max(0, i-1)
		case native.KeyEnter:
			a.openSearchMatch(w, m)
			return true
		default:
			return false
		}
		w.searchIndex = next
		revealSearchMatch(w, next)
		return true
	})
	if w.searchIndex == i && w.selected == m.Path {
		b.Background(t.SurfacePressed)
	}
	b.Children(func() {
		runes := []rune(m.Text)
		var spans []native.Span
		at := 0
		for _, r := range m.Ranges {
			if r.Start < at || r.End > len(runes) {
				continue
			}
			if r.Start > at {
				spans = append(spans, native.Span{Text: string(runes[at:r.Start]), Color: t.TextMuted})
			}
			spans = append(spans, native.Span{Text: string(runes[r.Start:r.End]), Color: t.Text, Background: t.Accent.Alpha(.25), Radius: 2})
			at = r.End
		}
		if at < len(runes) {
			spans = append(spans, native.Span{Text: string(runes[at:]), Color: t.TextMuted})
		}
		native.Row(rc).Fill().AlignItems(native.Center).Children(func() {
			native.RichText(rc, spans...).FontSize(11).SingleLine().Grow(1).MinWidth(0)
		})
	})
	if b.Clicked() {
		w.selected, w.searchIndex = m.Path, i
		a.openSearchMatch(w, m)
		if d := a.currentFile(); d != nil {
			d.focusOwner = w.rowHandles[key]
		}
	}
	return b
}

var searchIgnoreIcon = icon(`<path d="M4 5h16l-6 7.5V18l-4 2v-7.5z"/>`)

// searchClearIcon is the filled "clear field" glyph: the even-odd rule
// cuts the x out of the disc, so the hole takes the face's color.
var searchClearIcon = icon(`<path fill="currentColor" stroke="none" fill-rule="evenodd" d="M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18ZM9.5 16.6 12 14.1 14.5 16.6 16.6 14.5 14.1 12 16.6 9.5 14.5 7.4 12 9.9 9.5 7.4 7.4 9.5 10.6 12 7.4 14.5Z"/>`)

// searchRow is one row of the grouped content results: a file heading
// (match < 0) carrying the file's match count, or a matching line.
type searchRow struct {
	path  string
	match int
	count int
}

func contentSearchRows(matches []workspace.ContentMatch, collapsed map[string]bool, motion ...map[string]float32) []searchRow {
	var rows []searchRow
	heading := -1
	for i, m := range matches {
		if heading < 0 || rows[heading].path != m.Path {
			heading = len(rows)
			rows = append(rows, searchRow{path: m.Path, match: -1})
		}
		rows[heading].count += m.Count()
		if !collapsed[m.Path] || len(motion) > 0 && motion[0][m.Path] > 0 {
			rows = append(rows, searchRow{path: m.Path, match: i})
		}
	}
	return rows
}

// revealSearchMatch selects match i, expanding its file and scrolling to it.
func revealSearchMatch(w *workspacePage, i int) {
	m := w.contentSearch.Matches[i]
	delete(w.searchCollapsed, m.Path)
	w.selected = m.Path
	for row, r := range contentSearchRows(w.contentSearch.Matches, w.searchCollapsed) {
		if r.match == i {
			w.contentList.ScrollTo(row, native.Center)
			break
		}
	}
}

// searchField paints a text-input face around its children so small
// controls can sit inside the field, like a search box's match toggles.
func searchField(c *native.Context) native.Element {
	t := c.Theme()
	e := native.Row(c).FillWidth().MinWidth(0).Gap(2).AlignItems(native.Center).
		Padding(2, 4).Radius(t.Radius).Clip().Background(t.Surface)
	border := t.Border
	switch {
	case e.FocusWithin():
		border = t.Accent
	case e.Hovered():
		border = t.Border.Mix(t.Text, 0.25)
	}
	e.Border(1, border)
	return e
}

// searchClearButton empties the query and refocuses the input; it only
// shows while there is something to clear.
func searchClearButton(c *native.Context, w *workspacePage) {
	if w.query == "" {
		return
	}
	b := quietIconButton(c.Key("clear-search"), "清除搜索内容").Size(20, 20)
	b.Children(func() { native.Icon(c, searchClearIcon).Size(13, 13) })
	if b.Clicked() {
		w.query = ""
		w.searchFocus = true
	}
}

func searchOptionButton(c *native.Context, label, glyph string, on *bool) {
	b := native.ButtonBase(c.Key(label)).Label(label).Tooltip(label).Size(20, 20).Radius(4).TextColor(c.Theme().TextMuted)
	selectedButtonFace(c, b, *on, 4, 0)
	b.Children(func() { native.Text(c, glyph).FontSize(11) })
	if b.Clicked() {
		*on = !*on
	}
}

func contentSearchSummary(c *native.Context, w *workspacePage, matches []workspace.ContentMatch) {
	t := c.Theme()
	total, files := 0, 0
	for i, m := range matches {
		total += m.Count()
		if i == 0 || matches[i-1].Path != m.Path {
			files++
		}
	}
	allCollapsed := len(w.searchCollapsed) > 0
	for _, m := range matches {
		if !w.searchCollapsed[m.Path] {
			allCollapsed = false
			break
		}
	}
	native.Row(c.Key("content-search-summary")).FillWidth().Height(24).Padding(0, 8, 0, 14).Gap(4).AlignItems(native.Center).Children(func() {
		native.Text(c, fmt.Sprintf("%d 个结果，位于 %d 个文件", total, files)).FontSize(11).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
		label := "全部折叠结果"
		if allCollapsed {
			label = "全部展开结果"
		}
		b := quietIconButton(c, label).Size(22, 22)
		if b.Clicked() {
			w.searchCollapsed = nil
			if !allCollapsed {
				w.searchCollapsed = map[string]bool{}
				for _, m := range matches {
					w.searchCollapsed[m.Path] = true
				}
			}
			allCollapsed = !allCollapsed
			label = "全部折叠结果"
			if allCollapsed {
				label = "全部展开结果"
			}
			b.Label(label)
		}
		progress := animateDisclosure(b, "open", !allCollapsed)
		b.Children(func() {
			native.Icon(c, chevron).Size(12, 12).Rotate(-90 * (1 - progress))
		})
	})
}

func (a *App) searchFileRow(c *native.Context, w *workspacePage, r searchRow, motions ...native.Element) {
	t := c.Theme()
	rc := c.Key("file\x00" + r.path)
	rel, _ := filepath.Rel(w.root, r.path)
	dir, name := filepath.Split(rel)
	dir = strings.TrimSuffix(dir, string(filepath.Separator))
	collapsed := w.searchCollapsed[r.path]
	label := "折叠 " + rel
	if collapsed {
		label = "展开 " + rel
	}
	b := treeButton(rc, label).FillWidth().Height(26).Radius(5).Padding(0, 8)
	b.ContextMenu(a.fileMenu(w.root, r.path, false, false))
	if b.Clicked() {
		if w.searchCollapsed == nil {
			w.searchCollapsed = map[string]bool{}
		}
		collapsed = !collapsed
		w.searchCollapsed[r.path] = collapsed
		if collapsed {
			label = "展开 " + rel
		} else {
			label = "折叠 " + rel
		}
		b.Label(label)
		if len(motions) > 0 && motions[0].Valid() {
			animateDisclosure(motions[0], "open", !collapsed)
		}
		c.AnimationFrame()
	}
	progress := animateDisclosure(b, "search-file-open", !collapsed)
	b.Children(func() {
		native.Row(rc).Fill().Gap(5).AlignItems(native.Center).Children(func() {
			native.Icon(rc, chevron).Size(12, 12).Rotate(-90 * (1 - progress))
			native.Icon(rc, fileIcon).Size(14, 14).TextColor(t.TextMuted)
			native.Text(rc, name).FontSize(12).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
			native.Text(rc, dir).FontSize(10).TextColor(t.TextMuted.Alpha(.8)).SingleLine().Grow(1).MinWidth(0)
			native.Box(rc.Key("count")).MinWidth(22).Height(18).Padding(0, 5).Radius(9).Background(t.TextMuted.Alpha(.1)).
				AlignItems(native.Center).Justify(native.Center).Children(func() {
				native.Text(rc, fmt.Sprint(r.count)).FontSize(10).TextColor(t.TextMuted).SingleLine()
			})
		})
	})
}

func (a *App) openSearchMatch(w *workspacePage, m workspace.ContentMatch) {
	a.openFile(w.root, m.Path)
	if d := a.currentFile(); d != nil && d.root == w.root && d.path == m.Path {
		d.pendingLocation = &fileLocation{line: m.Line, column: m.Col}
		d.pinFrame = 0
	}
}
func (a *App) fileView(c *native.Context, d *filePage, left, right bool) {
	if d.pinFrame != 0 {
		d.scroll.Y = d.pinY
		d.pinFrame--
		c.AnimationFrame()
	}
	viewY := d.scroll.Y
	if !d.loaded && !d.loading && d.err == "" {
		if d.kind == "file" || d.turnReview != nil {
			a.loadFile(d)
		} else if strings.HasPrefix(d.compare, "提交 ") {
			d.gitCommit = strings.TrimPrefix(d.compare, "提交 ")
			a.gitLoadCommit(d, gitview.CommitInfo{Hash: d.gitCommit, Subject: gitCommitLabel(d.gitCommit)})
		} else if d.compare == "外部更改" {
			d.err = "比较快照未保存，请从原文件重新打开外部更改比较"
		} else {
			a.refreshGit(a.workspace(d.root))
		}
	}
	t := c.Theme()
	native.Column(c).Grow(1).FillHeight().MinWidth(0).Background(t.Background).Children(func() {
		a.sessionTabs(c, left, right)
		a.fileExitNotice(c)
		if d.kind == "diff" && d.compare != "外部更改" {
			a.gitDiffToolbar(c.Key("git-diff-toolbar"), d)
		}
		if d.kind == "file" && d.gitConflict != nil {
			a.gitConflictToolbar(c.Key("conflict"), d)
		}
		if d.compare == "外部更改" {
			source := a.files[d.comparisonSource]
			if source != nil && source.buffer != nil && source.buffer.Version() != d.comparisonVersion {
				d.comparisonStale = true
			}
			native.Row(c).FillWidth().Padding(8, 14).Gap(8).Background(t.Surface).Children(func() {
				label := "本地编辑 → 磁盘版本"
				if d.comparisonStale {
					label += " · 比较已过时"
				}
				native.Text(c, label).FontSize(11).TextColor(t.TextMuted).Grow(1)
				if source != nil {
					if actionButton(c, "刷新比较").Disabled(d.loading).Clicked() {
						a.loadDiskComparison(source, d)
					}
				}
			})
		}
		native.Row(c).Height(34).FillWidth().Padding(0, 16).Gap(8).AlignItems(native.Center).Children(func() {
			path, _ := d.diffOpenTarget()
			rel, _ := filepath.Rel(d.root, path)
			if d.turnReview != nil {
				names := make([]string, len(d.turnReview.files))
				for i, f := range d.turnReview.files {
					names[i] = a.relativeChangePath(f.Path)
				}
				current := names[d.turnReview.selected]
				native.Select(c, &current, names).Label("本轮修改的文件").Grow(1).MinWidth(0).FontSize(11)
				if current != names[d.turnReview.selected] {
					for i, name := range names {
						if name == current {
							d.turnReview.selected = i
							a.setTurnReviewFile(d)
							break
						}
					}
				}
			} else {
				native.ScrollHorizontal(c.Key("file-path")).Grow(1).FillHeight().MinWidth(0).Children(func() {
					native.Row(c).FillHeight().Gap(5).AlignItems(native.Center).Children(func() {
						native.Text(c, rel).FontSize(11).TextColor(t.TextMuted).SingleLine()
						if d.kind == "diff" && d.gitReview != nil {
							changeCounts(c, d.gitReview.Added, d.gitReview.Deleted, true)
						}
					})
				})
			}
			if d.turnReview != nil && d.gitReview != nil {
				f := d.turnReview.files[d.turnReview.selected]
				native.Row(c).Gap(5).Shrink(0).AlignItems(native.Center).Children(func() {
					changeCounts(c, d.gitReview.Added, d.gitReview.Deleted, f.Exact)
				})
			}
			for _, s := range a.sessions {
				if len(s.State.PendingInteractions()) > 0 {
					if actionButton(c, "返回待处理会话").Clicked() {
						a.selectSession(s)
					}
					break
				}
			}
			if d.kind == "diff" && d.compare != "外部更改" && d.gitReview != nil {
				native.Row(c).Gap(4).Shrink(0).AlignItems(native.Center).Children(func() {
					a.gitDiffControls(c, d)
				})
			}
			if d.kind != "diff" {
				if actionButton(c, "保存").Disabled(!d.dirty() || d.saving || d.file.Reason != "").Clicked() {
					a.saveFile(d, false, false, nil)
				}
			}
		})
		if d.conflict || d.missing {
			native.Row(c).FillWidth().Padding(10, 14).Gap(8).AlignItems(native.Center).Background(t.Surface).Children(func() {
				message := "文件已在外部更改"
				if d.missing {
					message = "文件已在外部删除"
				}
				native.Text(c, message).FontSize(11).TextColor(t.Danger)
				native.Spacer(c)
				if !d.missing {
					if actionButton(c, "查看磁盘版本").Clicked() {
						a.showDiskComparison(d)
					}
					if actionButton(c, "重新载入").Clicked() {
						a.confirmFile(d, "重新载入", "将丢弃此文件的本地未保存编辑。", func() { a.reloadFile(d) })
					}
				}
				label := "覆盖磁盘"
				if d.missing {
					label = "重新创建文件"
				}
				if actionButton(c, label).Clicked() {
					a.confirmOverwrite(d)
				}
			})
		}
		if d.err != "" {
			native.Row(c).FillWidth().Padding(8, 14).Gap(8).Children(func() {
				native.Text(c, d.err).FontSize(11).TextColor(t.Danger).MaxLines(3).Grow(1)
				if actionButton(c, "重试").Clicked() {
					if d.kind == "diff" {
						d.err = ""
						if d.compare == "外部更改" {
							if source := a.files[d.comparisonSource]; source != nil {
								a.loadDiskComparison(source, d)
							}
						} else {
							a.refreshGit(a.workspace(d.root))
						}
					} else if d.loaded && d.dirty() && d.file.Reason == "" {
						a.saveFile(d, false, false, nil)
					} else {
						a.loadFile(d)
					}
				}
				if actionButton(c, "在外部打开").Clicked() && a.win != nil {
					if err := mygo.Shell.OpenPath(d.path); err != nil {
						d.err = err.Error()
					}
				}
			})
		}
		if !d.loaded {
			if d.err == "" {
				native.Text(c, "正在打开文件…").FontSize(12).TextColor(t.TextMuted).Padding(20)
			}
			native.Spacer(c)
			return
		}
		a.observeFile(d)
		if !d.highlighting && (d.contentWidth == 0 || d.highlightVersion != d.buffer.Version()) && time.Since(d.changedAt) > 200*time.Millisecond {
			a.requestHighlight(d)
		}
		if d.file.Reason != "" && (d.kind != "diff" || d.compare == "外部更改") {
			native.Row(c).FillWidth().Padding(4, 14).Gap(8).AlignItems(native.Center).Children(func() {
				native.Text(c, d.file.Reason).FontSize(11).TextColor(t.TextMuted).Grow(1)
				if d.kind == "file" && actionButton(c, "在外部打开").Clicked() && a.win != nil {
					if err := mygo.Shell.OpenPath(d.path); err != nil {
						d.err = err.Error()
					}
				}
			})
		}
		if d.kind == "diff" && d.compare != "外部更改" && d.turnReview == nil {
			for _, source := range a.files {
				if source.root == d.root && source.path == d.path && source.dirty() {
					native.Text(c, "包含未保存编辑，当前差异基于磁盘").FontSize(11).TextColor(t.TextMuted).Padding(4, 14)
					break
				}
			}
		}
		available, _ := c.Size()
		if left {
			available -= a.sidebarWidth + 1
		}
		if right {
			total, _ := c.Size()
			if left {
				total -= a.sidebarWidth + 1
			}
			available -= a.effectiveRightPanelWidth(total) + 1
		}
		if d.kind == "diff" && d.gitSplit && d.gitReview != nil && len(d.gitReview.Hunks) > 0 && available >= 520 {
			a.gitSplitView(c, d, available)
			return
		}
		snap := d.buffer.Snapshot()
		d.centerInitialDiff(false, d.scroll.Bounds(c).H)
		viewY = d.scroll.Y
		if d.gitInitialCenter[0] {
			c.AnimationFrame()
		}
		d.resolveFileLocation(d.scroll.Bounds(c).H)
		if d.pendingLocation != nil {
			c.AnimationFrame()
		}
		spec := a.activeFileSearch(d)
		stale := !d.findScanned || d.findKey != spec.key || d.findBuffer != d.buffer || d.findVersion != snap.Version()
		if spec.key == "" {
			d.cancelFind()
			d.findMatches, d.findScanned, d.findKey = nil, false, ""
			d.findIndex, d.findResult = -1, ""
		} else if stale && d.findCancel == nil && time.Since(d.changedAt) > 150*time.Millisecond {
			a.scanFileMatches(d, spec)
		}
		width := max(available, d.contentWidth)
		palette := a.activeEditorPalette(t)
		search := a.activePalette(t).searchFor(palette.accent, .45)
		native.ScrollHorizontal(c.Key("editor-horizontal")).TrackScroll(&d.hscroll).FillWidth().Grow(1).MinHeight(0).Background(palette.background).Children(func() {
			area := palette.area(native.TextAreaBufferBase(c.Key("source"), d.buffer)).Label("文件内容 "+d.path).Font(a.codeFontFamily()).FontSize(13).FixedLineHeight(20).Width(width).Shrink(0).FillHeight().Padding(8, 24, 8, 58).NoWrap().TrackLines(&d.lines).TrackScroll(&d.scroll).ReadOnly(d.kind != "file" || d.file.Reason != "")
			var lineAtClick func() int
			switch {
			case d.kind == "file":
				lineAtClick = func() int {
					_, caret := area.TextAnchorCaret()
					return snap.LineAt(caret)
				}
			case d.kind == "diff" && len(d.gitRows) > 0:
				// Single-file diffs carry the file line per row; synthetic
				// paths like commit views have nothing meaningful to copy.
				if info, err := os.Stat(d.path); err == nil && info.Mode().IsRegular() {
					lineAtClick = func() int {
						_, caret := area.TextAnchorCaret()
						index := snap.LineAt(caret)
						if index < len(d.gitRows) {
							if number := d.gitRows[index].next; number > 0 {
								return number - 1
							}
							if number := d.gitRows[index].old; number > 0 {
								return number - 1
							}
						}
						return index
					}
				}
			}
			area.ContextMenu(a.fileMenuAt(d.root, d.path, true, false, lineAtClick))
			a.focusPressedPane(area)
			if d.pendingSelection != nil {
				area.SetTextSelection(d.pendingSelection[0], d.pendingSelection[1])
				d.pendingSelection = nil
				d.resync = false
			} else if d.resync {
				area.SetTextSelection(d.anchor, d.caret)
				d.pinFrame, d.pinY = 2, viewY
				c.AnimationFrame()
				d.resync = false
			}
			previousCaret := d.caret
			d.anchor, d.caret = area.TextAnchorCaret()
			d.matchBrackets(snap)
			if d.kind == "diff" && d.gitReview != nil && d.caret != previousCaret {
				line := snap.LineAt(d.caret)
				if line < len(d.gitRows) && d.gitRows[line].hunk >= 0 {
					d.gitHunk = d.gitRows[line].hunk
				}
			}
			if d.focus && a.paneIsFocused() {
				if d.focusOwner == nil || d.focusOwner.Focused(c) {
					area.Focus()
				}
				d.focus = false
			}
			if d.highlightVersion == d.buffer.Version() {
				first := max(0, int(d.scroll.Y/20)-2)
				last := first + 100
				lo, _ := snap.LineRange(first)
				_, hi := snap.LineRange(min(last, snap.LineCount()-1))
				area.TextRanges(palette.ranges(d.highlights, lo, hi)...)
			}
			composing := area.Composing()
			area.OnPaste(func(text string) bool {
				start, end := min(d.anchor, d.caret), max(d.anchor, d.caret)
				selectedBytes := len(d.buffer.Slice(start, end))
				if d.buffer.ByteLen()-selectedBytes+len(text) > 4<<20 {
					d.err = "粘贴超过 4 MiB，已拒绝本次粘贴"
					return true
				}
				return false
			})
			area.HandleInput(func(ev native.InputEvent) bool {
				if ev.Kind == native.InputCommand && (ev.Text == "undo" || ev.Text == "redo") && d.kind == "file" {
					d.undoEdit(ev.Text == "redo")
					return true
				}
				return false
			})
			area.OnTextKey(a.paneTextKey(func(k native.TextKey) bool {
				if composing {
					return false
				}
				if k.Mods == native.Cmd && k.Key == native.KeyS {
					a.saveFile(d, false, false, nil)
					return true
				}
				if k.Mods == native.Cmd && k.Key == native.KeyF {
					if d.anchor != d.caret {
						if text := d.buffer.Slice(d.anchor, d.caret); text != "" && len(text) < 200 && !strings.Contains(text, "\n") {
							d.query = text
						}
					}
					d.findOpen = true
					d.findFocus = true
					return true
				}
				if k.Mods == native.Cmd && k.Key == native.KeyW {
					a.closeCurrentPage()
					return true
				}
				if k.Key == native.KeyZ && k.Mods&native.Cmd != 0 {
					d.undoEdit(k.Mods&native.Shift != 0)
					k.SetText(d.buffer.String())
					d.observed = d.buffer.Snapshot()
					return true
				}
				return false
			}))
			area.Draw(func(p *native.Painter, r native.Rect) {
				if d.kind == "diff" {
					first := max(0, d.lines.At(d.scroll.Y-8)-1)
					last := min(len(d.gitRows), d.lines.At(d.scroll.Y+r.H-8)+2)
					for i := first; i < last; i++ {
						row := d.gitRows[i]
						var color native.Color
						if row.kind == '+' {
							color = palette.addedBackground
						}
						if row.kind == '-' {
							color = palette.deletedBackground
						}
						if color.A != 0 {
							y := r.Y + 8 + d.lines.Top(i) - d.scroll.Y
							height := d.lines.Top(i+1) - d.lines.Top(i)
							p.Fill(native.Rect{X: r.X + 48, Y: y, W: r.W - 48, H: height}, color, 0)
							if row.hunk == d.gitHunk {
								p.Fill(native.Rect{X: r.X + 50, Y: y, W: 2, H: height}, palette.accent, 0)
							}
						}
					}
				}
				a.paintWordMatches(p, r, d, snap, palette)
				a.paintFileMatches(p, r, d, snap, search)
				a.paintBracketPair(p, r, d, snap, palette)
			})
			area.DrawOver(func(p *native.Painter, r native.Rect) {
				first := max(0, d.lines.At(d.scroll.Y-8)-1)
				last := min(snap.LineCount(), d.lines.At(d.scroll.Y+r.H-8)+2)
				for i := first; i < last; i++ {
					y := r.Y + 8 + d.lines.Top(i) - d.scroll.Y
					if y >= r.Y && y < r.Y+r.H {
						number := i + 1
						if d.kind == "diff" {
							if i >= len(d.gitRows) {
								continue
							}
							row := d.gitRows[i]
							number = row.next
							if row.kind == '-' {
								number = row.old
							}
							drawDiffLineNumber(p, r.X+10, y, number, row.kind, palette.lineTheme())
							continue
						}
						if number > 0 {
							p.Text(r.X+10, y, fmt.Sprint(number), 11, palette.muted)
						}
					}
				}
			})
			if d.findOpen {
				native.Row(c).Attach(native.AnchorTopRight, native.AnchorTopRight).Top(6).Right(16).Padding(4, 10).Gap(8).AlignItems(native.Center).
					Background(t.Background).Border(1, t.Border.Alpha(.7)).Radius(t.Radius).Shadow(0, 2, 10, 0, native.RGBA(0, 0, 0, .08)).Children(func() {
					searchField(c).Width(280).Children(func() {
						input := native.TextInputBase(c, &d.query).Label("文件内查找").Placeholder("查找文本").Grow(1).MinWidth(0).FontSize(12).Padding(4)
						if d.findFocus {
							input.Focus()
							input.SetTextSelection(0, len([]rune(d.query)))
							d.findFocus = false
						}
						composing := input.Composing()
						input.OnTextKey(func(k native.TextKey) bool {
							if composing {
								return false
							}
							if k.Key == native.KeyEscape {
								d.findOpen = false
								d.cancelFind()
								d.focus = true
								return true
							}
							if k.Key == native.KeyEnter {
								a.findInFile(d, k.Mods&native.Shift != 0)
								return true
							}
							return false
						})
						native.Text(c, d.findResult).FontSize(11).TextColor(t.TextMuted).Padding(0, 4)
					})
					caseButton := native.ButtonBase(c).Label("区分大小写").Size(28, 26).
						Tooltip("区分大小写").Radius(8).TextColor(t.TextMuted)
					selectedButtonFace(c, caseButton, d.findCase, 8, 0)
					caseButton.Children(func() { native.Text(c, "Aa").FontSize(11) })
					if caseButton.Clicked() {
						d.findCase = !d.findCase
						a.findInFile(d, false)
					}
					if actionButton(c, "下一个").Clicked() {
						a.findInFile(d, false)
					}
					if quietIconButton(c, "关闭查找").Size(26, 26).Children(func() { native.Icon(c, tabCloseIcon).Size(12, 12) }).Clicked() {
						d.findOpen = false
						d.cancelFind()
						d.focus = true
					}
				})
			}
		})
		native.Row(c).FillWidth().Height(27).Padding(0, 14).Gap(12).AlignItems(native.Center).BorderWidth(1, 0, 0, 0).BorderColor(t.Border.Alpha(.5)).Children(func() {
			state := "UTF-8"
			if d.file.BOM {
				state += " BOM"
			}
			if d.saving {
				state = "正在保存…"
			}
			native.Text(c, state).FontSize(10).TextColor(t.TextMuted)
			ending := "LF"
			if d.file.CRLF {
				ending = "CRLF"
			}
			native.Text(c, ending).FontSize(10).TextColor(t.TextMuted)
			native.Spacer(c)
			line := snap.LineAt(d.caret)
			start, _ := snap.LineRange(line)
			lang := strings.TrimPrefix(filepath.Ext(d.path), ".")
			native.Text(c, fmt.Sprintf("%s · %d 行 · %d:%d", lang, snap.LineCount(), line+1, d.caret-start+1)).FontSize(10).TextColor(t.TextMuted)
		})
	})
}
func (d *filePage) cancelFind() {
	if d.findCancel != nil {
		d.findCancel()
		d.findCancel = nil
		d.findGeneration++
	}
	d.findStep = 0
	if d.findResult == "正在查找…" {
		d.findResult = ""
	}
}

// findMatch is one keyword match in a file buffer, as rune offsets.
type findMatch struct{ start, end int }

// fileSearchSpec is the search whose matches the file view highlights: the
// find bar's query while it is open, else the workspace content search's.
type fileSearchSpec struct {
	key  string
	q    workspace.ContentQuery
	find bool
	step int // navigation the find bar asked for with this scan: -1, 0 or 1
}

// activeFileSearch returns the spec of the search currently applying to d.
func (a *App) activeFileSearch(d *filePage) fileSearchSpec {
	if d.findOpen {
		if strings.TrimSpace(d.query) == "" {
			return fileSearchSpec{}
		}
		return fileSearchSpec{
			key:  "f\x00" + d.query + "\x00" + fmt.Sprint(d.findCase),
			q:    workspace.ContentQuery{Text: d.query, MatchCase: d.findCase},
			find: true,
		}
	}
	w := a.workspaces[d.root]
	if w == nil || strings.TrimSpace(w.query) == "" {
		return fileSearchSpec{}
	}
	q := w.contentQuery()
	return fileSearchSpec{
		key: fmt.Sprintf("s\x00%v\x00%v\x00%v\x00%s", q.MatchCase, q.WholeWord, q.Regex, q.Text),
		q:   q,
	}
}

// fileMatchScanLimit caps the matches one scan keeps.
const fileMatchScanLimit = 20000

// scanBufferContentMatches finds every match of q in snap, in rune offsets.
// It may run on any goroutine.
func scanBufferContentMatches(ctx context.Context, snap native.TextSnapshot, q workspace.ContentQuery) ([]findMatch, error) {
	re, err := workspace.CompileContentQuery(q)
	if err != nil {
		return nil, err
	}
	text := snap.String()
	var matches []findMatch
	runes, at := 0, 0
	for _, m := range re.FindAllStringIndex(text, fileMatchScanLimit) {
		if m[0] == m[1] {
			continue
		}
		if q.WholeWord && !workspace.IsWholeWord(text, m[0], m[1]) {
			continue
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		runes += utf8.RuneCountInString(text[at:m[0]])
		start := runes
		runes += utf8.RuneCountInString(text[m[0]:m[1]])
		at = m[1]
		matches = append(matches, findMatch{start, runes})
	}
	return matches, nil
}

// scanFileMatches finds spec's matches in d's buffer in the background. When
// the spec came from the find bar the current match also advances: to the
// requested step, to the first match from the caret when the query changed,
// or staying put over a rescan of the same query.
func (a *App) scanFileMatches(d *filePage, spec fileSearchSpec) {
	d.cancelFind()
	if !a.canAsync() || d.buffer == nil {
		return
	}
	if spec.step != 0 {
		d.findStep = spec.step
	}
	buffer := d.buffer
	snapshot := buffer.Snapshot()
	d.findGeneration++
	generation := d.findGeneration
	ctx, cancel := a.projectContext(5 * time.Second)
	d.findCancel = cancel
	a.runBackgroundFileTaskContext(ctx, func() {
		defer cancel()
		matches, err := scanBufferContentMatches(ctx, snapshot, spec.q)
		a.update(func() {
			if generation == d.findGeneration {
				d.findCancel = nil
			}
			if a.files[d.id] != d || d.buffer != buffer || generation != d.findGeneration || d.buffer.Version() != snapshot.Version() {
				return
			}
			if err != nil {
				if spec.find {
					d.findResult = "正则表达式无效"
				}
				return
			}
			queryChanged := d.findKey != spec.key
			d.findMatches = matches
			d.findKey, d.findBuffer, d.findVersion, d.findScanned = spec.key, buffer, snapshot.Version(), true
			if !spec.find || !d.findOpen || d.query != spec.q.Text || d.findCase != spec.q.MatchCase {
				return
			}
			n := len(matches)
			if n == 0 {
				d.findIndex, d.findResult, d.findStep = -1, "没有匹配", 0
				return
			}
			if d.findStep != 0 {
				var idx int
				if d.findStep > 0 {
					idx = sort.Search(n, func(i int) bool { return matches[i].start >= d.caret })
					if idx == n {
						idx = 0
					}
				} else {
					idx = sort.Search(n, func(i int) bool { return matches[i].end > d.anchor }) - 1
					if idx < 0 {
						idx = n - 1
					}
				}
				d.findStep = 0
				a.revealFileMatch(d, idx)
				return
			}
			if queryChanged || d.findIndex < 0 || d.findIndex >= n {
				d.findIndex = sort.Search(n, func(i int) bool { return matches[i].end > min(d.anchor, d.caret) })
				if d.findIndex == n {
					d.findIndex = 0
				}
				a.revealFileMatch(d, d.findIndex)
				return
			}
			d.findResult = fmt.Sprintf("%d/%d", d.findIndex+1, n)
		})
	})
}

// findInFile is the find bar's Enter or step button: with fresh matches it
// steps, else it asks the scan in flight or a new one to reveal a match.
func (a *App) findInFile(d *filePage, previous bool) {
	if d.query == "" || d.buffer == nil {
		return
	}
	spec := fileSearchSpec{
		key:  "f\x00" + d.query + "\x00" + fmt.Sprint(d.findCase),
		q:    workspace.ContentQuery{Text: d.query, MatchCase: d.findCase},
		find: true,
		step: 1,
	}
	if previous {
		spec.step = -1
	}
	if d.findScanned && d.findKey == spec.key && d.findBuffer == d.buffer && d.findVersion == d.buffer.Version() {
		a.stepFileMatch(d, previous)
		return
	}
	if d.findCancel != nil {
		// A scan for this query is on its way: it applies the step itself.
		d.findStep = spec.step
	} else {
		a.scanFileMatches(d, spec)
	}
	d.findResult = "正在查找…"
}

// stepFileMatch moves the current match over cached matches, wrapping around.
func (a *App) stepFileMatch(d *filePage, previous bool) {
	n := len(d.findMatches)
	if n == 0 {
		d.findIndex, d.findResult = -1, "没有匹配"
		return
	}
	if d.findIndex < 0 || d.findIndex >= n {
		d.findIndex = sort.Search(n, func(i int) bool { return d.findMatches[i].end > min(d.anchor, d.caret) })
		if d.findIndex == n {
			d.findIndex = 0
		}
	} else {
		by := 1
		if previous {
			by = -1
		}
		d.findIndex = (d.findIndex + by + n) % n
	}
	a.revealFileMatch(d, d.findIndex)
}

// revealFileMatch makes match i current: selected, in sight and counted.
func (a *App) revealFileMatch(d *filePage, i int) {
	m := d.findMatches[i]
	d.findIndex = i
	d.pendingSelection = &[2]int{m.start, m.end}
	d.anchor, d.caret = m.start, m.end
	d.findResult = fmt.Sprintf("%d/%d", i+1, len(d.findMatches))
}

// paintFileMatches draws the searched keywords under the text, the way
// Mayoi's find does: every match filled, the match under the selection
// stronger with a ring.
func (a *App) paintFileMatches(p *native.Painter, r native.Rect, d *filePage, snap native.TextSnapshot, search searchPalette) {
	if !d.findScanned || len(d.findMatches) == 0 || d.findBuffer != d.buffer || d.findVersion != snap.Version() {
		return
	}
	textX := r.X + 58
	first := max(0, d.lines.At(d.scroll.Y-8)-1)
	last := min(snap.LineCount()-1, d.lines.At(d.scroll.Y+r.H-8)+2)
	lo, _ := snap.LineRange(first)
	_, hi := snap.LineRange(last)
	i := sort.Search(len(d.findMatches), func(i int) bool { return d.findMatches[i].end > lo })
	font := native.Font{Family: a.codeFontFamily(), Size: 13}
	sel0, sel1 := min(d.anchor, d.caret), max(d.anchor, d.caret)
	line, lineStart, lineEnd := -1, 0, 0
	var lineRunes []rune
	var layout *native.TextLayout
	count := 0
	for ; i < len(d.findMatches) && count < 500; i++ {
		m := d.findMatches[i]
		if m.start > hi || m.end > snap.Len() {
			break
		}
		cur := m.start <= sel1 && sel0 <= m.end
		endLine := snap.LineAt(m.end)
		for l := snap.LineAt(m.start); l <= endLine && l <= last; l++ {
			if l != line {
				lineStart, lineEnd = snap.LineRange(l)
				lineRunes = []rune(snap.Slice(lineStart, lineEnd))
				layout = nil
				line = l
			}
			s, e := max(m.start, lineStart), min(m.end, lineEnd)
			if e <= s {
				continue
			}
			if layout == nil {
				layout = native.ShapeText(string(lineRunes), font, 0)
			}
			x0 := textX + layout.Caret(utf16Len(lineRunes[:s-lineStart])).X
			x1 := textX + layout.Caret(utf16Len(lineRunes[:e-lineStart])).X
			if x1 <= textX {
				continue
			}
			y := r.Y + 8 + d.lines.Top(l) - d.scroll.Y
			box := native.Rect{X: max(x0, textX) - 1, Y: y + 1, W: max(x1-x0, 2) + 2, H: 18}
			if cur {
				p.Fill(box, search.current, 3)
				p.Stroke(box, search.ring, 3, 1)
			} else {
				p.Fill(box, search.match, 3)
			}
			count++
		}
	}
}

// matchBrackets refreshes the bracket pair around the caret, caching by
// caret, buffer and version: the scan is cheap while no bracket touches.
func (d *filePage) matchBrackets(snap native.TextSnapshot) {
	if d.bracketCaret == d.caret && d.bracketBuffer == d.buffer && d.bracketVersion == snap.Version() {
		return
	}
	d.bracketCaret, d.bracketBuffer, d.bracketVersion = d.caret, d.buffer, snap.Version()
	d.bracketStart, d.bracketEnd = 0, 0
	if pos, mate, ok := findBracketPair(snap, d.caret); ok {
		d.bracketStart, d.bracketEnd = pos, mate
	}
}

// findBracketPair returns the rune positions of the bracket pair touching
// the caret: the bracket at the caret wins, else the one before it.
func findBracketPair(snap native.TextSnapshot, caret int) (int, int, bool) {
	pos := -1
	if s := snap.Slice(caret, caret+1); isBracket(s) {
		pos = caret
	} else if s := snap.Slice(caret-1, caret); isBracket(s) {
		pos = caret - 1
	}
	if pos < 0 {
		return 0, 0, false
	}
	rs := []rune(snap.String())
	switch rs[pos] {
	case '(', '[', '{':
		if mate := matchBracketForward(rs, pos); mate >= 0 {
			return pos, mate, true
		}
	default:
		if mate := matchBracketBackward(rs, pos); mate >= 0 {
			return mate, pos, true
		}
	}
	return 0, 0, false
}

func isBracket(s string) bool {
	if len(s) != 1 {
		return false
	}
	switch s[0] {
	case '(', ')', '[', ']', '{', '}':
		return true
	}
	return false
}

// bracketMate maps an open bracket to its close and back; 0 for others.
func bracketMate(b rune) rune {
	switch b {
	case '(':
		return ')'
	case '[':
		return ']'
	case '{':
		return '}'
	case ')':
		return '('
	case ']':
		return '['
	case '}':
		return '{'
	}
	return 0
}

// matchBracketForward finds the close bracket matching the open one at pos
// in rune space, counting only the same bracket type.
func matchBracketForward(rs []rune, pos int) int {
	open, close := rs[pos], bracketMate(rs[pos])
	depth := 0
	for i := pos + 1; i < len(rs); i++ {
		switch rs[i] {
		case open:
			depth++
		case close:
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

// matchBracketBackward finds the open bracket matching the close one at pos.
func matchBracketBackward(rs []rune, pos int) int {
	close, open := rs[pos], bracketMate(rs[pos])
	depth := 0
	for i := pos - 1; i >= 0; i-- {
		switch rs[i] {
		case close:
			depth++
		case open:
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

// paintBracketPair draws a rounded box around each bracket of the pair at
// the caret, the way editors mark bracket matching.
func (a *App) paintBracketPair(p *native.Painter, r native.Rect, d *filePage, snap native.TextSnapshot, palette editorPalette) {
	if d.bracketEnd <= 0 || d.bracketBuffer != d.buffer || d.bracketVersion != snap.Version() {
		return
	}
	first := max(0, d.lines.At(d.scroll.Y-8)-1)
	last := min(snap.LineCount()-1, d.lines.At(d.scroll.Y+r.H-8)+2)
	font := native.Font{Family: a.codeFontFamily(), Size: 13}
	textX := r.X + 58
	for _, pos := range [2]int{d.bracketStart, d.bracketEnd} {
		line := snap.LineAt(pos)
		if line < first || line > last {
			continue
		}
		lineStart, lineEnd := snap.LineRange(line)
		lineRunes := []rune(snap.Slice(lineStart, lineEnd))
		if pos >= lineEnd {
			continue
		}
		layout := native.ShapeText(string(lineRunes), font, 0)
		x0 := textX + layout.Caret(utf16Len(lineRunes[:pos-lineStart])).X
		x1 := textX + layout.Caret(utf16Len(lineRunes[:pos+1-lineStart])).X
		y := r.Y + 8 + d.lines.Top(line) - d.scroll.Y
		box := native.Rect{X: x0 - 1, Y: y + 1, W: max(x1-x0, 2) + 2, H: 18}
		p.Fill(box, palette.accent.Alpha(.15), 3)
		p.Stroke(box, palette.accent.Alpha(.6), 3, 1)
	}
}

func utf16Len(rs []rune) int {
	n := 0
	for _, r := range rs {
		n += utf16.RuneLen(r)
	}
	return n
}

// matchWords caches the visible occurrences of the text selected on one
// line, painted quietly by paintWordMatches. An open find with its own
// matches wins.
func (d *filePage) matchWords(snap native.TextSnapshot, first, last int) {
	sel := [2]int{d.anchor, d.caret}
	blocked := d.findOpen && len(d.findMatches) > 0
	if d.wordBuffer == d.buffer && d.wordVersion == snap.Version() && d.wordSel == sel && d.wordFirst == first && d.wordLast == last && d.wordBlocked == blocked {
		return
	}
	d.wordBuffer, d.wordVersion, d.wordSel = d.buffer, snap.Version(), sel
	d.wordFirst, d.wordLast, d.wordBlocked = first, last, blocked
	d.words = d.words[:0]
	a0, b0 := min(sel[0], sel[1]), max(sel[0], sel[1])
	a0, b0 = min(a0, snap.Len()), min(b0, snap.Len())
	if d.kind != "file" || blocked || a0 == b0 || b0-a0 > 200 || snap.LineAt(a0) != snap.LineAt(b0) {
		return
	}
	word := snap.Slice(a0, b0)
	if strings.TrimSpace(word) == "" {
		return
	}
	for i := first; i <= last && len(d.words) <= 400; i++ {
		s, e := snap.LineRange(i)
		if e-s > 2000 {
			continue
		}
		text := snap.Slice(s, e)
		for at := 0; ; {
			k := strings.Index(text[at:], word)
			if k < 0 {
				break
			}
			k += at
			start := s + utf8.RuneCountInString(text[:k])
			d.words = append(d.words, findMatch{start: start, end: start + utf8.RuneCountInString(word)})
			at = k + len(word)
		}
	}
}

// paintWordMatches fills the occurrences matchWords found with a quiet
// text-tinted block, lighter than the find-match accent blocks.
func (a *App) paintWordMatches(p *native.Painter, r native.Rect, d *filePage, snap native.TextSnapshot, palette editorPalette) {
	first := max(0, d.lines.At(d.scroll.Y-8)-1)
	last := min(snap.LineCount()-1, d.lines.At(d.scroll.Y+r.H-8)+2)
	d.matchWords(snap, first, last)
	if len(d.words) == 0 || d.wordBuffer != d.buffer || d.wordVersion != snap.Version() {
		return
	}
	textX := r.X + 58
	font := native.Font{Family: a.codeFontFamily(), Size: 13}
	sel0, sel1 := min(d.wordSel[0], d.wordSel[1]), max(d.wordSel[0], d.wordSel[1])
	line, lineStart, lineEnd := -1, 0, 0
	var lineRunes []rune
	var layout *native.TextLayout
	for _, m := range d.words {
		if sel0 != sel1 && m.start == sel0 && m.end == sel1 {
			continue // the selection itself
		}
		l := snap.LineAt(m.start)
		if l != line {
			lineStart, lineEnd = snap.LineRange(l)
			lineRunes = []rune(snap.Slice(lineStart, lineEnd))
			layout = nil
			line = l
		}
		if layout == nil {
			layout = native.ShapeText(string(lineRunes), font, 0)
		}
		x0 := textX + layout.Caret(utf16Len(lineRunes[:m.start-lineStart])).X
		x1 := textX + layout.Caret(utf16Len(lineRunes[:m.end-lineStart])).X
		if x1 <= textX {
			continue
		}
		y := r.Y + 8 + d.lines.Top(l) - d.scroll.Y
		p.Fill(native.Rect{X: max(x0, textX) - 1, Y: y + 1, W: max(x1-x0, 2) + 2, H: 18}, palette.text.Alpha(.14), 3)
	}
}
func (a *App) confirmFile(d *filePage, title, detail string, fn func()) {
	if a.quitDialog {
		return
	}
	a.quitDialog = true
	go func() {
		r, err := a.quitPrompt(mygo.MessageOptions{Parent: a.win, Type: mygo.MessageWarning, Title: title, Message: filepath.Base(d.path), Detail: detail, Buttons: []string{"取消", title}, DefaultButton: 0, CancelButton: 0})
		a.update(func() {
			a.quitDialog = false
			if err != nil {
				d.err = err.Error()
				return
			}
			if r.Button == 1 && a.files[d.id] == d {
				fn()
			}
		})
	}()
}
func (a *App) confirmOverwrite(d *filePage) {
	if d.loading || d.saving || a.quitDialog {
		return
	}
	if d.missing {
		a.confirmFile(d, "重新创建文件", "将用本地编辑内容重新创建目标文件。", func() { a.saveFile(d, false, true, nil) })
		return
	}
	// Capture the version before asking: confirmation must not authorize later writes.
	a.runFileTask(func() {
		base, err := editor.Read(d.path)
		a.update(func() {
			if a.files[d.id] != d {
				return
			}
			if err != nil {
				d.err = err.Error()
				return
			}
			a.confirmFile(d, "覆盖磁盘", "将用本地编辑替换当前磁盘版本；确认期间文件再次变化时会停止保存。", func() {
				d.overwriteBase = &base
				a.saveFile(d, true, false, nil)
			})
		})
	})
}
func (a *App) closeFile(d *filePage) {
	if d.saving || d.checkpointBusy {
		d.closeAfterCheckpoint = true
		return
	}
	a.closeFileWithResult(d, func(bool) {})
}

func (a *App) closeFileWithResult(d *filePage, done func(bool)) {
	if d.saving || d.checkpointBusy {
		d.err = "文件正在保存，请完成后再次关闭窗口"
		done(false)
		return
	}
	if !d.dirty() {
		a.finishCloseFile(d)
		done(a.files[d.id] == nil)
		return
	}
	if a.quitDialog {
		done(false)
		return
	}
	a.quitDialog = true
	go func() {
		r, err := a.quitPrompt(mygo.MessageOptions{Parent: a.win, Type: mygo.MessageWarning, Title: "关闭文件", Message: "保存对 " + filepath.Base(d.path) + " 的更改？", Buttons: []string{"取消", "不保存", "保存"}, DefaultButton: 2, CancelButton: 0})
		a.update(func() {
			a.quitDialog = false
			if err != nil {
				d.err = err.Error()
				done(false)
				return
			}
			if r.Button == 1 {
				if a.clearRecovery(d) {
					a.finishCloseFile(d)
				}
				done(a.files[d.id] == nil)
			} else if r.Button == 2 {
				a.saveFile(d, false, false, func(ok bool) {
					if ok && !d.dirty() {
						a.finishCloseFile(d)
					}
					done(a.files[d.id] == nil)
				})
			} else {
				done(false)
			}
		})
	}()
}
func (a *App) finishCloseFile(d *filePage) {
	if d.kind == "file" && !d.dirty() && !a.clearRecovery(d) {
		return
	}
	if d.highlightCancel != nil {
		d.highlightCancel()
	}
	a.cancelFileRead(d)
	d.cancelFind()
	// When the file was opened by jumping from another tab, closing it
	// returns to that origin before falling back to a same-pane neighbor.
	if origin := a.tabByID(d.openedFrom); origin != nil && a.activeTab == d.id {
		a.selectWorkspaceTab(*origin)
	}
	delete(a.files, d.id)
	a.removeWorkspaceTab(d.id)
}
func (a *App) selectWorkspaceTab(tab storage.Tab) {
	if tab.Kind == "empty" {
		a.selectWorkspace(tab.Workspace)
		a.activatePaneTab(tab.ID, a.activeWorkspace, false)
		a.activeTab = tab.ID
		a.rememberFolderTab()
	} else if tab.Kind == "terminal" {
		if page := a.terminals[tab.ID]; page != nil {
			a.selectTerminal(page)
		}
	} else if tab.Kind == "session" {
		for _, s := range a.sessions {
			if s.id == tab.SessionID {
				a.selectSession(s)
				return
			}
		}
	} else if d := a.files[tab.ID]; d != nil {
		a.selectFile(d)
		if !d.loaded && !d.loading {
			if d.kind == "file" || d.turnReview != nil {
				a.loadFile(d)
			} else {
				a.refreshGit(a.workspace(d.root))
			}
		}
	}
}
