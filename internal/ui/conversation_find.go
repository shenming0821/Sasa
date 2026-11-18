package ui

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
	"github.com/shenming0821/Sasa/internal/app"
)

// conversationFind is the session's Cmd+F find bar over message text: the
// query, its matches across conversation rows and the current match.
type conversationFind struct {
	open    bool
	focus   bool
	query   string
	scanned string // query the matches were computed for
	matches []sessionMatch
	index   int
	scroll  bool // scroll to matches[index] during this frame
}

// sessionMatch is one query hit in a row's rendered text. start/end are
// rune offsets of the hit: its source range for text hits, an
// approximate source position for code-block hits.
type sessionMatch struct {
	row        int
	start, end int
}

// markEdit replaces src[lo:hi] (rune offsets) with repl when marking up
// the source for the markdown view. cur reports the edit covers the
// current match; ovr is the replacement marking only the current match,
// for the overlay pass that brightens it.
type markEdit struct {
	lo, hi int
	repl   string
	cur    bool
	ovr    string
}

// findMarks is the marking plan for a query in markdown source: covers
// are the hit rune ranges counted per occurrence; edits rewrite the
// source so they render marked. curBlock is the block holding the
// current match and curSrc its source slice marked with that hit alone,
// which a transparent overlay renders over it for a brighter highlight.
type findMarks struct {
	covers   [][2]int
	edits    []markEdit
	curBlock int
	curSrc   string
}

// codeHit is one hit inside a code inline: its byte range and whether it
// is the current match.
type codeHit struct {
	s, e int
	cur  bool
}

// sessionFindRanges returns each case-insensitive occurrence of query in
// text as rune offsets. Per-rune lowering keeps offsets aligned with the
// original text.
func sessionFindRanges(text, query string) [][2]int {
	q := []rune(strings.ToLower(query))
	if len(q) == 0 {
		return nil
	}
	t := []rune(strings.ToLower(text))
	var out [][2]int
	for i := 0; i+len(q) <= len(t); {
		if slices.Equal(t[i:i+len(q)], q) {
			out = append(out, [2]int{i, i + len(q)})
			i += len(q)
		} else {
			i++
		}
	}
	return out
}

