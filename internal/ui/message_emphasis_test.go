package ui

import (
	"fmt"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shenming0821/Fuyu/internal/app"
)

func TestMessageStrongTextUsesThemeRed(t *testing.T) {
	for _, source := range []string{
		"**重要信息**\n\n普通正文\n\n`inlineCode`\n\n[文件链接](docs/design.md)",
		"- **重要信息**\n- 普通正文",
		"| 业务能力 | 操作 |\n| --- | --- |\n| **重要信息** | 普通正文 |",
	} {
		for _, done := range []bool{false, true} {
			t.Run(fmt.Sprintf("done=%v/%s", done, source), func(t *testing.T) {
				desktop := New(stubBackend{}, "test", t.TempDir(), nil)
				desktop.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
				message := &app.Message{ID: "emphasis", Role: "assistant", Text: source, Done: done}
				var theme native.Theme
				tester := native.NewTester(func(c *native.Context) {
					theme = *c.Theme()
					native.Column(c).Fill().Background(theme.Background).Children(func() {
						desktop.messageMarkdown(c, message)
					})
				}, 700, 300)
				for _, dark := range []bool{false, true, false} {
					tester.SetDark(dark)
					tester.Frame()
					for _, label := range []string{"重要信息", "普通正文", "inlineCode", "文件链接"} {
						bounds, found := tester.Find(label)
						if !found {
							if label == "inlineCode" || label == "文件链接" {
								continue
							}
							t.Fatalf("missing text %q", label)
						}
						redPixels := 0
						image := tester.Image()
						for pixelY := int(bounds.Y); pixelY < int(bounds.Y+bounds.H); pixelY++ {
							for pixelX := int(bounds.X); pixelX < int(bounds.X+bounds.W); pixelX++ {
								pixel := image.RGBAAt(pixelX, pixelY)
								if int(pixel.R) > int(pixel.G)+50 && int(pixel.R) > int(pixel.B)+50 {
									redPixels++
								}
							}
						}
						if label == "重要信息" && redPixels < 20 {
							t.Fatalf("strong text is not red: dark=%v pixels=%d", dark, redPixels)
						}
						if label != "重要信息" && redPixels != 0 {
							t.Fatalf("ordinary text became red: label=%q dark=%v pixels=%d", label, dark, redPixels)
						}
					}
				}
			})
		}
	}
}
