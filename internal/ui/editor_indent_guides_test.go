package ui

import (
	"image/color"
	"path/filepath"
	"testing"

	native "github.com/egoist/mygo/ui"
)

func TestEditorIndentGuides(t *testing.T) {
	for _, layout := range []struct {
		name, unit string
		spaces     int
	}{
		{"sample.vue", "  ", 2},
		{"sample.py", "    ", 4},
		{"sample.go", "\t", 0},
	} {
		t.Run(layout.name, func(t *testing.T) {
			a, queue, root := p4App(t)
			text := "{\n" + layout.unit + "{\n" + layout.unit + layout.unit + "x\n\n" + layout.unit + "}\n}\n"
			d := &filePage{
				id: layout.name, root: root, path: filepath.Join(root, layout.name),
				kind: "file", loaded: true, buffer: native.NewTextBuffer(text), savedText: text,
			}
			d.observed = d.buffer.Snapshot()
			a.files = map[string]*filePage{d.id: d}
			tester := native.NewTester(func(c *native.Context) {
				native.Row(c).Fill().Children(func() { a.fileView(c.Key(d.id), d, false, false) })
			}, 600, 360)
			p4Wait(t, tester, queue, func() bool { return !d.highlighting && d.contentWidth > 0 })
			if d.indentGuides.spaceCount != layout.spaces {
				t.Fatalf("detected indent width = %d, want %d", d.indentGuides.spaceCount, layout.spaces)
			}
			rect, ok := tester.Find("文件内容 " + d.path)
			if !ok {
				t.Fatal("missing editor surface")
			}
			font := native.Font{Family: a.codeFontFamily(), Size: 13}
			width := native.ShapeText(layout.unit+"x", font, 0).Caret(len(layout.unit)).X
			if width <= 0 {
				t.Fatalf("invalid indentation width %g", width)
			}
			x := int(rect.X + 58 + width + 0.5)
			pixel := func(row int) color.RGBA {
				return tester.Image().RGBAAt(x, int(rect.Y+8+d.lines.Top(row)+10))
			}
			moveCaret := func(line int) {
				_, end := d.buffer.Snapshot().LineRange(line)
				d.pendingSelection = &[2]int{end, end}
				tester.Frame()
				tester.Frame()
			}
			for _, dark := range []bool{false, true} {
				tester.SetDark(dark)
				moveCaret(5)
				plain, inactive := pixel(5), pixel(3)
				if inactive == plain || pixel(2) != inactive {
					t.Fatalf("dark=%t: guide should continue through a blank line, got %v / %v with background %v", dark, pixel(2), inactive, plain)
				}
				moveCaret(2)
				if active := pixel(3); active == inactive || active == plain || pixel(2) != active {
					t.Fatalf("dark=%t: active block should emphasize its guide, got %v / %v", dark, pixel(2), active)
				}
			}
		})
	}
}
