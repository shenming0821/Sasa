package ui

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

func TestDiffCodeRowsKeepSourceAndRealLineNumbers(t *testing.T) {
	patch := "diff --git a/example.go b/example.go\nindex 123..456 100644\n--- a/example.go\n+++ b/example.go\n@@ -6,3 +6,4 @@\n line 6\n-line 7\n+replacement\n+extra\n line 8\n@@ -12 +13 @@\n-line 12\n+last\n\\ No newline at end of file\n"
	review := gitview.ParsePatch(patch)
	for number := 1; number <= 14; number++ {
		review.BeforeText += fmt.Sprintf("line %d\n", number)
	}
	review.BeforeKnown = true
	rows, pairs := diffCodeRows(&review)
	text := diffRowsText(rows)
	if len(rows) != 17 || len(pairs) != 15 || rows[0].text != "line 1" || rows[len(rows)-1].text != "line 14" {
		t.Fatalf("full source missing: rows=%d pairs=%d text=%q", len(rows), len(pairs), text)
	}
	for _, marker := range []string{"diff --git", "index 123", "--- a/", "+++ b/", "@@", "No newline", "⋯"} {
		if strings.Contains(text, marker) {
			t.Fatalf("patch metadata leaked into source: %q", marker)
		}
	}
	removed, added, extra := rows[6], rows[7], rows[8]
	if removed.old != 7 || removed.next != 0 || removed.kind != '-' || added.next != 7 || added.old != 0 || extra.next != 8 {
		t.Fatal("wrong source coordinates", removed, added, extra)
	}
	if diffHunkRow(rows, 0) != 6 || rows[diffHunkRow(rows, 1)].old != 12 || pairs[7].left.old != 0 || pairs[7].right.next != 8 {
		t.Fatal("hunk navigation or aligned insertion lost its source mapping")
	}
	if review.Patch != patch || !strings.Contains(review.Hunks[1].Patch, "@@ -12 +13 @@") {
		t.Fatal("presentation changed the actionable patch")
	}
}

func TestDiffRowsHandleSparseEmptyAndMetadataOnlyChanges(t *testing.T) {
	for _, test := range []struct {
		name, patch, before, expected string
		known                         bool
	}{
		{"new empty line", "@@ -0,0 +1 @@\n+\n", "", "", true},
		{"deletion", "@@ -1,2 +0,0 @@\n-one\n-two\n", "one\ntwo\n", "one\ntwo", true},
		{"zero context insertion", "@@ -1,0 +2 @@\n+inserted\n", "one\ntwo\n", "one\ninserted\ntwo", true},
		{"zero context deletion", "@@ -2 +1,0 @@\n-two\n", "one\ntwo\nthree\n", "one\ntwo\nthree", true},
		{"source resembles metadata", "@@ -1 +1 @@\n---- option\n++++ option\n", "--- option\n", "--- option\n+++ option", true},
		{"sparse history", "@@ -8 +8 @@\n-old\n+new\n", "", "⋯\nold\nnew", false},
		{"unverified base", "@@ -2 +2 @@\n-old\n+new\n", "unrelated\ncontents\n", "⋯\nold\nnew", true},
		{"binary", "Binary files a/a and b/a differ\n", "", "", false},
		{"mode", "diff --git a/a b/a\nold mode 100644\nnew mode 100755\n", "", "", false},
		{"successive recorded patches", "@@ -1 +1 @@\n-old\n+middle\ndiff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-middle\n+new\n", "", "old\nmiddle\nmiddle\nnew", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			review := gitview.ParsePatch(test.patch)
			review.BeforeText, review.BeforeKnown = test.before, test.known
			rows, _ := diffCodeRows(&review)
			if text := diffRowsText(rows); text != test.expected {
				t.Fatalf("source=%q want=%q", text, test.expected)
			}
		})
	}
}

