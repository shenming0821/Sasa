package ui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shenming0821/Fuyu/internal/app"
)

func TestInlineCodeBackgroundsMeetWithoutOverlap(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, markedFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("dark=%v/markedFirst=%v", dark, markedFirst), func(t *testing.T) {
				tt := native.NewTester(func(c *native.Context) {
					th := c.Theme()
					plain, mark := th.Text.Alpha(.10), th.Accent.Alpha(.25)
					if markedFirst {
						plain, mark = mark, plain
					}
					native.Column(c).Fill().Padding(20).Background(th.Background).Children(func() {
						// Underscores leave the upper background free of glyph ink.
						// Equal advances place the join halfway through the text.
						native.RichText(c,
							native.Span{Text: "____", Size: 20, Background: plain, Radius: 5},
							native.Span{Text: "____", Size: 20, Background: mark, Radius: 5},
						).AlignSelf(native.Start).Label("joined backgrounds")
					})
				}, 240, 100)
				tt.SetDark(dark)
				tt.Frame()
				r, ok := tt.Find("joined backgrounds")
				if !ok {
					t.Fatal("missing background fixture")
				}
				img := tt.Image()
				x := int(r.X + r.W/2)
				rows := 0
				for y := int(r.Y) + 3; y < int(r.Y+r.H/2); y++ {
					left, right := img.RGBAAt(x-10, y), img.RGBAAt(x+10, y)
					if left == right {
						continue
					}
					rows++
					if got := img.RGBAAt(x-2, y); got != left {
						t.Fatalf("left background overlapped at (%d,%d): got %v want %v", x-2, y, got, left)
					}
					if got := img.RGBAAt(x+2, y); got != right {
						t.Fatalf("right background overlapped at (%d,%d): got %v want %v", x+2, y, got, right)
					}
				}
				if rows < 3 {
					t.Fatal("fixture did not draw enough background rows", rows, r)
				}
			})
		}
	}
}

func TestSearchInlineCodeBackgroundExamples(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, done := range []bool{false, true} {
			t.Run(fmt.Sprintf("dark=%v/done=%v", dark, done), func(t *testing.T) {
				a := New(stubBackend{}, "test", t.TempDir(), nil)
				a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
				a.find.open, a.find.query = true, "main"
				msg := &app.Message{ID: "code-background", Role: "assistant",
					Text: "`main →` 与 `origin/main`，以及 [`main.go`](docs/main.go)。", Done: done}
				a.session.projectedMessages = []*app.Message{msg}
				a.find.matches = sessionFindMatches(a.session.projectedMessages, "main")
				tt := native.NewTester(func(c *native.Context) {
					native.Column(c).Fill().Padding(20).Background(c.Theme().Background).Children(func() {
						a.messageMarkdown(c, msg)
					})
				}, 700, 120)
				tt.SetDark(dark)
				tt.Frame()
				for _, text := range []string{"main →", "origin/main", "main.go"} {
					if !tt.HasText(text) {
						t.Fatalf("search changed displayed code or link text: missing %q", text)
					}
				}
				if dir := os.Getenv("FUYU_INLINE_CODE_EVIDENCE"); dir != "" {
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					f, err := os.Create(filepath.Join(dir, fmt.Sprintf("dark-%v-done-%v.png", dark, done)))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					closeErr := f.Close()
					if err != nil || closeErr != nil {
						t.Fatal(err, closeErr)
					}
				}
			})
		}
	}
}
