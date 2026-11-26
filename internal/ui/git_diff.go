package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

var diffInlineIcon = icon(`<path d="M4 7h16M4 12h16M4 17h16"/>`)
var diffSplitIcon = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M12 4v16"/>`)
var diffUnfoldIcon = icon(`<path d="M12 22v-6"/><path d="M12 8V2"/><path d="M4 12H2"/><path d="M10 12H8"/><path d="M16 12h-2"/><path d="M22 12h-2"/><path d="m15 19-3 3-3-3"/><path d="m15 5-3-3-3 3"/>`)

// diffMidW is the width of the gutter between the sides of a split diff,
// where the bands joining each change's two sides run: Mayoi's diffMidW.
const diffMidW = float32(40)

const (
	diffViewModeInline = "inline"
	diffViewModeSplit  = "split"
)

func (a *App) diffViewSplit() bool {
	return a.application().preferences.DiffViewMode == diffViewModeSplit
}

func (a *App) setDiffViewMode(d *filePage, split bool) {
	d.gitSplit = split
	root := a.application()
	mode := diffViewModeInline
	if split {
		mode = diffViewModeSplit
	}
	if root.preferences.DiffViewMode == mode {
		return
	}
	root.preferences.DiffViewMode = mode
	_ = root.persist()
}

func (a *App) diffLayoutToggle(c *native.Context, d *filePage) {
	layout := 0
	if d.gitSplit {
		layout = 1
	}
	parts := native.SegmentedBase(c.Key("layout"), &layout, 2)
	t := c.Theme()
	parts.Track.Label("差异布局").Padding(1).Gap(1).Radius(6).Background(t.Surface).Border(1, t.Border).Children(func() {
		for index, glyph := range []*native.SVG{diffInlineIcon, diffSplitIcon} {
			label := "行内"
			if index == 1 {
				label = "并排"
			}
			segment := parts.Segment(index).Label(label).Tooltip(label).Padding(3, 5).Radius(5).TextColor(t.TextMuted)
			selectedButtonFace(c, segment, layout == index, 5, 0)
			segment.Children(func() { native.Icon(c, glyph).Size(13, 13) })
		}
	})
	if (layout == 1) != d.gitSplit {
		a.setDiffViewMode(d, layout == 1)
	}
}

// diffChangeRun is one visible change of a split diff: the run of pair
// rows it covers, all of one hunk, and the continuous span of rows its
// changed lines stand in on each side. The sides line up row by row, so
// a run's top and bottom are shared; where a change's lines of one side
// stand apart from the other's, the band curves between them as
// JetBrains' diff does.
type diffChangeRun struct {
	from, to      int  // pair rows [from, to) the change spans
	lt, lb        int  // rows [lt, lb) the change's left lines stand in
	rt, rb        int  // rows [rt, rb) the change's right lines stand in
	hunk          int  // the patch hunk the change belongs to
	pureInsertion bool // the change has no left lines at all
	pureDeletion  bool // the change has no right lines at all
}

// diffChangeRuns groups a split diff's pair rows into runs of changes,
// one per maximal run of rows carrying a hunk, as Mayoi's hunkRows do.
// Rows of context, section bands, gaps and fillers interrupt a run.
func diffChangeRuns(pairs []diffCodePair) []diffChangeRun {
	var runs []diffChangeRun
	for index := 0; index < len(pairs); {
		pair := pairs[index]
		if pair.left.hunk < 0 || (pair.left.kind != '-' && pair.left.kind != '+' && pair.right.kind != '-' && pair.right.kind != '+') {
			index++
			continue
		}
		run := diffChangeRun{from: index, hunk: pair.left.hunk, lt: index, rt: index, pureInsertion: true, pureDeletion: true}
		lb, rb := index, index
		for index < len(pairs) {
			row := pairs[index]
			if row.left.hunk != run.hunk {
				break
			}
			changed := row.left.kind == '-' || row.left.kind == '+' || row.right.kind == '-' || row.right.kind == '+'
			if !changed {
				break
			}
			if row.left.kind == '-' || row.left.kind == '+' {
				lb = index + 1
				run.pureInsertion = false
			}
			if row.right.kind == '-' || row.right.kind == '+' {
				rb = index + 1
				run.pureDeletion = false
			}
			index++
		}
		run.to = index
		run.lb, run.rb = max(run.lt, lb), max(run.rt, rb)
		runs = append(runs, run)
	}
	return runs
}

