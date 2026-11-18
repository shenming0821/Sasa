package ui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/shenming0821/Sasa/internal/app"
	"github.com/shenming0821/Sasa/internal/storage"
)

func performanceMessages(count int) []*app.Message {
	messages := make([]*app.Message, count)
	for index := range messages {
		messages[index] = &app.Message{ID: fmt.Sprint(index), Role: "assistant", Text: "A completed response", Done: true}
	}
	return messages
}

func BenchmarkConversationProjection(b *testing.B) {
	a := New(nil, "test", "/tmp", nil)
	a.State.Messages = performanceMessages(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		a.conversationRows()
		a.displayMessages()
	}
}

func BenchmarkStreamingUIUpdate(b *testing.B) {
	store, err := storage.Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { store.Close() })
	a := New(nil, "test", "/tmp", nil)
	a.store = store
	a.State.Messages = performanceMessages(1000)
	a.dispatch = func(fn func()) { fn() }
	if err := a.persist(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		a.update(func() { a.State.Messages[999].Text = "Streaming output" })
	}
}

func TestConversationProjectionReusesTextUpdatesAndRegroupsStructuralChanges(t *testing.T) {
	application := New(nil, "test", "/tmp", nil)
	first, second := commandMessage("first", "completed"), commandMessage("second", "completed")
	first.TurnID, second.TurnID = "turn", "turn"
	application.State.Messages = []*app.Message{first, second, {ID: "response", Role: "assistant"}}
	rows := application.conversationRows()
	if len(rows) != 1 || len(rows[0].commands) != 2 {
		t.Fatal("initial grouping failed")
	}
	second.Text = "more command output"
	if reused := application.conversationRows(); &rows[0] != &reused[0] || reused[0].commands[1].Text != second.Text {
		t.Fatal("text delta rebuilt rows or lost new content")
	}
	application.State.Messages[2].Text = "first response delta"
	if len(application.conversationRows()) != 2 {
		t.Fatal("empty assistant becoming visible did not invalidate projection")
	}
	second.Tool.ParentToolID = "child"
	if len(application.conversationRows()) != 3 {
		t.Fatal("parent tool change did not split command groups")
	}
	application.State.Messages = slices.Clone(application.State.Messages[1:])
	if messages := application.displayMessages(); len(messages) != 2 || messages[0] != second {
		t.Fatal("history trim kept stale anchors")
	}
	application.State.Messages = nil
	if len(application.displayMessages()) != 0 {
		t.Fatal("cleared conversation retained cached rows")
	}
}
