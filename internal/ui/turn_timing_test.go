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
				t.Fatal("streaming lost the timing below the user question")
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

func TestTimingFollowsUserQuestionAndKeepsSessionsSeparate(t *testing.T) {
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
	timings := desktop.conversationTimings(desktop.conversationRows())
	if len(timings) != 2 || timings[0] != first || timings[2] != second {
		t.Fatal("completed and running durations are not attached to their user questions")
	}
	desktop.State.Busy = false
	if timings := desktop.conversationTimings(desktop.conversationRows()); len(timings) != 1 || timings[0] != first {
		t.Fatal("idle session displayed an unconfirmed running clock")
	}
	other := New(stubBackend{}, "test", "/elsewhere", nil)
	if len(other.conversationTimings(other.conversationRows())) != 0 || other.maintenanceDelay(time.Now()) != 30*time.Second {
		t.Fatal("inactive session inherited another session's clock")
	}
}

func TestTimingLayoutBelowQuestionBeforeCommands(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{640, 1000} {
			for _, withReply := range []bool{false, true} {
				t.Run(fmt.Sprintf("dark=%v/width=%d/reply=%v", dark, width, withReply), func(t *testing.T) {
					desktop := New(stubBackend{}, "test", "/tmp", nil)
					desktop.sidebarOpen = false
					started := time.Now().Add(-52 * time.Second)
					desktop.State.Messages = []*app.Message{
						{ID: "question", Role: "user", Text: "提交并推送", TurnID: "turn", Timing: &app.TurnTiming{Started: started, Finished: started.Add(52 * time.Second)}},
						commandMessage("first-command", "completed"),
						{ID: "read", Role: "tool", TurnID: "turn", Tool: &app.Tool{Kind: "mcpToolCall", Name: "Read", Status: "completed"}},
						commandMessage("last-command", "completed"),
					}
					if withReply {
						desktop.State.Messages = append(desktop.State.Messages, &app.Message{ID: "reply", Role: "assistant", Text: "已提交并推送。", TurnID: "turn", Done: true})
					}
					tester := native.NewTester(desktop.view, width, 760)
					tester.SetDark(dark)
					tester.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
					var runningTiming, runningCommand native.Rect
					for _, running := range []bool{true, false} {
						desktop.State.Busy = running
						label := "第 1 轮 · 用时 52秒"
						if running {
							desktop.State.Messages[0].Timing.Finished = time.Time{}
							label = "第 1 轮 · 已处理 52秒"
						} else {
							desktop.State.Messages[0].Timing.Finished = started.Add(52 * time.Second)
						}
						tester.Frame()
						if !running {
							if tester.HasText("命令组 first-command") || tester.HasText("命令组 last-command") {
								t.Fatal("completion did not automatically collapse process records")
							}
							if err := tester.Click("展开执行过程 question"); err != nil {
								t.Fatal(err)
							}
							tester.Frame()
						}
						question, questionVisible := tester.Find("提交并推送")
						timing, timingVisible := tester.Find(label)
						firstCommand, firstVisible := tester.Find("命令组 first-command")
						if !questionVisible || !timingVisible || !firstVisible || tester.HasText("命令组 last-command") {
							t.Fatalf("missing question/timing/commands: %v", tester.Texts())
						}
						if timing.Y < question.Y+question.H || timing.Y+timing.H > firstCommand.Y {
							t.Fatalf("want question, duration, commands; got question=%+v timing=%+v first=%+v", question, timing, firstCommand)
						}
						if gap := firstCommand.Y - timing.Y - timing.H; gap > 24 {
							t.Fatalf("duration divider leaves too much space before the next record: %g", gap)
						}
						count := 0
						for _, text := range tester.Texts() {
							if text == label {
								count++
							}
						}
						if count != 1 {
							t.Fatalf("duration rendered %d times", count)
						}
						if running {
							runningTiming, runningCommand = timing, firstCommand
						} else if timing.Y != runningTiming.Y || firstCommand.Y != runningCommand.Y {
							t.Fatalf("completion moved duration or commands: running=%+v/%+v completed=%+v/%+v", runningTiming, runningCommand, timing, firstCommand)
						}
					}
				})
			}
		}
	}
}