// diffMidColumn draws the gutter between a split diff's sides: the 1 DIP
// border of its left edge over a faint wash, and the band of every change
// in view joining its lines of the left side to its lines of the right,
// as Mayoi's paintDiffMid and paintDiffBands do. The column scrolls with
// the panes; rows of the aligned pair layout share one height, so the
// band runs from the change's left span to its right span with a cubic
// curve where the two stand apart.
func (a *App) diffMidColumn(c *native.Context, d *filePage, palette editorPalette) {
	native.Column(c.Key("diff-mid")).Width(diffMidW).FillHeight().MinWidth(0).Children(func() {
		// The spacer keeps the bands aligned with the panes' rows: it
		// stands as tall as their 原版本/新版本 headers.
		native.Text(c, " ").FontSize(10).Padding(5, 10).TextColor(native.RGBA(0, 0, 0, 0))
		box := native.Box(c.Key("diff-mid-bands"))
		box.FillWidth().Grow(1).MinHeight(0)
		rows := diffChangeRuns(d.gitPairs)
		// The pointer's row decides which change glows, as Mayoi's arrow
		// hover does: the glow fades in and out over 140ms.
		hovered := -1
		if x, y, over := box.PointerPosition(); over && x >= 0 && x <= diffMidW {
			hovered = d.diffMidRowAt(y)
		}
		hot := -1
		if hovered >= 0 {
			for index, run := range rows {
				if hovered >= run.from && hovered < run.to {
					hot = index
					break
				}
			}
		}
		if d.gitMidGlow == nil {
			d.gitMidGlow = map[int]float32{}
		}
		for key := range d.gitMidGlow {
			delete(d.gitMidGlow, key)
		}
		for index := range rows {
			if glow := box.Animate(struct{ k int }{index}, btoiff(index == hot), 140*time.Millisecond); glow > 0 {
				d.gitMidGlow[index] = glow
			}
		}
		box.Draw(func(p *native.Painter, r native.Rect) {
			p.Fill(r, palette.text.Alpha(0.02).Over(palette.background), 0)
			p.Fill(native.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, palette.border, 0)
			if len(rows) == 0 || len(d.gitPairs) == 0 {
				return
			}
			padding := float32(6)
			lines := &d.gitLeftLines
			top := func(index int) float32 {
				if lines.Count() == len(d.gitPairs) {
					return lines.Top(index)
				}
				return float32(index) * 20
			}
			rowY := func(index int) float32 {
				return r.Y + padding + top(index) - d.gitSplitY
			}
			// Gap markers and section bands span the diff's whole width
			// in Mayoi, so they cover the mid gutter too: the band's own
			// faint tint and rail, without icon or label, which the left
			// pane's strip carries.
			first := max(0, int((d.gitSplitY-padding)/20)-1)
			last := min(len(d.gitPairs), int((d.gitSplitY+r.H-padding)/20)+2)
			for index := first; index < last; index++ {
				row := d.gitPairs[index].left
				height := float32(20)
				if lines.Count() == len(d.gitPairs) {
					height = lines.Top(index+1) - lines.Top(index)
				}
				y := rowY(index)
				switch {
				case row.foldMarker():
					p.Fill(native.Rect{X: r.X, Y: y, W: diffMidW, H: height}, palette.text.Alpha(0.025), 0)
				case row.hunkHeader():
					p.Fill(native.Rect{X: r.X, Y: y, W: diffMidW, H: height}, palette.text.Alpha(0.035), 0)
				}
				d.gitRowSelection.paint(p, native.Rect{X: r.X, Y: y, W: diffMidW, H: height}, index, palette, false)
			}
			xL, xR := r.X+1, r.X+diffMidW-1
			cx := (xL + xR) / 2
			line := palette.lineTheme()
			for index, run := range rows {
				yt, yb := rowY(run.lt), rowY(run.lb)
				yrT, yrB := rowY(run.rt), rowY(run.rb)
				if max(yt, yrT) > r.Y+r.H || min(yb, yrB) < r.Y {
					continue
				}
				lc, rc := line.Danger, line.Success
				if run.pureInsertion {
					lc = line.Success
				}
				if run.pureDeletion {
					rc = line.Danger
				}
				alpha := float32(0.16)
				if run.hunk == d.gitHunk {
					alpha = 0.22
				}
				alpha += 0.22 * d.gitMidGlow[index]
				lt, lb := yt, yb
				rt, rb := yrT, yrB
				var band native.Path
				band.MoveTo(xL, lt).CubeTo(cx, lt, cx, rt, xR, rt).LineTo(xR, rb).CubeTo(cx, rb, cx, lb, xL, lb).Close()
				p.FillPathGradient(&band, native.LinearGradient{From: lc.Alpha(alpha), To: rc.Alpha(alpha), Angle: 90})
				var edges native.Path
				edges.MoveTo(xL, lt).CubeTo(cx, lt, cx, rt, xR, rt)
				edges.MoveTo(xL, lb).CubeTo(cx, lb, cx, rb, xR, rb)
				edge := float32(0.4) + 0.4*d.gitMidGlow[index]
				p.StrokePathGradient(&edges, 1, native.LinearGradient{From: lc.Alpha(edge), To: rc.Alpha(edge), Angle: 90})
			}
		})
		if d.canRevertDiff() {
			box.Children(func() {
				height := box.Bounds().H
				if height <= 0 {
					_, height = c.Size()
					c.AnimationFrame()
				}
				for _, run := range rows {
					y := 6 + float32(run.from)*20 - d.gitSplitY - 8
					if !run.pureInsertion {
						y += 10
					}
					if y >= 0 && y+16 <= height {
						a.diffArrow(c, d, d.gitPairs[run.from], (diffMidW-16)/2, y, 16, palette)
					}
				}
			})
		}
	})
}

