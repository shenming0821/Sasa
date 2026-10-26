package ui

import (
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
)

func TestEditorClickRestartsVisibleCaretPhase(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		name := "string"
		if buffered {
			name = "buffer"
		}
		t.Run(name, func(t *testing.T) {
			value := "\n\n\n\n"
			buffer := native.NewTextBuffer(value)
			var anchor, caret int
			tt := native.NewTester(func(c *native.Context) {
				var area native.Element
				if buffered {
					area = native.TextAreaBufferBase(c, buffer)
				} else {
					area = native.TextAreaBase(c, &value)
				}
				area.Fill().Padding(10).FixedLineHeight(20).AutoFocus()
				anchor, caret = area.TextAnchorCaret()
			}, 200, 140)
			visible := func(line int) bool {
				img := tt.Image()
				background := img.RGBAAt(30, 10+line*20)
				for y := 10 + line*20; y < 30+line*20; y++ {
					for x := 10; x < 13; x++ {
						if img.RGBAAt(x, y) != background {
							return true
						}
					}
				}
				return false
			}
			tt.ClickAt(11, 20)
			for _, line := range []int{1, 3, 3, 0} {
				tt.Advance(600 * time.Millisecond)
				if visible(caret) {
					t.Fatal("fixture did not reach the hidden blink phase")
				}
				tt.ClickAt(11, float32(20+line*20))
				if anchor != line || caret != line {
					t.Fatalf("click selected %d:%d, want line %d", anchor, caret, line)
				}
				if !visible(line) {
					t.Fatalf("caret stayed hidden immediately after clicking line %d", line)
				}
				tt.Advance(500 * time.Millisecond)
				if !visible(line) {
					t.Fatal("caret hid before its new visible phase elapsed")
				}
				tt.Advance(40 * time.Millisecond)
				if visible(line) {
					t.Fatal("caret stopped blinking after a click")
				}
				// Start another visible phase before the next hidden-phase click.
				tt.ClickAt(11, float32(20+line*20))
			}
		})
	}
}