// sessionFindMarks locates markable hits in markdown source. A hit
// inside a text inline wraps just the hit; one spanning inlines wraps
// the smallest covering run of top-level inlines; hits in a code span
// split it so only the hit parts carry the mark. Hits in code blocks
// are highlighted by the code block hook, and hits outside every
// block's inlines (syntax characters) stay unmarkable. cur is the
// current match's hit range: where a transparent overlay can paint it
// brighter (paragraphs and headings) it stays a plain mark; elsewhere
// it is underlined instead.
func sessionFindMarks(src, query string, cur [2]int) *findMarks {
	fm := &findMarks{curBlock: -1}
	raw := sessionFindRanges(src, query)
	if len(raw) == 0 {
		return fm
	}
	doc := parser.Parse(src)
	runes := []rune(src)
	byteOf := make([]int, len(runes)+1)
	for i, r := range runes {
		byteOf[i+1] = byteOf[i] + utf8.RuneLen(r)
	}
	runeOf := func(b int) int { return utf8.RuneCountInString(src[:b]) }
	wrap := func(lo, hi int, mark bool, kind parser.Kind) markEdit {
		inner := string(runes[lo:hi])
		if !mark {
			return markEdit{lo: lo, hi: hi, repl: "<mark>" + inner + "</mark>"}
		}
		repl := "<mark>" + inner + "</mark>"
		if kind == parser.TableCell {
			repl = "<mark><u>" + inner + "</u></mark>" // no overlay there
		}
		return markEdit{lo: lo, hi: hi, repl: repl, cur: true, ovr: "<mark>" + inner + "</mark>"}
	}
	done := make([]bool, len(raw))
	for i := 0; i < doc.Len(); i++ {
		b := doc.Block(i)
		if b.Kind != parser.Paragraph && b.Kind != parser.Heading && b.Kind != parser.TableCell {
			continue
		}
		inlines := doc.Inlines(i)
		var tops, texts []parser.Inline
		autoEnd := -1 // subtree end of a wrapping autolink
		for j := 0; j < len(inlines); j = inlines[j].Next {
			tops = append(tops, inlines[j])
		}
		for j, inl := range inlines {
			if j < autoEnd {
				continue
			}
			if inl.Kind == parser.Link && inl.Auto {
				autoEnd = inl.Next // a tag inside its <> renders literally
				continue
			}
			if inl.Kind == parser.Text {
				texts = append(texts, inl)
			}
		}
		codeHits := map[int][]codeHit{}
		var codeOrder []int
		for k, r := range raw {
			if done[k] {
				continue
			}
			s, e := byteOf[r[0]], byteOf[r[1]]
			mark := r == cur
			tight := false
			for _, inl := range texts {
				if s >= inl.Start && e <= inl.End {
					fm.covers = append(fm.covers, r)
					fm.edits = append(fm.edits, wrap(r[0], r[1], mark, b.Kind))
					done[k], tight = true, true
					if mark {
						fm.curBlock = i
					}
					break
				}
			}
			if tight {
				continue
			}
			lo := -1
			for j, tp := range tops {
				if tp.Start <= s && s < tp.End {
					lo = j
					break
				}
			}
			if lo < 0 {
				continue
			}
			hi := -1
			for j := lo; j < len(tops); j++ {
				if e <= tops[j].End {
					hi = j
					break
				}
			}
			if hi < 0 {
				continue
			}
			fm.covers = append(fm.covers, r)
			done[k] = true
			if mark {
				fm.curBlock = i
			}
			if lo == hi && tops[lo].Kind == parser.Code {
				if _, ok := codeHits[lo]; !ok {
					codeOrder = append(codeOrder, lo)
				}
				codeHits[lo] = append(codeHits[lo], codeHit{s, e, mark})
			} else {
				fm.edits = append(fm.edits, wrap(runeOf(tops[lo].Start), runeOf(tops[hi].End), mark, b.Kind))
			}
		}
		for _, j := range codeOrder {
			hits := codeHits[j]
			e := markEdit{lo: runeOf(tops[j].Start), hi: runeOf(tops[j].End)}
			e.repl = splitCodeSpan(src, tops[j], hits, b.Kind == parser.TableCell)
			for _, h := range hits {
				if h.cur {
					e.cur = true
					e.ovr = splitCodeSpan(src, tops[j], []codeHit{h}, false)
				}
			}
			fm.edits = append(fm.edits, e)
		}
	}
	fm.curSrc = findCurSource(src, doc, fm.curBlock, fm.edits)
	return fm
}

// findCurSource renders block i's source marked with only the edits of
// the current match, for the overlay pass. Blocks the overlay cannot
// paint — tables, which pad their cells, and blocks holding images,
// which would draw twice — report none.
func findCurSource(src string, doc *parser.Document, i int, edits []markEdit) string {
	if i < 0 {
		return ""
	}
	b := doc.Block(i)
	if b.Kind != parser.Paragraph && b.Kind != parser.Heading {
		return ""
	}
	for _, inl := range doc.Inlines(i) {
		if inl.Kind == parser.Image {
			return ""
		}
	}
	base := utf8.RuneCountInString(src[:b.Start])
	var ces []markEdit
	for _, e := range edits {
		if e.cur {
			ces = append(ces, markEdit{lo: e.lo - base, hi: e.hi - base, repl: e.ovr})
		}
	}
	return applyMarkEdits(src[b.Start:b.End], ces)
}

// sessionFindMarkRanges returns the hit rune ranges counted for a query
// in markdown source, without building the markup edits.
func sessionFindMarkRanges(src, query string) [][2]int {
	return sessionFindMarks(src, query, [2]int{-1, -1}).covers
}