// diffMidRowAt maps a y inside the mid column, relative to the column's
// top, to the pair row under it, or -1 outside the rows.
func (d *filePage) diffMidRowAt(y float32) int {
	padding := float32(6)
	offset := y + d.gitSplitY - padding
	if offset < 0 {
		return -1
	}
	row := int(offset / 20)
	if row < 0 || row >= len(d.gitPairs) {
		return -1
	}
	return row
}

func btoiff(on bool) float32 {
	if on {
		return 1
	}
	return 0
}

func (d *filePage) diffOpenTarget() (string, bool) {
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
	return path, openable
}

func (a *App) gitDiffToolbar(c *native.Context, d *filePage) {
	native.Column(c).FillWidth().Children(func() {
		if d.gitCommit != "" && len(d.gitCommitFiles) > 0 {
			native.Text(c, d.gitCommitInfo.Subject).FontSize(12).Bold().SingleLine().Padding(4, 14)
			native.Row(c).FillWidth().Padding(0, 14, 4, 14).Gap(6).Children(func() {
				names := make([]string, len(d.gitCommitFiles))
				for i, entry := range d.gitCommitFiles {
					names[i] = entry.Path
				}
				current := names[d.gitCommitFile]
				native.Select(c, &current, names).Label("提交中的文件").Grow(1).MinWidth(0).FontSize(11).Disabled(d.loading)
				if current != names[d.gitCommitFile] {
					for i, name := range names {
						if name == current {
							d.gitCommitFile = i
							a.gitLoadCommit(d, d.gitCommitInfo)
							break
						}
					}
				}
			})
		}
		if d.gitNote != "" {
			native.Text(c, d.gitNote).FontSize(11).TextColor(c.Theme().TextMuted).Padding(4, 14)
		}
	})
}