func TestDiffReadsTheActualComparisonBase(t *testing.T) {
	_, _, root, git := gitWorkflowApp(t)
	path := filepath.Join(root, "source.go")
	var before strings.Builder
	for line := range 30 {
		fmt.Fprintf(&before, "var value%d = %d\n", line, line)
	}
	os.WriteFile(path, []byte(before.String()), 0600)
	git("add", "source.go")
	git("commit", "-m", "source baseline")
	indexed := strings.Replace(before.String(), "value20 = 20", "value20 = 21", 1)
	os.WriteFile(path, []byte(indexed), 0600)
	git("add", "source.go")
	os.WriteFile(path, []byte(strings.Replace(indexed, "value20 = 21", "value20 = 22", 1)), 0600)
	entry := gitview.Entry{Path: "source.go", XY: "MM"}
	for _, staged := range []bool{false, true} {
		review, err := gitview.ReadReview(context.Background(), root, entry, staged)
		if err != nil {
			t.Fatal(err)
		}
		expected := indexed
		if staged {
			expected = before.String()
		}
		rows, _ := diffCodeRows(&review)
		if !review.BeforeKnown || review.BeforeText != expected || len(rows) != 31 || rows[0].old != 1 || rows[len(rows)-1].next != 30 {
			t.Fatalf("wrong baseline staged=%v known=%v rows=%d", staged, review.BeforeKnown, len(rows))
		}
	}
}

func TestDiffContextFolding(t *testing.T) {
	for _, mode := range []string{"inline", "split", "narrow"} {
		t.Run(mode, func(t *testing.T) {
			a, queue, root := p4App(t)
			patch := "@@ -10 +10,2 @@\n-line 10\n+changed\n+added\n@@ -23 +24 @@\n-line 23\n+changed again\n"
			review := gitview.ParsePatch(patch)
			for number := 1; number <= 30; number++ {
				review.BeforeText += fmt.Sprintf("line %d\n", number)
			}
			review.BeforeKnown = true
			d := &filePage{id: "diff", root: root, path: filepath.Join(root, "fold.txt"), kind: "diff"}
			d.gitSplit = mode != "inline"
			a.gitSetDiffText(d, patch, &review)
			a.files = map[string]*filePage{d.id: d}
			width := 1000
			if mode == "narrow" {
				width = 480
			}
			tester := native.NewTester(func(c *native.Context) {
				native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
			}, width, 800)
			p4Wait(t, tester, queue, func() bool { return !d.highlighting && d.contentWidth > 0 })
			_, hasMarker := tester.Find("显示 4 行未更改的内容")
			if !hasMarker {
				t.Fatal("missing gap row")
			}
			markers := 0
			for _, text := range tester.Texts() {
				if text == "显示 4 行未更改的内容" {
					markers++
				}
			}
			if markers != 1 {
				t.Fatalf("found %d accessibility rows for one fold, want 1", markers)
			}
			before := d.buffer.String()
			if strings.Contains(before, "line 17\n") || !strings.Contains(before, "line 11\nline 12\nline 13\n\n\nline 20\nline 21\nline 22") {
				t.Fatalf("unexpected context around changes: %q", before)
			}
			if err := tester.Click("显示 4 行未更改的内容"); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				tester.Frame()
			}
			if !strings.Contains(d.buffer.String(), "line 27\nline 28\nline 29\nline 30") || strings.Contains(d.buffer.String(), "line 17\n") {
				t.Fatalf("wrong range expanded: %q", d.buffer.String())
			}
			expanded := 0
			for _, row := range d.gitRows {
				if row.fold != nil && row.fold.count == 4 && !row.foldMarker() {
					expanded++
				}
			}
			if expanded != 4 {
				t.Fatal("open range lost its source rows")
			}
			a.gitSetDiffText(d, patch, &review)
			d.gitSplit = !d.gitSplit
			tester.Frame()
			if row := d.gitRows[diffHunkRow(d.gitRows, 1)]; row.old != 23 || row.kind != '-' {
				t.Fatal("lost source line or hunk", row)
			}
			last := d.gitPairs[len(d.gitPairs)-1]
			if last.left.old != 30 || last.right.next != 31 {
				t.Fatal("lost split source numbering", last)
			}
			foldRow := -1
			for index, row := range d.gitRows {
				if row.fold != nil && !row.foldMarker() {
					foldRow = index
					break
				}
			}
			if foldRow < 0 {
				t.Fatal("no open line to fold from")
			}
			a.gitToggleRange(d, d.gitRows[foldRow].fold)
			tester.Frame()
			if d.buffer.String() != before || d.gitReview.Patch != patch {
				t.Fatal("collapse did not restore the view or changed the patch")
			}
		})
	}
}

