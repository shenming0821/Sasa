package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shenming0821/Sasa/internal/app"
	"github.com/shenming0821/Sasa/internal/storage"
)

func TestConversationFindStepsAndEscapes(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	a.State.Messages = []*app.Message{
		{ID: "u1", Role: "user", Text: "请检查 sasa 的搜索功能", Done: true},
		{ID: "a1", Role: "assistant", Text: "好的，先看代码。\n\n" + strings.Repeat("填充段落。\n\n", 200), Done: true},
		{ID: "u2", Role: "user", Text: "再验证一遍", Done: true},
		{ID: "a2", Role: "assistant", Text: "sasa 搜索已就绪，sasa 再见", Done: true},
	}
	tt := native.NewTester(a.view, 1100, 760)
	tt.Key(native.Cmd, native.KeyF)
	if !a.find.open {
		t.Fatal("Cmd+F did not open the conversation find bar")
	}
	tt.Type("sasa")
	tt.Frame()
	if len(a.find.matches) != 3 {
		t.Fatal("wrong match count", len(a.find.matches))
	}
	// The rescan lands on the match nearest below the viewport top: the list
	// sits at its end, so the row-3 hits come first.
	if a.find.index != 1 || !tt.HasText("2/3") {
		t.Fatal("rescan did not select the nearest match", a.find.index)
	}
	// Enter cycles forward through matches and scrolls their rows into view.
	tt.Key(0, native.KeyEnter)
	tt.Frame()
	if a.find.index != 2 || a.find.matches[2].row != 3 {
		t.Fatal("Enter did not step to the next match", a.find.index)
	}
	if !tt.HasText("3/3") {
		t.Fatal("count did not follow the current match")
	}
	tt.Key(0, native.KeyEnter)
	tt.Frame()
	if a.find.index != 0 || a.find.matches[0].row != 0 {
		t.Fatal("Enter did not wrap to the first match", a.find.index)
	}
	// Shift+Enter steps backward.
	tt.Key(native.Shift, native.KeyEnter)
	tt.Frame()
	if a.find.index != 2 {
		t.Fatal("Shift+Enter did not step back to the last match", a.find.index)
	}
	// Esc exits the search.
	tt.Key(0, native.KeyEscape)
	tt.Frame()
	if a.find.open {
		t.Fatal("Escape did not close the find bar")
	}
	if tt.HasText("会话内查找") {
		t.Fatal("find bar still rendered after Escape")
	}
}

func TestSessionFindMarkedScopesToInlines(t *testing.T) {
	// A hit inside a text inline marks just the hit; one inside a code
	// span splits it so only the hit parts mark; one in a link
	// destination or autolink marks the covering inline. Fenced code
	// stays untouched: the code block hook highlights its hits.
	src := "plain sasa、**加粗 sasa**、`代码 sasa`、[链接 sasa](https://x)、<https://sasa.dev>。\n\n```\nsasa 围栏\n```"
	marked := sessionFindMarked(src, "sasa", [2]int{-1, -1})
	if got := strings.Count(marked, "<mark>"); got != 5 {
		t.Fatal("wrong mark count", got, marked)
	}
	for _, want := range []string{
		"plain <mark>sasa</mark>",
		"加粗 <mark>sasa</mark>**",
		"`代码 `<mark>`sasa`</mark>",
		"链接 <mark>sasa</mark>](https://x)",
		"<mark><https://sasa.dev></mark>",
	} {
		if !strings.Contains(marked, want) {
			t.Fatal("missing mark", want, marked)
		}
	}
	if !strings.Contains(marked, "```\nsasa 围栏\n```") {
		t.Fatal("code block source changed", marked)
	}
	// A hit spanning a code span's boundary marks the covering inlines.
	if marked := sessionFindMarked("a sasa 和 `sasa` 结束", "和 `s", [2]int{-1, -1}); marked != "<mark>a sasa 和 `sasa`</mark> 结束" {
		t.Fatal("boundary hit not covered", marked)
	}
	// The current match keeps a plain mark where the overlay can brighten
	// it and gets an underline only where the overlay cannot reach, as in
	// a table cell.
	cur := sessionFindRanges("a sasa 和 sasa", "sasa")[1]
	fm := sessionFindMarks("a sasa 和 sasa", "sasa", cur)
	marked = applyMarkEdits("a sasa 和 sasa", fm.edits)
	if strings.Contains(marked, "<u>") || !strings.Contains(marked, "<mark>sasa</mark>") {
		t.Fatal("current hit should mark without underline", marked)
	}
	if fm.curSrc != "a sasa 和 <mark>sasa</mark>" {
		t.Fatal("overlay source not scoped to the current hit", fm.curSrc)
	}
	cell := "| a |\n| - |\n| sasa 与 sasa |"
	fm = sessionFindMarks(cell, "sasa", sessionFindRanges(cell, "sasa")[1])
	if marked = applyMarkEdits(cell, fm.edits); !strings.Contains(marked, "<mark><u>sasa</u></mark>") {
		t.Fatal("cell hit should fall back to underline", marked)
	}
	if fm.curSrc != "" {
		t.Fatal("table cell should not get an overlay source", fm.curSrc)
	}
	// Literal text keeps every hit markable.
	if got := sessionFindMarkRanges("sAsA 和 sasa", "sasa"); len(got) != 2 {
		t.Fatal("case-insensitive ranges", got)
	}
}