// splitCodeSpan rewrites a code inline as alternating unmarked and
// marked code spans so only the hit parts carry the mark. Hits touching
// the delimiters fall back to marking the whole span; u underlines the
// current match where the overlay cannot reach it.
func splitCodeSpan(src string, node parser.Inline, hits []codeHit, u bool) string {
	n := 0
	for node.Start+n < node.End && src[node.Start+n] == '`' {
		n++
	}
	cs, ce := node.Start+n, node.End-n
	if n == 0 || cs > ce {
		return "<mark>" + src[node.Start:node.End] + "</mark>"
	}
	var b strings.Builder
	pos := cs
	for _, h := range hits {
		if h.s < pos || h.e > ce {
			return "<mark>" + src[node.Start:node.End] + "</mark>"
		}
		b.WriteString(codeSpan(src[pos:h.s]))
		if h.cur && u {
			b.WriteString("<mark><u>" + codeSpan(src[h.s:h.e]) + "</u></mark>")
		} else {
			b.WriteString("<mark>" + codeSpan(src[h.s:h.e]) + "</mark>")
		}
		pos = h.e
	}
	b.WriteString(codeSpan(src[pos:ce]))
	return b.String()
}

// codeSpan renders piece as a code span, preserving backticks and
// leading or trailing spaces.
func codeSpan(piece string) string {
	if piece == "" {
		return ""
	}
	if piece[0] == ' ' && piece[len(piece)-1] == ' ' && strings.Trim(piece, " ") != "" {
		l := len(piece) - len(strings.TrimLeft(piece, " "))
		r := len(piece) - len(strings.TrimRight(piece, " "))
		return codeSpan(piece[:l]) + codeSpan(piece[l:len(piece)-r]) + codeSpan(piece[len(piece)-r:])
	}
	run := 0
	for i := 0; i < len(piece); {
		n := 0
		for i < len(piece) && piece[i] == '`' {
			i++
			n++
		}
		run = max(run, n)
		i++
	}
	d := strings.Repeat("`", run+1)
	return d + piece + d
}

// sessionFindCodeRanges returns hits inside code blocks as rune offsets
// approximating their source position, for ordering matches; the code
// block hook highlights them in the rendered lines.
func sessionFindCodeRanges(src, query string) [][2]int {
	if len(sessionFindRanges(src, query)) == 0 {
		return nil
	}
	doc := parser.Parse(src)
	var out [][2]int
	for i := 0; i < doc.Len(); i++ {
		if doc.Block(i).Kind != parser.CodeBlock {
			continue
		}
		off := utf8.RuneCountInString(src[:doc.Block(i).Start])
		for _, r := range sessionFindRanges(doc.Lines(i), query) {
			out = append(out, [2]int{off + r[0], off + r[1]})
		}
	}
	return out
}

// sessionFindMarked applies the mark edits to src; the markdown view
// renders <mark> with the theme's mark background.
func sessionFindMarked(src, query string, cur [2]int) string {
	return applyMarkEdits(src, sessionFindMarks(src, query, cur).edits)
}

// applyMarkEdits applies edits to src; an edit inside one already
// emitted is dropped.
func applyMarkEdits(src string, edits []markEdit) string {
	if len(edits) == 0 {
		return src
	}
	slices.SortFunc(edits, func(x, y markEdit) int { return x.lo - y.lo })
	runes := []rune(src)
	var b strings.Builder
	b.Grow(len(src) * 2)
	pos, last := 0, -1
	for _, e := range edits {
		if e.lo < last {
			continue // covered by an enclosing mark already emitted
		}
		b.WriteString(string(runes[pos:e.lo]))
		b.WriteString(e.repl)
		pos, last = e.hi, e.hi
	}
	b.WriteString(string(runes[pos:]))
	return b.String()
}

// findMarkColor is the background every match highlights with and
// findCurColor the brighter one the current match takes. The markdown
// overlay's mark stacks over the dim mark underneath it, so it asks for
// the difference between the two.
func findMarkColor(t *native.Theme) native.Color    { return t.Accent.Alpha(.25) }
func findCurColor(t *native.Theme) native.Color     { return t.Accent.Alpha(.55) }
func findOverlayColor(t *native.Theme) native.Color { return t.Accent.Alpha(.4) }