func TestDiffContextFoldBoundaries(t *testing.T) {
	patch := "@@ -5 +5 @@\n-old\n+first\n@@ -13 +13 @@\n-old\n+second\n@@ -22 +22 @@\n-old\n+third\n"
	review := gitview.ParsePatch(patch)
	for number := 1; number <= 26; number++ {
		text := fmt.Sprint(number)
		if number == 5 || number == 13 || number == 22 {
			text = "old"
		}
		review.BeforeText += text + "\n"
	}
	review.BeforeKnown = true
	rows := foldDiffContext(diffSourceRows(&review), nil)
	var folds []diffFold
	for _, row := range rows {
		if row.fold != nil {
			folds = append(folds, *row.fold)
		}
	}
	if len(folds) != 1 || folds[0].count != 2 || !strings.HasPrefix(diffRowsText(rows), "\n1\n2\n3\n4\n") {
		t.Fatalf("only the eight-line interior run should fold: %+v", folds)
	}
	review.BeforeKnown = false
	for _, row := range foldDiffContext(diffSourceRows(&review), nil) {
		if row.fold != nil {
			t.Fatal("missing source exposed an expandable fold", row)
		}
	}
}

func TestDiffHunkHeaderRows(t *testing.T) {
	patch := "@@ -10 +10,2 @@\n-line 10\n+changed\n+added\n@@ -23 +24 @@\n-line 23\n+changed again\n"
	review := gitview.ParsePatch(patch)
	for number := 1; number <= 30; number++ {
		review.BeforeText += fmt.Sprintf("line %d\n", number)
	}
	review.BeforeKnown = true
	rows := foldDiffContext(diffSourceRows(&review), nil)
	sections := 0
	for _, row := range rows {
		if row.hunkHeader() {
			sections++
			if row.text != "" {
				t.Fatalf("section band carries text %q: the @@ line numbers were asked not to show", row.text)
			}
		}
	}
	// One band before each of the two runs of changes, as Mayoi's
	// rowHunk rows, but without their @@ -a,b +c,d @@ line numbers.
	if sections != 2 {
		t.Fatalf("found %d section bands, want 2", sections)
	}
	for _, row := range rows {
		if strings.Contains(row.text, "@@") {
			t.Fatalf("diff text shows hunk line numbers: %q", row.text)
		}
	}
}

func diffFoldTestPage(t *testing.T, split bool) (*App, *native.Tester, *filePage) {
	a, queue, root := p4App(t)
	patch := "@@ -10 +10,2 @@\n-line 10\n+changed\n+added\n@@ -23 +24 @@\n-line 23\n+changed again\n"
	review := gitview.ParsePatch(patch)
	for number := 1; number <= 30; number++ {
		review.BeforeText += fmt.Sprintf("line %d\n", number)
	}
	review.BeforeKnown = true
	d := &filePage{id: "diff", root: root, path: filepath.Join(root, "fold.txt"), kind: "diff", gitDiffState: gitDiffState{gitSplit: split}}
	a.gitSetDiffText(d, patch, &review)
	a.files = map[string]*filePage{d.id: d}
	tester := native.NewTester(func(c *native.Context) {
		native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
	}, 1000, 800)
	p4Wait(t, tester, queue, func() bool { return !d.highlighting && d.contentWidth > 0 })
	return a, tester, d
}

func TestDiffFoldKeyboardToggle(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprint("split=", split), func(t *testing.T) {
			a, tester, d := diffFoldTestPage(t, split)
			if err := tester.Click("显示 4 行未更改的内容"); err != nil {
				t.Fatal(err)
			}
			for range 3 {
				tester.Frame()
			}
			if len(d.gitOpenFolds) != 1 {
				t.Fatalf("click did not open the gap: %v", d.gitOpenFolds)
			}
			foldRow := -1
			for index, row := range d.gitRows {
				if row.fold != nil && row.fold.count == 4 && !row.foldMarker() {
					foldRow = index
					break
				}
			}
			if foldRow < 0 {
				t.Fatal("no open line to fold from")
			}
			if split {
				start, _ := d.gitLeft.Snapshot().LineRange(foldRow)
				d.gitSplitFocus = &[2]int{start, start}
				for range 3 {
					tester.Frame()
				}
				left, ok := tester.Find("差异 原版本")
				if !ok {
					t.Fatal("missing left diff pane")
				}
				tester.ClickAt(left.X+100, left.Y+6+float32(foldRow)*20+10-d.gitLeftScroll.Y)
				for range 2 {
					tester.Frame()
				}
			} else {
				start, _ := d.buffer.Snapshot().LineRange(foldRow)
				d.pendingSelection = &[2]int{start, start}
				d.anchor, d.caret = start, start
				for range 3 {
					tester.Frame()
				}
				area, ok := tester.Find("文件内容 " + d.path)
				if !ok {
					t.Fatal("missing diff area")
				}
				tester.ClickAt(area.X+100, area.Y+8+float32(foldRow)*20+10-d.scroll.Y)
				for range 2 {
					tester.Frame()
				}
			}
			tester.Key(0, native.KeyZ)
			for range 3 {
				tester.Frame()
			}
			if len(d.gitOpenFolds) != 0 {
				t.Fatalf("Z did not fold the open range: %v", d.gitOpenFolds)
			}
			_ = a
		})
	}
}

