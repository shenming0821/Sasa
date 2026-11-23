// Portions adapted from Mayoi, Copyright (c) 2026 chencheng.
// Licensed under MIT; see THIRD_PARTY_NOTICES/Mayoi-LICENSE.
package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

const graphLaneW float32 = 10
const graphMaxLanes = 6

type gitCommitsRow struct {
	key, kind string
	at, depth int
	entry     gitview.Entry
}

func commitsOpen(w *workspacePage) bool { return !w.collapsed["COMMITS"] }
func (a *App) gitCommitsRows(w *workspacePage) []gitCommitsRow {
	var rows []gitCommitsRow
	rows = append(rows, gitCommitsRow{key: "commits:header", kind: "header"})
	if !commitsOpen(w) {
		return rows
	}
	for i, cm := range w.gitHistory {
		rows = append(rows, gitCommitsRow{key: "commit:" + cm.Hash, kind: "commit", at: i})
		if tree := w.gitCommitTrees[cm.Hash]; tree != nil && tree.expanded {
			if tree.loading || tree.err != "" || len(tree.entries) == 0 {
				rows = append(rows, gitCommitsRow{key: cm.Hash + ":status", kind: "files-status", at: i})
			}
			rows = append(rows, a.gitCommitFileRows(w, cm.Hash, i, tree.entries)...)

		}
	}
	if w.gitHistoryLoading || w.gitHistoryError != "" || len(w.gitHistory) == 0 {
		rows = append(rows, gitCommitsRow{key: "commits:status", kind: "status"})
	}
	return rows
}

