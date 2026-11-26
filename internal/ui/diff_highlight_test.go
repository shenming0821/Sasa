package ui

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

func lexedTokens(t *testing.T, path, text string) []syntaxToken {
	t.Helper()
	lexer := editorLexer(path)
	if lexer == nil {
		t.Fatal("no lexer for", path)
	}
	ranges, overflow := collectHighlightText(context.Background(), lexer, text, 0, editorHighlightTokenLimit)
	if overflow {
		t.Fatal("test input overflowed the token limit")
	}
	return ranges
}

func tokenTypesAtRow(ranges []syntaxToken, rows []diffHighlightRow, rowIndex int) []chroma.TokenType {
	start := 0
	for index := 0; index < rowIndex; index++ {
		start += rows[index].length + 1
	}
	var types []chroma.TokenType
	for _, token := range ranges {
		if token.Start >= start && token.Start < start+rows[rowIndex].length {
			types = append(types, token.Type)
		}
	}
	return types
}

func TestDiffHighlightSourcesRestoreFullContext(t *testing.T) {
	patch := "@@ -2,4 +2,5 @@\n two\n-three\n+three newer\n+three added\n four\n five\n"
	review := gitview.ParsePatch(patch)
	review.BeforeText = "one\ntwo\nthree\nfour\nfive\nsix\n"
	review.BeforeKnown = true
	left, right := diffHighlightSources(&review)
	if left != review.BeforeText {
		t.Fatal("left is the base text")
	}
	if want := "one\ntwo\nthree newer\nthree added\nfour\nfive\nsix\n"; right != want {
		t.Fatalf("right text:\n%q\nwant:\n%q", right, want)
	}
}

func TestDiffHighlightSourcesStopAtMissingContext(t *testing.T) {
	review := gitview.ParsePatch("@@ -1 +1,2 @@\n-a\n+b\n+c\n@@ -5 +6 @@\n-d\n+e\n")
	for _, baseline := range []string{"", "different\n"} {
		review.BeforeText, review.BeforeKnown = baseline, baseline != ""
		left, right := diffHighlightSources(&review)
		if left != "a\n" || right != "b\nc\n" {
			t.Fatalf("source crossed a gap: left %q, right %q", left, right)
		}
	}
}

func TestProjectDiffTokens(t *testing.T) {
	full := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	tokens := lexedTokens(t, "main.go", full)
	lines := strings.Split(strings.TrimSuffix(full, "\n"), "\n")
	var rows []diffHighlightRow
	for index, text := range lines {
		rows = append(rows, diffHighlightRow{line: index + 1, length: utf8.RuneCountInString(text)})
	}
	projected := projectDiffTokens(tokens, full, rows)
	if len(projected) != len(tokens) {
		t.Fatalf("a full view should project every token: %d of %d", len(projected), len(tokens))
	}
	for index, token := range tokens {
		if projected[index] != token {
			t.Fatalf("token %d shifted: %+v != %+v", index, projected[index], token)
		}
	}
	visible := rows[2:]
	projected = projectDiffTokens(tokens, full, visible)
	first := rows[0].length + 1 + rows[1].length + 1
	if len(projected) != len(tokens)-2 {
		t.Fatalf("the cut view keeps the tokens of its rows: %d", len(projected))
	}
	for index, token := range projected {
		want := tokens[index+2]
		if token.Start != want.Start-first || token.End != want.End-first {
			t.Fatalf("token %d of the cut view: %+v, want start %d", index, token, want.Start-first)
		}
	}
}

func TestProjectDiffTokensSkipsFillers(t *testing.T) {
	full := "a := 1\nb := 2\n"
	tokens := lexedTokens(t, "main.go", full)
	rows := []diffHighlightRow{
		{line: 1, length: 6},
		{line: 0, length: 0},
		{line: 2, length: 6},
	}
	projected := projectDiffTokens(tokens, full, rows)
	if len(projected) == 0 || projected[0].Start != 0 {
		t.Fatal("tokens of the first row missing")
	}
	for _, token := range projected {
		if token.Start >= 7 && token.Start < 8 {
			t.Fatal("the filler row should carry no token")
		}
	}
	// Row 2 starts after row 1 (7 runes) and the filler row (1 rune): offset 8.
	last := projected[len(projected)-1]
	if last.Start < 8 {
		t.Fatal("tokens of the last row should land after the filler")
	}
}