func (a *App) gitDiffControls(c *native.Context, d *filePage) {
	w := a.workspace(d.root)
	total := len(d.gitReview.Hunks)
	if total > 0 {
		d.gitHunk = min(max(d.gitHunk, 0), total-1)
		native.Text(c, fmt.Sprintf("%d/%d", d.gitHunk+1, total)).FontSize(10).TextColor(c.Theme().TextMuted)
	}
	if gitIconButton(c.Key("prev-hunk"), "上一个修改", upIcon, total == 0).Clicked() {
		a.gitStepHunk(d, -1)
	}
	if gitIconButton(c.Key("next-hunk"), "下一个修改", downIcon, total == 0).Clicked() {
		a.gitStepHunk(d, 1)
	}
	a.diffLayoutToggle(c, d)
	path, openable := d.diffOpenTarget()
	if gitIconButton(c.Key("open-file"), "打开文件", gitOpenFileIcon, !openable).Clicked() {
		a.openFile(d.root, path)
	}
	more := gitIconButton(c.Key("more"), "更多差异操作", gitMoreIcon, false)
	more.Menu(func(m *native.Menu) {
		if m.Item("上一个文件").Chosen() {
			a.gitStepFile(d, -1)
		}
		if m.Item("下一个文件").Chosen() {
			a.gitStepFile(d, 1)
		}
		m.Separator()
		if d.turnReview != nil {
			return
		}
		for _, intent := range []string{"解释这些改动", "检查问题", "继续修改"} {
			if m.Item(intent).Disabled(total == 0).Chosen() {
				hunk := d.gitReview.Hunks[d.gitHunk]
				entry := d.gitReview.Entry
				if d.gitCommit != "" && len(d.gitCommitFiles) > 0 {
					entry = d.gitCommitFiles[d.gitCommitFile]
				}
				a.gitToCodex(w, []gitview.Entry{entry}, d.gitReview.Staged, intent, &hunk)
			}
		}
	})
}

func (a *App) gitStepHunk(d *filePage, step int) {
	if d.gitReview == nil || len(d.gitReview.Hunks) == 0 {
		return
	}
	total := len(d.gitReview.Hunks)
	d.gitHunk = (d.gitHunk + step + total) % total
	d.scroll.Y = float32(diffHunkRow(d.gitRows, d.gitHunk)) * 20
	d.pinFrame, d.pinY = 2, d.scroll.Y
	if d.gitSplit {
		for index, pair := range d.gitPairs {
			if pair.left.hunk == d.gitHunk && (pair.left.kind == '-' || pair.right.kind == '+') {
				d.gitSplitY = float32(index) * 20
				d.gitLeftScroll.Y, d.gitRightScroll.Y = d.gitSplitY, d.gitSplitY
				break
			}
		}
	}
}

func (a *App) gitStepFile(d *filePage, step int) {
	if d.turnReview != nil {
		d.turnReview.selected = (d.turnReview.selected + step + len(d.turnReview.files)) % len(d.turnReview.files)
		a.setTurnReviewFile(d)
		return
	}
	if d.loading {
		return
	}
	if d.gitCommit != "" && len(d.gitCommitFiles) > 0 {
		d.gitCommitFile = (d.gitCommitFile + step + len(d.gitCommitFiles)) % len(d.gitCommitFiles)
		a.gitLoadCommit(d, d.gitCommitInfo)
		return
	}
	w := a.workspace(d.root)
	group := "更改"
	if d.compare == "已暂存" {
		group = "已暂存的更改"
	}
	entries := gitEntries(w, group)
	if len(entries) == 0 {
		return
	}
	relative, _ := filepath.Rel(d.root, d.path)
	index := 0
	for i, e := range entries {
		if e.Path == relative {
			index = i
			break
		}
	}
	next := (index + step + len(entries)) % len(entries)
	a.openGitDiff(w, entries[next], group == "已暂存的更改")
}

func splitPatch(review gitview.Review) (string, string) {
	_, pairs := diffCodeRows(&review)
	return diffPairText(pairs)
}

func (d *filePage) centerInitialDiff(split bool, viewportHeight float32) {
	layout := 0
	if split {
		layout = 1
	}
	if !d.gitInitialCenter[layout] || viewportHeight <= 0 {
		return
	}
	row, count, padding := diffHunkRow(d.gitRows, 0), len(d.gitRows), float32(8)
	if split {
		count, padding = len(d.gitPairs), 6
		for index, pair := range d.gitPairs {
			if pair.left.kind == '-' || pair.right.kind == '+' {
				row = index
				break
			}
		}
	}
	maxScroll := max(0, float32(count)*20+padding*2-viewportHeight)
	offset := min(maxScroll, max(0, padding+float32(row)*20+10-viewportHeight/2))
	if split {
		d.gitSplitY, d.gitLeftScroll.Y, d.gitRightScroll.Y = offset, offset, offset
	} else {
		d.scroll.Y = offset
		d.pinFrame = 0
	}
	d.gitInitialCenter[layout] = false
}