// Also used by isolated native rendering tests.
func (a *App) gitHistoryView(c *native.Context, w *workspacePage, busy bool, _ float32) {
	a.gitCommitsListBody(c, w, busy)
}
func (a *App) gitCommitsListBody(c *native.Context, w *workspacePage, busy bool) {
	if commitsOpen(w) && !w.gitHistoryLoaded && !w.gitHistoryLoading && w.gitHistoryError == "" {
		a.gitLoadHistory(w, false)
	}
	if w.gitGraph == nil && len(w.gitHistory) > 0 {
		w.gitGraph = layoutGraph(w.gitHistory)
	}
	rows := a.gitCommitsRows(w)
	state := &w.gitList
	state.Key = func(i int) any {
		if i < 0 || i >= len(rows) {
			return i
		}
		return rows[i].key
	}
	state.Label = func(i int) string {
		if i < 0 || i >= len(rows) {
			return ""
		}
		r := rows[i]
		if r.kind == "commit" {
			return w.gitHistory[r.at].Subject
		}
		return r.key
	}
	state.Header = func(i int) bool { return i >= 0 && i < len(rows) && rows[i].kind == "header" }
	if state.Selected == nil {
		w.gitListRow = -1
	}
	state.Selected = &w.gitListRow
	var action func()
	out := native.List(c.Key("commits-list"), state, len(rows), func(i int) {
		r := rows[i]
		switch r.kind {
		case "header":
			a.gitCommitsHeader(c, w, busy, &action)
		case "commit":
			a.gitCommitRow(c, w, r.at, &action)
		case "file", "directory", "files-status":
			a.gitCommitFileRow(c, w, r, &action)
		case "status":
			native.Column(c).FillWidth().Padding(4, 14).Gap(4).Children(func() {
				if w.gitHistoryError != "" {
					native.Text(c, w.gitHistoryError).FontSize(11).TextColor(c.Theme().Danger).MaxLines(3)
				}
				if w.gitHistoryLoading {
					native.Text(c, "正在读取提交…").FontSize(11).TextColor(c.Theme().TextMuted)
				} else if w.gitHistoryError != "" {
					if actionButton(c, "重试读取提交").Clicked() {
						action = func() { a.gitLoadHistory(w, false) }
					}
				} else if len(w.gitHistory) == 0 {
					native.Text(c, "尚无提交").FontSize(11).TextColor(c.Theme().TextMuted)
				}
			})
		}
	}).Label("COMMITS 列表").Grow(1).MinHeight(0).FillWidth()
	if action != nil {
		action()
		c.Invalidate()
		return
	}
	a.gitAutoLoadHistory(c, out, w, len(rows), busy)
	chosen := w.gitListRow
	if chosen < 0 || chosen >= len(rows) {
		return
	}
	r := rows[chosen]
	if out.Changed() && r.kind == "commit" {
		cm := w.gitHistory[r.at]
		a.gitSelectCommit(w, cm.Hash, 0)
		a.gitPreviewCommit(w, cm, false)
	}
	if out.Submitted() {
		switch r.kind {
		case "commit":
			a.gitPreviewCommit(w, w.gitHistory[r.at], true)
		case "file":
			a.gitOpenCommit(w, w.gitHistory[r.at], r.entry.Path)
		}
	}
	if (r.kind == "file" || r.kind == "directory") && out.Shortcut(0, native.KeyLeft) {
		for i, row := range rows {
			if row.kind == "commit" && row.at == r.at {
				w.gitListRow = i
				state.ScrollIntoView(i)
				break
			}
		}
	}
	if r.kind == "commit" {
		cm := w.gitHistory[r.at]
		if out.Shortcut(0, native.KeyRight) {
			a.gitExpandCommit(w, cm, true)
		}
		if out.Shortcut(0, native.KeyLeft) {
			a.gitExpandCommit(w, cm, false)
		}
		for _, mods := range []native.Modifiers{native.Cmd, native.Ctrl} {
			if out.Shortcut(mods, native.KeyA) {
				w.gitCommitSelection = map[string]bool{}
				for _, cm := range w.gitHistory {
					w.gitCommitSelection[cm.Hash] = true
				}
			}
			if out.Shortcut(mods, native.KeyC) {
				var hashes []string
				for _, cm := range w.gitHistory {
					if w.gitCommitSelection[cm.Hash] {
						hashes = append(hashes, cm.Short())
					}
				}
				c.WriteClipboard(strings.Join(hashes, "\n"))
			}
		}
		for _, key := range []native.Key{native.KeyUp, native.KeyDown, native.KeyHome, native.KeyEnd} {
			if out.Shortcut(native.Shift, key) {
				at := r.at
				switch key {
				case native.KeyUp:
					at--
				case native.KeyDown:
					at++
				case native.KeyHome:
					at = 0
				case native.KeyEnd:
					at = len(w.gitHistory) - 1
				}
				if at >= 0 && at < len(w.gitHistory) {
					a.gitSelectCommit(w, w.gitHistory[at].Hash, native.Shift)
					for i, row := range rows {
						if row.kind == "commit" && row.at == at {
							w.gitListRow = i
							state.ScrollIntoView(i)
							break
						}
					}
				}
			}
		}
	}
}

func (a *App) gitAutoLoadHistory(c *native.Context, list native.Element, w *workspacePage, rows int, busy bool) {
	// Visible describes the previous layout. Let changed rows and their heights
	// reach a later frame before deciding whether the list is near its end.
	first, last := w.gitList.Visible()
	type layout struct {
		rows, first, last int
		loading           bool
	}
	sample := layout{rows, first, last, w.gitHistoryLoading}
	previous := native.Local(list, "pagination-layout", func() layout { return layout{} })
	changedAt := native.Local(list, "pagination-changed-at", func() time.Time { return time.Time{} })
	if *previous != sample {
		*previous, *changedAt = sample, c.Now()
		c.Invalidate()
		return
	}
	if c.Now() == *changedAt {
		c.Invalidate()
		return
	}
	// Four compact commit rows are about 80 DIP. A nonzero first row keeps
	// an unscrolled list that fits its viewport from draining every page.
	if commitsOpen(w) && !busy && w.gitBusy == "" && w.gitHistoryLoaded &&
		w.gitHistoryMore && !w.gitHistoryLoading && w.gitHistoryError == "" &&
		first > 0 && last >= rows-4 {
		a.gitLoadHistory(w, true)
		if w.gitHistoryLoading {
			c.Invalidate()
		}
	}
}

