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
					if tab.id == "history" && len(a.nativeThreads) == 0 {
						a.loadThreadList()
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
				native.Segmented(c, &a.historyScope, "当前项目", "全部项目").FillWidth()
				scope := "全部项目"
				if a.historyScope == 0 {
					scope = projectName(a.workspaceCwd())
				}
				native.Text(c, scope).FontSize(11).TextColor(t.TextMuted).SingleLine()
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
		b := quietButton(c, "全部折叠").Size(26, 26)
		b.Children(func() { native.Icon(c, chevron).Size(14, 14).Rotate(-90) })
		if b.Clicked() {
			w.expanded = map[string]bool{w.root: true}
			if filepath.Dir(w.selected) != w.root {
				w.selected = ""
			}
		}
		b = quietButton(c, "刷新文件").Size(26, 26).Disabled(w.loading[w.root])
		b.Children(func() { native.Icon(c, refreshProjectIcon).Size(14, 14) })
		if b.Clicked() {
			w.error = ""
			for path := range w.expanded {
				a.loadDirectory(w, path)
			}
		}
	})
	native.Column(c).FillWidth().Padding(4, 12, 10, 12).Children(func() {
		input := native.TextInput(c, &w.query).Label("查找文件").Placeholder("查找文件").FillWidth().FontSize(12)
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
			if k.Key == native.KeyEnter && !w.searchLoading && w.query == w.searchQuery && len(w.search.Entries) > 0 {
				index := min(w.searchIndex, len(w.search.Entries)-1)
				a.openFile(w.root, w.search.Entries[index].Path)
				return true
			}
			if len(w.search.Entries) > 0 && (k.Key == native.KeyDown || k.Key == native.KeyUp) {
				delta := 1
				if k.Key == native.KeyUp {
					delta = -1
				}
				w.searchIndex = (w.searchIndex + delta + len(w.search.Entries)) % len(w.search.Entries)
				w.selected = w.search.Entries[w.searchIndex].Path
				w.searchList.ScrollTo(w.searchIndex, native.Center)
				return true
			}
			return false
		})
	})
	a.searchFiles(w)
	if w.error != "" {
		native.Text(c, w.error).FontSize(11).TextColor(t.Danger).MaxLines(3).Padding(8, 12)
	}
	if w.searchLoading {
		native.Text(c, "正在查找…").FontSize(11).TextColor(t.TextMuted).Padding(8, 14)
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
	list := &w.list
	listContext := c.Key("file-tree-list")
	if strings.TrimSpace(w.query) != "" {
		list = &w.searchList
		listContext = c.Key("file-search-list")
		rows = nil
		for _, entry := range w.search.Entries {
			rows = append(rows, treeRow{Entry: entry, Scale: 1})
		}
		if !w.searchLoading && len(rows) == 0 {
			native.Text(c, "没有匹配的文件").FontSize(12).TextColor(t.TextMuted).Padding(16)
		}
		if w.search.Truncated {
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
	list.Key = func(i int) any { return rows[i].Path }
	native.List(listContext, list, len(rows), func(i int) {
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
func (a *App) fileView(c *native.Context, d *filePage, left, right bool) {
	if d.pinFrame != 0 {
		d.scroll.Y = d.pinY
		d.pinFrame--
		c.AnimationFrame()
	}
	viewY := d.scroll.Y
	if !d.loaded && !d.loading && d.err == "" {
		if d.kind == "file" {
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
		native.Row(c).Height(46).FillWidth().Padding(0, 12).Gap(8).AlignItems(native.Center).Children(func() {
			if !left {
				a.sidebarToggle(c, true)
			}
			native.Text(c, projectName(d.root)).FontSize(13).Bold()
			native.Spacer(c)
			for _, s := range a.sessions {
				if len(s.State.PendingInteractions()) > 0 {
					b := actionButton(c, "返回待处理会话")
					if b.Clicked() {
						a.selectSession(s)
					}
					break
				}
			}
			if !right {
				a.sidebarToggle(c, false)
			}
		})
		a.sessionTabs(c)
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
			copyPath := quietButton(c, "复制完整路径").Size(24, 24)
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
		if !d.highlighting && d.highlightVersion != d.buffer.Version() && time.Since(d.changedAt) > 200*time.Millisecond {
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
				if quietButton(c, "关闭查找").Size(26, 26).Children(func() { native.Icon(c, tabCloseIcon).Size(12, 12) }).Clicked() {
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
		if d.kind == "diff" && d.compare != "外部更改" {
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
			a.gitSplitView(c, d)
			return
		}
		snap := d.buffer.Snapshot()
		width := float32(600)
		if d.contentWidth > width {
			width = d.contentWidth
		}
		native.ScrollHorizontal(c.Key("editor-horizontal")).TrackScroll(&d.hscroll).FillWidth().Grow(1).MinHeight(0).Children(func() {
			area := native.TextAreaBufferBase(c.Key("source"), d.buffer).Label("文件内容 "+d.path).Font("monospace").FontSize(13).FixedLineHeight(20).Width(width).FillHeight().Padding(8, 24, 8, 58).NoWrap().TrackLines(&d.lines).TrackScroll(&d.scroll).ReadOnly(d.kind != "file" || d.file.Reason != "")
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
				for i, hunk := range d.gitReview.Hunks {
					if hunk.PatchLine <= line {
						d.gitHunk = i
					}
				}
			}
			if d.focus {
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
				var ranges []native.TextRange
				for _, r := range d.highlights {
					if r.End >= lo && r.Start <= hi {
						ranges = append(ranges, r)
					}
				}
				area.TextRanges(ranges...)
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
			area.OnTextKey(func(k native.TextKey) bool {
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
					a.closeFile(d)
					return true
				}
				if k.Key == native.KeyZ && k.Mods&native.Cmd != 0 {
					d.undoEdit(k.Mods&native.Shift != 0)
					k.SetText(d.buffer.String())
					d.observed = d.buffer.Snapshot()
					return true
				}
				return false
			})
			if d.kind == "diff" {
				area.Draw(func(p *native.Painter, r native.Rect) {
					first := max(0, int(d.scroll.Y/20))
					last := min(snap.LineCount(), first+int(r.H/20)+3)
					for i := first; i < last; i++ {
						line := snap.Line(i)
						var color native.Color
						if d.gitReview != nil && d.gitHunk < len(d.gitReview.Hunks) && i == d.gitReview.Hunks[d.gitHunk].PatchLine {
							color = t.Accent.Alpha(.12)
						}
						if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
							color = t.Success.Alpha(.1)
						}
						if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
							color = t.Danger.Alpha(.08)
						}
						if color.A != 0 {
							y := r.Y + 8 + float32(i)*20 - d.scroll.Y
							p.Fill(native.Rect{X: r.X + 48, Y: y, W: r.W - 48, H: 20}, color, 0)
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
						p.Text(r.X+10, y, fmt.Sprint(i+1), 11, t.TextMuted)
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
	if !d.dirty() {
		a.finishCloseFile(d)
		return
	}
	if a.quitDialog {
		return
	}
	a.quitDialog = true
	go func() {
		r, err := a.quitPrompt(mygo.MessageOptions{Parent: a.win, Type: mygo.MessageWarning, Title: "关闭文件", Message: "保存对 " + filepath.Base(d.path) + " 的更改？", Buttons: []string{"取消", "不保存", "保存"}, DefaultButton: 2, CancelButton: 0})
		a.update(func() {
			a.quitDialog = false
			if err != nil {
				d.err = err.Error()
				return
			}
			if r.Button == 1 {
				if a.clearRecovery(d) {
					a.finishCloseFile(d)
				}
			} else if r.Button == 2 {
				a.saveFile(d, false, false, func(ok bool) {
					if ok && !d.dirty() {
						a.finishCloseFile(d)
					}
				})
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
	old := a.activeTab
	index := 0
	for i, t := range a.tabs {
		if t.ID == d.id {
			index = i
			break
		}
	}
	delete(a.files, d.id)
	a.removeWorkspaceTab(d.id)
	if old != d.id {
		return
	}
	a.activeTab = ""
	if len(a.tabs) > 0 {
		a.selectWorkspaceTab(a.tabs[max(0, index-1)])
	} else {
		for _, s := range a.sessions {
			s.open = false
		}
	}
}
func (a *App) selectWorkspaceTab(tab storage.Tab) {
	if tab.Kind == "session" {
		for _, s := range a.sessions {
			if s.id == tab.SessionID {
				a.selectSession(s)
				return
			}
		}
	} else if d := a.files[tab.ID]; d != nil {
		a.selectFile(d)
		if !d.loaded && !d.loading {
			if d.kind == "file" {
				a.loadFile(d)
			} else {
				a.refreshGit(a.workspace(d.root))
			}
		}
	}
}
