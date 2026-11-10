package ui

import (
	"context"
	"fmt"
	"path/filepath"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
)

func (a *App) gitDiffToolbar(c *native.Context, d *filePage) {
	w := a.workspace(d.root)
	native.Column(c).FillWidth().Children(func() {
		if d.turnReview != nil {
			names := make([]string, len(d.turnReview.files))
			for i, f := range d.turnReview.files {
				names[i] = f.Path
			}
			current := names[d.turnReview.selected]
			native.Select(c, &current, names).Label("本轮修改的文件").FillWidth().FontSize(12)
			if current != names[d.turnReview.selected] {
				for i, name := range names {
					if name == current {
						d.turnReview.selected = i
						a.setTurnReviewFile(d)
						break
					}
				}
			}
		}
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
		native.Row(c).FillWidth().Height(34).Padding(0, 12).Gap(4).AlignItems(native.Center).Children(func() {
			native.Text(c, d.compare).FontSize(10).TextColor(c.Theme().TextMuted).SingleLine().Grow(1).MinWidth(0)
			if d.gitReview == nil {
				return
			}
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
			layout := "并排"
			if d.gitSplit {
				layout = "行内"
			}
			if actionButton(c.Key("layout"), layout).Height(26).Clicked() {
				d.gitSplit = !d.gitSplit
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
		})
		if d.gitNote != "" {
			native.Text(c, d.gitNote).FontSize(11).TextColor(c.Theme().TextMuted).Padding(4, 14)
		}
		if d.gitReview != nil && d.gitReview.Blockable && len(d.gitReview.Hunks) > 0 {
			native.Row(c).FillWidth().Padding(0, 14, 5, 14).Gap(6).AlignItems(native.Center).Children(func() {
				label := "暂存此修改块"
				if d.gitReview.Staged {
					label = "取消暂存此修改块"
				}
				if actionButton(c, label).Height(26).Disabled(w.gitBusy != "" || w.gitPreparing).Clicked() {
					review, index := *d.gitReview, d.gitHunk
					apply := func() {
						a.gitAction(w, label, func(ctx context.Context) error {
							return gitview.ApplyHunk(ctx, w.root, review, index)
						}, nil)
					}
					if review.Staged {
						apply()
					} else {
						a.gitWithSavedFiles(w, []string{review.Entry.Path}, apply)
					}
				}
				native.Text(c, fmt.Sprintf("+%d −%d", d.gitReview.Added, d.gitReview.Deleted)).FontSize(10).TextColor(c.Theme().TextMuted)
			})
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
	palette := githubEditorPalette(t.Dark)
	_, height := c.Size()
	if d.gitLeft == nil || d.gitRight == nil {
		if d.gitPairs == nil {
			d.gitRows, d.gitPairs = diffCodeRows(d.gitReview)
		}
		left, right := diffPairText(d.gitPairs)
		d.gitLeft, d.gitRight = native.NewTextBuffer(left), native.NewTextBuffer(right)
	}
	d.centerInitialDiff(true, d.gitLeftScroll.Bounds(c).H)
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
	native.Row(c.Key("split")).FillWidth().Grow(1).MinHeight(0).Gap(1).Background(palette.border).Children(func() {
		for sideIndex, side := range []struct {
			name    string
			buffer  *native.TextBuffer
			scroll  *native.ScrollState
			lines   *native.TextLines
			hscroll *native.ScrollState
			width   float32
			color   native.Color
		}{{"原版本", d.gitLeft, &d.gitLeftScroll, &d.gitLeftLines, &d.gitLeftHScroll, d.gitLeftWidth, palette.deletedBackground}, {"新版本", d.gitRight, &d.gitRightScroll, &d.gitRightLines, &d.gitRightHScroll, d.gitRightWidth, palette.addedBackground}} {
			native.Column(c.Key(side.name)).Grow(1).FillHeight().MinWidth(0).Background(palette.background).Children(func() {
				native.Text(c, side.name).FontSize(10).TextColor(t.TextMuted).Padding(5, 10)
				native.ScrollHorizontal(c.Key("horizontal")).TrackScroll(side.hscroll).FillWidth().Grow(1).MinHeight(0).Children(func() {
					area := palette.area(native.TextAreaBufferBase(c, side.buffer)).Label("差异 "+side.name).ReadOnly(true).Font(a.codeFontFamily()).FontSize(12).
						FixedLineHeight(20).NoWrap().Width(max((available-1)/2, side.width)).Shrink(0).FillHeight().Padding(6, 10, 6, 58).TrackScroll(side.scroll).TrackLines(side.lines)
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
					_, caret := area.TextAnchorCaret()
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
							if row.kind == '-' || row.kind == '+' {
								y := r.Y + 6 + side.lines.Top(i) - side.scroll.Y
								height := side.lines.Top(i+1) - side.lines.Top(i)
								p.Fill(native.Rect{X: r.X, Y: y, W: r.W, H: height}, side.color, 0)
								if row.hunk == d.gitHunk {
									p.Fill(native.Rect{X: r.X + 50, Y: y, W: 2, H: height}, palette.accent, 0)
								}
							}
						}
					})
					area.DrawOver(func(p *native.Painter, r native.Rect) {
						first := max(0, side.lines.At(side.scroll.Y-6)-1)
						last := min(len(d.gitPairs), side.lines.At(side.scroll.Y+r.H-6)+2)
						for index := first; index < last; index++ {
							row := d.gitPairs[index].left
							if sideIndex == 1 {
								row = d.gitPairs[index].right
							}
							number := row.old
							if sideIndex == 1 {
								number = row.next
							}
							y := r.Y + 6 + side.lines.Top(index) - side.scroll.Y
							drawDiffLineNumber(p, r.X+10, y, number, row.kind, palette.lineTheme())
						}
					})
				})
			})
		}
	})
}