func TestDiffClickThenZ(t *testing.T) {
	for _, split := range []bool{false, true} {
		_, tester, page := diffFoldTestPage(t, split)
		if err := tester.Click("显示 4 行未更改的内容"); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			tester.Frame()
		}
		if page.gitFocusFold != nil || len(page.gitOpenFolds) != 1 {
			t.Fatalf("split=%v: focus did not follow expanded lines", split)
		}
		tester.Key(0, native.KeyZ)
		for range 3 {
			tester.Frame()
		}
		if len(page.gitOpenFolds) != 0 {
			t.Fatalf("split=%v: Z did not collapse the clicked range", split)
		}
	}
}

func TestDiffWordHighlights(t *testing.T) {
	rows := []diffCodeRow{{kind: '-', text: "调用(old, tail)"}, {kind: '+', text: "调用(new, tail)"}, {kind: '+', text: "extra"}}
	pairs := diffRowPairs(rows)
	words := pairs[0].left.words
	if words == nil || words != rows[1].words || words != pairs[0].right.words || pairs[1].right.words != nil {
		t.Fatal("word highlights did not follow paired replacement lines")
	}
	words.compare()
	for side, text := range []string{words.before, words.after} {
		spans := words.spans[side]
		if len(spans) != 1 || text[spans[0][0]:spans[0][1]] != []string{"old", "new"}[side] {
			t.Fatalf("unexpected changed words on side %d: %v", side, spans)
		}
	}
}

func TestDiffFoldRowPresentation(t *testing.T) {
	_, tester, _ := diffFoldTestPage(t, false)
	marker, ok := tester.Find("显示 4 行未更改的内容")
	if !ok {
		t.Fatal("missing gap row")
	}
	image := tester.Image()
	palette := githubEditorPalette(false)
	middle := int(marker.Y + marker.H/2)
	background := image.RGBAAt(int(marker.X+120), middle)
	plain := palette.background
	if background.R == plain.R && background.G == plain.G && background.B == plain.B {
		t.Fatalf("gap row shows no tint: %+v at (%v,%v)", background, int(marker.X+120), middle)
	}
	rail := image.RGBAAt(int(marker.X+1), middle)
	if rail == background || (rail.R == plain.R && rail.G == plain.G && rail.B == plain.B) {
		t.Fatalf("gap row rail missing: %+v background %+v", rail, background)
	}
	iconColumn := -1
	for x := int(marker.X + 12); x < int(marker.X+26); x++ {
		pixel := image.RGBAAt(x, middle)
		if pixel != background && !(pixel.R == plain.R && pixel.G == plain.G && pixel.B == plain.B) {
			iconColumn = x
			break
		}
	}
	if iconColumn < 0 {
		t.Fatal("no unfold glyph near the 13 DIP icon position")
	}
}

func TestDiffFoldEnterOpensGap(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprint("split=", split), func(t *testing.T) {
			_, tester, d := diffFoldTestPage(t, split)
			markerRow := -1
			for index, row := range d.gitRows {
				if row.foldMarker() && row.fold.count == 4 {
					markerRow = index
					break
				}
			}
			if markerRow < 0 {
				t.Fatal("no marker row")
			}
			if split {
				for index, pair := range d.gitPairs {
					if pair.left.foldMarker() && pair.left.fold.count == 4 {
						markerRow = index
						break
					}
				}
				start, _ := d.gitLeft.Snapshot().LineRange(markerRow)
				d.gitSplitFocus = &[2]int{start, start}
				for range 3 {
					tester.Frame()
				}
				left, ok := tester.Find("差异 原版本")
				if !ok {
					t.Fatal("missing left diff pane")
				}
				tester.ClickAt(left.X+100, left.Y+6+float32(markerRow)*20+10-d.gitLeftScroll.Y)
			} else {
				start, _ := d.buffer.Snapshot().LineRange(markerRow)
				d.pendingSelection = &[2]int{start, start}
				d.anchor, d.caret = start, start
				for range 3 {
					tester.Frame()
				}
				area, ok := tester.Find("文件内容 " + d.path)
				if !ok {
					t.Fatal("missing diff area")
				}
				tester.ClickAt(area.X+100, area.Y+8+float32(markerRow)*20+10-d.scroll.Y)
			}
			for range 2 {
				tester.Frame()
			}
			tester.Key(0, native.KeyEnter)
			for range 3 {
				tester.Frame()
			}
			if len(d.gitOpenFolds) != 1 {
				t.Fatalf("Enter on the gap row did not open it: %v", d.gitOpenFolds)
			}
		})
	}
}

