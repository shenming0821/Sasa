package ui

import (
	"regexp"
	"unicode/utf16"

	native "github.com/egoist/mygo/ui"
)

var diffWordToken = regexp.MustCompile(`[A-Za-z0-9_\x{0080}-\x{10ffff}]+|[ \t]+|.`)

type diffWordPair struct {
	before, after string
	spans         [2][][2]int
	ready         bool
}

func (pair *diffWordPair) compare() {
	pair.ready = true
	const limit = 600
	left := diffWordToken.FindAllStringIndex(pair.before, limit+1)
	right := diffWordToken.FindAllStringIndex(pair.after, limit+1)
	if len(left) > limit || len(right) > limit {
		pair.spans[0] = [][2]int{{0, len(pair.before)}}
		pair.spans[1] = [][2]int{{0, len(pair.after)}}
		return
	}
	equal := func(old, next int) bool {
		return pair.before[left[old][0]:left[old][1]] == pair.after[right[next][0]:right[next][1]]
	}
	start, oldEnd, newEnd := 0, len(left), len(right)
	for start < min(oldEnd, newEnd) && equal(start, start) {
		start++
	}
	for min(oldEnd, newEnd) > start && equal(oldEnd-1, newEnd-1) {
		oldEnd--
		newEnd--
	}
	stride := newEnd - start + 1
	common := make([]uint16, (oldEnd-start+1)*stride)
	for old := oldEnd - 1; old >= start; old-- {
		for next := newEnd - 1; next >= start; next-- {
			cell := (old-start)*stride + next - start
			if equal(old, next) {
				common[cell] = 1 + common[cell+stride+1]
			} else {
				common[cell] = max(common[cell+1], common[cell+stride])
			}
		}
	}
	mark := func(side int, token []int) {
		spans := pair.spans[side]
		if count := len(spans); count > 0 && spans[count-1][1] == token[0] {
			spans[count-1][1] = token[1]
		} else {
			pair.spans[side] = append(spans, [2]int{token[0], token[1]})
		}
	}
	for old, next := start, start; old < oldEnd || next < newEnd; {
		cell := (old-start)*stride + next - start
		switch {
		case old < oldEnd && next < newEnd && equal(old, next):
			old++
			next++
		case next == newEnd || old < oldEnd && common[cell+stride] >= common[cell+1]:
			mark(0, left[old])
			old++
		default:
			mark(1, right[next])
			next++
		}
	}
}

func (row diffCodeRow) paintWords(p *native.Painter, rect native.Rect, font native.Font, palette editorPalette) {
	if row.words == nil || row.text == "" {
		return
	}
	if !row.words.ready {
		row.words.compare()
	}
	side, tint := 0, palette.deleted
	if row.kind == '+' {
		side, tint = 1, palette.added
	}
	spans := row.words.spans[side]
	if len(spans) == 0 {
		return
	}
	layout := native.ShapeText(row.text, font, 0)
	byteOffset, units := 0, 0
	caret := func(end int) float32 {
		for _, char := range row.text[byteOffset:end] {
			units += utf16.RuneLen(char)
		}
		byteOffset = end
		return layout.Caret(units).X
	}
	for _, span := range spans {
		left, right := caret(span[0]), caret(span[1])
		p.Fill(native.Rect{X: rect.X + left, Y: rect.Y, W: max(2, right-left), H: rect.H}, tint.Alpha(.30), 0)
	}
}