func (a *App) gitSplitView(c *native.Context, d *filePage, available float32) {
	t := c.Theme()
	palette := a.activeEditorPalette(t)
	_, height := c.Size()
	if d.gitLeft == nil || d.gitRight == nil {
		if d.gitPairs == nil {
			d.rebuildDiffRows()
		}
		left, right := diffPairText(d.gitPairs)
		d.gitLeft, d.gitRight = native.NewTextBuffer(left), native.NewTextBuffer(right)
	}
	d.centerInitialDiff(true, d.gitLeftScroll.Bounds(c).H)
	viewport := d.gitLeftScroll.Bounds(c).H
	if viewport <= 0 {
		_, viewport = c.Size()
	}
	if a.gitPlaceFoldFocus(d, true, viewport) {
		d.gitLeftScroll.Y = d.gitSplitY
		c.AnimationFrame()
	}
	if d.gitInitialCenter[1] {
		c.AnimationFrame()
	}
	// Both buffers contain the same number of aligned rows.
	y := d.gitSplitY
	if d.gitLeftScroll.Y != y {
		y = d.gitLeftScroll.Y
	} else if d.gitRightScroll.Y != y {
		y = d.gitRightScroll.Y
	}
	d.gitSplitY, d.gitLeftScroll.Y, d.gitRightScroll.Y = y, y, y
	leftName, rightName := "原版本", "新版本"
	if d.compare == "外部更改" {
		leftName, rightName = "本地编辑", "磁盘版本"
	}
	native.Row(c.Key("split")).FillWidth().Grow(1).MinHeight(0).Children(func() {
		for sideIndex, side := range []struct {
			name    string
			buffer  *native.TextBuffer
			scroll  *native.ScrollState
			lines   *native.TextLines
			hscroll *native.ScrollState
			width   float32
			color   native.Color
		}{{leftName, d.gitLeft, &d.gitLeftScroll, &d.gitLeftLines, &d.gitLeftHScroll, d.gitLeftWidth, palette.deletedBackground}, {rightName, d.gitRight, &d.gitRightScroll, &d.gitRightLines, &d.gitRightHScroll, d.gitRightWidth, palette.addedBackground}} {
			if sideIndex == 1 {
				a.diffMidColumn(c, d, palette)
			}
			native.Column(c.Key(side.name)).Grow(1).FillHeight().MinWidth(0).Background(palette.background).Children(func() {
				headerText := native.Text(c, side.name).FontSize(10).TextColor(t.TextMuted).Padding(5, 10)
				if sideIndex == 0 {
					d.gitSplitHeader = headerText.Bounds().H
				}
				native.ScrollHorizontal(c.Key("horizontal")).TrackScroll(side.hscroll).FillWidth().Grow(1).MinHeight(0).Children(func() {
					area := palette.area(native.TextAreaBufferBase(c, side.buffer)).Label("差异 "+side.name).ReadOnly(true).Font(a.codeFontFamily()).FontSize(12).
						FixedLineHeight(20).NoWrap().Width(max((available-diffMidW)/2, side.width)).Shrink(0).FillHeight().Padding(6, 10, 6, diffSideTextX).TrackScroll(side.scroll).TrackLines(side.lines)
					highlights := d.gitLeftHighlights
					if sideIndex == 1 {
						highlights = d.gitRightHighlights
					}
					snapshot := side.buffer.Snapshot()
					firstLine := max(0, int(side.scroll.Y/20)-2)
					start, _ := snapshot.LineRange(firstLine)
					_, end := snapshot.LineRange(min(snapshot.LineCount()-1, firstLine+int(height/20)+4))
					if d.buffer != nil && d.highlightVersion == d.buffer.Version() {
						area.TextRanges(palette.ranges(highlights, start, end)...)
					}
					anchor, caret := area.TextAnchorCaret()
					focused := area.Focused()
					if focused {
						d.gitCursorSide = sideIndex
					}
					if d.gitSplitFocus != nil && d.gitFocusSide == sideIndex {
						area.SetTextSelection(d.gitSplitFocus[0], d.gitSplitFocus[1])
						if !focused {
							area.Focus()
							focused = true
						}
						anchor, caret = d.gitSplitFocus[0], d.gitSplitFocus[1]
						d.gitSplitCaret[sideIndex] = caret
						d.gitSplitFocus = nil
					}
					area.OnTextKey(func(k native.TextKey) bool {
						if k.Mods != 0 {
							return false
						}
						row := snapshot.LineAt(d.gitSplitCaret[sideIndex])
						if k.Key == native.KeyEnter {
							if row < len(d.gitPairs) && d.gitPairs[row].left.foldMarker() {
								d.gitFocusSide = sideIndex
								a.gitToggleRange(d, d.gitPairs[row].left.fold)
								return true
							}
							a.gitOpenFoldFileAt(d, row, true)
							return true
						}
						return false
					})
					if area.Shortcut(0, native.KeyZ) {
						row := snapshot.LineAt(d.gitSplitCaret[sideIndex])
						if row < len(d.gitPairs) && d.gitPairs[row].left.fold != nil {
							d.gitFocusSide = sideIndex
							a.gitToggleRange(d, d.gitPairs[row].left.fold)
						}
					}
					if caret != d.gitSplitCaret[sideIndex] {
						d.gitSplitCaret[sideIndex] = caret
						line := side.buffer.Snapshot().LineAt(caret)
						if line < len(d.gitPairs) && d.gitPairs[line].left.hunk >= 0 {
							d.gitHunk = d.gitPairs[line].left.hunk
						}
					}
					area.Draw(func(p *native.Painter, r native.Rect) {
						first := max(0, side.lines.At(side.scroll.Y-6)-1)
						last := min(len(d.gitPairs), side.lines.At(side.scroll.Y+r.H-6)+2)
						for i := first; i < last; i++ {
							row := d.gitPairs[i].left
							if sideIndex == 1 {
								row = d.gitPairs[i].right
							}
							y := r.Y + 6 + side.lines.Top(i) - side.scroll.Y
							height := side.lines.Top(i+1) - side.lines.Top(i)
							if row.hunkHeader() {
								p.Fill(native.Rect{X: r.X, Y: y, W: r.W, H: height}, palette.text.Alpha(0.035), 0)
								p.Fill(native.Rect{X: r.X, Y: y, W: 3, H: height}, palette.muted.Alpha(0.45), 0)
								continue
							}
							// The row across from lines only the other
							// side has carries no text: it fills with the
							// faint diagonal stripes of Mayoi's
							// paintFiller, so it reads as a gap in the
							// side rather than an empty line.
							if row.fold == nil && row.hunk >= 0 && row.text == "" && row.kind != '-' && row.kind != '+' {
								fill := native.Rect{X: r.X, Y: y, W: r.W, H: height}
								p.Fill(fill, palette.text.Alpha(0.025), 0)
								var stripes native.Path
								for x := fill.X - fill.H; x < fill.X+fill.W; x += 8 {
									stripes.MoveTo(x, fill.Y+fill.H).LineTo(x+fill.H, fill.Y)
								}
								p.Clip(fill, 0, func() {
									p.StrokePath(&stripes, 1, palette.text.Alpha(0.04))
								})
								continue
							}
							if row.kind == '-' || row.kind == '+' {
								p.Fill(native.Rect{X: r.X, Y: y, W: r.W, H: height}, side.color, 0)
								row.paintWords(p, native.Rect{X: r.X + diffSideTextX, Y: y, H: height}, native.Font{Family: a.codeFontFamily(), Size: 12}, palette)
							}
						}
						if focused {
							font := native.Font{Family: a.codeFontFamily(), Size: 12}
							d.gitSplitWords.paint(p, r, side.buffer, snapshot, [2]int{anchor, caret}, false, side.lines, side.scroll.Y, 6, diffSideTextX, font, palette.text)
						}
					})
					area.DrawOver(func(p *native.Painter, r native.Rect) {
						gutterText := measureGutterText(p, a.codeFontFamily(), 12)
						first := max(0, side.lines.At(side.scroll.Y-6)-1)
						last := min(len(d.gitPairs), side.lines.At(side.scroll.Y+r.H-6)+2)
						for index := first; index < last; index++ {
							row := d.gitPairs[index].left
							if sideIndex == 1 {
								row = d.gitPairs[index].right
							}
							y := r.Y + 6 + side.lines.Top(index) - side.scroll.Y
							height := side.lines.Top(index+1) - side.lines.Top(index)
							vx := r.X + side.hscroll.X
							paintCursor := func() {
								selection := d.gitRowSelection
								selection.paint(p, native.Rect{X: vx, Y: y, W: r.W, H: height}, index, palette, sideIndex == 0)
							}
							if row.hunkHeader() {
								gutter := native.Rect{X: vx, Y: y, W: diffSideTextX - 4, H: height}
								p.Fill(gutter, palette.text.Alpha(0.035).Over(palette.background), 0)
								p.Fill(native.Rect{X: vx, Y: y, W: 3, H: height}, palette.muted.Alpha(0.45), 0)
								paintCursor()
								continue
							}
							// A filler row's gutter stays plain over the
							// text scrolled beneath it; the stripes of
							// the text pass cover the rest, so the side
							// reads empty from the rail to the text's end.
							if row.fold == nil && row.hunk >= 0 && row.text == "" && row.kind != '-' && row.kind != '+' {
								gutter := native.Rect{X: vx, Y: y, W: diffSideTextX - 4, H: height}
								p.Fill(gutter, palette.text.Alpha(0.025).Over(palette.background), 0)
								p.Fill(native.Rect{X: vx, Y: y, W: 3, H: height}, palette.muted.Alpha(0.45), 0)
								paintCursor()
								continue
							}
							// As in the unified diff, the side's gutter
							// stays at the view's edge: the pass scrolls
							// with the text, so it draws the scroll's
							// width to the right and covers whatever
							// moves beneath it with the row's backing.
							gutter := native.Rect{X: vx, Y: y, W: diffSideTextX - 4, H: height}
							rowbg := palette.background
							if row.kind == '+' {
								rowbg = palette.addedBackground
							}
							if row.kind == '-' {
								rowbg = palette.deletedBackground
							}
							p.Fill(gutter, rowbg, 0)
							rail := palette.muted.Alpha(0.45)
							if row.kind == '-' || row.kind == '+' {
								ink := palette.lineTheme().Success
								if row.kind == '-' {
									ink = palette.lineTheme().Danger
								}
								if row.hunk == d.gitHunk {
									rail = ink
								} else {
									rail = ink.Mix(palette.background, 0.55)
								}
							}
							p.Fill(native.Rect{X: vx, Y: y, W: 3, H: height}, rail, 0)
							number := row.old
							if sideIndex == 1 {
								number = row.next
							}
							gutterText.drawDiff(p, vx+3, y, height, row.kind, palette.lineTheme(), number)
							paintCursor()
						}
					})
					if d.gitCursorSide == sideIndex {
						d.gitRowSelection = diffSelection(snapshot, anchor, caret, focused)
					}
				})
			})
		}
		d.diffSplitFoldButtons(c, palette, available)
	})
}