// markSpans splits text at ranges into rich-text spans, marking hits;
// the current match, cur, highlights brighter.
func markSpans(text string, ranges [][2]int, mark, curMark native.Color, cur *[2]int) []native.Span {
	var spans []native.Span
	runes := []rune(text)
	pos := 0
	for _, r := range ranges {
		if pos < r[0] {
			spans = append(spans, native.Span{Text: string(runes[pos:r[0]])})
		}
		sp := native.Span{Text: string(runes[r[0]:r[1]]), Background: mark, Radius: 2}
		if cur != nil && r == *cur {
			sp.Background = curMark
		}
		spans = append(spans, sp)
		pos = r[1]
	}
	if pos < len(runes) {
		spans = append(spans, native.Span{Text: string(runes[pos:])})
	}
	return spans
}

// sessionFindCurrent returns the current match's hit range when it lies
// in message m, for its brighter highlight.
func (a *App) sessionFindCurrent(m *app.Message) *[2]int {
	f := &a.find
	if !f.open || f.index < 0 || f.index >= len(f.matches) {
		return nil
	}
	cur := f.matches[f.index]
	messages := a.session.projectedMessages
	if cur.row < 0 || cur.row >= len(messages) || messages[cur.row] != m {
		return nil
	}
	return &[2]int{cur.start, cur.end}
}

// markedMessageText renders a message's literal text with its find hits
// highlighted while the find bar is open.
func (a *App) markedMessageText(c *native.Context, m *app.Message, text string) native.Element {
	if a.find.open {
		if ranges := sessionFindRanges(text, a.find.query); len(ranges) > 0 {
			t := c.Theme()
			return native.RichText(c, markSpans(text, ranges, findMarkColor(t), findCurColor(t), a.sessionFindCurrent(m))...)
		}
	}
	return native.Text(c, text)
}

// findMessageOptions shares the app's markdown options with per-message
// hooks, copying fields rather than the struct, which holds a sync.Map.
func findMessageOptions(o *markdown.Options) *markdown.Options {
	return &markdown.Options{
		Theme:          o.Theme,
		Extensions:     o.Extensions,
		OnLink:         o.OnLink,
		LoadImage:      o.LoadImage,
		Code:           o.Code,
		WrapCode:       o.WrapCode,
		Math:           o.Math,
		HTML:           o.HTML,
		Reveal:         o.Reveal,
		ShareSelection: o.ShareSelection,
	}
}

// findOverlayOptions derives the options of the transparent overlay a
// current match's block renders over itself: every text and chip color
// becomes a ghost — a hair off transparent so the theme resolver keeps
// it rather than defaulting it — while mark alone paints, at the
// brighter current-match alpha.
func findOverlayOptions(o *markdown.Options, t *native.Theme) *markdown.Options {
	out := findMessageOptions(o)
	ghost := native.RGBA(0, 0, 0, .004)
	if out.Theme != nil {
		th := *out.Theme
		th.Text, th.Muted, th.Link, th.Strong = ghost, ghost, ghost, ghost
		th.Code, th.CodeBackground = ghost, ghost
		th.QuoteText, th.QuoteBar, th.Rule = ghost, ghost, ghost
		for i := range th.Headings {
			th.Headings[i].Color = ghost
		}
		th.Mark = findOverlayColor(t)
		out.Theme = &th
	}
	out.OnLink, out.LoadImage = nil, nil
	out.CodeBlock, out.Code, out.Block, out.HTML, out.Math, out.Reveal = nil, nil, nil, nil, nil, nil
	// The overlay must stay in the conversation's selection scope: its
	// texts sit over the block they brighten, so a selection made there
	// has to reach SelectedTexts for Cmd+F to reseed from it.
	out.ShareSelection = true
	return out
}

// findCodeBlock renders code blocks while the find bar is open: blocks
// containing hits get their code re-spanned with marked ranges; all
// others render as usual.
func (a *App) findCodeBlock(c *native.Context, m *app.Message, b *markdown.CodeBlockView) {
	if a.find.open && a.find.query != "" && len(sessionFindRanges(b.Code(), a.find.query)) > 0 {
		a.markedCodeBlock(c, m, b)
		return
	}
	markdown.DefaultCodeBlock(c, b)
}

