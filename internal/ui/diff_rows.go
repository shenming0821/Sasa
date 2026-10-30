package ui

import (
	"fmt"
	"strings"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
)

type diffCodeRow struct {
	text            string
	old, next, hunk int
	kind            byte
}

type diffCodePair struct {
	left, right diffCodeRow
}

func drawDiffLineNumber(p *native.Painter, x, y float32, number int, kind byte, theme *native.Theme) {
	if number <= 0 {
		return
	}
	spans := []native.Span{{Text: fmt.Sprint(number), Size: 11, Color: theme.TextMuted}}
	if kind == '+' || kind == '-' {
		color := theme.Success
		if kind == '-' {
			color = theme.Danger
		}
		spans = append(spans, native.Span{Text: string(kind), Size: 11, Color: color})
	}
	p.RichText(x, y, 0, spans...)
}

func diffCodeRows(review *gitview.Review) ([]diffCodeRow, []diffCodePair) {
	if review == nil || review.Binary || len(review.Hunks) == 0 {
		return nil, nil
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
	var pairs []diffCodePair
	for index := 0; index < len(rows); {
		row := rows[index]
		if row.kind != '-' && row.kind != '+' {
			pairs = append(pairs, diffCodePair{left: row, right: row})
			index++
			continue
		}
		var deleted, added []diffCodeRow
		for index < len(rows) && rows[index].hunk == row.hunk && (rows[index].kind == '-' || rows[index].kind == '+') {
			if rows[index].kind == '-' {
				deleted = append(deleted, rows[index])
			} else {
				added = append(added, rows[index])
			}
			index++
		}
		for offset := range max(len(deleted), len(added)) {
			pair := diffCodePair{left: diffCodeRow{hunk: row.hunk}, right: diffCodeRow{hunk: row.hunk}}
			if offset < len(deleted) {
				pair.left = deleted[offset]
			}
			if offset < len(added) {
				pair.right = added[offset]
			}
			pairs = append(pairs, pair)
		}
	}
	return rows, pairs
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

func diffSyntaxColor(color native.Color, theme *native.Theme) native.Color {
	if !theme.Dark || color.A == 0 {
		return color
	}
	return native.RGB(
		uint8((int(color.R)+2*int(theme.Text.R))/3),
		uint8((int(color.G)+2*int(theme.Text.G))/3),
		uint8((int(color.B)+2*int(theme.Text.B))/3),
	)
}