// diffSplitFoldButtons draws Mayoi's gap rows across the whole split view,
// as one strip over both panes with the rail at the left edge of the view,
// the unfold icon at 13 DIP and the label at 31. The strip sits inside the
// split row, above both text areas; horizontal scrolling moves it with the
// left pane, as the unified row of a side by side diff.
func (d *filePage) diffSplitFoldButtons(c *native.Context, palette editorPalette, available float32) {
	padding := float32(6)
	header := d.gitSplitHeader
	count := len(d.gitPairs)
	lines := &d.gitLeftLines
	scroll := &d.gitLeftScroll
	height := scroll.Bounds(c).H
	if height <= 0 {
		_, height = c.Size()
	}
	first := max(0, lines.At(scroll.Y-padding)-1)
	last := min(count, lines.At(scroll.Y+height-padding)+2)
	top := lines.Top
	if lines.Count() != count || lines.Top(1) < 20 {
		top = func(index int) float32 { return float32(index) * 20 }
		first = max(0, int((scroll.Y-padding)/20)-1)
		last = min(count, int((scroll.Y+height-padding)/20)+2)
	}
	for index := first; index < last; index++ {
		row := d.gitPairs[index].left
		if !row.foldMarker() {
			continue
		}
		fold := row.fold
		y := header + padding + top(index) - scroll.Y
		title := fmt.Sprintf("显示 %d 行未更改的内容", fold.count)
		button := native.ButtonBase(c.Key(fold.start)).Label(title).Tooltip(title).Role(native.RoleDisclosure).Expanded(false).
			Absolute().Left(d.gitLeftHScroll.X).Top(y).Width(available).Height(20)
		button.HandleInput(func(event native.InputEvent) bool {
			if event.Kind != native.InputScroll || event.DY == 0 || event.Mods&native.Shift != 0 {
				return false
			}
			next := min(scroll.MaxY, max(0, scroll.Y+event.DY))
			if next == scroll.Y {
				return false
			}
			scroll.Y = next
			return true
		})
		hovered := button.Hovered() || button.FocusVisible()
		button.Draw(func(p *native.Painter, r native.Rect) {
			background := palette.text.Alpha(0.025).Over(palette.background)
			ink := palette.muted
			if hovered {
				background = palette.accent.Alpha(0.08).Over(palette.background)
				ink = palette.text
			}
			p.Fill(r, background, 0)
			p.Fill(native.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}, palette.muted.Alpha(0.45), 0)
			p.Icon(diffUnfoldIcon, native.Rect{X: r.X + 13, Y: r.Y + r.H/2 - 6, W: 12, H: 12}, ink)
			p.Text(r.X+31, r.Y, title, 12, ink)
		})
		if button.Clicked() {
			d.gitToggleFold = fold
			d.gitFocusSide = 0
			c.AnimationFrame()
		}
	}
}

