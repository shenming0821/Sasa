package ui

import (
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
)

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