func TestDiffSourcePresentationAndNavigation(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, split := range []bool{false, true} {
			t.Run(fmt.Sprintf("dark=%v/split=%v", dark, split), func(t *testing.T) {
				a, queue, root := p4App(t)
				patch := "diff --git a/example.go b/example.go\nindex 123..456 100644\n--- a/example.go\n+++ b/example.go\n@@ -1,5 +1,6 @@\n package ui\n \n func greeting() string {\n-\treturn \"old\"\n+\tmessage := \"你好，Sasa\"\n+\treturn message\n }\n"
				review := gitview.ParsePatch(patch)
				d := &filePage{id: "diff", root: root, path: filepath.Join(root, "example.go"), kind: "diff", compare: "更改", gitDiffState: gitDiffState{gitSplit: split}}
				a.gitSetDiffText(d, patch, &review)
				a.files = map[string]*filePage{d.id: d}
				tester := native.NewTester(func(c *native.Context) {
					native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
				}, 1100, 600)
				tester.SetDark(dark)
				p4Wait(t, tester, queue, func() bool { return !d.highlighting && d.highlightVersion == d.buffer.Version() })
				if strings.Contains(d.buffer.String(), "@@") || strings.Contains(d.buffer.String(), "+\t") || len(d.highlights) == 0 {
					t.Fatal("raw patch or missing syntax highlighting")
				}
				if split && (len(d.gitLeftHighlights) == 0 || len(d.gitRightHighlights) == 0) {
					t.Fatal("split syntax highlighting missing")
				}
				a.gitStepHunk(d, 1)
				if d.scroll.Y != 80 {
					t.Fatal("navigation used a patch coordinate", d.scroll.Y)
				}
				d.scroll.Y, d.gitLeftScroll.Y, d.gitRightScroll.Y, d.gitSplitY = 0, 0, 0, 0
				tester.Frame()
				if directory := os.Getenv("SASA_DIFF_SCREENSHOTS"); directory != "" {
					if err := os.MkdirAll(directory, 0700); err != nil {
						t.Fatal(err)
					}
					file, err := os.Create(filepath.Join(directory, fmt.Sprintf("diff-dark-%v-split-%v.png", dark, split)))
					if err != nil {
						t.Fatal(err)
					}
					encodeErr := png.Encode(file, tester.Image())
					closeErr := file.Close()
					if encodeErr != nil || closeErr != nil {
						t.Fatal(encodeErr, closeErr)
					}
				}
			})
		}
	}
}

func TestCommitDiffSelectionHighlights(t *testing.T) {
	for _, test := range []struct {
		name, label string
		split       bool
		padding     float32
		inset       float32
	}{
		{"inline", "文件内容 ", false, 8, diffTextX},
		{"old version", "差异 原版本", true, 6, diffSideTextX},
		{"new version", "差异 新版本", true, 6, diffSideTextX},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, queue, root := p4App(t)
			path := filepath.Join(root, "提交 abc1234")
			patch := "@@ -1,3 +1,3 @@\n same first\n same second\n-old\n+new\n"
			review := gitview.ParsePatch(patch)
			review.Entry.Path = "sample.txt"
			d := &filePage{id: "commit", root: root, path: path, kind: "diff", gitDiffState: gitDiffState{gitCommit: "abc1234", gitSplit: test.split}}
			a.gitSetDiffText(d, patch, &review)
			a.files = map[string]*filePage{d.id: d}
			tester := native.NewTester(func(c *native.Context) {
				native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
			}, 1100, 600)
			p4Wait(t, tester, queue, func() bool { return !d.highlighting && d.contentWidth > 0 })
			label, cache := test.label, &d.gitSplitWords
			if !test.split {
				label, cache = label+path, &d.wordMatchCache
			}
			area, ok := tester.Find(label)
			if !ok {
				t.Fatalf("missing diff area %q", label)
			}
			// The buffer starts with the hunk-header row, so "same first"
			// is the line at +20 and "same second" the one at +40, each
			// centered 10px below its top. Selecting the first "same"
			// must highlight its match in the other line, which the
			// sample pixel watches.
			textX := area.X + test.inset
			firstY, secondY := area.Y+test.padding+20+10, area.Y+test.padding+40+10
			matchX, matchY := int(textX+4), int(secondY)
			before := tester.Image().RGBAAt(matchX, matchY)
			tester.ClickAt(textX+3, firstY)
			for range len("same ") {
				tester.Key(native.Shift, native.KeyRight)
			}
			tester.Frame()
			if len(cache.words) != 2 || cache.words[1] != (findMatch{start: 12, end: 17}) {
				t.Fatalf("missing matching word: %v (selection %v)", cache.words, cache.selection)
			}
			if tester.Image().RGBAAt(matchX, matchY) == before {
				t.Fatal("matching word background was not painted")
			}
			tester.ClickAt(textX+140, firstY)
			tester.Frame()
			if color := tester.Image().RGBAAt(matchX, matchY); len(cache.words) != 0 || color != before {
				t.Fatalf("clearing the selection kept the match highlighted: words %v, selection %v, pixel %v, baseline %v", cache.words, cache.selection, color, before)
			}
		})
	}
}