// findCurInCode locates the current match within code block b of m as
// code rune offsets, when it lies there.
func (a *App) findCurInCode(m *app.Message, b *markdown.CodeBlockView) *[2]int {
	cur := a.sessionFindCurrent(m)
	if cur == nil || b.Index < 0 {
		return nil
	}
	doc := parser.Parse(m.Text)
	if b.Index >= doc.Len() || doc.Block(b.Index).Kind != parser.CodeBlock {
		return nil
	}
	base := utf8.RuneCountInString(m.Text[:doc.Block(b.Index).Start])
	code := doc.Lines(b.Index)
	lo, hi := cur[0]-base, cur[1]-base
	if lo < 0 || hi > utf8.RuneCountInString(code) {
		return nil
	}
	return &[2]int{lo, hi}
}

// markedCodeBlock mirrors markdown.DefaultCodeBlock's frame, swapping
// the highlighted body for spans that carry the mark background on hits.
func (a *App) markedCodeBlock(c *native.Context, m *app.Message, b *markdown.CodeBlockView) {
	t := c.Theme()
	th := markdown.DefaultTheme(t)
	th.FontSize = a.md.Theme.FontSize
	th.CodeFont = a.md.Theme.CodeFont
	th.Mark = findMarkColor(t)
	cur := a.findCurInCode(m, b)
	em := func(v float32) float32 { return v * th.FontSize }
	pad := em(th.CodeBlockPadding)
	label := "Code"
	if b.Language != "" {
		label = "Code, " + b.Language
	}
	box := native.Column(c).Direction(native.LeftToRight).Background(th.CodeBlockBackground).Border(1, th.CodeBlockBorder).Radius(th.CodeBlockRadius).AlignSelf(native.Stretch).Clip()
	box.Role(native.RoleGroup).Label(label)
	box.Children(func() {
		native.Row(c).AlignItems(native.Center).Padding(em(0.35), em(0.4), 0, pad).Children(func() {
			native.Text(c, b.Language).FontSize(em(0.8)).TextColor(th.Muted).Unselectable()
			native.Spacer(c)
			b.CopyButton(c)
		})
		var outer native.Element
		if a.md.WrapCode {
			outer = native.Column(c)
		} else {
			outer = native.ScrollHorizontal(c)
		}
		outer.Direction(native.LeftToRight).AlignSelf(native.Stretch).Font(th.CodeFont).FontSize(em(th.CodeSize)).LineHeight(th.CodeLineHeight)
		outer.Children(func() {
			col := native.Column(c).Padding(em(0.25), pad, pad, pad)
			if !a.md.WrapCode {
				col.Shrink(0)
			}
			col.Children(func() {
				code := b.Code()
				el := native.RichText(c, markedCodeSpans(code, b.Language, sessionFindRanges(code, a.find.query), th, cur, findCurColor(t))...)
				if !a.md.WrapCode {
					el.NoWrap()
				}
			})
		})
	})
}

// markedCodeSpans rebuilds a code block's highlighted spans like the
// markdown view's body does, then applies the mark background to hits;
// the hit at cur, code rune offsets, highlights brighter.
func markedCodeSpans(code, lang string, ranges [][2]int, th *markdown.Theme, cur *[2]int, curMark native.Color) []native.Span {
	var tok editor.Tokenizer
	if l := editor.LanguageNamed(lang); l != nil {
		tok = l.Tokenizer
	}
	ct := th.CodeColors
	var spans []native.Span
	state := uint64(0)
	for i, line := range strings.Split(strings.TrimSuffix(code, "\n"), "\n") {
		if i > 0 {
			spans = append(spans, native.Span{Text: "\n"})
		}
		start := 0
		if tok != nil {
			var toks []editor.Token
			toks, state = tok.Tokenize(line, state, nil)
			for _, t := range toks {
				end := min(t.End, len(line))
				if end > start {
					sp := native.Span{Text: line[start:end], Color: ct.TokenColor(t.Kind)}
					if s := ct.Tokens[t.Kind]; s.Bold {
						sp.Weight = 600
					} else if s.Italic {
						sp.Italic = true
					}
					spans = append(spans, sp)
				}
				start = max(start, end)
			}
		}
		if start < len(line) {
			spans = append(spans, native.Span{Text: line[start:], Color: ct.Text})
		}
	}
	if len(spans) == 0 {
		spans = append(spans, native.Span{Text: " "})
	}
	var curBytes *[2]int
	if cur != nil {
		br := codeByteRanges(code, [][2]int{*cur})
		curBytes = &br[0]
	}
	return applyCodeMarks(spans, codeByteRanges(code, ranges), th.Mark, curMark, curBytes)
}