func (a *App) gitCommitsHeader(c *native.Context, w *workspacePage, busy bool, action *func()) {
	t := c.Theme()
	row := native.Row(c).FillWidth().Height(22).Padding(0, 6).Gap(4).Radius(5).AlignItems(native.Center).Background(t.Surface)
	hover := row.Hovered() || row.FocusWithin()
	if hover {
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		b := native.ButtonBase(c.Key("toggle")).Label("切换 COMMITS").Grow(1).FillHeight().AlignItems(native.Center).Justify(native.Start).Gap(5)
		b.Children(func() {
			rotation := float32(-90)
			if commitsOpen(w) {
				rotation = 0
			}
			native.Icon(c, chevron).Size(12, 12).Rotate(rotation).TextColor(t.TextMuted)
			native.Text(c, "COMMITS").FontSize(11).Bold().TextColor(t.TextMuted)
		})
		if b.Clicked() {
			*action = func() { w.collapsed["COMMITS"] = !w.collapsed["COMMITS"] }
		}
		if hover || a.preferences.CommitsAllBranches {
			label := "显示全部分支"
			if a.preferences.CommitsAllBranches {
				label = "仅显示当前分支"
			}
			b := gitIconButton(c.Key("all"), label, gitIcon, busy).Size(20, 20)
			if a.preferences.CommitsAllBranches {
				b.TextColor(t.Accent)
			}
			if b.Clicked() {
				*action = func() { a.gitToggleAllBranches(w) }
			}
		}
		if hover {
			if gitIconButton(c.Key("refresh"), "刷新提交历史", refreshProjectIcon, busy || w.gitHistoryLoading).Size(20, 20).Clicked() {
				*action = func() { a.gitLoadHistory(w, false) }
			}
		}
		native.Text(c, fmt.Sprint(len(w.gitHistory))).FontSize(10.5).Padding(0, 5).Radius(8).Background(t.SurfaceHover).TextColor(t.TextMuted)
	})
	row.ContextMenu(func(m *native.Menu) {
		if m.Item("显示全部分支").Checked(a.preferences.CommitsAllBranches).Chosen() {
			*action = func() { a.gitToggleAllBranches(w) }
		}
		if m.Item("以目录树显示提交文件").Checked(a.preferences.CommitsTree).Chosen() {
			*action = func() { a.gitToggleCommitTree(w) }
		}
		if m.Item("刷新").Disabled(busy || w.gitHistoryLoading).Chosen() {
			*action = func() { a.gitLoadHistory(w, false) }
		}
	})
}
func (a *App) gitExpandCommit(w *workspacePage, cm gitview.CommitInfo, open bool) {
	if w.gitCommitTrees == nil {
		w.gitCommitTrees = map[string]*gitCommitTree{}
	}
	tree := w.gitCommitTrees[cm.Hash]
	if tree == nil {
		tree = &gitCommitTree{}
		w.gitCommitTrees[cm.Hash] = tree
	}
	tree.expanded = open
	if open {
		a.gitLoadCommitTree(w, cm, tree)
	}
}
func (a *App) gitSelectCommit(w *workspacePage, hash string, mods native.Modifiers) {
	if key := w.gitList.Key; key != nil {
		for i := range len(a.gitCommitsRows(w)) {
			if key(i) == "commit:"+hash {
				w.gitListRow = i
				break
			}
		}
	}
	if w.gitCommitSelection == nil {
		w.gitCommitSelection = map[string]bool{}
	}
	if mods&native.Shift != 0 {
		first, last := -1, -1
		for i, cm := range w.gitHistory {
			if cm.Hash == w.gitCommitAnchor {
				first = i
			}
			if cm.Hash == hash {
				last = i
			}
		}
		if first < 0 {
			first = last
		}
		for i := min(first, last); i >= 0 && i <= max(first, last); i++ {
			w.gitCommitSelection[w.gitHistory[i].Hash] = true
		}
		return
	}
	if mods&(native.Cmd|native.Ctrl) != 0 {
		w.gitCommitSelection[hash] = !w.gitCommitSelection[hash]
	} else {
		w.gitCommitSelection = map[string]bool{hash: true}
	}
	w.gitCommitAnchor = hash
}
func (a *App) gitCommitRow(c *native.Context, w *workspacePage, at int, action *func()) {
	cm := w.gitHistory[at]
	t := c.Theme()
	head := w.git.Head == cm.Hash
	if w.gitCommitTrees == nil {
		w.gitCommitTrees = map[string]*gitCommitTree{}
	}
	tree := w.gitCommitTrees[cm.Hash]
	if tree == nil {
		tree = &gitCommitTree{}
		w.gitCommitTrees[cm.Hash] = tree
	}
	var gr *graphRow
	if at < len(w.gitGraph) {
		gr = &w.gitGraph[at]
	}
	row := native.Row(c).Label("提交 "+cm.Hash).FillWidth().Height(22).Padding(0, 6).Gap(6).AlignItems(native.Center)
	if w.gitCommitSelection[cm.Hash] {
		row.Background(t.SurfacePressed)
	} else if row.Hovered() {
		row.Background(t.SurfaceHover)
	}
	row.Description(commitTip(cm, w.gitUnpushed[cm.Hash]))
	a.gitCommitHoverCard(c, w, row, cm)
	row.ContextMenu(func(m *native.Menu) { a.gitCommitMenu(c, w, cm, m, action) })
	clicked, double, mods := row.Clicked(), row.DoubleClicked(), row.ClickModifiers()
	row.Children(func() {
		b := native.ButtonBase(c.Key("expand")).Label("展开提交文件 "+cm.Hash).Size(12, 22)
		b.Children(func() {
			rotation := float32(-90)
			if tree.expanded {
				rotation = 0
			}
			native.Icon(c, chevron).Size(10, 10).Rotate(rotation).TextColor(t.TextMuted)
		})
		if b.Clicked() {
			*action = func() { a.gitExpandCommit(w, cm, !tree.expanded) }
		}
		native.Box(c).Size(graphWidth(w), 22).Shrink(0).Draw(func(p *native.Painter, r native.Rect) {
			if gr != nil {
				paintCommitGraph(p, r, gr, cm.Hash, head, c.Theme())
			}
		})
		ring := t.Background
		if w.gitCommitSelection[cm.Hash] {
			ring = t.SurfacePressed
		} else if row.Hovered() {
			ring = t.SurfaceHover
		}
		gitCommitAvatar(c, cm, 15, ring)
		color := t.Accent
		if gr != nil {
			color = commitGraphColor(t, gr.color)
		}
		refs := refBadges(cm.Refs)
		// Reserve the title and time, matching Mayoi's bounded badge layout.
		shown := 1
		if row.Bounds().W > 400 {
			shown = 3
		}
		if row.Bounds().W > 600 {
			shown = 4
		}
		for i, ref := range refs {
			if i >= shown {
				native.Text(c, fmt.Sprintf("+%d", len(refs)-i)).FontSize(10).TextColor(t.TextMuted).Tooltip(badgeNames(refs[i:]))
				break
			}
			fg, bg := color, color.Alpha(.16)
			if ref.Kind == gitview.RefRemote {
				fg, bg = t.TextMuted, color.Alpha(.08)
			}
			if ref.Kind == gitview.RefTag {
				fg, bg = t.TextMuted, t.SurfaceHover
			}
			native.Text(c, ref.label()).FontSize(10.5).TextColor(fg).Background(bg).Radius(4).Padding(0, 4).MaxWidth(110).SingleLine().Shrink(1).Tooltip(ref.label())
		}
		color = t.Text
		if len(cm.Parents) > 1 {
			color = t.TextMuted
		}
		native.Text(c, cm.Subject).FontSize(12.5).TextColor(color).SingleLine().Grow(1).Shrink(1).MinWidth(24)
		if w.gitUnpushed[cm.Hash] {
			native.Text(c, "↑").FontSize(11).TextColor(t.Accent).Tooltip("尚未推送")
		}
		native.Text(c, commitAgo(cm.Time, time.Now())).FontSize(11).TextColor(t.TextMuted).Shrink(0)
	})
	if (clicked || double) && *action == nil {
		*action = func() {
			a.gitSelectCommit(w, cm.Hash, mods)
			if mods&(native.Cmd|native.Ctrl|native.Shift) == 0 {
				a.gitPreviewCommit(w, cm, double)
			}
			w.gitList.Focus(c)
		}
	}
}
func refNames(rs []gitview.Ref) string {
	var names []string
	for _, r := range rs {
		names = append(names, r.Name)
	}
	return strings.Join(names, ", ")
}
func commitTip(cm gitview.CommitInfo, unpushed bool) string {
	text := cm.Subject + "\n" + cm.Author + " <" + cm.Email + "> · " + cm.Time.Local().Format("2006-01-02 15:04:05 MST") + "\n" + cm.Hash
	if cm.Body != "" {
		text += "\n" + cm.Body
	}
	for _, p := range cm.CoAuthors {
		text += "\n共同作者：" + p.Name + " <" + p.Email + ">"
	}
	if len(cm.Refs) > 0 {
		text += "\n" + refNames(cm.Refs)
	}
	if unpushed {
		text += "\n尚未推送"
	}
	if len(cm.Parents) > 1 {
		text += "\n合并提交"
	}
	return text
}
func commitAgo(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	default:
		return t.Format("Jan 2")
	}
}
func graphWidth(w *workspacePage) float32 {
	return graphLaneW*float32(min(graphLanes(w.gitGraph), graphMaxLanes)-1) + 12
}
func commitGraphColor(t *native.Theme, i int) native.Color {
	cols := []native.Color{t.Accent, native.Hex("#b98a24"), native.Hex("#2b9c69"), t.Danger, native.Hex("#b557c7"), native.Hex("#269eb8")}
	return cols[i%len(cols)]
}
func paintCommitGraph(p *native.Painter, r native.Rect, gr *graphRow, hash string, head bool, t *native.Theme) {
	x := func(i int) float32 { return r.X + 6 + graphLaneW*float32(min(i, graphMaxLanes-1)) }
	top, mid, bottom := r.Y, r.Y+r.H/2, r.Y+r.H
	line := func(x1, y1, x2, y2 float32, color native.Color) {
		var path native.Path
		path.MoveTo(x1, y1)
		if x1 == x2 {
			path.LineTo(x2, y2)
		} else {
			ym := (y1 + y2) / 2
			path.CubeTo(x1, ym, x2, ym, x2, y2)
		}
		p.StrokePath(&path, 1.5, color)
	}
	for i, l := range gr.above {
		if l.hash == "" {
			continue
		}
		if l.hash == hash {
			line(x(i), top, x(gr.lane), mid, commitGraphColor(t, l.color))
		} else {
			line(x(i), top, x(i), mid, commitGraphColor(t, l.color))
		}
	}
	for i, l := range gr.below {
		if l.hash == "" {
			continue
		}
		through := i != gr.lane && i < len(gr.above) && gr.above[i].hash == l.hash
		if through || !gr.from[i] {
			line(x(i), mid, x(i), bottom, commitGraphColor(t, l.color))
		}
		if gr.from[i] {
			line(x(gr.lane), mid, x(i), bottom, commitGraphColor(t, l.color))
		}
	}
	color := commitGraphColor(t, gr.color)
	cx := x(gr.lane)
	radius := float32(4)
	if head {
		radius = 5
	} else if gr.merge {
		radius = 3.5
	}
	dot := native.Rect{X: cx - radius, Y: mid - radius, W: radius * 2, H: radius * 2}
	if head || gr.merge {
		p.Fill(dot, t.Background, radius)
		p.Stroke(dot, color, radius, 1.5)
	} else {
		p.Fill(dot, color, radius)
	}
}
func (a *App) gitCommitFileRow(c *native.Context, w *workspacePage, r gitCommitsRow, action *func()) {
	cm := w.gitHistory[r.at]
	tree := w.gitCommitTrees[cm.Hash]
	t := c.Theme()
	native.Row(c).Height(22).FillWidth().Padding(0, 24).Gap(6).AlignItems(native.Center).Children(func() {
		// Expanded content keeps every outgoing lane continuous.
		native.Box(c).Size(graphWidth(w), 22).Draw(func(p *native.Painter, b native.Rect) {
			if r.at < len(w.gitGraph) {
				for i, l := range w.gitGraph[r.at].below {
					if l.hash != "" {
						var path native.Path
						x := b.X + 6 + graphLaneW*float32(min(i, graphMaxLanes-1))
						path.MoveTo(x, b.Y)
						path.LineTo(x, b.Y+b.H)
						p.StrokePath(&path, 1.5, commitGraphColor(t, l.color))
					}
				}
			}
		})
		if r.depth > 0 {
			native.Box(c).Width(float32(r.depth) * 12).Shrink(0)
		}
		switch r.kind {
		case "files-status":
			label := "此提交没有文件改动"
			if tree.loading {
				label = "正在读取文件…"
			}
			if tree.err != "" {
				label = tree.err
			}
			native.Text(c, label).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1)
			if tree.err != "" && actionButton(c, "重试读取文件").Clicked() {
				*action = func() { a.gitLoadCommitTree(w, cm, tree) }
			}
		case "directory":
			b := treeButton(c, "提交目录 "+cm.Hash+" "+r.entry.Path).Grow(1).FillHeight().Gap(5).AlignItems(native.Center).Padding(0, 6, 0, 0)
			b.Children(func() {
				rotation := float32(0)
				collapsed := w.gitCommitDirs[cm.Hash+":"+r.entry.Path]
				if collapsed {
					rotation = -90
				}
				// Match the commit avatar's 15-wide column so the folder centers under it.
				native.Box(c).Width(15).Shrink(0).AlignItems(native.Center).Justify(native.Center).Children(func() {
					native.Icon(c, chevron).Size(10, 10).Rotate(rotation)
				})
				drawFileTreeFolderIcon(c, 12, !collapsed, false)
				native.Text(c, filepath.Base(r.entry.Path)).FontSize(11).SingleLine()
			})
			if b.Clicked() {
				*action = func() {
					if w.gitCommitDirs == nil {
						w.gitCommitDirs = map[string]bool{}
					}
					key := cm.Hash + ":" + r.entry.Path
					w.gitCommitDirs[key] = !w.gitCommitDirs[key]
				}
			}
		case "file":
			b := treeButton(c, "提交文件 "+cm.Hash+" "+r.entry.Path).Grow(1).FillHeight().Gap(5).AlignItems(native.Center).Padding(0, 6, 0, 0)
			b.Children(func() {
				// Match the commit avatar's 15-wide column so the icon centers under it.
				native.Box(c).Width(15).Shrink(0).AlignItems(native.Center).Justify(native.Center).Children(func() {
					drawFileIcon(c, r.entry.Path, 12, false)
				})
				native.Text(c, filepath.Base(r.entry.Path)).FontSize(11).SingleLine().Shrink(1)
				if dir := filepath.Dir(r.entry.Path); dir != "." && !a.preferences.CommitsTree {
					native.Text(c, dir).FontSize(10).TextColor(t.TextMuted).SingleLine().Shrink(3)
				}
				native.Spacer(c)
				native.Text(c, gitStatusCode(r.entry, true)).FontSize(10).TextColor(t.TextMuted)
			})
			if b.Clicked() {
				*action = func() { a.gitOpenCommit(w, cm, r.entry.Path) }
			}
		}
	})
}

