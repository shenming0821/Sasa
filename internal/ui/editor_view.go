package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/editor"
	gitview "github.com/shenming0821/Fuyu/internal/git"
	"github.com/shenming0821/Fuyu/internal/storage"
	"github.com/shenming0821/Fuyu/internal/workspace"
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
		b.Children(func() { native.Icon(c, chevron).Size(14, 14).Rotate(-90) })
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
				searchOptionButton(c, "区分大小写", "Aa", &w.searchCase)
				searchOptionButton(c, "全字匹配", "ab", &w.searchWord)
				searchOptionButton(c, "使用正则表达式", ".*", &w.searchRegex)
			})
			label := "展开搜索筛选"
			if w.searchDetails {
				label = "收起搜索筛选"
			}
			b := quietIconButton(c, label).Size(24, 26)
			if !w.searchDetails && (strings.TrimSpace(w.searchInclude) != "" || strings.TrimSpace(w.searchExclude) != "" || w.searchIgnored) {
				b.TextColor(t.Accent)
			}
			b.Children(func() {
				g := native.Icon(c, chevron).Size(14, 14)
				if !w.searchDetails {
					g.Rotate(-90)
				}
			})
			if b.Clicked() {
				w.searchDetails = !w.searchDetails
			}
		})
		if !w.searchDetails {
			return
		}
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
	for path := range w.expanded {
		target := float32(0)
		if w.expanded[path] && len(w.children[path]) > 0 {
			target = 1
		}
		if treeAnimationSize(w, path) > 200 {
			w.directoryProgress[path] = target
			continue
		}
		w.directoryProgress[path] = native.Box(c.Key("directory-motion:"+path)).Size(0, 0).Animate("open", target, 200*time.Millisecond)
	}
	rows := a.flattenTree(w)
	var matches []workspace.ContentMatch
	var resultRows []searchRow
	list := &w.list
	listContext := c.Key("file-tree-list")
	searching := strings.TrimSpace(w.query) != ""
	if searching {
		rows = nil
		list = &w.contentList
		listContext = c.Key("file-content-search-list")
		matches = w.contentSearch.Matches
		resultRows = contentSearchRows(matches, w.searchCollapsed)
		if !w.searchLoading && len(matches) == 0 {
			native.Text(c, "没有匹配的内容").FontSize(12).TextColor(t.TextMuted).Padding(16)
		} else if len(matches) > 0 {
			contentSearchSummary(c, w, matches)
		}
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
				a.searchFileRow(c, w, r)
			} else {
				a.contentMatchRow(c, w, matches[r.match], r.match)
			}
			return
		}
		row := rows[i]
		rc := c.Key(row.Path)
		b := treeButton(rc, "文件树 "+row.Path).FillWidth().Height(32*row.Scale).Radius(5).Padding(0, 10, 0, float32(12+row.Depth*14))
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
			b.Background(t.SurfacePressed)
		}
		b.Children(func() {
			native.Row(rc).Fill().Gap(6).AlignItems(native.Center).Children(func() {
				if row.Dir {
					g := native.Icon(rc, chevron).Size(12, 12)
					target := float32(0)
					if w.expanded[row.Path] {
						target = 1
					}
					progress := g.Animate("directory-open", target, 200*time.Millisecond)
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
		if b.Clicked() {
			w.selected = row.Path
			if row.Dir {
				a.toggleFileDirectory(w, row.Path)
			} else {
				a.openFile(w.root, row.Path)
				if d := a.currentFile(); d != nil {
					d.focusOwner = w.rowHandles[row.Path]
				}
			}
		}
	}).FillWidth().Grow(1).MinHeight(0)
}
func (a *App) contentMatchRow(c *native.Context, w *workspacePage, m workspace.ContentMatch, i int) {
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
}

var searchIgnoreIcon = icon(`<path d="M4 5h16l-6 7.5V18l-4 2v-7.5z"/>`)

// searchRow is one row of the grouped content results: a file heading
// (match < 0) carrying the file's match count, or a matching line.
type searchRow struct {
	path  string
	match int
	count int
}

func contentSearchRows(matches []workspace.ContentMatch, collapsed map[string]bool) []searchRow {
	var rows []searchRow
	heading := -1
	for i, m := range matches {
		if heading < 0 || rows[heading].path != m.Path {
			heading = len(rows)
			rows = append(rows, searchRow{path: m.Path, match: -1})
		}
		rows[heading].count += m.Count()
		if !collapsed[m.Path] {
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
		b.Children(func() {
			g := native.Icon(c, chevron).Size(12, 12)
			if allCollapsed {
				g.Rotate(-90)
			}
		})
		if b.Clicked() {
			w.searchCollapsed = nil
			if !allCollapsed {
				w.searchCollapsed = map[string]bool{}
				for _, m := range matches {
					w.searchCollapsed[m.Path] = true
				}
			}
		}
	})
}

func (a *App) searchFileRow(c *native.Context, w *workspacePage, r searchRow) {
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
	b.Children(func() {
		native.Row(rc).Fill().Gap(5).AlignItems(native.Center).Children(func() {
			g := native.Icon(rc, chevron).Size(12, 12)
			if collapsed {
				g.Rotate(-90)
			}
			native.Icon(rc, fileIcon).Size(14, 14).TextColor(t.TextMuted)
			native.Text(rc, name).FontSize(12).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
			native.Text(rc, dir).FontSize(10).TextColor(t.TextMuted.Alpha(.8)).SingleLine().Grow(1).MinWidth(0)
			native.Box(rc.Key("count")).MinWidth(22).Height(18).Padding(0, 5).Radius(9).Background(t.TextMuted.Alpha(.1)).
				AlignItems(native.Center).Justify(native.Center).Children(func() {
				native.Text(rc, fmt.Sprint(r.count)).FontSize(10).TextColor(t.TextMuted).SingleLine()
			})
		})
	})
	if b.Clicked() {
		if w.searchCollapsed == nil {
			w.searchCollapsed = map[string]bool{}
		}
		w.searchCollapsed[r.path] = !collapsed
	}
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
			a.gitLoadCommit(d, gitview.CommitInfo{Hash: d.gitCommit, Subject: "提交 " + d.gitCommit[:min(8, len(d.gitCommit))]})
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
			path, openable := d.path, true
			if d.gitCommit != "" {
				openable = false
				if len(d.gitCommitFiles) > 0 {
					entry := d.gitCommitFiles[d.gitCommitFile]
					path = filepath.Join(d.root, entry.Path)
					openable = !strings.HasPrefix(entry.XY, "D") && !d.loading
				}
			} else if d.gitReview != nil {
				openable = gitStatusLabel(d.gitReview.Entry, d.gitReview.Staged) != "删除"
			}
			rel, _ := filepath.Rel(d.root, path)
			native.ScrollHorizontal(c.Key("file-path")).Grow(1).FillHeight().MinWidth(0).Children(func() {
				native.Row(c).FillHeight().AlignItems(native.Center).Children(func() {
					native.Text(c, rel).FontSize(11).TextColor(t.TextMuted).SingleLine()
				})
			})
			for _, s := range a.sessions {
				if len(s.State.PendingInteractions()) > 0 {
					if actionButton(c, "返回待处理会话").Clicked() {
						a.selectSession(s)
					}
					break
				}
			}
			copyPath := quietIconButton(c, "复制完整路径").Size(24, 24)
			copyPath.Children(func() { native.Icon(c, copyIcon).Size(12, 12) })
			if copyPath.Clicked() && a.win != nil {
				mygo.Clipboard.WriteText(path)
			}
			if d.kind == "diff" {
				if actionButton(c, "打开文件").Disabled(!openable).Clicked() {
					a.openFile(d.root, path)
				}
			} else {
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
		if d.findOpen {
			native.Row(c).FillWidth().Padding(4, 12).Gap(8).Children(func() {
				input := native.TextInput(c, &d.query).Label("文件内查找").Placeholder("查找文本").Grow(1).FontSize(12)
				if d.findFocus {
					input.Focus()
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
				native.Text(c, d.findResult).FontSize(11).TextColor(t.TextMuted)
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
		if d.file.Reason != "" {
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
		width := max(available, d.contentWidth)
		palette := githubEditorPalette(t.Dark)
		native.ScrollHorizontal(c.Key("editor-horizontal")).TrackScroll(&d.hscroll).FillWidth().Grow(1).MinHeight(0).Background(palette.background).Children(func() {
			area := palette.area(native.TextAreaBufferBase(c.Key("source"), d.buffer)).Label("文件内容 "+d.path).Font(a.codeFontFamily()).FontSize(13).FixedLineHeight(20).Width(width).Shrink(0).FillHeight().Padding(8, 24, 8, 58).NoWrap().TrackLines(&d.lines).TrackScroll(&d.scroll).ReadOnly(d.kind != "file" || d.file.Reason != "")
			area.ContextMenu(a.fileMenu(d.root, d.path, true, false))
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
			if d.kind == "diff" {
				area.Draw(func(p *native.Painter, r native.Rect) {
					first := max(0, int(d.scroll.Y/20))
					last := min(len(d.gitRows), first+int(r.H/20)+3)
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
							y := r.Y + 8 + float32(i)*20 - d.scroll.Y
							p.Fill(native.Rect{X: r.X + 48, Y: y, W: r.W - 48, H: 20}, color, 0)
							if row.hunk == d.gitHunk {
								p.Fill(native.Rect{X: r.X + 50, Y: y, W: 2, H: 20}, palette.accent, 0)
							}
						}
					}
				})
			}
			area.DrawOver(func(p *native.Painter, r native.Rect) {
				first := max(0, int(d.scroll.Y/20))
				last := min(snap.LineCount(), first+int(r.H/20)+3)
				for i := first; i < last; i++ {
					y := r.Y + 8 + float32(i)*20 - d.scroll.Y
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
	if d.findResult == "正在查找…" {
		d.findResult = ""
	}
}

func (a *App) findInFile(d *filePage, previous bool) {
	d.cancelFind()
	if d.query == "" || d.buffer == nil {
		return
	}
	buffer := d.buffer
	snapshot := buffer.Snapshot()
	query, exact := d.query, d.findCase
	anchor, caret := d.anchor, d.caret
	d.findGeneration++
	generation := d.findGeneration
	d.findResult = "正在查找…"
	ctx, cancel := a.projectContext(5 * time.Second)
	d.findCancel = cancel
	a.runBackgroundFileTaskContext(ctx, func() {
		defer cancel()
		if ctx.Err() != nil {
			a.update(func() {
				if a.files[d.id] == d && generation == d.findGeneration {
					d.findCancel = nil
					d.findResult = "查找已超时，请重试"
				}
			})
			return
		}
		text := snapshot.String()
		needle := query
		if !exact {
			text, needle = strings.ToLower(text), strings.ToLower(needle)
		}
		runes := []rune(text)
		anchor, caret = min(max(anchor, 0), len(runes)), min(max(caret, 0), len(runes))
		offset := -1
		if previous {
			offset = strings.LastIndex(string(runes[:anchor]), needle)
			if offset < 0 {
				offset = strings.LastIndex(text, needle)
			}
		} else {
			i := strings.Index(string(runes[caret:]), needle)
			if i >= 0 {
				offset = len(string(runes[:caret])) + i
			} else {
				offset = strings.Index(text, needle)
			}
		}
		start, end := 0, 0
		if offset >= 0 {
			start = utf8.RuneCountInString(text[:offset])
			end = start + utf8.RuneCountInString(needle)
		}
		a.update(func() {
			if a.files[d.id] != d || d.buffer != buffer || generation != d.findGeneration || d.buffer.Version() != snapshot.Version() || d.query != query || d.findCase != exact || !d.findOpen {
				return
			}
			d.findCancel = nil
			if offset < 0 {
				d.findResult = "没有匹配"
				return
			}
			d.pendingSelection = &[2]int{start, end}
			d.anchor, d.caret = start, end
			d.findResult = "已定位"
		})
	})
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