// codeByteRanges converts rune-offset ranges into byte offsets of code.
func codeByteRanges(code string, ranges [][2]int) [][2]int {
	runes := []rune(code)
	byteOf := make([]int, len(runes)+1)
	for i, r := range runes {
		byteOf[i+1] = byteOf[i] + utf8.RuneLen(r)
	}
	out := make([][2]int, len(ranges))
	for i, r := range ranges {
		out[i] = [2]int{byteOf[r[0]], byteOf[r[1]]}
	}
	return out
}

// applyCodeMarks overlays byte-offset matches on colored spans, marking
// only the covered parts; the range equal to cur highlights brighter.
func applyCodeMarks(spans []native.Span, ranges [][2]int, mark, curMark native.Color, cur *[2]int) []native.Span {
	var out []native.Span
	pos, j := 0, 0
	for _, sp := range spans {
		end := pos + len(sp.Text)
		for j < len(ranges) && ranges[j][1] <= pos {
			j++
		}
		at := pos
		for j < len(ranges) && ranges[j][0] < end {
			lo, hi := max(ranges[j][0], pos), min(ranges[j][1], end)
			if lo > at {
				plain := sp
				plain.Text = sp.Text[at-pos : lo-pos]
				out = append(out, plain)
			}
			marked := sp
			marked.Text = sp.Text[lo-pos : hi-pos]
			marked.Background, marked.Radius = mark, 2
			if cur != nil && ranges[j] == *cur {
				marked.Background = curMark
			}
			out = append(out, marked)
			at = hi
			if ranges[j][1] > end {
				break // the hit continues into the next span
			}
			j++
		}
		if at < end {
			plain := sp
			plain.Text = sp.Text[at-pos:]
			out = append(out, plain)
		}
		pos = end
	}
	return out
}

// sessionViewActive reports whether the focused pane shows a session
// conversation, so Cmd+F searches messages rather than a file's own find.
func (a *App) sessionViewActive() bool {
	return a.open && a.activeTab != "" && !a.isEmptyTab(a.activeTab) && a.currentTerminal() == nil && a.currentFile() == nil
}

func (a *App) openSessionFind() {
	if len(a.State.Messages) == 0 {
		return
	}
	a.find.open, a.find.focus = true, true
	if a.selSeed != "" {
		a.find.query = a.selSeed
	}
}