// diffReviewFixture diffs the two texts as git would and reports the review
// with the base known, as a real diff of a worktree file does.
func diffReviewFixture(t *testing.T, before, after string) gitview.Review {
	t.Helper()
	patch, err := gitview.CompareText(context.Background(), before, after)
	if err != nil {
		t.Fatal(err)
	}
	review := gitview.ParsePatch(patch)
	review.BeforeText, review.BeforeKnown = before, true
	return review
}

const highlightFixtureBefore = `<script>
export default {
  methods: {
    alpha() {
      first()
      second()
      third()
      fourth()
      fifth()
      sixth()
      const big = c > 1
      seventh()
      eighth()
      pad1()
      pad2()
      pad3()
      use(big)
    },
  },
}
</script>
<style>
.z1 { color: blue }
.z2 { color: blue }
.z3 { color: blue }
.z4 { color: blue }
.a { color: blue }
.b { color: blue }
.c { color: blue }
.d { color: blue }
.e { color: blue }
.f { color: blue }
</style>
`

const highlightFixtureAfter = `<script>
export default {
  methods: {
    alpha() {
      const cmp = a<b
      first()
      second()
      third()
      fourth()
      fifth()
      sixth()
      const big = c > 1
      seventh()
      eighth()
      pad1()
      pad2()
      pad3()
      use(big)
    },
  },
}
</script>
<style>
.z1 { color: blue }
.z2 { color: blue }
.z3 { color: blue }
.z4 { color: blue }
.a { color: blue }
.b { color: blue }
.c { color: blue }
.d { color: red }
.e { color: blue }
.f { color: blue }
</style>
`

func TestDiffInlineHighlightColorsMatchSource(t *testing.T) {
	const filename = "example.vue"
	before := strings.Replace(highlightFixtureBefore, "first()", `first("中文🩺")`, 1)
	after := strings.Replace(highlightFixtureAfter, "first()", `first("中文🩺")`, 1)
	sources := [2]native.TextSnapshot{native.NewTextBuffer(before).Snapshot(), native.NewTextBuffer(after).Snapshot()}
	sourceTokens := [2][]syntaxToken{lexedTokens(t, filename, before), lexedTokens(t, filename, after)}
	for _, ending := range []string{"LF", "CRLF"} {
		t.Run(ending, func(t *testing.T) {
			diffBefore, diffAfter := before, after
			if ending == "CRLF" {
				diffBefore = strings.ReplaceAll(before, "\n", "\r\n")
				diffAfter = strings.ReplaceAll(after, "\n", "\r\n")
			}
			a, queue, root := p4App(t)
			review := diffReviewFixture(t, diffBefore, diffAfter)
			d := &filePage{id: "diff", root: root, path: filepath.Join(root, filename), kind: "diff"}
			a.gitSetDiffText(d, review.Patch, &review)
			a.files = map[string]*filePage{d.id: d}
			tester := native.NewTester(func(c *native.Context) {
				native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
			}, 1100, 800)
			for _, expanded := range []bool{false, true} {
				if expanded {
					marker := slices.IndexFunc(d.gitRows, diffCodeRow.foldMarker)
					if marker < 0 {
						t.Fatal("fixture has no fold to expand")
					}
					a.gitToggleRange(d, d.gitRows[marker].fold)
				}
				p4Wait(t, tester, queue, func() bool {
					return !d.highlighting && d.contentWidth > 0 && d.highlightVersion == d.buffer.Version()
				})
				snapshot := d.buffer.Snapshot()
				for _, dark := range []bool{false, true} {
					palette := claudeEditorPalette(dark)
					for index, row := range d.gitRows {
						side, line := 1, row.next
						if row.kind == '-' {
							side, line = 0, row.old
						}
						if line == 0 {
							continue
						}
						start, _ := snapshot.LineRange(index)
						srcStart, srcEnd := sources[side].LineRange(line - 1)
						got := diffTestLineColors(palette, d.highlights, start, start+srcEnd-srcStart)
						want := diffTestLineColors(palette, sourceTokens[side], srcStart, srcEnd)
						for column, color := range got {
							if color != want[column] {
								t.Fatalf("expanded=%v dark=%v: row %d (%q), column %d: got %#v, want %#v from source line %d", expanded, dark, index+1, row.text, column+1, color, want[column], line)
							}
						}
					}
				}
			}
		})
	}
}

