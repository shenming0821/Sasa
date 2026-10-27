package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

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
	hunk := d.gitReview.Hunks[d.gitHunk]
	d.scroll.Y = float32(hunk.PatchLine) * 20
	d.pinFrame, d.pinY = 2, d.scroll.Y
	// Split rows have their own line mapping because additions/deletions align.
	if d.gitSplit && d.gitLeft != nil {
		left := d.gitLeft.Snapshot()
		for i := 0; i < left.LineCount(); i++ {
			if left.Line(i) == hunk.Header {
				d.gitLeftScroll.Y, d.gitRightScroll.Y = float32(i)*20, float32(i)*20
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
	var left, right strings.Builder
	for _, hunk := range review.Hunks {
		left.WriteString(hunk.Header + "\n")
		right.WriteString(hunk.Header + "\n")
		old, next := hunk.OldStart, hunk.NewStart
		lines := strings.Split(strings.TrimSuffix(hunk.Patch, "\n"), "\n")
		for i := 1; i < len(lines); {
			line := lines[i]
			if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "+") {
				var deleted, added []string
				for i < len(lines) && (strings.HasPrefix(lines[i], "-") || strings.HasPrefix(lines[i], "+")) {
					if lines[i][0] == '-' {
						deleted = append(deleted, lines[i][1:])
					} else {
						added = append(added, lines[i][1:])
					}
					i++
				}
				for j := range max(len(deleted), len(added)) {
					if j < len(deleted) {
						fmt.Fprintf(&left, "%4d − %s\n", old, deleted[j])
						old++
					} else {
						left.WriteString("\n")
					}
					if j < len(added) {
						fmt.Fprintf(&right, "%4d + %s\n", next, added[j])
						next++
					} else {
						right.WriteString("\n")
					}
				}
			} else {
				if strings.HasPrefix(line, " ") {
					fmt.Fprintf(&left, "%4d   %s\n", old, line[1:])
					fmt.Fprintf(&right, "%4d   %s\n", next, line[1:])
					old++
					next++
				} else {
					left.WriteString(line + "\n")
					right.WriteString(line + "\n")
				}
				i++
			}
		}
	}
	return left.String(), right.String()
}

func (a *App) gitSplitView(c *native.Context, d *filePage, available float32) {
	t := c.Theme()
	if d.gitLeft == nil || d.gitRight == nil {
		left, right := splitPatch(*d.gitReview)
		d.gitLeft, d.gitRight = native.NewTextBuffer(left), native.NewTextBuffer(right)
	}
	// Both buffers contain the same number of aligned rows.
	y := d.gitSplitY
	if d.gitLeftScroll.Y != y {
		y = d.gitLeftScroll.Y
	} else if d.gitRightScroll.Y != y {
		y = d.gitRightScroll.Y
	}
	d.gitSplitY, d.gitLeftScroll.Y, d.gitRightScroll.Y = y, y, y
	native.Row(c.Key("split")).FillWidth().Grow(1).MinHeight(0).Gap(1).Background(t.Border).Children(func() {
		for sideIndex, side := range []struct {
			name    string
			buffer  *native.TextBuffer
			scroll  *native.ScrollState
			hscroll *native.ScrollState
			width   float32
			color   native.Color
		}{{"原版本", d.gitLeft, &d.gitLeftScroll, &d.gitLeftHScroll, d.gitLeftWidth, t.Danger}, {"新版本", d.gitRight, &d.gitRightScroll, &d.gitRightHScroll, d.gitRightWidth, t.Success}} {
			native.Column(c.Key(side.name)).Grow(1).FillHeight().MinWidth(0).Background(t.Background).Children(func() {
				native.Text(c, side.name).FontSize(10).TextColor(t.TextMuted).Padding(5, 10)
				native.ScrollHorizontal(c.Key("horizontal")).TrackScroll(side.hscroll).FillWidth().Grow(1).MinHeight(0).Children(func() {
					area := native.TextAreaBufferBase(c, side.buffer).Label("差异 "+side.name).ReadOnly(true).Font(codeFont).FontSize(12).
						FixedLineHeight(20).NoWrap().Width(max((available-1)/2, side.width)).Shrink(0).FillHeight().Padding(6, 10).TrackScroll(side.scroll)
					_, caret := area.TextAnchorCaret()
					if caret != d.gitSplitCaret[sideIndex] {
						d.gitSplitCaret[sideIndex] = caret
						line := side.buffer.Snapshot().LineAt(caret)
						selected := -1
						for i := 0; i <= line; i++ {
							if strings.HasPrefix(side.buffer.Snapshot().Line(i), "@@ ") {
								selected++
							}
						}
						if selected >= 0 {
							d.gitHunk = selected
						}
					}
					area.Draw(func(p *native.Painter, r native.Rect) {
						snap := side.buffer.Snapshot()
						first := max(0, int(side.scroll.Y/20))
						last := min(snap.LineCount(), first+int(r.H/20)+2)
						for i := first; i < last; i++ {
							line := snap.Line(i)
							if d.gitReview != nil && d.gitHunk < len(d.gitReview.Hunks) && line == d.gitReview.Hunks[d.gitHunk].Header {
								p.Fill(native.Rect{X: r.X, Y: r.Y + 6 + float32(i)*20 - side.scroll.Y, W: r.W, H: 20}, t.Accent.Alpha(.12), 0)
							}
							if strings.Contains(line, " − ") || strings.Contains(line, " + ") {
								p.Fill(native.Rect{X: r.X, Y: r.Y + 6 + float32(i)*20 - side.scroll.Y, W: r.W, H: 20}, side.color.Alpha(.09), 0)
							}
						}
					})
				})
			})
		}
	})
}
