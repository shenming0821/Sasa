package ui

import (
	"math"
	"reflect"
	"runtime"
	"testing"

	native "github.com/egoist/mygo/ui"
)

func TestBundledLatinFontAndChineseFallback(t *testing.T) {
	registered := false
	for _, family := range native.FontFamilies() {
		if family.Name == latinFont && family.Registered {
			registered = true
		}
	}
	if !registered {
		t.Fatal("bundled Latin font is unavailable")
	}
	for _, family := range []string{textFont, codeFont} {
		glyphs := native.Shape("iW01", native.Font{Family: family, Size: 15})
		if len(glyphs) != 4 {
			t.Fatal("Latin glyphs missing", glyphs)
		}
		for _, g := range glyphs {
			if math.Abs(float64(g.Advance-glyphs[0].Advance)) > 0.01 {
				t.Fatal("Latin characters did not use the monospaced font", glyphs)
			}
		}
		chinese := native.Shape("中文显示保持原样", native.Font{Family: family, Size: 15})
		original := native.Shape("中文显示保持原样", native.Font{Size: 15})
		if len(chinese) != len(original) || len(chinese) == 0 {
			t.Fatal("Chinese glyphs missing")
		}
		if runtime.GOOS == "darwin" {
			for i, g := range chinese {
				// Glyph IDs distinguish the existing UI face from regular PingFang SC.
				if reflect.ValueOf(g).FieldByName("id").Uint() != reflect.ValueOf(original[i]).FieldByName("id").Uint() {
					t.Fatalf("Chinese UI glyph changed at %d with %s", i, family)
				}
			}
		}
	}
}

func TestDefaultFontThemePreservesColors(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tt := native.NewTester(func(c *native.Context) {
			before := *c.Theme()
			applyFontTheme(c)
			after := *c.Theme()
			if after.Font != textFont {
				t.Fatal("default font did not reach the window theme")
			}
			after.Font = before.Font
			if after != before {
				t.Fatal("font setup changed theme colors or metrics")
			}
		}, 640, 480)
		tt.SetDark(dark)
	}
}
