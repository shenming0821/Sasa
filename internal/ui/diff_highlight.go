package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/shenming0821/Sasa/internal/editor"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

func diffHighlightSources(review *gitview.Review) (string, string) {
	rows := diffSourceRows(review)
	return diffHighlightSource(rows, true), diffHighlightSource(rows, false)
}

// diffHighlightSource uses the uninterrupted source from the start of a
// version. A sparse historical patch may be missing earlier lexer context.
func diffHighlightSource(rows []diffCodeRow, old bool) string {
	var text strings.Builder
	nextLine := 1
	for _, row := range rows {
		line := row.next
		if old {
			line = row.old
		}
		if line == 0 {
			continue
		}
		if line != nextLine {
			break
		}
		if text.Len()+len(row.text)+1 > editor.EditBytes {
			return ""
		}
		text.WriteString(row.text)
		text.WriteByte('\n')
		nextLine++
	}
	return text.String()
}

// diffHighlightRow is one visible row of a diff layout: its 1-based line in
// the full text of its side, and its length in runes. Rows with no line of
// the side — fillers, fold markers and section bands — carry line 0.
type diffHighlightRow struct {
	line, length int
}

// diffHighlightRows converts diff rows for the projection: line is the
// row's old or next number as the side asks, length its text in runes.
func diffHighlightRows(rows []diffCodeRow, left bool) []diffHighlightRow {
	out := make([]diffHighlightRow, len(rows))
	for index, row := range rows {
		line := row.next
		if left {
			line = row.old
		}
		out[index] = diffHighlightRow{line: line, length: utf8.RuneCountInString(row.text)}
	}
	return out
}

// diffInlineHighlightRows selects a source for each unified row. Both maps
// account for the full width of every row in the shared text buffer.
func diffInlineHighlightRows(rows []diffCodeRow) ([]diffHighlightRow, []diffHighlightRow) {
	left, right := make([]diffHighlightRow, len(rows)), make([]diffHighlightRow, len(rows))
	for index, row := range rows {
		length := utf8.RuneCountInString(row.text)
		left[index].length, right[index].length = length, length
		if row.kind == '-' {
			left[index].line = row.old
		} else {
			right[index].line = row.next
		}
	}
	return left, right
}

// projectDiffTokens maps the syntax tokens of a side's source text onto the
// visible rows of its diff buffer, so the highlighting comes from the file
// as it really reads rather than from the aligned, folded diff text. Tokens
// are rune ranges of the full text; rows arrive in buffer order, and the
// returned ranges are rune ranges of the buffer. Rows whose line does not
// match the full text's length — a rebuilt text the patch disagrees with —
// take no tokens rather than shifting the rest.
func projectDiffTokens(tokens []syntaxToken, full string, rows []diffHighlightRow) []syntaxToken {
	if len(tokens) == 0 || len(rows) == 0 {
		return nil
	}
	lines := strings.Split(full, "\n")
	lineRunes := make([]int, len(lines))
	for index, text := range lines {
		lineRunes[index] = utf8.RuneCountInString(text)
	}
	var out []syntaxToken
	line, lineStart := 0, 0
	cursor := 0
	bufferStart := 0
	for _, row := range rows {
		rowStart := bufferStart
		bufferStart += row.length + 1
		target := row.line - 1
		if target < line {
			continue
		}
		if target >= len(lineRunes) {
			break
		}
		for line < target {
			lineStart += lineRunes[line] + 1
			line++
		}
		if lineRunes[target] != row.length {
			continue
		}
		lineEnd := lineStart + row.length
		for cursor < len(tokens) && tokens[cursor].End <= lineStart {
			cursor++
		}
		for index := cursor; index < len(tokens) && tokens[index].Start < lineEnd; index++ {
			token := tokens[index]
			from, to := max(token.Start, lineStart), min(token.End, lineEnd)
			if from < to {
				out = append(out, syntaxToken{Start: rowStart + from - lineStart, End: rowStart + to - lineStart, Type: token.Type})
			}
		}
	}
	return out
}

// mergeDiffTokens merges two token lists ordered by start, as the unified
// diff's rows draw tokens of the old and the new text into one buffer.
func mergeDiffTokens(a, b []syntaxToken) []syntaxToken {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make([]syntaxToken, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i].Start <= b[j].Start {
			out = append(out, a[i])
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}
	return append(append(out, a[i:]...), b[j:]...)
}
