package ui

import (
	"fmt"
	"math"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
)

func TestConversationLayoutAndDetails(t *testing.T) {
	for _, size := range [][2]int{{640, 480}, {1000, 760}, {1600, 900}} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/dark=%v", size[0], size[1], dark), func(t *testing.T) {
				a := New(stubBackend{}, "test version", "/tmp/fuyu-layout", nil)
				a.State.Model, a.State.Policy, a.State.Sandbox = "provider/model", `"never"`, `{"type":"dangerFullAccess"}`
				a.State.Busy, a.State.TurnID = true, "turn"
				a.State.Messages = []*app.Message{
					{ID: "user", Role: "user", Text: "用户任务", Done: true},
					{ID: "reply", Role: "assistant", Text: "回复正文。\n\n```go\n" + strings.Repeat("x", 300) + "\n```", Done: true},
				}
				tt := native.NewTester(a.view, size[0], size[1])
				tt.SetDark(dark)
				composer, ok := tt.Find("任务输入")
				stop, visible := tt.Find("停止")
				if !ok || !visible || composer.H <= 0 || stop.Y < composer.Y ||
					stop.Y+stop.H > float32(size[1])-8 || stop.X+stop.W > float32(size[0]) {
					t.Fatalf("controls outside window: composer=%+v stop=%+v", composer, stop)
				}
				if composer.W > readingWidth || math.Abs(float64(composer.X+composer.W/2-float32(size[0])/2)) > 1 {
					t.Fatalf("composer not centered/bounded: %+v", composer)
				}
				if tt.HasText(a.version) || tt.HasText(a.State.Policy) {
					t.Fatal("runtime details visible before opening")
				}
				if err := tt.Click("运行详情"); err != nil {
					t.Fatal(err)
				}
				for _, value := range []string{a.version, a.State.Model, a.State.Policy, a.State.Sandbox} {
					r, ok := tt.Find(value)
					if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > float32(size[0]) || r.Y+r.H > float32(size[1]) {
						t.Fatalf("detail missing or clipped: %q %+v", value, r)
					}
				}
				tt.Key(0, native.KeyEscape)
				if tt.HasText(a.version) {
					t.Fatal("Escape did not dismiss details")
				}
				if err := tt.Click("复制回复"); err != nil {
					t.Fatal(err)
				}
				if tt.Clipboard() != a.State.Messages[1].Text || !tt.HasText("已复制") {
					t.Fatal("reply copy did not preserve Markdown")
				}
			})
		}
	}
}

func TestUserBubbleAlignment(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	a.State.Messages = []*app.Message{{ID: "user", Role: "user", Text: "简短任务", Done: true}}
	tt := native.NewTester(a.view, 1000, 760)
	bubble, ok := tt.Find("简短任务")
	if !ok || bubble.X <= 500 || bubble.X+bubble.W > 900.5 {
		t.Fatalf("user message not aligned to reading column's right: %+v", bubble)
	}
}

func TestFloatingReturnControlDoesNotMoveComposerOrBlockScroll(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := New(stubBackend{}, "test", "/tmp", nil)
		for i := 0; i < 60; i++ {
			a.State.Messages = append(a.State.Messages, &app.Message{
				ID: fmt.Sprint(i), Role: "user", Text: fmt.Sprintf("Message %d", i), Done: true,
			})
		}
		tt := native.NewTester(a.view, 640, 480)
		tt.SetDark(dark)
		before, _ := tt.Find("任务输入")
		tt.Scroll(200, 150, 0, -250)
		button, ok := tt.Find("返回最新消息")
		after, _ := tt.Find("任务输入")
		if !ok || before != after {
			t.Fatalf("return control shifted composer: before=%+v after=%+v", before, after)
		}
		// The input's inner padding is 16 DIPs above its text area.
		if after.Y-16-(button.Y+button.H) < 19 {
			t.Fatalf("return control touches composer: button=%+v input=%+v", button, after)
		}
		first, _ := a.list.Visible()
		tt.Scroll(80, button.Y+button.H/2, 0, -180)
		next, _ := a.list.Visible()
		if next >= first {
			t.Fatal("bottom fade intercepted scrolling")
		}
		if err := tt.Click("返回最新消息"); err != nil {
			t.Fatal(err)
		}
		if !a.list.AtEnd() || tt.HasText("返回最新消息") {
			t.Fatal("return control did not restore following")
		}
		final, _ := tt.Find("任务输入")
		if before != final {
			t.Fatal("dismissing floating control moved composer")
		}
	}
}
