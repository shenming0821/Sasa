package ui

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
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
			expandAllTestDiffContext(a, d)
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
			changed := diffHunkRow(d.gitRows, 0)
			if !ok || math.Abs(float64(padding+float32(changed)*20+10-offset-area.H/2)) > 1 {
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

func TestGitDiffScrolledRowAlignment(t *testing.T) {
	for _, mode := range []string{"inline", "left", "right", "narrow"} {
		t.Run(mode, func(t *testing.T) {
			split := mode == "left" || mode == "right"
			a, queue, root := p4App(t)
			var before strings.Builder
			for line := 1; line <= 600; line++ {
				if line%7 == 0 {
					before.WriteString("// 🧑‍💻 修改记录\n")
					continue
				}
				fmt.Fprintf(&before, "const value%d = '436821081002393600'; // 转介绍挂号校验患者挂号记录\n", line)
			}
			old := "const value494 = '436821081002393600'; // 转介绍挂号校验患者挂号记录"
			patch := "@@ -494 +494 @@\n-" + old + "\n+" + strings.Replace(old, "436821081002393600", "554738962583736320", 1) + "\n"
			review := gitview.ParsePatch(patch)
			review.BeforeKnown, review.BeforeText = true, before.String()
			d := &filePage{id: "alignment", root: root, path: filepath.Join(root, "source.js"), kind: "diff", gitSplit: mode != "inline"}
			a.gitSetDiffText(d, patch, &review)
			a.files = map[string]*filePage{d.id: d}
			expandAllTestDiffContext(a, d)
			width := 1400
			if mode == "narrow" {
				width = 480
			}
			tester := native.NewTester(func(c *native.Context) {
				native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
			}, width, 600)
			p4Wait(t, tester, queue, func() bool { return !d.highlighting && d.contentWidth > 0 })
			label, scroll, lines, padding := "文件内容 "+d.path, &d.scroll, &d.lines, float32(8)
			if split {
				label, scroll, lines, padding = "差异 新版本", &d.gitRightScroll, &d.gitRightLines, 6
			}
			if mode == "left" {
				label, scroll, lines = "差异 原版本", &d.gitLeftScroll, &d.gitLeftLines
			}
			area, ok := tester.Find(label)
			if !ok {
				t.Fatal("missing diff")
			}
			tester.Scroll(area.X+150, area.Y+100, 0, -800)
			tester.Scroll(area.X+150, area.Y+100, 0, 800)
			row := diffHunkRow(d.gitRows, 0)
			if split {
				for index, pair := range d.gitPairs {
					if pair.left.old == 494 {
						row = index
						break
					}
				}
			} else {
				row++
			}
			scroll.Y = max(0, lines.Top(row)-area.H/2)
			tester.Frame()
			for _, delta := range []float32{0, 113, -57, 51, -127, 43} {
				tester.Scroll(area.X+150, area.Y+100, 0, delta)
				tester.Frame()
				image := tester.Image()
				palette := githubEditorPalette(false)
				background, kind := palette.addedBackground, byte('+')
				if mode == "left" {
					background = palette.deletedBackground
				}
				_ = kind
				changedY := -1
				for y := int(area.Y + padding); y < int(area.Y+area.H)-20; y++ {
					pixel := image.RGBAAt(int(area.X+area.W-30), y)
					if pixel.R == background.R && pixel.G == background.G && pixel.B == background.B {
						changedY = y
						break
					}
				}
				if changedY < 0 {
					t.Fatalf("changed row missing at scroll %v", scroll.Y)
				}
				textY := area.Y + padding + lines.Top(row) - scroll.Y
				if math.Abs(float64(textY-float32(changedY))) > 1 {
					t.Fatalf("background and text differ at scroll %v: background=%v text=%v", scroll.Y, changedY, textY)
				}
				// The line number must sit on the same row as the text: its
				// digits appear inside the row's band and nowhere a row above
				// or below. Scan the gutter for the digit ink of the changed
				// line's number and check its vertical center matches textY.
				inked := -1
				count := 0
				for y := int(math.Ceil(float64(textY))) - 2; y < int(textY+22); y++ {
					for x := int(area.X + 4); x < int(area.X+diffTextX-4); x++ {
						px := image.RGBAAt(x, y)
						bg := image.RGBAAt(int(area.X+area.W-30), y)
						if px != bg {
							dr := int(px.R) - int(bg.R)
							dg := int(px.G) - int(bg.G)
							db := int(px.B) - int(bg.B)
							if dr*dr+dg*dg+db*db > 900 {
								if inked < 0 {
									inked = y
								}
								count++
							}
						}
					}
				}
				if count == 0 {
					t.Fatalf("no line-number digits near the changed row at scroll %v", scroll.Y)
				}
				if inked < int(textY)-1 || inked > int(textY)+4 {
					t.Fatalf("line-number digits start at %v, row top %v, scroll %v", inked, textY, scroll.Y)
				}
			}
		})
	}
}

func expandAllTestDiffContext(a *App, d *filePage) {
	d.gitOpenFolds = make(map[int]bool)
	for _, row := range d.gitRows {
		if row.fold != nil {
			d.gitOpenFolds[row.fold.start] = true
		}
	}
	a.gitSetDiffText(d, d.gitReview.Patch, d.gitReview)
}