func TestDiffChangeRuns(t *testing.T) {
	patch := "@@ -10 +10,2 @@\n-line 10\n+changed\n+added\n@@ -23 +24 @@\n-line 23\n+changed again\n"
	review := gitview.ParsePatch(patch)
	for number := 1; number <= 30; number++ {
		review.BeforeText += fmt.Sprintf("line %d\n", number)
	}
	review.BeforeKnown = true
	pairs := diffRowPairs(foldDiffContext(diffSourceRows(&review), nil))
	runs := diffChangeRuns(pairs)
	if len(runs) != 2 {
		t.Fatalf("found %d change runs, want 2: %+v", len(runs), runs)
	}
	first, second := runs[0], runs[1]
	// The first change pairs one deletion with two insertions: its left
	// span is the deletion's row, its right span both insertion rows,
	// where the two stand apart the mid band curves.
	if first.hunk != 0 || first.pureInsertion || first.pureDeletion {
		t.Fatalf("first run misread: %+v", first)
	}
	if first.rb-first.rt != 2 || first.lb-first.lt != 1 {
		t.Fatalf("first run spans left [%v,%v) right [%v,%v), want 1 and 2 rows", first.lt, first.lb, first.rt, first.rb)
	}
	if second.hunk != 1 || second.lb-second.lt != 1 || second.rb-second.rt != 1 {
		t.Fatalf("second run misread: %+v", second)
	}
}

func TestDiffChangeRunPureInsertionAndDeletion(t *testing.T) {
	insert := diffRowPairs(foldDiffContext(diffSourceRows(&gitview.Review{
		Hunks: []gitview.Hunk{{OldStart: 2, NewStart: 2, NewCount: 2, Patch: "@@ -2,0 +2,2 @@\n+a\n+b\n"}},
	}), nil))
	runs := diffChangeRuns(insert)
	if len(runs) != 1 || !runs[0].pureInsertion || runs[0].pureDeletion {
		t.Fatalf("pure insertion misread: %+v", runs)
	}
	if runs[0].rt != runs[0].lt {
		t.Fatalf("insertion spans should start at the same row: %+v", runs[0])
	}
	if runs[0].rb-runs[0].rt != 2 || runs[0].lb != runs[0].lt {
		t.Fatalf("insertion right span should cover 2 rows, left none: %+v", runs[0])
	}
}

func TestDiffMidRowAt(t *testing.T) {
	d := &filePage{gitDiffState: gitDiffState{gitPairs: make([]diffCodePair, 30)}}
	// Rows are 20 DIP tall below a 6 DIP padding: a y inside the first
	// row maps to row 0 wherever the view scrolled.
	if row := d.diffMidRowAt(8); row != 0 {
		t.Fatalf("y=8 mapped to row %d, want 0", row)
	}
	if row := d.diffMidRowAt(27); row != 1 {
		t.Fatalf("y=27 mapped to row %d, want 1", row)
	}
	if row := d.diffMidRowAt(3); row != -1 {
		t.Fatalf("padding y mapped to row %d, want -1", row)
	}
	d.gitSplitY = 100
	if row := d.diffMidRowAt(8); row != 5 {
		t.Fatalf("scrolled y=8 mapped to row %d, want 5", row)
	}
}
