package ui

import (
	"fmt"
	"math"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
)

func TestDiffHorizontalEndRevealsWholeLine(t *testing.T) {
	for _, split := range []bool{false, true} {
		for _, text := range []string{
			strings.Repeat("中文宽字符", 100) + "END",
			strings.Repeat("\t中文 mixed ", 60) + "END    ",
			strings.Repeat("emoji 😀 全角Ｗ ", 60) + "END",
		} {
			t.Run(fmt.Sprintf("split=%v/%s", split, text[:6]), func(t *testing.T) {
				a, q, root := p4App(t)
				patch := "diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -1 +1 @@\n-" + text + "\n+" + text + " new\n"
				review := gitview.ParsePatch(patch)
				d := &filePage{id: "diff", root: root, path: root + "/file", kind: "diff", compare: "更改",
					gitDiffState: gitDiffState{gitSplit: split}}
				a.gitSetDiffText(d, patch, &review)
				a.files = map[string]*filePage{d.id: d}
				tt := native.NewTester(func(c *native.Context) {
					native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
				}, 1000, 600)
				p4Wait(t, tt, q, func() bool { return d.contentWidth > 0 && !d.highlighting })
				sides := []struct {
					label, line       string
					scroll            *native.ScrollState
					size, left, right float32
				}{{"文件内容 " + d.path, text + " new", &d.hscroll, 13, 58, 24}}
				if split {
					sides = []struct {
						label, line       string
						scroll            *native.ScrollState
						size, left, right float32
					}{
						{"差异 原版本", text, &d.gitLeftHScroll, 12, 58, 10},
						{"差异 新版本", text + " new", &d.gitRightHScroll, 12, 58, 10},
					}
				}
				for _, side := range sides {
					side.scroll.X = math.MaxFloat32
					tt.Frame()
					viewport, _ := tt.Find(side.label)
					actual, _ := native.ShapeText(side.line, native.Font{Family: codeFont, Size: side.size}, 0).Size()
					end := side.left + actual - side.scroll.X
					if side.scroll.X != side.scroll.MaxX || end > viewport.W-side.right+.5 {
						t.Fatalf("line end still clipped at maximum scroll: end=%v viewport=%v scroll=%v", end, viewport, *side.scroll)
					}
				}
			})
		}
	}
}

func TestDiffContentFillsViewportAndScrollsHorizontally(t *testing.T) {
	for _, split := range []bool{false, true} {
		for _, width := range []int{480, 1000, 1800} {
			for _, long := range []bool{false, true} {
				t.Run(fmt.Sprintf("split=%v/width=%d/long=%v", split, width, long), func(t *testing.T) {
					a, q, root := p4App(t)
					text := "short"
					if long {
						text = strings.Repeat("wide ", 100)
					}
					patch := "diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -1 +1 @@\n-old\n+" + text + "\n"
					review := gitview.ParsePatch(patch)
					d := &filePage{id: "diff", root: root, path: root + "/file", kind: "diff", compare: "更改",
						gitDiffState: gitDiffState{gitReview: &review, gitSplit: split}}
					a.gitSetDiffText(d, patch, &review)
					a.files = map[string]*filePage{d.id: d}
					tt := native.NewTester(func(c *native.Context) {
						native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
					}, width, 600)
					p4Wait(t, tt, q, func() bool { return d.contentWidth > 0 && !d.highlighting })
					label := "文件内容 " + d.path
					paneWidth := float32(width)
					scroll := &d.hscroll
					if split && width >= 520 {
						label = "差异 新版本"
						paneWidth = float32(width-1) / 2
						scroll = &d.gitRightHScroll
					}
					r, ok := tt.Find(label)
					if !ok || r.W < paneWidth-1 {
						t.Fatal("diff content does not fill viewport", r, paneWidth)
					}
					if !long && r.W > paneWidth+1 {
						t.Fatal("short diff creates unnecessary horizontal overflow", r, paneWidth)
					}
					if !long && scroll.MaxX != 0 {
						t.Fatal("short diff should not scroll horizontally", *scroll)
					}
					if long {
						tt.Scroll(float32(width)-40, r.Y+40, 180, 0)
						tt.Frame()
						if scroll.X <= 0 || scroll.MaxX <= 0 {
							t.Fatal("long diff did not scroll horizontally", *scroll)
						}
						// A visible thumb at the bottom must accept dragging.
						scroll.X = 0
						tt.Frame()
						tt.Move(float32(width)-paneWidth+15, r.Y+r.H-4)
						tt.Press(float32(width)-paneWidth+15, r.Y+r.H-4)
						tt.Move(float32(width)-30, r.Y+r.H-4)
						tt.Release(float32(width)-30, r.Y+r.H-4)
						tt.Frame()
						if scroll.X < scroll.MaxX/2 {
							t.Fatal("horizontal scrollbar thumb did not drag", *scroll)
						}
						shortPatch := strings.Replace(patch, text, "short", 1)
						shortReview := gitview.ParsePatch(shortPatch)
						a.gitSetDiffText(d, shortPatch, &shortReview)
						p4Wait(t, tt, q, func() bool { return d.contentWidth > 0 && !d.highlighting })
						if scroll.MaxX != 0 || scroll.X != 0 {
							t.Fatal("refreshing with short lines retained horizontal overflow", *scroll)
						}
					}
				})
			}
		}
	}
}