type refBadge struct {
	gitview.Ref
	remotes []string
}

// label is what a badge says, as "main ⇄ origin".
func (b refBadge) label() string {
	if len(b.remotes) == 0 {
		return b.Name
	}
	return b.Name + " ⇄ " + strings.Join(b.remotes, ", ")
}

// refBadges returns the badges of a commit's refs: HEAD when detached,
// the branch checked out, the other branches, those of remotes, then
// the tags.
func refBadges(refs []gitview.Ref) []refBadge {
	local := map[string]int{}
	var out []refBadge
	rank := func(r gitview.Ref) int {
		switch {
		case r.Kind == gitview.RefHead:
			return 0
		case r.Head:
			return 1
		case r.Kind == gitview.RefBranch:
			return 2
		case r.Kind == gitview.RefRemote:
			return 3
		}
		return 4
	}
	sorted := slices.Clone(refs)
	slices.SortStableFunc(sorted, func(x, y gitview.Ref) int { return rank(x) - rank(y) })
	for _, r := range sorted {
		if r.Kind == gitview.RefRemote {
			if remote, name, ok := strings.Cut(r.Name, "/"); ok {
				if i, ok := local[name]; ok {
					out[i].remotes = append(out[i].remotes, remote)
					continue
				}
			}
		}
		if r.Kind == gitview.RefBranch {
			local[r.Name] = len(out)
		}
		out = append(out, refBadge{Ref: r})
	}
	return out
}

