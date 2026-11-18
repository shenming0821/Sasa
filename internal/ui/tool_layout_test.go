package ui

import (
	"fmt"
	"image"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
)

func TestLongToolCommandDoesNotPaintOverFollowingMessages(t *testing.T) {
	for _, tc := range []struct {
		width int
		dark  bool
	}{{320, false}, {800, false}, {320, true}, {800, true}} {
		width := tc.width
		t.Run(fmt.Sprintf("width=%d/dark=%v", width, tc.dark), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/tmp", nil)
			tool := &app.Tool{Kind: "commandExecution", Status: "completed", Command: strings.Repeat("print('a long command 中文内容 with wrapped lines')\n", 90), Cwd: "/tmp/project"}
			tt := native.NewTester(func(c *native.Context) {
				native.Column(c).Fill().Gap(12).Background(c.Theme().Background).Children(func() {
					a.toolCard(c, tool)
					native.Text(c, "following message").Label("following message")
				})
			}, width, 720)
			tt.SetDark(tc.dark)
			marker, ok := tt.Find("following message")
			if !ok || marker.Y > 450 {
				t.Fatalf("collapsed command consumed following message: %+v", marker)
			}
			assertBlankBelow(t, tt.Image(), int(marker.Y+marker.H)+10)
			preview, ok := tt.Find("命令预览")
			cwd, cwdOK := tt.Find("工作目录")
			if !ok || !cwdOK || preview.Y+preview.H > cwd.Y {
				t.Fatalf("command overlaps metadata: preview=%+v cwd=%+v", preview, cwd)
			}
			assertNoTextInGap(t, tt.Image(), int(preview.Y+preview.H)+2, int(cwd.Y)-2)
			if err := tt.Click("展开详情"); err != nil {
				t.Fatal(err)
			}
			if !tool.Expanded || !tt.HasText("完整命令") || !tt.HasText("收起详情") {
				t.Fatal("command-only card cannot reveal complete command")
			}
			details, ok := tt.Find("工具详情")
			if !ok || details.H > 180 {
				t.Fatalf("details exceeded scroll viewport: %+v", details)
			}
			marker, _ = tt.Find("following message")
			assertBlankBelow(t, tt.Image(), int(marker.Y+marker.H)+10)
			tt.Scroll(details.X+details.W/2, details.Y+details.H/2, 0, -400)
			marker, _ = tt.Find("following message")
			assertBlankBelow(t, tt.Image(), int(marker.Y+marker.H)+10)
			if err := tt.Click("收起详情"); err != nil {
				t.Fatal(err)
			}
			if tool.Expanded {
				t.Fatal("details did not collapse")
			}
			tool.Command += strings.Repeat("streaming update 中文\n", 30)
			tt.Frame()
			marker, _ = tt.Find("following message")
			assertBlankBelow(t, tt.Image(), int(marker.Y+marker.H)+10)
			tt.SetSize(480, 720)
			marker, _ = tt.Find("following message")
			assertBlankBelow(t, tt.Image(), int(marker.Y+marker.H)+10)
		})
	}
}

func assertBlankBelow(t *testing.T, img *image.RGBA, top int) {
	t.Helper()
	background := img.RGBAAt(5, img.Bounds().Max.Y-5)
	for y := top; y < img.Bounds().Max.Y-10; y++ {
		for x := 10; x < img.Bounds().Max.X-10; x++ {
			color := img.RGBAAt(x, y)
			if color != background {
				t.Fatalf("text painted outside card below following message at (%d,%d): %v", x, y, color)
			}
		}
	}
}

func assertNoTextInGap(t *testing.T, img *image.RGBA, top, bottom int) {
	t.Helper()
	for y := top; y < bottom; y++ {
		background := img.RGBAAt(img.Bounds().Max.X-8, y)
		for x := 20; x < img.Bounds().Max.X-20; x++ {
			color := img.RGBAAt(x, y)
			difference := int(color.R) - int(background.R)
			if difference > 20 || difference < -20 {
				t.Fatalf("command painted into metadata gap at (%d,%d)", x, y)
			}
		}
	}
}