func TestSessionFindCodeBlockHits(t *testing.T) {
	src := "前文 sasa。\n\n```go\nfunc sasa() {\n\tmain := \"sasa\"\n}\n```"
	marks := sessionFindMarkRanges(src, "sasa")
	codes := sessionFindCodeRanges(src, "sasa")
	if len(marks) != 1 || len(codes) != 2 {
		t.Fatal("wrong hit split", marks, codes)
	}
	matches := sessionFindMatches([]*app.Message{{Role: "assistant", Text: src}}, "sasa")
	if len(matches) != 3 {
		t.Fatal("code-block hits not counted", matches)
	}
	// The block hook keeps syntax colors and marks only the hits; the
	// current hit highlights brighter.
	th := markdown.DefaultTheme(&native.Theme{})
	th.Mark = native.RGBA(1, 2, 3, .5)
	curMark := native.RGBA(9, 9, 9, .9)
	cur := &[2]int{24, 28}
	spans := markedCodeSpans("func sasa() {\n\tmain := \"sasa\"\n}\n", "go", sessionFindRanges("func sasa() {\n\tmain := \"sasa\"\n}\n", "sasa"), th, cur, curMark)
	var texts []string
	marked, brighter := 0, 0
	for _, sp := range spans {
		texts = append(texts, sp.Text)
		if sp.Background.A != 0 {
			marked++
			if sp.Text != "sasa" {
				t.Fatal("marked non-hit span", sp.Text)
			}
			if sp.Background == curMark {
				brighter++
			}
		}
	}
	if marked != 2 || brighter != 1 || strings.Join(texts, "") != "func sasa() {\n\tmain := \"sasa\"\n}" {
		t.Fatal("marked code spans rebuilt wrong", marked, brighter, texts)
	}
}

func TestMarkSpansHighlightsOnlyMatches(t *testing.T) {
	ranges := sessionFindRanges("ab sasa cd sasa", "sasa")
	mark := native.RGBA(1, 2, 3, .25)
	curMark := native.RGBA(1, 2, 3, .55)
	spans := markSpans("ab sasa cd sasa", ranges, mark, curMark, &ranges[1])
	if len(spans) != 4 {
		t.Fatal("wrong span count", len(spans))
	}
	for i, want := range []string{"ab ", "sasa", " cd ", "sasa"} {
		if spans[i].Text != want {
			t.Fatal("wrong span text", i, spans[i].Text)
		}
		var wantBg native.Color
		if want == "sasa" {
			wantBg = mark
			if i == 3 {
				wantBg = curMark
			}
		}
		if spans[i].Background != wantBg {
			t.Fatal("wrong span marking", i, want)
		}
		if spans[i].Underline {
			t.Fatal("current hit should brighten, not underline", i, want)
		}
	}
	// Assistant rows count inline-cover and code hits; user rows every hit.
	messages := []*app.Message{
		{Role: "assistant", Text: "见 `sasa` 与 sasa"},
		{Role: "user", Text: "见 `sasa` 与 sasa"},
	}
	matches := sessionFindMatches(messages, "sasa")
	if len(matches) != 4 || matches[0].row != 0 || matches[1].row != 0 || matches[2].row != 1 || matches[3].row != 1 {
		t.Fatal("row match count did not follow render path", matches)
	}
}

