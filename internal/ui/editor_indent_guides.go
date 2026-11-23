package ui

import (
	"math"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

const indentGuideLookaround = 256

type editorIndentGuides struct {
	buffer     *native.TextBuffer
	version    uint64
	path       string
	spaceCount int
}

func (guides *editorIndentGuides) update(buffer *native.TextBuffer, snap native.TextSnapshot, path string) {
	if guides.buffer == buffer && guides.version == snap.Version() && guides.path == path {
		return
	}
	guides.buffer, guides.version, guides.path = buffer, snap.Version(), path
	guides.spaceCount = detectEditorIndent(snap, path)
}

func detectEditorIndent(snap native.TextSnapshot, path string) int {
	var increases [9]int
	tabs, spaces, previous := 0, 0, 0
	for line := range min(snap.LineCount(), 256) {
		start, end := snap.LineRange(line)
		text := snap.Slice(start, min(end, start+96))
		content := strings.TrimLeft(text, " \t")
		if content == "" {
			continue
		}
		prefix := text[:len(text)-len(content)]
		if strings.HasPrefix(prefix, "\t") {
			tabs++
			previous = 0
			continue
		}
		if strings.HasPrefix(prefix, " ") {
			spaces++
		}
		if !strings.Contains(prefix, "\t") {
			if increase := len(prefix) - previous; increase >= 2 && increase < len(increases) {
				increases[increase]++
			}
			previous = len(prefix)
		}
	}
	if tabs > spaces {
		return 0
	}
	best := 0
	for width := 2; width < len(increases); width++ {
		if increases[width] > increases[best] {
			best = width
		}
	}
	if best > 0 {
		return best
	}
	if language := editor.LanguageFor(path); language != nil {
		if language.HardTabs {
			return 0
		}
		if language.TabSize > 0 {
			return language.TabSize
		}
	}
	return 4
}

func indentGuideDepths(first, last, lineCount int, depth func(int) int) []int {
	depths := make([]int, last-first)
	for line := first; line < last; line++ {
		depths[line-first] = depth(line)
	}
	for start := 0; start < len(depths); {
		if depths[start] >= 0 {
			start++
			continue
		}
		end := start + 1
		for end < len(depths) && depths[end] < 0 {
			end++
		}
		above, below := 0, 0
		if start > 0 {
			above = depths[start-1]
		} else {
			for line := first - 1; line >= max(0, first-indentGuideLookaround); line-- {
				if above = depth(line); above >= 0 {
					break
				}
			}
		}
		if end < len(depths) {
			below = depths[end]
		} else {
			for line := last; line < min(lineCount, last+indentGuideLookaround); line++ {
				if below = depth(line); below >= 0 {
					break
				}
			}
		}
		for index := start; index < end; index++ {
			depths[index] = max(0, above, below)
		}
		start = end
	}
	return depths
}

func activeIndentGuide(depths []int, caretRow int) (level, start, end int) {
	if caretRow < 0 || caretRow >= len(depths) {
		return 0, 0, 0
	}
	level = depths[caretRow]
	switch {
	case caretRow+1 < len(depths) && depths[caretRow+1] > level:
		caretRow++
		level = depths[caretRow]
	case caretRow > 0 && depths[caretRow-1] > level:
		caretRow--
		level++
	}
	start, end = caretRow, caretRow+1
	if level > 0 {
		for start > 0 && depths[start-1] >= level {
			start--
		}
		for end < len(depths) && depths[end] >= level {
			end++
		}
	}
	return level, start, end
}

func (a *App) paintIndentGuides(p *native.Painter, r native.Rect, d *filePage, snap native.TextSnapshot, palette editorPalette) {
	first := max(0, d.lines.At(d.scroll.Y-8)-1)
	last := min(snap.LineCount(), d.lines.At(d.scroll.Y+r.H-8)+2)
	if first >= last {
		return
	}
	d.indentGuides.update(d.buffer, snap, d.path)
	font := native.Font{Family: a.codeFontFamily(), Size: 13}
	spaceWidth := native.ShapeText(" x", font, 0).Caret(1).X
	guideWidth := spaceWidth * float32(d.indentGuides.spaceCount)
	if d.indentGuides.spaceCount == 0 {
		guideWidth = native.ShapeText("\tx", font, 0).Caret(1).X
	}
	if guideWidth <= 0 {
		return
	}
	depths := indentGuideDepths(first, last, snap.LineCount(), func(line int) int {
		text := snap.Line(line)
		prefixLength := len(text) - len(strings.TrimLeft(text, " \t"))
		if prefixLength == len(text) || text[prefixLength:] == "\r" {
			return -1
		}
		prefix := text[:prefixLength]
		width := float32(prefixLength) * spaceWidth
		if strings.Contains(prefix, "\t") {
			width = native.ShapeText(prefix+"x", font, 0).Caret(prefixLength).X
		}
		return int(math.Ceil(float64((width - 0.01) / guideWidth)))
	})
	active, activeStart, activeEnd := activeIndentGuide(depths, snap.LineAt(d.caret)-first)
	guide, activeGuide := palette.text.Alpha(0.08), palette.text.Alpha(0.24)
	for row, depth := range depths {
		line := first + row
		y := r.Y + 8 + d.lines.Top(line) - d.scroll.Y
		top := max(r.Y, y)
		bottom := min(r.Y+r.H, y+d.lines.Top(line+1)-d.lines.Top(line))
		if bottom <= top {
			continue
		}
		for level := range depth {
			x := r.X + 58 + float32(level)*guideWidth
			if x >= r.X+r.W {
				break
			}
			color := guide
			if level+1 == active && row >= activeStart && row < activeEnd {
				color = activeGuide
			}
			p.Fill(native.Rect{X: x, Y: top, W: 1, H: bottom - top}, color, 0)
		}
	}
}
