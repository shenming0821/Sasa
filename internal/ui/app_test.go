package ui

import (
	"context"
	"fmt"
	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/codex"
	"strings"
	"testing"
)

type stubBackend struct{}

func (stubBackend) StartThread(context.Context, string) (codex.Thread, error) {
	panic("unexpected runtime call")
}

func TestReadingWhileMessageGrows(t *testing.T) {
	a := New(stubBackend{}, "test version", "/tmp/fuyu-component", nil)
	a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	msg := &app.Message{ID: "stream", Role: "assistant", Text: strings.Repeat("A garden paragraph.\n\n", 100)}
	a.State.Messages = []*app.Message{msg}
	a.State.Busy = true
	a.State.TurnID = "turn"
	tt := native.NewTester(a.view, 640, 480)
	tt.Scroll(300, 200, 0, -300)
	if a.list.AtEnd() {
		t.Fatal("scroll did not leave end")
	}
	for i := 0; i < 10; i++ {
		msg.Text += "Another streamed paragraph.\n\n"
		tt.Frame()
		if a.list.AtEnd() {
			t.Fatal("stream resumed following while reading")
		}
	}
	if err := tt.Click("任务输入"); err != nil {
		t.Fatal(err)
	}
	tt.Type("draft")
	if a.list.AtEnd() {
		t.Fatal("focusing composer resumed following")
	}
	if err := tt.Click("返回最新消息"); err != nil {
		t.Fatal(err)
	}
	if !a.list.AtEnd() {
		t.Fatal("return to latest did not reach end")
	}
}

func TestStreamListMarkersStayOnOneLine(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
	msg := &app.Message{ID: "list", Role: "assistant"}
	tt := native.NewTester(func(c *native.Context) { a.message(c, msg) }, 640, 4000)
	for i := 1; i <= 120; i++ {
		msg.Text += fmt.Sprintf("%d. Item\n", i)
		tt.Frame()
		marker, ok := tt.Find(fmt.Sprintf("%d.", i))
		body, found := tt.Find("Item")
		if !ok || !found {
			t.Fatalf("missing marker/body at %d", i)
		}
		if marker.H > body.H+1 {
			t.Fatalf("marker %d wraps: marker height %.2f, body height %.2f", i, marker.H, body.H)
		}
	}
}
func (stubBackend) Send(context.Context, string, string) (codex.Turn, error) {
	panic("unexpected runtime call")
}
func (stubBackend) Interrupt(context.Context, string, string) error { panic("unexpected runtime call") }
func (stubBackend) Events() <-chan codex.Event                      { return nil }
func TestComposerAndSendGates(t *testing.T) {
	a := New(stubBackend{}, "test version", "", nil)
	count := 0
	a.submit = func() { count++; a.State.Draft = "" }
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.Click("任务输入"); err != nil {
		t.Fatal(err)
	}
	tt.Type("第一行")
	tt.Key(native.Cmd, native.KeyEnter)
	tt.Type("第二行")
	if a.State.Draft != "第一行\n第二行" {
		t.Fatalf("draft=%q", a.State.Draft)
	}
	tt.Key(0, native.KeyEnter)
	if count != 0 {
		t.Fatal("sent without directory")
	}
	a.State.Cwd = "/tmp/fuyu-component"
	tt.Frame()
	tt.Key(native.Cmd, native.KeyA)
	tt.Type("甲乙")
	tt.Key(0, native.KeyLeft)
	tt.Key(native.Cmd, native.KeyEnter)
	tt.Type("中")
	if count != 0 || a.State.Draft != "甲\n中乙" {
		t.Fatalf("newline did not preserve caret: count=%d draft=%q", count, a.State.Draft)
	}
	tt.Key(native.Cmd, native.KeyA)
	tt.Key(native.Cmd, native.KeyEnter)
	if count != 0 || a.State.Draft != "\n" {
		t.Fatalf("newline did not replace selection: count=%d draft=%q", count, a.State.Draft)
	}
	tt.Type("任务")
	tt.Compose("输入法组合", 5)
	tt.Key(0, native.KeyEnter)
	if count != 0 {
		t.Fatal("sent while IME composition was active")
	}
	tt.Compose("", 0)
	tt.Key(0, native.KeyEnter)
	if count != 1 || a.State.Draft != "" {
		t.Fatalf("count=%d draft=%q", count, a.State.Draft)
	}
	a.State.Busy = true
	a.State.Draft = "wait"
	tt.Frame()
	tt.Key(0, native.KeyEnter)
	if count != 1 {
		t.Fatal("sent during active turn")
	}
	a.State.Busy = false
	a.State.Disconnected = true
	tt.Frame()
	tt.Key(0, native.KeyEnter)
	if count != 1 {
		t.Fatal("sent while disconnected")
	}
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		tt.SetSize(640, 480)
		if !tt.HasText("任务输入") {
			t.Fatal("composer missing")
		}
	}
}

func TestUnorderedListUsesCompactMarkerColumn(t *testing.T) {
	for _, width := range []int{320, 700} {
		for _, done := range []bool{false, true} {
			a := New(stubBackend{}, "test", "/tmp", nil)
			a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
			msg := &app.Message{ID: "bullets", Role: "assistant", Done: done,
				Text: "- First item\n  - Nested item\n- Second item\n"}
			tt := native.NewTester(func(c *native.Context) { a.message(c, msg) }, width, 480)
			first, ok := tt.Find("First item")
			nested, nestedOK := tt.Find("Nested item")
			second, secondOK := tt.Find("Second item")
			if !ok || !nestedOK || !secondOK || first.X > 22 || nested.X-first.X > 22 || second.X != first.X {
				t.Fatalf("wide or misaligned bullet columns: first=%+v nested=%+v second=%+v", first, nested, second)
			}
		}
	}
}