// TestFindOverlayBrightensTheCurrentMatch renders a marked message and
// checks the paint shows two highlight levels: the dim mark of every
// hit and the brighter composite the transparent overlay adds on the
// current one.
func TestFindOverlayBrightensTheCurrentMatch(t *testing.T) {
	a := New(stubBackend{}, "test", t.TempDir(), nil)
	a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	msg := &app.Message{ID: "m1", Role: "assistant", Text: "sasa sasa sasa sasa", Done: true}
	a.session.projectedMessages = []*app.Message{msg}
	a.find.open, a.find.query, a.find.index = true, "sasa", 1
	a.find.matches = sessionFindMatches(a.session.projectedMessages, "sasa")
	var accent native.Color
	tt := native.NewTester(func(c *native.Context) {
		accent = c.Theme().Accent
		native.Column(c).Fill().Background(c.Theme().Background).Children(func() {
			a.messageMarkdown(c, msg)
		})
	}, 500, 80)
	tt.Frame()
	img := tt.Image()
	bg := img.RGBAAt(2, 2)
	mix := func(alpha float32) [3]int {
		w := func(bc, ac uint8) int {
			return int(float32(bc)*(1-alpha) + float32(ac)*alpha + 0.5)
		}
		return [3]int{w(bg.R, accent.R), w(bg.G, accent.G), w(bg.B, accent.B)}
	}
	near := func(p [3]int, q [3]int) bool {
		d := 0
		for i := range p {
			if v := p[i] - q[i]; v < 0 {
				d -= v
			} else {
				d += v
			}
		}
		return d < 60
	}
	dimWant, brightWant := mix(.25), mix(.55)
	dim, bright := 0, 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			p := img.RGBAAt(x, y)
			q := [3]int{int(p.R), int(p.G), int(p.B)}
			switch {
			case near(q, brightWant):
				bright++
			case near(q, dimWant):
				dim++
			}
		}
	}
	if dim == 0 {
		t.Fatal("no dim highlight painted")
	}
	if bright == 0 {
		t.Fatal("overlay did not brighten the current match")
	}
}

func TestConversationFindStepButtons(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	a.State.Messages = []*app.Message{
		{ID: "u1", Role: "user", Text: "sasa", Done: true},
		{ID: "a1", Role: "assistant", Text: "sasa sasa", Done: true},
	}
	tt := native.NewTester(a.view, 1100, 760)
	tt.Key(native.Cmd, native.KeyF)
	tt.Type("sasa")
	tt.Frame()
	if len(a.find.matches) != 3 {
		t.Fatal("wrong match count", len(a.find.matches))
	}
	at := a.find.index
	if err := tt.Click("下一个匹配"); err != nil {
		t.Fatal("cannot click the next button", err)
	}
	tt.Frame()
	if a.find.index != (at+1)%3 {
		t.Fatal("next button did not step forward", at, a.find.index)
	}
	if err := tt.Click("上一个匹配"); err != nil {
		t.Fatal("cannot click the previous button", err)
	}
	tt.Frame()
	if a.find.index != at {
		t.Fatal("previous button did not step back", at, a.find.index)
	}
	if !tt.HasText(fmt.Sprintf("%d/3", a.find.index+1)) {
		t.Fatal("count did not follow the stepped match", a.find.index)
	}
}

