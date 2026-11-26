package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/editor"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

type diffCodeRow struct {
	text            string
	old, next, hunk int
	kind            byte
	fold            *diffFold
	section         bool
	words           *diffWordPair
}

type diffFold struct {
	start, count int
}

func (row diffCodeRow) foldMarker() bool {
	return row.fold != nil && row.old == 0 && row.next == 0
}

func (row diffCodeRow) hunkHeader() bool {
	return row.section
}

type diffCodePair struct {
	left, right diffCodeRow
}

// diffNumW is the width of one line-number column of a unified diff, as
// Mayoi's diffGutter: three digits of the number font plus 16.
const diffNumW = float32(3*7 + 16)

// diffTextX is where the text of a unified diff row starts: the rail, both
// number columns and the room of the +/- mark, as Mayoi's textX.
const diffTextX = float32(3 + 2*diffNumW + 22)

// diffSideTextX is where the text of one side of a split diff starts: the
// rail, its one number column and the room of the +/- mark, as Mayoi's
// split textX.
const diffSideTextX = float32(3 + diffNumW + 22)

// editorTextX is the left inset of a plain file's text, matching the
// padding the editor area is laid out with.
const editorTextX = float32(58)

// diffInlineTextX is where the text of a diff page starts when its rows
// show, or editorTextX while there is nothing row-shaped to align with.
func (d *filePage) diffInlineTextX() float32 {
	if d.kind == "diff" && len(d.gitRows) > 0 {
		return diffTextX
	}
	return editorTextX
}

// drawDiffLineNumber draws the two line-number columns of a unified diff
// row, as Mayoi does: the old line right-aligned in the first column, the
// new line right-aligned in the second, each skipped where its side has
// none, and the +/- mark right after the second column. Numbers are
// right-aligned by measuring, so the digit width need not be exact.
func drawDiffLineNumber(p *native.Painter, x, y float32, row diffCodeRow, theme *native.Theme) {
	numCol := theme.TextMuted
	mark := byte(0)
	markCol := theme.TextMuted
	switch row.kind {
	case '-':
		numCol = theme.Danger
		mark, markCol = '-', theme.Danger
	case '+':
		numCol = theme.Success
		mark, markCol = '+', theme.Success
	}
	number := func(n int, right float32) {
		if n <= 0 {
			return
		}
		label := fmt.Sprint(n)
		// Mayoi 用整数 digit 宽度右对齐：x 始终整数，文本按像素对齐，
		// 不会退化成亚像素抗锯齿。diffNumW 的 7 与这里一致。
		p.Text(right-float32(len(label))*7, y, label, 11, numCol)
	}
	number(row.old, x+diffNumW)
	number(row.next, x+2*diffNumW)
	if mark != 0 {
		p.Text(x+2*diffNumW+8, y, string(mark), 11, markCol)
	}
}

// drawDiffLineNumberSide draws the one line-number column of a split diff
// side, right-aligned as the columns of the unified layout are, with the
// +/- mark of a changed line after it.
func drawDiffLineNumberSide(p *native.Painter, x, y float32, number int, kind byte, theme *native.Theme) {
	numCol := theme.TextMuted
	mark := byte(0)
	markCol := theme.TextMuted
	switch kind {
	case '-':
		numCol = theme.Danger
		mark, markCol = '-', theme.Danger
	case '+':
		numCol = theme.Success
		mark, markCol = '+', theme.Success
	}
	if number > 0 {
		label := fmt.Sprint(number)
		p.Text(x+diffNumW-float32(len(label))*7, y, label, 11, numCol)
	}
	if mark != 0 {
		p.Text(x+diffNumW+8, y, string(mark), 11, markCol)
	}
}

func diffCodeRows(review *gitview.Review) ([]diffCodeRow, []diffCodePair) {
	rows := diffSourceRows(review)
	return rows, diffRowPairs(rows)
}

func (d *filePage) rebuildDiffRows() {
	d.gitRows = foldDiffContext(diffSourceRows(d.gitReview), d.gitOpenFolds)
	d.gitPairs = diffRowPairs(d.gitRows)
}

// gitToggleFoldAt opens the fold at row, or folds again the open range its
// line is in, as Mayoi's toggleGap does with Z, Enter or a click. After the
// rebuild the same place takes the caret: the first line opened, or the gap
// the line folded into.
func (a *App) gitToggleRange(d *filePage, fold *diffFold) {
	if fold == nil || d.gitReview == nil {
		return
	}
	if d.gitOpenFolds == nil {
		d.gitOpenFolds = make(map[int]bool)
	}
	if d.gitOpenFolds[fold.start] {
		delete(d.gitOpenFolds, fold.start)
	} else {
		d.gitOpenFolds[fold.start] = true
	}
	d.gitFocusFold = fold
	a.gitSetDiffText(d, d.gitReview.Patch, d.gitReview)
}

