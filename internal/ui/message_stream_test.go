package ui

import (
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
)

func TestConversationFollowsLargeStreamingReply(t *testing.T) {
	desktop := New(stubBackend{}, "test", t.TempDir(), nil)
	message := &app.Message{ID: "streaming", Role: "assistant", Text: "开始输出。"}
	desktop.State.Messages = []*app.Message{{ID: "question", Role: "user", Text: "查看结果"}, message}
	tester := native.NewTester(desktop.view, 640, 480)
	checkEnd := func(stage string) {
		t.Helper()
		if !desktop.list.AtEnd() || desktop.scroll.MaxY-desktop.scroll.Y > 0.5 {
			t.Fatalf("%s: scroll %g / %g, at end %v", stage, desktop.scroll.Y, desktop.scroll.MaxY, desktop.list.AtEnd())
		}
	}
	for _, chunk := range []string{
		strings.Repeat("\n\n这是一段会换行的长正文，用来检查连续输出期间的滚动位置。", 36),
		"\n\n```text\n" + strings.Repeat("新增工具输出和执行状态\n", 60),
		"```\n\n" + strings.Repeat("继续输出新的 Markdown 正文。\n\n", 40),
	} {
		message.Text += chunk
		tester.Frame()
		checkEnd("append")
		for range 5 {
			tester.Advance(100 * time.Millisecond)
			checkEnd("reveal")
		}
	}
	tester.Scroll(300, 200, 0, -300)
	before := desktop.scroll.Y
	message.Text += "\n\n" + strings.Repeat("继续输出。\n\n", 20)
	tester.Frame()
	if desktop.list.AtEnd() || desktop.scroll.Y != before {
		t.Fatalf("stream moved the reading position: %g -> %g", before, desktop.scroll.Y)
	}
	tester.Scroll(300, 200, 0, 1e6)
	checkEnd("manual return")
	tester.SetSize(640, 180)
	message.Text += "\n\n窗口变小时继续输出。"
	tester.Frame()
	checkEnd("small viewport")
	tester.SetSize(640, 480)
	message.Done = true
	tester.Frame()
	checkEnd("completed")
}

func TestConversationResumesFollowingAfterScrollHold(t *testing.T) {
	for _, scrollUp := range []bool{false, true} {
		name := "at bottom"
		if scrollUp {
			name = "reading history"
		}
		t.Run(name, func(t *testing.T) {
			desktop := New(stubBackend{}, "test", t.TempDir(), nil)
			message := &app.Message{ID: "streaming", Role: "assistant", Text: strings.Repeat("已输出的正文。\n\n", 50), Done: true}
			desktop.State.Messages = []*app.Message{message}
			appendOnResume := false
			tester := native.NewTester(func(c *native.Context) {
				if appendOnResume && desktop.editScrollHold == 0 && !desktop.list.FollowEnd {
					appendOnResume = false
					message.Text += strings.Repeat("新收到的大段正文。\n\n", 40)
				}
				desktop.view(c)
			}, 640, 480)
			if scrollUp {
				tester.Scroll(300, 200, 0, -300)
			}
			before := desktop.scroll.Y
			desktop.editScrollHold = 1
			appendOnResume = true
			tester.Frame()
			tester.Frame()
			if appendOnResume {
				t.Fatal("did not append output when following resumed")
			}
			if scrollUp {
				if desktop.list.AtEnd() || desktop.scroll.Y != before {
					t.Fatalf("stream moved the reading position: %g -> %g", before, desktop.scroll.Y)
				}
			} else if !desktop.list.AtEnd() || desktop.scroll.MaxY-desktop.scroll.Y > 0.5 {
				t.Fatalf("stream left the bottom: %g / %g", desktop.scroll.Y, desktop.scroll.MaxY)
			}
		})
	}
}

// The streaming reveal lives on the element that mounted it: the first
// view of an unfinished message paces its text and keeps doing so, while
// any later view of it shows the text as it is instead of typing again.
func TestStreamRevealPacesOnlyTheFirstView(t *testing.T) {
	s := &session{}
	message := &app.Message{ID: "streaming", Role: "assistant", Text: "正文"}
	var first, remount bool
	if !s.streamReveal(message, &first) || !first {
		t.Fatal("first mounted view did not stream")
	}
	if !s.streamedMessages[message.ID] {
		t.Fatal("first mount did not record the presented message")
	}
	if !s.streamReveal(message, &first) {
		t.Fatal("the same view stopped streaming")
	}
	if s.streamReveal(message, &remount) {
		t.Fatal("a rebuilt view retyped the presented text")
	}
	if !s.streamReveal(&app.Message{ID: "next", Role: "assistant", Text: "下一条"}, &remount) {
		t.Fatal("a different first view did not stream")
	}
	message.Done = true
	if s.streamReveal(message, &first) {
		t.Fatal("a completed message still streamed")
	}
}

// A row that unmounts and comes back still shows its whole text.
func TestRemountedStreamShowsCompletedText(t *testing.T) {
	desktop := New(stubBackend{}, "test", t.TempDir(), nil)
	message := &app.Message{ID: "streaming", Role: "assistant", Text: strings.Repeat("正在输出的正文内容。", 60) + "结尾唯一标记"}
	mounted := true
	tester := native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Children(func() {
			if mounted {
				desktop.messageMarkdown(c, message)
			}
		})
	}, 700, 400)
	mounted = false
	tester.Frame()
	mounted = true
	tester.Frame()
	if !tester.HasText("结尾唯一标记") {
		t.Fatal("remounted view lost the shown text")
	}
}