func TestConversationFindSeedsFromSelection(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	a.State.Messages = []*app.Message{
		{ID: "a1", Role: "assistant", Text: "markword", Done: true},
		{ID: "u1", Role: "user", Text: "filler", Done: true},
		{ID: "a2", Role: "assistant", Text: "tailword", Done: true},
	}
	tt := native.NewTester(a.view, 1100, 760)
	// A double click selects the word under the pointer in the
	// conversation's shared selection; Cmd+F searches for it directly.
	doubleClick := func(s string) {
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("text %q not rendered", s)
		}
		x, y := r.X+r.W/2, r.Y+r.H/2
		tt.ClickAt(x, y)
		tt.ClickAt(x, y)
	}
	// With no selection, Cmd+F opens with an empty query.
	tt.Key(native.Cmd, native.KeyF)
	if !a.find.open || a.find.query != "" {
		t.Fatal("Cmd+F without a selection changed the query", a.find.query)
	}
	tt.Key(0, native.KeyEscape)
	doubleClick("markword")
	tt.Key(native.Cmd, native.KeyF)
	if a.find.query != "markword" {
		t.Fatal("Cmd+F did not seed the selected word", a.find.query)
	}
	tt.Frame()
	if len(a.find.matches) != 1 || a.find.matches[0].row != 0 {
		t.Fatal("seeded query did not match its message", a.find.matches)
	}
	// Selecting again while the bar is open reseeds.
	doubleClick("tailword")
	tt.Key(native.Cmd, native.KeyF)
	if a.find.query != "tailword" {
		t.Fatal("Cmd+F did not reseed from the new selection", a.find.query)
	}
	tt.Frame()
	if len(a.find.matches) != 1 || a.find.matches[0].row != 2 {
		t.Fatal("reseeded query did not match its message", a.find.matches)
	}
}

func TestConversationFindReseedsOverTheCurrentMatch(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	a.State.Messages = []*app.Message{
		{ID: "a1", Role: "assistant", Text: "alpha firstword beta secondword", Done: true},
		{ID: "a2", Role: "assistant", Text: "thirdword", Done: true},
	}
	tt := native.NewTester(a.view, 1100, 760)
	drag := func(s string, lo, hi float32) {
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("text %q not rendered", s)
		}
		tt.Press(r.X+lo, r.Y+r.H/2)
		tt.Move(r.X+hi, r.Y+r.H/2)
		tt.Release(r.X+hi, r.Y+r.H/2)
	}
	// The seeded query marks "firstword" and lays the current-match
	// overlay over its paragraph. A second selection dragged over that
	// same paragraph must still seed: the overlay's texts share the
	// conversation's selection scope.
	drag("alpha firstword beta secondword", 40, 110)
	if a.selSeed == "" {
		t.Fatal("first selection did not seed")
	}
	tt.Key(native.Cmd, native.KeyF)
	if a.find.query == "" {
		t.Fatal("Cmd+F did not open with the selected text")
	}
	drag("alpha firstword beta secondword", 150, 230)
	if a.selSeed == "" {
		t.Fatal("selection over the overlay-covered block seeded nothing")
	}
	tt.Key(native.Cmd, native.KeyF)
	if a.find.query != a.selSeed {
		t.Fatal("Cmd+F did not replace the query with the new selection", a.find.query, a.selSeed)
	}
}

func TestConversationFindShortcutScope(t *testing.T) {
	a, q, root := p4App(t)
	a.State.Messages = []*app.Message{
		{ID: "u1", Role: "user", Text: "开始", Done: true},
		{ID: "a1", Role: "assistant", Text: "完成", Done: true},
	}
	tt := native.NewTester(a.view, 1100, 760)
	// An empty tab has no conversation: Cmd+F stays out of the way.
	a.tabs = append(a.tabs, storage.Tab{ID: "empty-1", Kind: "empty", Workspace: root})
	a.activeTab = "empty-1"
	tt.Key(native.Cmd, native.KeyF)
	if a.find.open {
		t.Fatal("Cmd+F opened session find on an empty tab")
	}
	// A file page keeps Cmd+F for its in-file find bar.
	a.openFile(root, filepath.Join(root, "README.md"))
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	if err := tt.Click("文件内容 " + filepath.Join(root, "README.md")); err != nil {
		t.Fatal(err)
	}
	tt.Key(native.Cmd, native.KeyF)
	if a.find.open {
		t.Fatal("Cmd+F opened session find on a file page")
	}
	if !a.currentFile().findOpen {
		t.Fatal("Cmd+F did not open the in-file find on a file page")
	}
}
