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
				if d.scroll.Y != 60 {
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
	}{
		{"inline", "文件内容 ", false, 8},
		{"old version", "差异 原版本", true, 6},
		{"new version", "差异 新版本", true, 6},
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
			matchX, matchY := int(area.X+62), int(area.Y+test.padding+36)
			before := tester.Image().RGBAAt(matchX, matchY)
			tester.ClickAt(area.X+58, area.Y+test.padding+10)
			for range len("same") {
				tester.Key(native.Shift, native.KeyRight)
			}
			tester.Frame()
			if len(cache.words) != 2 || cache.words[1] != (findMatch{start: 11, end: 15}) {
				t.Fatalf("missing matching word: %v (selection %v)", cache.words, cache.selection)
			}
			if tester.Image().RGBAAt(matchX, matchY) == before {
				t.Fatal("matching word background was not painted")
			}
			tester.ClickAt(area.X+100, area.Y+test.padding+10)
			tester.Frame()
			if color := tester.Image().RGBAAt(matchX, matchY); len(cache.words) != 0 || color != before {
				t.Fatalf("clearing the selection kept the match highlighted: words %v, selection %v, pixel %v, baseline %v", cache.words, cache.selection, color, before)
			}
		})
	}
}
