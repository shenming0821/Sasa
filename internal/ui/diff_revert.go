package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/editor"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

type diffLineSelection struct {
	cursor, first, last int
	focused, selected   bool
}

func diffSelection(snap native.TextSnapshot, anchor, caret int, focused bool) diffLineSelection {
	first, last := min(anchor, caret), max(anchor, caret)
	return diffLineSelection{cursor: snap.LineAt(caret), first: snap.LineAt(first), last: snap.LineAt(max(first, last-1)), selected: first != last, focused: focused}
}

func (selection diffLineSelection) paint(p *native.Painter, r native.Rect, row int, palette editorPalette, rail bool) {
	strip := native.Rect{X: r.X, Y: r.Y, W: 3, H: r.H}
	if selection.selected && row >= selection.first && row <= selection.last {
		p.Fill(r, palette.accent.Alpha(.12), 0)
		if rail {
			p.Fill(strip, palette.accent.Alpha(.6), 0)
		}
	}
	if row == selection.cursor {
		alpha := float32(.05)
		if selection.focused {
			alpha = .09
		}
		p.Fill(r, palette.text.Alpha(alpha), 0)
		if rail {
			p.Fill(strip, palette.accent, 0)
		}
	}
}

func (d *filePage) canRevertDiff() bool {
	review := d.gitReview
	return review != nil && d.gitCommit == "" && d.turnReview == nil &&
		!review.Staged && review.Blockable && review.BeforeKnown && review.Token != "" &&
		len(review.Entry.XY) == 2 && review.Entry.XY[1] == 'M'
}

type diffBlockEdit struct {
	start, end int
	text       string
}

func diffBlockReplacement(review *gitview.Review, row diffCodePair, file editor.File, appendLines bool) (diffBlockEdit, error) {
	stale := errors.New("文件与当前差异不一致，请刷新后重试")
	if row.left.hunk < 0 || row.left.hunk >= len(review.Hunks) {
		return diffBlockEdit{}, stale
	}
	hunk := review.Hunks[row.left.hunk]
	old, next := max(0, hunk.OldStart-1), max(0, hunk.NewStart-1)
	if hunk.OldCount == 0 {
		old = hunk.OldStart
	}
	if hunk.NewCount == 0 {
		next = hunk.NewStart
	}
	lines := diffHunkLines(hunk)
	for index := 0; index < len(lines); {
		if lines[index][0] == ' ' {
			old++
			next++
			index++
			continue
		}
		oldStart, newStart, first := old, next, index
		for index < len(lines) && lines[index][0] != ' ' {
			if lines[index][0] == '-' {
				old++
			} else {
				next++
			}
			index++
		}
		if (old == oldStart || row.left.old != oldStart+1) && (next == newStart || row.right.next != newStart+1) {
			continue
		}
		before := strings.SplitAfter(strings.ReplaceAll(strings.TrimPrefix(review.BeforeText, "\ufeff"), "\r\n", "\n"), "\n")
		work := strings.SplitAfter(file.Text, "\n")
		if old > len(before) || next > len(work) {
			return diffBlockEdit{}, stale
		}
		oldLine, newLine := oldStart, newStart
		for _, line := range lines[first:index] {
			var actual string
			if line[0] == '-' {
				actual = before[oldLine]
				oldLine++
			} else {
				actual = work[newLine]
				newLine++
			}
			expected := strings.TrimSuffix(line[1:], "\r")
			if line[0] == '-' && oldLine == 1 || line[0] == '+' && newLine == 1 {
				expected = strings.TrimPrefix(expected, "\ufeff")
			}
			if strings.TrimSuffix(actual, "\n") != expected {
				return diffBlockEdit{}, stale
			}
		}
		start := utf8.RuneCountInString(strings.Join(work[:newStart], ""))
		end := start + utf8.RuneCountInString(strings.Join(work[newStart:next], ""))
		text := strings.Join(before[oldStart:old], "")
		if appendLines {
			start = end
			if text != "" && next > 0 && !strings.HasSuffix(work[next-1], "\n") {
				text = "\n" + text
			}
		}
		return diffBlockEdit{start: start, end: end, text: text}, nil
	}
	return diffBlockEdit{}, stale
}

