package ui

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	native "github.com/egoist/mygo/ui"
)

const (
	editorHighlightWholeLines = 6000
	editorHighlightAbove      = 400
	editorHighlightBelow      = 200
	editorHighlightAhead      = 60
	editorHighlightViewRows   = 40
	editorHighlightDrawRows   = 100
)

type editorHighlightWindow struct {
	active      bool
	first, last int
	margin      int
	vueSections []vueHighlightSection
	startTokens []syntaxToken
	endTokens   []syntaxToken
}

func (window *editorHighlightWindow) cacheDocumentEnds(ctx context.Context, lexer chroma.Lexer, snap native.TextSnapshot) {
	edge := editorHighlightWindow{vueSections: window.vueSections, last: min(snap.LineCount()-1, editorHighlightDrawRows)}
	window.startTokens, _ = highlightLineWindow(ctx, lexer, snap, edge)
	edge.first = max(edge.last+1, snap.LineCount()-1-editorHighlightDrawRows)
	edge.last = snap.LineCount() - 1
	if edge.first <= edge.last {
		window.endTokens, _ = highlightLineWindow(ctx, lexer, snap, edge)
	}
}

func (window editorHighlightWindow) covers(first, last, lineCount int) bool {
	return window.first <= max(0, first-window.margin) && window.last >= min(lineCount-1, last+window.margin)
}

func highlightViewport(ctx context.Context, lexer chroma.Lexer, snap native.TextSnapshot, sections []vueHighlightSection, first, last int) ([]syntaxToken, editorHighlightWindow) {
	window := editorHighlightWindow{
		active:      true,
		first:       max(0, first-editorHighlightAbove),
		last:        min(snap.LineCount()-1, last+editorHighlightBelow),
		margin:      editorHighlightAhead,
		vueSections: sections,
	}
	ranges, overflow := highlightLineWindow(ctx, lexer, snap, window)
	if overflow {
		window.first, window.last, window.margin = first, last, 0
		ranges, _ = highlightLineWindow(ctx, lexer, snap, window)
	}
	return ranges, window
}

func highlightLineWindow(ctx context.Context, lexer chroma.Lexer, snap native.TextSnapshot, window editorHighlightWindow) ([]syntaxToken, bool) {
	start, _ := snap.LineRange(window.first)
	_, end := snap.LineRange(window.last)
	var ranges []syntaxToken
	collect := func(current chroma.Lexer, from, to int) bool {
		if from >= to {
			return false
		}
		tokens, overflow := collectHighlightText(ctx, current, snap.Slice(from, to), from, editorHighlightTokenLimit-len(ranges))
		ranges = append(ranges, tokens...)
		return overflow
	}
	for _, section := range window.vueSections {
		if section.end <= start {
			continue
		}
		if section.start >= end {
			break
		}
		if collect(lexer, start, section.start) || collect(section.lexer, max(start, section.start), min(end, section.end)) {
			return ranges, true
		}
		start = section.end
	}
	overflow := collect(lexer, start, end)
	return ranges, overflow
}

type vueHighlightSection struct {
	start, end int
	lexer      chroma.Lexer
}

var (
	vueHighlightTag         = regexp.MustCompile(`(?is)<!--.*?(?:-->|$)|<(/?)([a-z][a-z0-9:_-]*)(\s+(?:"[^"]*"|'[^']*'|[^"'<>])*)?/?>`)
	vueHighlightAttribute   = regexp.MustCompile(`(?i)([^\s=/'"<>]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|[^\s>]+))?`)
	vueHighlightScriptClose = regexp.MustCompile(`(?i)</script\s*>`)
	vueHighlightStyleClose  = regexp.MustCompile(`(?i)</style\s*>`)
)

func indexVueHighlightSections(ctx context.Context, text string) []vueHighlightSection {
	var sections []vueHighlightSection
	offset := 0
	for len(text) > 0 && ctx.Err() == nil {
		match := vueHighlightTag.FindStringSubmatchIndex(text)
		if match == nil {
			break
		}
		tagEnd := match[1]
		openTag := text[match[0]:tagEnd]
		offset += utf8.RuneCountInString(text[:tagEnd])
		var tag, attributes string
		if match[4] >= 0 && match[2] == match[3] && !strings.HasSuffix(openTag, "/>") {
			tag = strings.ToLower(text[match[4]:match[5]])
			if match[6] >= 0 {
				attributes = text[match[6]:match[7]]
			}
		}
		text = text[tagEnd:]
		var closeTag *regexp.Regexp
		var fallback string
		switch tag {
		case "script":
			closeTag, fallback = vueHighlightScriptClose, "javascript"
		case "style":
			closeTag, fallback = vueHighlightStyleClose, "css"
		default:
			continue
		}
		bodyEnd, closeEnd := len(text), len(text)
		if closing := closeTag.FindStringIndex(text); closing != nil {
			bodyEnd, closeEnd = closing[0], closing[1]
		}
		if bodyEnd > 0 {
			language := vueSectionLanguage(tag, attributes, fallback)
			sections = append(sections, vueHighlightSection{offset, offset + utf8.RuneCountInString(text[:bodyEnd]), lexers.Get(language)})
		}
		offset += utf8.RuneCountInString(text[:closeEnd])
		text = text[closeEnd:]
	}
	return sections
}

func vueSectionLanguage(tag, attributes, fallback string) string {
	for _, attribute := range vueHighlightAttribute.FindAllStringSubmatch(attributes, -1) {
		if !strings.EqualFold(attribute[1], "lang") {
			continue
		}
		language := strings.ToLower(attribute[2] + attribute[3])
		for _, section := range vueEmbeddedLanguages {
			if section.tag != tag {
				continue
			}
			for alias := range strings.SplitSeq(section.names, "|") {
				if alias == language {
					return section.lexer
				}
			}
		}
		break
	}
	return fallback
}
