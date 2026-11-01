package ui

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
)

func TestTimingText(t *testing.T) {
	now := time.Now()
	for _, example := range []struct {
		elapsed time.Duration
		text    string
	}{{0, "0秒"}, {59 * time.Second, "59秒"}, {309 * time.Second, "5分钟 9秒"}, {3661 * time.Second, "1小时 1分钟 1秒"}, {-time.Second, "0秒"}} {
		timing := &app.TurnTiming{Started: now.Add(-example.elapsed)}
		if actual := timingText(timing, now); actual != "已处理 "+example.text {
			t.Fatal(actual)
		}
		timing.Finished = now
		if actual := timingText(timing, now.Add(time.Hour)); actual != "用时 "+example.text {
			t.Fatal(actual)
		}
	}
}

func TestTimingVisibleThroughStreamingAndCompletion(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			desktop := New(stubBackend{}, "test", "/tmp", nil)
			desktop.State.Begin("task")
			desktop.State.Messages[0].Timing.Started = time.Now().Add(-224 * time.Second)
			if delay := desktop.maintenanceDelay(time.Now()); delay != time.Second {
				t.Fatalf("running timer does not refresh each second: %s", delay)
			}
			tester := native.NewTester(desktop.view, 1000, 760)
			tester.SetDark(dark)
			tester.Frame()
			if !tester.HasText("已处理 3分钟 44秒") {
				t.Fatal("initial waiting time is missing")
			}
			desktop.State.Apply(agent.Event{Method: "turn/started", Params: json.RawMessage(`{"turn":{"id":"turn"}}`)})
			desktop.State.Apply(agent.Event{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"turnId":"turn","itemId":"reply","delta":"回复正文"}`)})
			tester.Frame()
			if !tester.HasText("已处理 3分钟 44秒") || tester.HasText("正在思考") {
				t.Fatal("streaming lost the timing footer")
			}
			desktop.State.Apply(agent.Event{Method: "turn/completed", Params: json.RawMessage(`{"turn":{"id":"turn","status":"completed"}}`)})
			desktop.State.Messages[0].Timing.Finished = desktop.State.Messages[0].Timing.Started.Add(309 * time.Second)
			tester.Frame()
			if !tester.HasText("用时 5分钟 9秒") || tester.HasText("已处理 3分钟 44秒") {
				t.Fatal("completed duration did not replace the running clock")
			}
			if delay := desktop.maintenanceDelay(time.Now()); delay != 30*time.Second {
				t.Fatalf("idle clock still polls: %s", delay)
			}
		})
	}
}

func TestTimingPersistsAndRestoresNativeHistory(t *testing.T) {
	desktop := savingApp(t)
	desktop.State.ThreadID = "thread"
	desktop.State.Begin("task")
	desktop.State.Apply(agent.Event{Method: "turn/completed", Params: json.RawMessage(`{"turn":{"id":"turn","status":"completed"}}`)})
	want := *desktop.State.Messages[0].Timing
	if err := desktop.persist(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := desktop.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	state, _, err := app.History("/tmp", "thread", []agent.HistoryTurn{{ID: "turn", Status: "completed", Items: []json.RawMessage{json.RawMessage(`{"id":"native-user","type":"userMessage","content":[{"type":"text","text":"task"}]}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	state.RestoreTimings(snapshot.Sessions[0].TurnTimings)
	timing := state.Messages[0].Timing
	if timing == nil || !timing.Started.Equal(want.Started) || !timing.Finished.Equal(want.Finished) {
		t.Fatal("native message ID replacement lost persisted duration")
	}
}

func TestTimingGroupsFinalRepliesAndKeepsSessionsSeparate(t *testing.T) {
	desktop := New(stubBackend{}, "test", "/tmp", nil)
	first := &app.TurnTiming{Started: time.Now().Add(-time.Minute), Finished: time.Now()}
	second := &app.TurnTiming{Started: time.Now()}
	desktop.State.Busy = true
	desktop.State.Messages = []*app.Message{
		{ID: "user-one", Role: "user", Timing: first},
		{ID: "progress", Role: "assistant", Text: "处理中"},
		{ID: "final", Role: "assistant", Text: "完成"},
		{ID: "user-two", Role: "user", Timing: second},
		{ID: "stream", Role: "assistant", Text: "正在回复"},
	}
	completed, active := desktop.conversationTimings(desktop.conversationRows())
	if len(completed) != 1 || completed[2] != first || active != second {
		t.Fatal("duration is not above the final reply or running footer lost ownership")
	}
	other := New(stubBackend{}, "test", "/elsewhere", nil)
	completed, active = other.conversationTimings(other.conversationRows())
	if len(completed) != 0 || active != nil || other.maintenanceDelay(time.Now()) != 30*time.Second {
		t.Fatal("inactive session inherited another session's clock")
	}
}
