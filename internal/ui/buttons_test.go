package ui

import (
	"fmt"
	"image/color"
	"math"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
)

func TestPanelTabsKeepSelectionAndInsetFace(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprintf("dark=%v", dark), func(t *testing.T) {
			active := "文件"
			focused := ""
			tt := native.NewTester(func(c *native.Context) {
				focused = ""
				native.Row(c).Fill().Background(c.Theme().Surface).Gap(4).Children(func() {
					for _, label := range []string{"文件", "会话历史", "Git"} {
						b := panelTabButton(c.Key(label), label, active == label)
						if b.FocusVisible() {
							focused = label
						}
						b.Children(func() { native.Icon(c, fileIcon).Size(18, 18) })
						if b.Clicked() {
							active = label
						}
					}
				})
			}, 160, 60)
			tt.SetDark(dark)
			tt.Advance(150 * time.Millisecond)
			r, _ := tt.Find("文件")
			sample := func(dx, dy float32) color.RGBA {
				return tt.Image().RGBAAt(int(r.X+dx), int(r.Y+dy))
			}
			selected := sample(5, 18)
			background := sample(1, 18)
			if selected == background || sample(18, 34) != background {
				t.Fatal("selection should have an inset face with no bottom underline")
			}
			tt.Move(150, 50)
			tt.Advance(150 * time.Millisecond)
			if sample(5, 18) != selected {
				t.Fatal("selection disappeared when the pointer left")
			}
			if err := tt.Click("会话历史"); err != nil {
				t.Fatal(err)
			}
			tt.Move(150, 50)
			tt.Advance(150 * time.Millisecond)
			if active != "会话历史" || sample(5, 18) != background {
				t.Fatal("switching tabs did not move the selection")
			}
			if after, _ := tt.Find("文件"); after != r {
				t.Fatal("tab interaction moved the hit target")
			}
			tt.Key(0, native.KeyTab)
			if focused != "Git" || active != "会话历史" {
				t.Fatal("keyboard focus should move independently of selection")
			}
			tt.Key(0, native.KeyEnter)
			if active != "Git" {
				t.Fatal("focused tab did not activate from the keyboard")
			}
		})
	}
}

func TestActionButtonFeedbackAndLateDisabled(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		t.Run(fmt.Sprintf("reduce=%v", reduce), func(t *testing.T) {
			disabled, clicks := false, 0
			tt := native.NewTester(func(c *native.Context) {
				native.Column(c).Fill().Background(c.Theme().Background).Padding(10).Children(func() {
					if actionButton(c, "保存").Disabled(disabled).Clicked() {
						clicks++
					}
				})
			}, 160, 80)
			tt.SetPreferences(native.Preferences{ReduceMotion: reduce, TextScale: 1})
			r, _ := tt.Find("保存")
			x, y := r.X+5, r.Y+r.H/2
			pixel := func() color.RGBA { return tt.Image().RGBAAt(int(x), int(y)) }
			idle := pixel()
			tt.Move(x, y)
			if !reduce && pixel() != idle {
				t.Fatal("hover skipped its color transition")
			}
			tt.Advance(150 * time.Millisecond)
			hover := pixel()
			if hover == idle {
				t.Fatal("outlined button has no hover feedback")
			}
			tt.Press(x, y)
			tt.Advance(150 * time.Millisecond)
			if pixel() == hover {
				t.Fatal("outlined button has no pressed feedback")
			}
			tt.Release(x, y)
			tt.Advance(150 * time.Millisecond)
			if clicks != 1 || pixel() != hover {
				t.Fatal("release did not restore hover or dispatch one click")
			}
			disabled = true
			tt.Frame()
			disabledPixel := pixel()
			tt.Move(150, 70)
			tt.Advance(150 * time.Millisecond)
			if pixel() != disabledPixel {
				t.Fatal("disabled button retained hover feedback")
			}
			tt.Press(x, y)
			tt.Release(x, y)
			tt.Advance(150 * time.Millisecond)
			if clicks != 1 || pixel() != disabledPixel {
				t.Fatal("disabled button reacted to a click")
			}
			if after, _ := tt.Find("保存"); after != r {
				t.Fatal("button states changed its geometry")
			}
		})
	}
}

func TestSearchClearButton(t *testing.T) {
	var surface, muted native.Color
	w := &workspacePage{query: "关键字"}
	tt := native.NewTester(func(c *native.Context) {
		th := c.Theme()
		surface, muted = th.Surface, th.TextMuted
		searchField(c).Children(func() {
			native.TextInputBase(c, &w.query).Grow(1).MinWidth(0)
			searchClearButton(c, w)
		})
	}, 240, 40)
	is := func(got color.RGBA, want native.Color) bool {
		return got.R == want.R && got.G == want.G && got.B == want.B
	}
	r, found := tt.Find("清除搜索内容")
	if !found {
		t.Fatal("clear button missing with a non-empty query")
	}
	cx, cy := int(r.X+r.W/2), int(r.Y+r.H/2)
	if got := tt.Image().RGBAAt(cx, cy); !is(got, surface) {
		t.Fatalf("the x hole should show the field face, got %v", got)
	}
	if got := tt.Image().RGBAAt(cx+3, cy); !is(got, muted) {
		t.Fatalf("the disc should paint the muted color, got %v", got)
	}
	if err := tt.Click("清除搜索内容"); err != nil {
		t.Fatal(err)
	}
	if w.query != "" || !w.searchFocus {
		t.Fatalf("click should clear the query and refocus, query=%q focus=%v", w.query, w.searchFocus)
	}
	if _, found := tt.Find("清除搜索内容"); found {
		t.Fatal("clear button should hide once the query is empty")
	}
}

func TestQuietIconButtonCentersGlyph(t *testing.T) {
	for _, sizes := range [][2]float32{{24, 16}, {28, 18}, {26, 16}, {30, 16}, {24, 13}} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/dark=%v", sizes, dark), func(t *testing.T) {
				tester := native.NewTester(func(c *native.Context) {
					quietIconButton(c, "按钮").Size(sizes[0], sizes[0]).Children(func() {
						native.Icon(c, photoIcon).Label("图标").Size(sizes[1], sizes[1])
					})
				}, 120, 100)
				tester.SetDark(dark)
				button, found := tester.Find("按钮")
				if !found {
					t.Fatal("button missing")
				}
				for _, hover := range []bool{false, true} {
					if hover {
						tester.Move(button.X+button.W/2, button.Y+button.H/2)
					}
					glyph, found := tester.Find("图标")
					if !found || glyph.W != sizes[1] || glyph.H != sizes[1] ||
						math.Abs(float64(glyph.X+glyph.W/2-button.X-button.W/2)) > .5 ||
						math.Abs(float64(glyph.Y+glyph.H/2-button.Y-button.H/2)) > .5 {
						t.Fatalf("icon not centered, hover=%v: button=%+v glyph=%+v", hover, button, glyph)
					}
				}
			})
		}
	}
}
