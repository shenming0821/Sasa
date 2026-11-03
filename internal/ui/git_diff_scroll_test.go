package ui

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
)

func TestGitDiffInitiallyCentersFirstChange(t *testing.T) {
	for _, mode := range []string{"inline", "split", "narrow"} {
		t.Run(mode, func(t *testing.T) {
			a, _, root := p4App(t)
			var before strings.Builder
			for line := 1; line <= 160; line++ {
				fmt.Fprintf(&before, "line %d\n", line)
			}
			patch := "diff --git a/source.go b/source.go\n--- a/source.go\n+++ b/source.go\n@@ -78,7 +78,7 @@\n line 78\n line 79\n line 80\n-line 81\n+changed 81\n line 82\n line 83\n line 84\n"
			review := gitview.ParsePatch(patch)
			review.BeforeKnown, review.BeforeText = true, before.String()
			review.Entry.Path = "source.go"
			d := &filePage{id: "center-diff", root: root, path: filepath.Join(root, "source.go"), kind: "diff"}
			d.gitSplit = mode != "inline"
			a.gitSetDiffText(d, patch, &review)
			a.files = map[string]*filePage{d.id: d}
			width := 1000
			if mode == "narrow" {
				width = 480
			}
			tester := native.NewTester(func(c *native.Context) {
				native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
			}, width, 600)
			for range 6 {
				tester.Frame()
			}
			label, offset, padding := "文件内容 "+d.path, d.scroll.Y, float32(8)
			if mode == "split" {
				label, offset, padding = "差异 原版本", d.gitLeftScroll.Y, 6
				if offset != d.gitRightScroll.Y {
					t.Fatal("split panes did not center together")
				}
			}
			area, ok := tester.Find(label)
			if !ok || math.Abs(float64(padding+80*20+10-offset-area.H/2)) > 1 {
				t.Fatalf("first change not centered: area=%+v offset=%v", area, offset)
			}
			tester.Scroll(area.X+100, area.Y+80, 0, 100)
			tester.Frame()
			inlineY, splitY := d.scroll.Y, d.gitLeftScroll.Y
			a.gitSetDiffText(d, patch, &review)
			for range 3 {
				tester.Frame()
			}
			if d.scroll.Y != inlineY || d.gitLeftScroll.Y != splitY {
				t.Fatal("refresh moved the reading position")
			}
			next := review
			next.Entry.Path = "other.go"
			a.gitSetDiffText(d, patch, &next)
			if !d.gitInitialCenter[0] || !d.gitInitialCenter[1] {
				t.Fatal("switching files did not request initial centering")
			}
		})
	}
}

func TestGitDiffInitialCenterWaitsForLayoutAndClamps(t *testing.T) {
	for _, row := range []int{0, 99} {
		for _, split := range []bool{false, true} {
			d := &filePage{}
			d.gitRows = make([]diffCodeRow, 100)
			d.gitPairs = make([]diffCodePair, 100)
			for index := range d.gitRows {
				d.gitRows[index].hunk = -1
			}
			d.gitRows[row] = diffCodeRow{hunk: 0, kind: '+'}
			d.gitPairs[row].right.kind = '+'
			d.gitInitialCenter = [2]bool{true, true}
			d.centerInitialDiff(split, 0)
			if d.gitInitialCenter != [2]bool{true, true} {
				t.Fatal("centering consumed before layout")
			}
			d.centerInitialDiff(split, 400)
			offset, padding := d.scroll.Y, float32(8)
			if split {
				offset, padding = d.gitSplitY, 6
			}
			expected := float32(0)
			if row == 99 {
				expected = 2000 + padding*2 - 400
			}
			if offset != expected {
				t.Fatalf("row=%d split=%v: got %v want %v", row, split, offset, expected)
			}
		}
	}
}