func (a *App) revertDiffBlock(d *filePage, row diffCodePair, appendLines bool) {
	if !d.canRevertDiff() {
		return
	}
	review := d.gitReview
	verb := "撤销此处更改"
	root, path := d.root, d.path
	if appendLines {
		verb = "追加原版本行"
	}
	openAndEdit := func() {
		var file editor.File
		var edit diffBlockEdit
		a.gitAction(a.workspace(root), verb, func(ctx context.Context) error {
			var err error
			file, err = editor.Read(path)
			if err != nil {
				return err
			}
			if file.Reason != "" {
				return errors.New(file.Reason)
			}
			current, err := gitview.ReadReview(ctx, root, review.Entry, false)
			if err != nil {
				return err
			}
			if current.Token != review.Token || current.Patch != review.Patch {
				return errors.New("文件或暂存区已更改，请刷新差异")
			}
			edit, err = diffBlockReplacement(review, row, file, appendLines)
			return err
		}, func() {
			if a.files[d.id] != d || d.path != path || edit.start == edit.end && edit.text == "" {
				return
			}
			a.openFile(root, path)
			doc := a.currentFile()
			if doc == nil || doc.kind != "file" || doc.path != d.path {
				return
			}
			apply := func() {
				if doc.buffer == nil || doc.file.Stamp != file.Stamp || doc.buffer.String() != file.Text || doc.saving || doc.conflict || doc.missing {
					doc.err = "编辑器内容已变化，请重新核对差异"
					return
				}
				doc.buffer.Replace(edit.start, edit.end, edit.text)
				doc.observe()
				doc.pendingSelection = &[2]int{edit.start, edit.start}
				doc.pendingLocation = &fileLocation{line: doc.buffer.Snapshot().LineAt(edit.start) + 1, column: 1}
			}
			if doc.loading {
				doc.afterDiffRead = apply
			} else {
				apply()
			}
		})
	}
	for _, doc := range a.files {
		if doc.kind == "file" && doc.root == d.root && doc.path == d.path && doc.loaded {
			openAndEdit()
			return
		}
	}
	a.workspace(d.root).gitDecision = &gitDecision{title: fmt.Sprintf("%s · %s", verb, filepath.Base(d.path)), detail: "在编辑器中应用这处改动，可撤销，保存后写入磁盘。", labels: []string{"取消", verb}, actions: []func(){nil, openAndEdit}}
}

func (a *App) diffArrow(c *native.Context, d *filePage, pair diffCodePair, x, y, size float32, palette editorPalette) {
	btn := native.ButtonBase(c.Key(fmt.Sprintf("revert:%d:%d", pair.left.old, pair.right.next))).Absolute().Left(x).Top(y).Size(size, size).Radius(size/2).
		AlignItems(native.Center).Justify(native.Center).Label("撤销此处更改").Tooltip("撤销此处更改；按住 Control 点击可追加原版本行").
		Background(palette.background).Border(1, palette.border)
	glyph, ink := "→", palette.muted
	pose := native.Pose{}
	if btn.Hovered() {
		btn.Background(palette.accent).Border(1, palette.accent)
		ink = palette.background
		pose.Scale = 1.12
		if c.Modifiers()&native.Ctrl != 0 {
			glyph = "↓"
			btn.Label("追加原版本行")
		}
	}
	if btn.Pressed() {
		pose.Scale = .88
	}
	btn.AnimatePose(pose, native.Snappy)
	btn.Children(func() { native.Text(c, glyph).FontSize(size - 3).TextColor(ink) })
	if btn.Clicked() {
		a.revertDiffBlock(d, pair, btn.ClickModifiers()&native.Ctrl != 0)
	}
}