// gitPlaceFoldFocus moves the caret to the first line of an opened range, or
// back onto the marker of the range just folded, and scrolls there when the
// place left the viewport. It reports whether a request was consumed.
func (a *App) gitPlaceFoldFocus(d *filePage, split bool, height float32) bool {
	fold := d.gitFocusFold
	if fold == nil || d.buffer == nil || height <= 0 {
		return false
	}
	target := -1
	open := d.gitOpenFolds[fold.start]
	if split {
		for index, pair := range d.gitPairs {
			if pair.left.fold == nil || *pair.left.fold != *fold {
				continue
			}
			if open && pair.left.foldMarker() {
				continue
			}
			target = index
			break
		}
	} else {
		for index, row := range d.gitRows {
			if row.fold == nil || *row.fold != *fold {
				continue
			}
			if open && row.foldMarker() {
				continue
			}
			target = index
			break
		}
	}
	if target < 0 {
		return false
	}
	buffer := d.buffer
	if split && d.gitLeft != nil {
		buffer = d.gitLeft
		if d.gitFocusSide == 1 {
			buffer = d.gitRight
		}
	}
	snap := buffer.Snapshot()
	if target < snap.LineCount() {
		start, _ := snap.LineRange(target)
		if split {
			d.gitSplitFocus = &[2]int{start, start}
		} else {
			d.pendingSelection = &[2]int{start, start}
			d.anchor, d.caret = start, start
			d.focus, d.focusOwner = true, nil
		}
	}
	padding := float32(8)
	if split {
		padding = 6
	}
	top := padding + float32(target)*20
	count := len(d.gitRows)
	if split {
		count = len(d.gitPairs)
	}
	maxScroll := max(0, float32(count)*20+padding*2-height)
	scrollY := d.scroll.Y
	if split {
		scrollY = d.gitSplitY
	}
	if top < scrollY+padding || top+20 > scrollY+height-padding {
		scrollY = min(maxScroll, max(0, top+10-height/2))
		if split {
			d.gitSplitY, d.gitLeftScroll.Y, d.gitRightScroll.Y = scrollY, scrollY, scrollY
		} else {
			d.scroll.Y = scrollY
		}
		d.pinFrame = 0
	}
	d.gitFocusFold = nil
	return true
}

// gitOpenFoldFileAt opens the diff's file at the line of row, as Mayoi's
// Enter on a line does. Fold markers themselves open their range instead.
// Split views index into gitPairs, inline views into gitRows.
func (a *App) gitOpenFoldFileAt(d *filePage, row int, split bool) {
	path, openable := d.diffOpenTarget()
	if !openable || row < 0 {
		return
	}
	line := 0
	if split {
		if row >= len(d.gitPairs) {
			return
		}
		side := d.gitPairs[row].right
		line = side.next
		if line <= 0 {
			line = d.gitPairs[row].left.old
		}
		if side.foldMarker() {
			return
		}
	} else if row < len(d.gitRows) {
		line = d.gitRows[row].next
		if line <= 0 {
			line = d.gitRows[row].old
		}
		if d.gitRows[row].foldMarker() {
			return
		}
	}
	if line <= 0 {
		return
	}
	a.openFile(d.root, path)
	if target := a.files["file-"+editor.RecoveryID(d.root, filepath.Clean(path))]; target != nil {
		target.pendingLocation = &fileLocation{line: line, column: 1}
	}
}

func diffSourceRows(review *gitview.Review) []diffCodeRow {
	if review == nil || review.Binary || len(review.Hunks) == 0 {
		return nil
	}
	base := strings.Split(strings.TrimSuffix(review.BeforeText, "\n"), "\n")
	if review.BeforeText == "" {
		base = nil
	}
	complete := review.BeforeKnown
	for _, hunk := range review.Hunks {
		old := max(0, hunk.OldStart-1)
		for _, line := range diffHunkLines(hunk) {
			if len(line) > 0 && (line[0] == ' ' || line[0] == '-') {
				if old >= len(base) || line[1:] != base[old] {
					complete = false
				}
				old++
			}
		}
	}
	var rows []diffCodeRow
	old, next := 1, 1
	for index, hunk := range review.Hunks {
		start := hunk.OldStart
		if hunk.OldCount == 0 {
			start++
		}
		if complete {
			for old < start && old <= len(base) {
				rows = append(rows, diffCodeRow{text: base[old-1], old: old, next: next, hunk: -1})
				old, next = old+1, next+1
			}
		} else if old < start {
			rows = append(rows, diffCodeRow{text: "⋯", hunk: -1, kind: '.'})
		}
		old, next = max(1, hunk.OldStart), max(1, hunk.NewStart)
		if hunk.OldCount == 0 {
			old = hunk.OldStart + 1
		}
		if hunk.NewCount == 0 {
			next = hunk.NewStart + 1
		}
		for _, line := range diffHunkLines(hunk) {
			if len(line) == 0 {
				continue
			}
			row := diffCodeRow{text: line[1:], hunk: index, kind: line[0]}
			switch line[0] {
			case ' ':
				row.old, row.next = old, next
				old, next = old+1, next+1
			case '-':
				row.old = old
				old++
			case '+':
				row.next = next
				next++
			default:
				continue
			}
			rows = append(rows, row)
		}
	}
	if complete {
		for old <= len(base) {
			rows = append(rows, diffCodeRow{text: base[old-1], old: old, next: next, hunk: -1})
			old, next = old+1, next+1
		}
	}
	return rows
}