func badgeNames(rs []refBadge) string {
	var names []string
	for _, r := range rs {
		names = append(names, r.label())
	}
	return strings.Join(names, ", ")
}
func (a *App) gitCommitFileRows(w *workspacePage, hash string, at int, entries []gitview.Entry) []gitCommitsRow {
	var rows []gitCommitsRow
	if !a.preferences.CommitsTree {
		for _, e := range entries {
			rows = append(rows, gitCommitsRow{key: hash + ":file:" + e.Path, kind: "file", at: at, entry: e})
		}
		return rows
	}
	type node struct {
		dirs  map[string]*node
		files []gitview.Entry
	}
	root := &node{}
	for _, e := range entries {
		current := root
		parts := strings.Split(e.Path, "/")
		for _, part := range parts[:len(parts)-1] {
			if current.dirs == nil {
				current.dirs = map[string]*node{}
			}
			if current.dirs[part] == nil {
				current.dirs[part] = &node{}
			}
			current = current.dirs[part]
		}
		current.files = append(current.files, e)
	}
	var walk func(*node, string, int)
	walk = func(n *node, prefix string, depth int) {
		var names []string
		for name := range n.dirs {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			path := name
			if prefix != "" {
				path = prefix + "/" + name
			}
			rows = append(rows, gitCommitsRow{key: hash + ":dir:" + path, kind: "directory", at: at, depth: depth, entry: gitview.Entry{Path: path}})
			if !w.gitCommitDirs[hash+":"+path] {
				walk(n.dirs[name], path, depth+1)
			}
		}
		files := slices.Clone(n.files)
		slices.SortFunc(files, func(a, b gitview.Entry) int { return strings.Compare(a.Path, b.Path) })
		for _, e := range files {
			rows = append(rows, gitCommitsRow{key: hash + ":file:" + e.Path, kind: "file", at: at, depth: depth, entry: e})
		}
	}
	walk(root, "", 0)
	return rows
}