func diffTestLineColors(palette editorPalette, tokens []syntaxToken, start, end int) []native.Color {
	colors := make([]native.Color, end-start)
	for index := range colors {
		colors[index] = palette.text
	}
	for _, span := range palette.ranges(tokens, start, end) {
		for index := max(start, span.Start); index < min(end, span.End); index++ {
			colors[index-start] = span.Color
		}
	}
	return colors
}

func TestDiffHighlightProjectsFullText(t *testing.T) {
	review := diffReviewFixture(t, highlightFixtureBefore, highlightFixtureAfter)
	fullLeft, fullRight := diffHighlightSources(&review)
	if fullRight != highlightFixtureAfter {
		t.Fatalf("rebuilt right text:\n%q\nwant:\n%q", fullRight, highlightFixtureAfter)
	}
	rows := foldDiffContext(diffSourceRows(&review), nil)
	pairs := diffRowPairs(rows)
	target := -1
	for index, pair := range pairs {
		if strings.Contains(pair.left.text, "first()") {
			target = index
		}
	}
	if target < 0 {
		t.Fatal("the first line should stay visible before the fold")
	}
	leftTokens := lexedTokens(t, "example.vue", fullLeft)
	rightTokens := lexedTokens(t, "example.vue", fullRight)
	leftRows, rightRows := make([]diffCodeRow, len(pairs)), make([]diffCodeRow, len(pairs))
	for index, pair := range pairs {
		leftRows[index], rightRows[index] = pair.left, pair.right
	}
	leftMap, rightMap := diffHighlightRows(leftRows, true), diffHighlightRows(rightRows, false)
	leftProjected := projectDiffTokens(leftTokens, fullLeft, leftMap)
	rightProjected := projectDiffTokens(rightTokens, fullRight, rightMap)
	leftTypes := tokenTypesAtRow(leftProjected, leftMap, target)
	rightTypes := tokenTypesAtRow(rightProjected, rightMap, target)
	if len(rightTypes) == 0 {
		t.Fatal("the shared line should carry tokens on the right")
	}
	for _, tokenType := range rightTypes {
		if tokenType == chroma.NameAttribute || tokenType == chroma.Error {
			t.Fatalf("right side lexed as a tag: %v", rightTypes)
		}
	}
	if len(leftTypes) != len(rightTypes) {
		t.Fatalf("sides diverge: left %v, right %v", leftTypes, rightTypes)
	}
	for index, tokenType := range rightTypes {
		if tokenType != leftTypes[index] {
			t.Fatalf("sides diverge at token %d: left %v, right %v", index, leftTypes, rightTypes)
		}
	}
}

// TestDiffHighlightAlignedTextDiverges is the control of the projection: the
// aligned, folded text of the fixture leaves the new side in the vue tag
// state, coloring shared lines as attributes — the defect the projection
// fixes. It pins the fixture so the test above cannot pass vacuously.
func TestDiffHighlightAlignedTextDiverges(t *testing.T) {
	review := diffReviewFixture(t, highlightFixtureBefore, highlightFixtureAfter)
	rows := foldDiffContext(diffSourceRows(&review), nil)
	pairs := diffRowPairs(rows)
	_, alignedRight := diffPairText(pairs)
	target := -1
	for index, pair := range pairs {
		if strings.Contains(pair.right.text, "first()") {
			target = index
		}
	}
	if target < 0 {
		t.Fatal("the first line should stay visible before the fold")
	}
	aligned := lexedTokens(t, "example.vue", alignedRight)
	rightRows := make([]diffCodeRow, len(pairs))
	for index, pair := range pairs {
		rightRows[index] = pair.right
	}
	rightMap := diffHighlightRows(rightRows, false)
	types := tokenTypesAtRow(aligned, rightMap, target)
	for _, tokenType := range types {
		if tokenType == chroma.NameAttribute {
			return
		}
	}
	t.Fatalf("the aligned text should misread the new side as tag attributes: %v", types)
}
