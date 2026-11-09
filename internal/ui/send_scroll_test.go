package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/codex"
)

type scrollSendBackend struct{ stubBackend }

func (scrollSendBackend) Send(context.Context, string, string) (codex.Turn, error) {
	return codex.Turn{ID: "new-turn"}, nil
}

func TestSendKeepsComposerFocused(t *testing.T) {
	for _, button := range []bool{true, false} {
		t.Run(map[bool]string{true: "button", false: "enter"}[button], func(t *testing.T) {
			desktop := New(scrollSendBackend{}, "test", t.TempDir(), nil)
			desktop.State.ThreadID = "thread"
			updates := make(chan func(), 1)
			desktop.dispatch = func(update func()) { updates <- update }
			tester := native.NewTester(desktop.view, 640, 480)
			if err := tester.Click("任务输入"); err != nil {
				t.Fatal(err)
			}
			tester.Type("继续提问")
			if button {
				if err := tester.Click("发送"); err != nil {
					t.Fatal(err)
				}
			} else {
				tester.Key(0, native.KeyEnter)
			}
			tester.Frame()
			if !tester.Focused("任务输入") {
				t.Fatal("send moved focus away from composer")
			}
			tester.Type("下一句")
			tester.Frame()
			if desktop.State.Draft != "下一句" {
				t.Fatalf("draft=%q", desktop.State.Draft)
			}
		})
	}
}

func TestSendReturnsToLatestAndResumesFollowing(t *testing.T) {
	for _, hold := range []bool{false, true} {
		t.Run(map[bool]string{false: "reading", true: "disclosure"}[hold], func(t *testing.T) {
			desktop := New(scrollSendBackend{}, "test", t.TempDir(), nil)
			desktop.State.ThreadID = "thread"
			desktop.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
			desktop.State.Messages = []*app.Message{{ID: "history", Role: "assistant", Text: strings.Repeat("History paragraph.\n\n", 100), Done: true}}
			updates := make(chan func(), 1)
			desktop.dispatch = func(update func()) { updates <- update }
			tester := native.NewTester(desktop.view, 640, 480)
			tester.Scroll(300, 200, 0, -300)
			if desktop.list.AtEnd() {
				t.Fatal("scroll did not leave end")
			}
			desktop.processScrollHold = hold
			desktop.editScrollHold = 2
			desktop.State.Draft = "new question"
			desktop.send()
			select {
			case update := <-updates:
				update()
			case <-time.After(5 * time.Second):
				t.Fatal("send did not finish")
			}
			tester.Frame()
			if !desktop.list.AtEnd() || !desktop.list.FollowEnd || desktop.processScrollHold || desktop.editScrollHold != 0 {
				t.Fatal("sending did not restore bottom following")
			}
			reply := &app.Message{ID: "new-reply", Role: "assistant", Text: strings.Repeat("New paragraph.\n\n", 30)}
			desktop.State.Messages = append(desktop.State.Messages, reply)
			tester.Frame()
			if !desktop.list.AtEnd() {
				t.Fatal("new reply did not follow bottom")
			}
			tester.Scroll(300, 200, 0, -300)
			reply.Text += strings.Repeat("Streamed paragraph.\n\n", 10)
			tester.Frame()
			if desktop.list.AtEnd() {
				t.Fatal("stream interrupted history reading")
			}
		})
	}
}