// diffFoldButtons draws Mayoi's gap rows: a whole-row hover strip over the
// full view width with the neutral rail at its left edge, the unfold icon at
// 13 DIP and the label at 31, all drawn in one pass so no source text shows
// through. Only folded gaps get a row; open ranges are plain lines that Z
// folds again. Clicking anywhere on the row or pressing Enter opens the gap.
// In split diffs both halves share one accessibility control, and only the
// left half carries the icon and label.
func (d *filePage) diffFoldButtons(c *native.Context, palette editorPalette, split, showLabel bool, lines *native.TextLines, scroll *native.ScrollState, horizontal, width float32) {
	padding, count := float32(8), len(d.gitRows)
	if split {
		padding, count = 6, len(d.gitPairs)
	}
	height := scroll.Bounds(c).H
	if height <= 0 {
		_, height = c.Size()
	}
	first := max(0, lines.At(scroll.Y-padding)-1)
	last := min(count, lines.At(scroll.Y+height-padding)+2)
	top := lines.Top
	if lines.Count() != count || lines.Top(1) < 20 {
		top = func(index int) float32 { return float32(index) * 20 }
		first = max(0, int((scroll.Y-padding)/20)-1)
		last = min(count, int((scroll.Y+height-padding)/20)+2)
	}
	for index := first; index < last; index++ {
		var row diffCodeRow
		if split {
			row = d.gitPairs[index].left
		} else {
			row = d.gitRows[index]
		}
		if !row.foldMarker() {
			continue
		}
		fold := row.fold
		y := padding + top(index) - scroll.Y
		title := fmt.Sprintf("显示 %d 行未更改的内容", fold.count)
		button := native.ButtonBase(c.Key(struct {
			start int
			label bool
		}{fold.start, showLabel})).Label(title).Tooltip(title).Role(native.RoleDisclosure).Expanded(false).
			Absolute().Left(horizontal).Top(y).Width(width).Height(20)
		button.HandleInput(func(event native.InputEvent) bool {
			if event.Kind != native.InputScroll || event.DY == 0 || event.Mods&native.Shift != 0 {
				return false
			}
			next := min(scroll.MaxY, max(0, scroll.Y+event.DY))
			if next == scroll.Y {
				return false
			}
			scroll.Y = next
			return true
		})
		hovered := button.Hovered() || button.FocusVisible()
		button.Draw(func(p *native.Painter, r native.Rect) {
			background := palette.text.Alpha(0.025).Over(palette.background)
			ink := palette.muted
			if hovered {
				background = palette.accent.Alpha(0.08).Over(palette.background)
				ink = palette.text
			}
			p.Fill(r, background, 0)
			p.Fill(native.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}, palette.muted.Alpha(0.45), 0)
			if showLabel {
				p.Icon(diffUnfoldIcon, native.Rect{X: r.X + 13, Y: r.Y + r.H/2 - 6, W: 12, H: 12}, ink)
				p.Text(r.X+31, r.Y, title, 12, ink)
			}
		})
		if button.Clicked() {
			d.gitToggleFold = fold
			c.AnimationFrame()
		}
	}
}
