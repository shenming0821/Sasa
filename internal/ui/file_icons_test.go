package ui

import (
	"testing"

	native "github.com/egoist/mygo/ui"
)

func TestPathFileIcon(t *testing.T) {
	muted := native.Hex("#808080")
	for _, test := range []struct {
		name        string
		glyph       *native.SVG
		light, dark native.Color
	}{
		{"src/MAIN.GO", fileCodeGlyph, native.Hex("#00809f"), native.Hex("#00add8")},
		{"nested/GO.SUM", fileCodeGlyph, native.Hex("#00809f"), native.Hex("#00add8")},
		{"pnpm-lock.yaml", fileJSONGlyph, native.Hex("#9a8a00"), native.Hex("#cbcb41")},
		{"LICENSE.md", fileTextGlyph, native.Hex("#a07d10"), native.Hex("#d4af37")},
		{"nested/.env.local", fileLockGlyph, native.Hex("#a86a0c"), native.Hex("#e8c482")},
		{".gitignore", fileSettingsGlyph, muted, muted},
		{"config.yml", fileSettingsGlyph, native.Hex("#8a5aa8"), native.Hex("#c9a0dc")},
		{"README.md", fileTextGlyph, native.Hex("#3d78ad"), native.Hex("#6ba4d8")},
		{"image.SVG", fileImageGlyph, native.Hex("#5f8a26"), native.Hex("#a6c56a")},
		{"start.sh", fileTerminalGlyph, native.Hex("#4f9a1f"), native.Hex("#89e051")},
		{"other.lock", fileLockGlyph, muted.Alpha(.7), muted.Alpha(.7)},
		{"unknown.bin", filePlainGlyph, muted, muted},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, dark := range []bool{false, true} {
				want := test.light
				if dark {
					want = test.dark
				}
				glyph, tint := pathFileIcon(&native.Theme{Dark: dark, TextMuted: muted}, test.name)
				if glyph != test.glyph || tint != want {
					t.Errorf("dark=%t: matching glyph=%t, tint=%v, want %v", dark, glyph == test.glyph, tint, want)
				}
			}
		})
	}
}

func TestQuickOpenRendersFileIconInBothAppearances(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	tester := native.NewTester(func(c *native.Context) {
		a.quickOpenRow(c, quickOpenItem{name: "main.go", path: "/project/src/main.go", rel: "src/main.go", parent: "src"}, false, quickOpenQuery{})
	}, 300, 40)
	for _, appearance := range []struct {
		dark bool
		tint native.Color
	}{
		{false, native.Hex("#00809f")},
		{true, native.Hex("#00add8")},
	} {
		tester.SetDark(appearance.dark)
		tester.Frame()
		row, exists := tester.Find("打开 src/main.go")
		if !exists {
			t.Fatal("quick open file row missing")
		}
		image := tester.Image()
		colored := 0
		for vertical := int(row.Y); vertical < int(row.Y+row.H); vertical++ {
			for horizontal := int(row.X); horizontal < int(row.X+24); horizontal++ {
				pixel := image.RGBAAt(horizontal, vertical)
				if pixel.R == appearance.tint.R && pixel.G == appearance.tint.G && pixel.B == appearance.tint.B {
					colored++
				}
			}
		}
		if colored == 0 {
			t.Errorf("dark=%t: quick open icon did not render with tint %v", appearance.dark, appearance.tint)
		}
	}
}