// foldDiffContext collapses the runs of unchanged lines more than context
// lines away from a change, as Mayoi's foldRows does: a gap row replaces
// each folded run unless it is open, and a hunk header row precedes every
// visible run that holds a change. rows carry 1-based line numbers of the
// two sides, 0 where a side has none.
func foldDiffContext(rows []diffCodeRow, opened map[int]bool) []diffCodeRow {
	const contextLines = 3
	if len(rows) == 0 {
		return nil
	}
	type span struct{ from, to int }
	var folds []span
	for start := 0; start < len(rows); {
		// A row is unchanged when it has a line of each side, wherever
		// it stands: inside the patch's hunks, in the lines around them
		// the source adds, or in either. Mayoi's rows say the same with
		// hunk < 0, as its unchanged rows never carry a hunk.
		unchanged := func(index int) bool {
			row := rows[index]
			return row.old > 0 && row.next > 0
		}
		if !unchanged(start) {
			start++
			continue
		}
		end := start + 1
		for end < len(rows) && unchanged(end) {
			end++
		}
		from, to := start, end
		if start > 0 {
			from += contextLines
		}
		if end < len(rows) {
			to -= contextLines
		}
		if to-from >= 2 {
			folds = append(folds, span{from, to})
		}
		start = end
	}
	var visible []diffCodeRow
	section := func(from, to int) {
		if from >= to {
			return
		}
		changes := false
		for _, row := range rows[from:to] {
			changes = changes || row.hunk >= 0
		}
		if changes {
			// The section band only separates runs of changes: the
			// user asked its @@ -a,b +c,d @@ line numbers not to show.
			visible = append(visible, diffCodeRow{hunk: -1, section: true})
		}
		visible = append(visible, rows[from:to]...)
	}
	at := 0
	for _, gap := range folds {
		section(at, gap.from)
		fold := &diffFold{start: gap.from, count: gap.to - gap.from}
		if opened[gap.from] {
			for index := gap.from; index < gap.to; index++ {
				row := rows[index]
				row.fold = fold
				visible = append(visible, row)
			}
		} else {
			visible = append(visible, diffCodeRow{hunk: -1, fold: fold})
		}
		at = gap.to
	}
	section(at, len(rows))
	return visible
}

func diffRowPairs(rows []diffCodeRow) []diffCodePair {
	var pairs []diffCodePair
	for index := 0; index < len(rows); {
		row := rows[index]
		if row.kind != '-' && row.kind != '+' {
			pairs = append(pairs, diffCodePair{left: row, right: row})
			index++
			continue
		}
		var deleted, added []int
		for index < len(rows) && rows[index].hunk == row.hunk && (rows[index].kind == '-' || rows[index].kind == '+') {
			if rows[index].kind == '-' {
				deleted = append(deleted, index)
			} else {
				added = append(added, index)
			}
			index++
		}
		for offset := range max(len(deleted), len(added)) {
			pair := diffCodePair{left: diffCodeRow{hunk: row.hunk}, right: diffCodeRow{hunk: row.hunk}}
			if offset < min(len(deleted), len(added)) {
				left, right := &rows[deleted[offset]], &rows[added[offset]]
				words := &diffWordPair{before: left.text, after: right.text}
				left.words, right.words = words, words
			}
			if offset < len(deleted) {
				pair.left = rows[deleted[offset]]
			}
			if offset < len(added) {
				pair.right = rows[added[offset]]
			}
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

func diffHunkLines(hunk gitview.Hunk) []string {
	lines := strings.Split(strings.TrimSuffix(hunk.Patch, "\n"), "\n")
	var result []string
	old, next := 0, 0
	for _, line := range lines[1:] {
		if old >= hunk.OldCount && next >= hunk.NewCount {
			break
		}
		if line == "" {
			continue
		}
		switch line[0] {
		case ' ':
			old, next = old+1, next+1
		case '-':
			old++
		case '+':
			next++
		default:
			continue
		}
		result = append(result, line)
	}
	return result
}

func diffRowsText(rows []diffCodeRow) string {
	var text strings.Builder
	for index, row := range rows {
		if index > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(row.text)
	}
	return text.String()
}

func diffPairText(pairs []diffCodePair) (string, string) {
	left, right := make([]diffCodeRow, len(pairs)), make([]diffCodeRow, len(pairs))
	for index, pair := range pairs {
		left[index], right[index] = pair.left, pair.right
	}
	return diffRowsText(left), diffRowsText(right)
}

func diffHunkRow(rows []diffCodeRow, hunk int) int {
	first := -1
	for index, row := range rows {
		if row.hunk == hunk {
			if first < 0 {
				first = index
			}
			if row.kind == '+' || row.kind == '-' {
				return index
			}
		}
	}
	return max(0, first)
}