// findSeed joins the parts of a conversation selection into a query:
// text selected in the messages becomes what Cmd+F looks for. Parts of
// a selection spanning paragraphs join in build order; edge whitespace
// around a selection is never meant to be searched.
func findSeed(parts []native.SelectedText) string {
	var b strings.Builder
	var last string
	for _, part := range parts {
		runes := []rune(part.Text)
		end := min(part.End, len(runes))
		if part.Start < 0 || part.Start >= end {
			continue
		}
		piece := string(runes[part.Start:end])
		// The current-match overlay duplicates its paragraph's text in
		// the scope; a selection crossing that block reads it twice.
		if piece == last {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(piece)
		last = piece
	}
	return strings.TrimSpace(b.String())
}

func (a *App) closeSessionFind() {
	a.find.open = false
	a.composerFocus = true
}

func (a *App) sessionFindStep(back bool) {
	f := &a.find
	if len(f.matches) == 0 {
		return
	}
	if back {
		f.index = (f.index - 1 + len(f.matches)) % len(f.matches)
	} else {
		f.index = (f.index + 1) % len(f.matches)
	}
	f.scroll = true
}

func (a *App) sessionFindResult() string {
	switch {
	case a.find.query == "":
		return ""
	case len(a.find.matches) == 0:
		return "没有匹配"
	default:
		return fmt.Sprintf("%d/%d", a.find.index+1, len(a.find.matches))
	}
}

// sessionFindRescan refreshes matches for the visible rows once per frame: a
// changed query selects the first match at or below the viewport top, while
// streaming keeps the selected hit when it still exists.
func (a *App) sessionFindRescan(messages []*app.Message) {
	f := &a.find
	var previous sessionMatch
	hadPrevious := f.index >= 0 && f.index < len(f.matches)
	if hadPrevious {
		previous = f.matches[f.index]
	}
	f.matches = sessionFindMatches(messages, f.query)
	switch {
	case len(f.matches) == 0:
		f.index = 0
	case f.query != f.scanned:
		f.scanned = f.query
		first, _ := a.list.Visible()
		f.index = len(f.matches) - 1
		for i, m := range f.matches {
			if m.row >= first {
				f.index = i
				break
			}
		}
		f.scroll = true
	case hadPrevious:
		f.index = min(f.index, len(f.matches)-1)
		for i, m := range f.matches {
			if m == previous {
				f.index = i
				break
			}
		}
	default:
		f.index = min(f.index, len(f.matches)-1)
	}
	if f.scroll {
		f.scroll = false
		if len(f.matches) > 0 {
			a.restoreOffset = false
			a.list.ScrollTo(f.matches[f.index].row, native.Center)
		}
	}
}

// sessionFindMatches counts a row's occurrences where its rendered text
// takes them: literal rows match anywhere, while markdown-rendered
// assistant replies count markable inline hits plus code-block hits.
func sessionFindMatches(messages []*app.Message, query string) []sessionMatch {
	var matches []sessionMatch
	for row, m := range messages {
		if m == nil {
			continue
		}
		ranges := sessionFindRanges(m.Text, query)
		if m.Role == "assistant" {
			ranges = append(sessionFindMarkRanges(m.Text, query), sessionFindCodeRanges(m.Text, query)...)
			slices.SortFunc(ranges, func(x, y [2]int) int { return x[0] - y[0] })
		}
		for _, r := range ranges {
			matches = append(matches, sessionMatch{row: row, start: r[0], end: r[1]})
		}
	}
	return matches
}

func (a *App) conversationFindBar(c *native.Context) {
	if !a.find.open {
		return
	}
	t := c.Theme()
	// A stable Key keeps the input's element identity when the "返回最新
	// 消息" overlay appears or vanishes, otherwise stepping matches drops
	// the input's focus.
	native.Row(c.Key("conversation-find")).Attach(native.AnchorTopRight, native.AnchorTopRight).Top(6).Right(16).Padding(4, 10).Gap(8).AlignItems(native.Center).
		Background(t.Background).Border(1, t.Border.Alpha(.7)).Radius(t.Radius).Shadow(0, 2, 10, 0, native.RGBA(0, 0, 0, .08)).Children(func() {
		searchField(c).Width(280).Children(func() {
			input := native.TextInputBase(c, &a.find.query).Label("会话内查找").Placeholder("查找会话内容").Grow(1).MinWidth(0).FontSize(12).Padding(4)
			if a.find.focus {
				input.Focus()
				input.SetTextSelection(0, len([]rune(a.find.query)))
				a.find.focus = false
			}
			composing := input.Composing()
			input.OnTextKey(func(k native.TextKey) bool {
				if composing {
					return false
				}
				if k.Key == native.KeyEscape {
					a.closeSessionFind()
					return true
				}
				if k.Key == native.KeyEnter {
					a.sessionFindStep(k.Mods&native.Shift != 0)
					return true
				}
				return false
			})
			native.Text(c, a.sessionFindResult()).FontSize(11).TextColor(t.TextMuted).Padding(0, 4)
		})
		none := len(a.find.matches) == 0
		if gitIconButton(c.Key("find-prev"), "上一个匹配", upIcon, none).Clicked() {
			a.sessionFindStep(true)
		}
		if gitIconButton(c.Key("find-next"), "下一个匹配", downIcon, none).Clicked() {
			a.sessionFindStep(false)
		}
		if quietIconButton(c, "关闭查找").Size(26, 26).Children(func() { native.Icon(c, tabCloseIcon).Size(12, 12) }).Clicked() {
			a.closeSessionFind()
		}
	})
}
