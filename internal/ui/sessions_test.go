package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/codex"
)

type delayedBackend struct {
	stubBackend
	started chan string
	release chan struct{}
}

func (b *delayedBackend) StartThread(_ context.Context, cwd string) (codex.Thread, error) {
	b.started <- cwd
	<-b.release
	var thread codex.Thread
	thread.Thread.ID, thread.Cwd, thread.Model = "first", cwd, "test-model"
	return thread, nil
}

func (b *delayedBackend) Send(_ context.Context, threadID, text string) (codex.Turn, error) {
	if threadID != "first" || text != "first task" {
		return codex.Turn{}, fmt.Errorf("incorrect submission: %q %q", threadID, text)
	}
	return codex.Turn{ID: "first-turn"}, nil
}

func TestSwitchWhileStartingThreadKeepsSubmissionAndEffectiveSettings(t *testing.T) {
	b := &delayedBackend{started: make(chan string, 1), release: make(chan struct{})}
	a := New(b, "test", "/tmp/first", nil)
	defer close(a.shutdown)
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	first := a.session
	a.State.Draft = "first task"
	a.send()
	select {
	case cwd := <-b.started:
		if cwd != "/tmp/first" {
			t.Fatal(cwd)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("thread/start did not run")
	}
	a.newSession("/tmp/second")
	second := a.session
	second.State.Draft = "second draft"
	close(b.release)
	for i := 0; i < 2; i++ {
		select {
		case fn := <-queue:
			fn()
		case <-time.After(3 * time.Second):
			t.Fatal("submission did not finish")
		}
	}
	if first.State.ThreadID != "first" || first.State.TurnID != "first-turn" ||
		first.State.Cwd != "/tmp/first" || first.State.Model != "test-model" || !first.State.Busy {
		t.Fatalf("submission lost owner: %+v", first.State)
	}
	if a.session != second || second.State.ThreadID != "" || second.State.Draft != "second draft" || second.State.Cwd != "/tmp/second" {
		t.Fatalf("submission overwrote selection: %+v", second.State)
	}
}

func TestBackgroundEventsAndCallbacksKeepSessionOwner(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp/first", nil)
	first := a.session
	first.State.ThreadID = "first"
	first.State.Begin("first task")
	first.State.TurnID = "turn-first"
	owner := a.forSession(first)
	a.newSession("/tmp/second")
	second := a.session
	second.State.ThreadID = "second"
	second.State.Draft = "second draft"
	second.State.Begin("second task")
	second.State.TurnID, second.State.Draft = "turn-second", "second draft"
	a.applyBatch([]codex.Event{
		{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"threadId":"first","turnId":"turn-first","itemId":"reply","delta":"background reply"}`)},
		{ID: json.RawMessage(`42`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"threadId":"first","turnId":"turn-first","itemId":"command","command":"pwd","cwd":"/tmp/first"}`)},
	})
	if len(second.State.Messages) != 1 || second.State.Draft != "second draft" || !second.State.Busy {
		t.Fatalf("background event changed selected session: %+v", second.State)
	}
	if len(first.State.PendingInteractions()) != 1 || len(first.State.Messages) < 3 || !a.anyBusy() {
		t.Fatalf("background workflow lost: %+v", first.State)
	}
	// Simulate a rejected asynchronous start arriving after selection changed.
	owner.applySubmissionFailure(first.State.Sequence, "first task", &codex.RPCError{Message: "rejected"}, false)
	if first.State.Busy || first.State.Draft != "first task" || second.State.Error != "" {
		t.Fatal("callback changed the selected session instead of its owner")
	}
	a.applyBatch([]codex.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"first","turn":{"id":"turn-first","status":"completed"}}`)}})
	if !second.State.Busy || second.State.TurnID != "turn-second" {
		t.Fatal("another thread's completion stopped selected task")
	}
	a.applyBatch([]codex.Event{{Method: "fuyu/disconnected"}})
	for _, s := range a.sessions {
		if !s.State.Disconnected || s.State.Busy || len(s.State.PendingInteractions()) != 0 {
			t.Fatalf("shared connection loss not projected: %+v", s.State)
		}
	}
	a.newSession("/tmp/third")
	if !a.State.Disconnected {
		t.Fatal("new session bypassed disconnected transport")
	}
}

func TestSessionSwitchKeepsDraftReadingAndToolState(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp/first", nil)
	a.sidebarOpen = false
	first := a.session
	for i := 0; i < 80; i++ {
		first.State.Messages = append(first.State.Messages, &app.Message{
			ID: fmt.Sprint(i), Role: "assistant", Text: strings.Repeat("Reading paragraph. ", 10), Done: true,
		})
	}
	first.State.Messages[0].Tool = &app.Tool{Expanded: true}
	tt := native.NewTester(a.view, 1000, 760)
	tt.Scroll(300, 200, 0, -800)
	firstRow, _ := first.list.Visible()
	if first.list.AtEnd() {
		t.Fatal("failed to leave latest")
	}
	if err := tt.Click("任务输入"); err != nil {
		t.Fatal(err)
	}
	tt.Type("first draft")
	a.newSession("/tmp/second")
	second := a.session
	tt.Frame()
	if err := tt.Click("任务输入"); err != nil {
		t.Fatal(err)
	}
	tt.Type("second draft")
	a.selectSession(first)
	tt.Frame()
	row, _ := first.list.Visible()
	if row != firstRow || first.list.AtEnd() || first.State.Draft != "first draft" ||
		second.State.Draft != "second draft" || !first.State.Messages[0].Tool.Expanded {
		t.Fatalf("session state changed: row=%d want=%d first=%q second=%q", row, firstRow, first.State.Draft, second.State.Draft)
	}
	// The remounted editor must bind to the restored draft, not stale text.
	if err := tt.Click("任务输入"); err != nil {
		t.Fatal(err)
	}
	tt.Key(native.Cmd, native.KeyA)
	tt.Type("edited first")
	if first.State.Draft != "edited first" || second.State.Draft != "second draft" {
		t.Fatal("editor binding crossed sessions")
	}
}

func TestSidebarNewSessionWhileOtherTaskRuns(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp/first", nil)
	first := a.session
	first.State.Begin("working")
	first.State.ThreadID, first.State.TurnID = "first", "turn"
	tt := native.NewTester(a.view, 1000, 760)
	a.newSession("/tmp/first")
	tt.Frame()
	if a.session == first || !first.State.Busy || a.State.Cwd != "/tmp/first" || a.State.Busy {
		t.Fatal("new session interrupted previous task")
	}
	if err := tt.Click("切换会话 1"); err != nil {
		t.Fatal(err)
	}
	if a.session != first || !tt.HasText("停止") {
		t.Fatal("cannot return to background task")
	}
	if err := tt.Click("切换左侧边栏"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("切换会话 1") || !tt.HasText("任务输入") {
		t.Fatal("sidebar collapse lost composer")
	}
}
