package ui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/attachment"
	"github.com/shenming0821/Fuyu/internal/storage"
)

type followupCall struct{ thread, turn, id, text string }

type followupBackend struct {
	stubBackend
	calls chan followupCall
	reply chan error
}

func (backend *followupBackend) Steer(ctx context.Context, thread, turn, id, text string, _ []agent.Skill, _ []attachment.Image) error {
	backend.calls <- followupCall{thread, turn, id, text}
	select {
	case err := <-backend.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func activeFollowupApp(backend Backend) *App {
	desktop := New(backend, "test", "/project", nil)
	desktop.State.ThreadID = "thread"
	desktop.State.Begin("original")
	desktop.State.TurnID = "turn"
	desktop.State.Messages[0].TurnID = "turn"
	desktop.State.Messages[0].Delivery = "sent"
	desktop.State.Messages[0].NativeID = "original"
	return desktop
}

func receiveFollowup(t *testing.T, calls <-chan followupCall) followupCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(3 * time.Second):
		t.Fatal("follow-up was not sent")
		return followupCall{}
	}
}

func TestFollowupsKeepTurnAndOwnerAndAllowContinuousTyping(t *testing.T) {
	backend := &followupBackend{calls: make(chan followupCall, 4), reply: make(chan error, 4)}
	desktop := activeFollowupApp(backend)
	updates := make(chan func(), 8)
	desktop.dispatch = func(update func()) { updates <- update }
	desktop.State.Draft = "first"
	desktop.send()
	first := receiveFollowup(t, backend.calls)
	firstMessage := desktop.State.Messages[1]
	if firstMessage.ID != first.id || firstMessage.Text != "first" || firstMessage.NativeID != "" {
		t.Fatal("submitted input did not appear immediately")
	}
	desktop.State.Draft = "second"
	desktop.send()
	if desktop.State.Draft != "" || len(desktop.pendingInputs) != 2 || desktop.State.Sequence != 1 || !desktop.State.Busy {
		t.Fatal("follow-up changed task identity or blocked continuous input")
	}
	owner := desktop.session
	desktop.newSession("/other")
	desktop.State.Draft = "other draft"
	backend.reply <- nil
	pumpUpdate(t, updates)
	second := receiveFollowup(t, backend.calls)
	desktop.forSession(owner).applySessionBatch([]agent.Event{{Method: "item/completed", Params: json.RawMessage(`{"threadId":"thread","turnId":"turn","item":{"id":"native-first","type":"userMessage","content":[{"type":"text","text":"first"}]}}`)}})
	backend.reply <- nil
	pumpUpdate(t, updates)
	desktop.forSession(owner).applySessionBatch([]agent.Event{{Method: "item/completed", Params: json.RawMessage(`{"threadId":"thread","turnId":"turn","item":{"id":"native-second","type":"userMessage","content":[{"type":"text","text":"second"}]}}`)}})
	if first.thread != "thread" || first.turn != "turn" || first.text != "first" || second.turn != "turn" || second.text != "second" || first.id == second.id {
		t.Fatal("follow-ups lost FIFO order or native turn identity", first, second)
	}
	if desktop.State.Draft != "other draft" || len(owner.pendingInputs) != 0 || len(owner.State.Messages) != 3 {
		t.Fatal("asynchronous result modified the selected session or lost messages")
	}
	if owner.State.Messages[1].Delivery != "appended" || !owner.State.Messages[0].Timing.Finished.IsZero() {
		t.Fatal("follow-up was not acknowledged or ended original timing")
	}
	if owner.State.Messages[1] != firstMessage || firstMessage.ID != first.id || firstMessage.NativeID != "native-first" {
		t.Fatal("confirmation replaced the optimistic bubble")
	}
	owner.State.Apply(agent.Event{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)})
	if owner.State.Messages[0].Timing.Finished.IsZero() {
		t.Fatal("supplemental message prevented task timing from finishing")
	}
}

func TestFollowupFailureNeverRetriesAutomatically(t *testing.T) {
	for _, failure := range []error{&agent.RPCError{Code: -32600, Message: "no active turn"}, errors.New("connection lost")} {
		backend := &followupBackend{calls: make(chan followupCall, 4), reply: make(chan error, 4)}
		desktop := activeFollowupApp(backend)
		updates := make(chan func(), 8)
		desktop.dispatch = func(update func()) { updates <- update }
		desktop.State.Draft = "supplement"
		desktop.send()
		receiveFollowup(t, backend.calls)
		backend.reply <- failure
		pumpUpdate(t, updates)
		desktop.drainInputs()
		if len(backend.calls) != 0 || len(desktop.pendingInputs) != 1 || desktop.pendingInputs[0].Status == "queued" || len(desktop.State.Messages) != 2 {
			t.Fatal("failure was retried or displayed as delivered")
		}
		desktop.retrieveInput(desktop.pendingInputs[0].ID)
		if desktop.State.Draft != "supplement" || len(desktop.pendingInputs) != 0 || len(desktop.State.Messages) != 1 {
			t.Fatal("failed content could not be retrieved")
		}
	}
}

type queuedFollowupBackend struct {
	stubBackend
	sent chan string
}

func (backend *queuedFollowupBackend) Send(_ context.Context, _, text string) (agent.Turn, error) {
	backend.sent <- text
	return agent.Turn{ID: "next", Status: "inProgress"}, nil
}

func TestFollowupFallbackWaitsAndPreservesNewDraft(t *testing.T) {
	backend := &queuedFollowupBackend{sent: make(chan string, 4)}
	desktop := activeFollowupApp(stubBackend{})
	if err := desktop.RegisterBackend(agent.Claude, "fixture", backend); err != nil {
		t.Fatal(err)
	}
	desktop.kind, desktop.State.Agent = agent.Claude, agent.Claude
	desktop.runtimeKey = "fixture"
	updates := make(chan func(), 8)
	desktop.dispatch = func(update func()) { updates <- update }
	desktop.State.Draft = "next task"
	desktop.send()
	if len(backend.sent) != 0 || len(desktop.pendingInputs) != 1 {
		t.Fatal("fallback sent while the original task was active")
	}
	tester := native.NewTester(func(c *native.Context) { desktop.composer(c) }, 640, 320)
	if tester.HasText("等待本轮结束") || len(desktop.State.Messages) != 2 || desktop.State.Messages[1].Text != "next task" {
		t.Fatal("input did not appear directly in the conversation")
	}
	message := desktop.State.Messages[1]
	desktop.State.Draft = "still typing"
	desktop.applySessionBatch([]agent.Event{{Session: agent.SessionKey{Agent: agent.Claude, Runtime: "fixture", SessionID: "thread"}, Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)}})
	select {
	case text := <-backend.sent:
		if text != "next task" {
			t.Fatal(text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fallback did not send after completion")
	}
	pumpUpdate(t, updates)
	if desktop.State.Draft != "still typing" || len(desktop.pendingInputs) != 0 || desktop.State.TurnID != "next" {
		t.Fatal("fallback overwrote the draft or failed to start the next turn")
	}
	if len(desktop.State.Messages) != 2 || desktop.State.Messages[1] != message || message.TurnID != "next" {
		t.Fatal("fallback duplicated or failed to bind the existing bubble")
	}
}

func TestInterruptedFollowupQueueRemainsPaused(t *testing.T) {
	desktop := activeFollowupApp(stubBackend{})
	desktop.State.Draft = "later"
	desktop.send()
	desktop.applySessionBatch([]agent.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)}})
	if len(desktop.pendingInputs) != 1 || desktop.pendingInputs[0].Status != "paused" {
		t.Fatal("interrupted task automatically resumed queued work")
	}
}

func TestPendingFollowupsPersistAndRestoreWithoutReplay(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	desktop := activeFollowupApp(stubBackend{})
	desktop.store = store
	desktop.State.Draft = "saved supplement"
	desktop.send()
	snapshot, err := store.Load()
	if err != nil || snapshot.Sessions[0].Draft != "" || len(snapshot.Sessions[0].PendingInputs) != 1 {
		t.Fatal("queue not persisted independently from the draft", err)
	}
	snapshot.Sessions[0].PendingInputs = append(snapshot.Sessions[0].PendingInputs, storage.PendingInput{ID: "uncertain", Text: "maybe sent", Status: "sending"})
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	pending := restored.sessions[0].pendingInputs
	if messages := restored.sessions[0].State.Messages; len(messages) != 2 || messages[0].Text != "saved supplement" || messages[1].ID != "uncertain" {
		t.Fatal("restart lost optimistic messages")
	}
	if len(pending) != 2 || pending[0].Status != "paused" || pending[1].Status != "uncertain" {
		t.Fatal("restart allowed automatic replay", pending)
	}
}

func TestFollowupComposerSwitchesSingleButtonWithDraft(t *testing.T) {
	desktop := activeFollowupApp(stubBackend{})
	tester := native.NewTester(func(c *native.Context) { desktop.composer(c) }, 640, 320)
	for _, example := range []struct {
		busy          bool
		draft, button string
	}{
		{true, "", "停止"},
		{true, "supplement", "发送"},
		{true, " \n\t", "停止"},
		{true, "another supplement", "发送"},
		{true, "", "停止"},
		{false, "", "发送"},
		{false, "next task", "发送"},
	} {
		desktop.State.Busy, desktop.State.Draft = example.busy, example.draft
		tester.Frame()
		for _, label := range []string{"发送", "停止"} {
			_, found := tester.Find(label)
			if found != (label == example.button) {
				t.Fatalf("busy=%t draft=%q: expected only %s, found %s=%t", example.busy, example.draft, example.button, label, found)
			}
		}
	}
	desktop.State.Busy, desktop.State.Draft = true, "supplement"
	if !desktop.canSend() || desktop.canSubmitTask() {
		t.Fatal("running composer confused supplementary input with a new turn")
	}
}

func TestIdenticalFollowupsMatchDistinctNativeMessages(t *testing.T) {
	desktop := activeFollowupApp(stubBackend{})
	desktop.pendingInputs = []storage.PendingInput{
		{ID: "client-first", Text: "same", TurnID: "turn", Status: "accepted", KnownIDs: []string{"original"}},
		{ID: "client-second", Text: "same", TurnID: "turn", Status: "accepted", KnownIDs: []string{"original"}},
	}
	desktop.restorePendingMessages()
	desktop.State.AcceptUserInput("native-first", "turn", "same", nil)
	desktop.settleInputs()
	desktop.settleInputs()
	if len(desktop.pendingInputs) != 1 || desktop.pendingInputs[0].ID != "client-second" {
		t.Fatal("one native message acknowledged two identical inputs")
	}
	desktop.State.AcceptUserInput("native-second", "turn", "same", nil)
	desktop.settleInputs()
	if len(desktop.pendingInputs) != 0 {
		t.Fatal("second native message was not matched")
	}
	if len(desktop.State.Messages) != 3 || desktop.State.Messages[1].ID != "client-first" || desktop.State.Messages[2].ID != "client-second" {
		t.Fatal("identical followups duplicated or replaced optimistic bubbles")
	}
}

func TestFollowupBubbleHasNoNormalDeliveryFeedback(t *testing.T) {
	desktop := activeFollowupApp(stubBackend{})
	desktop.State.Draft = "supplement"
	desktop.send()
	message := desktop.State.Messages[1]
	tester := native.NewTester(func(c *native.Context) { desktop.userMessage(c, message) }, 640, 320)
	for _, status := range []string{"queued", "sending", "accepted"} {
		desktop.pendingInputs[0].Status = status
		tester.Frame()
		if !tester.HasText("supplement") {
			t.Fatal("conversation bubble did not show the input")
		}
		for _, label := range []string{"等待本轮结束", "等待追加", "正在追加", "已追加", "继续发送", "取回编辑"} {
			if tester.HasText(label) {
				t.Fatalf("normal delivery exposed %q", label)
			}
		}
	}
}

func TestFollowupQueuedBubbleKeepsForeignTurnOutputVisible(t *testing.T) {
	desktop := activeFollowupApp(stubBackend{})
	desktop.State.Messages[0].Timing.Finished = time.Now()
	desktop.State.Messages = append(desktop.State.Messages,
		&app.Message{ID: "next", Role: "user", Text: "next", TurnID: "next-turn", Timing: &app.TurnTiming{Started: time.Now(), Finished: time.Now()}},
		&app.Message{ID: "old-reply", Role: "assistant", Text: "old reply", TurnID: "turn", Done: true},
		&app.Message{ID: "next-reply", Role: "assistant", Text: "next reply", TurnID: "next-turn", Done: true},
	)
	if len(desktop.visibleConversationMessages()) != 4 {
		t.Fatal("folding a queued turn hid the prior reply")
	}
}

func TestFollowupPreservesTurnTimingAndProcessDisclosure(t *testing.T) {
	desktop := activeFollowupApp(stubBackend{})
	desktop.State.Messages[0].Timing.Started = time.Now().Add(-time.Minute)
	desktop.State.Messages = append(desktop.State.Messages,
		&app.Message{ID: "progress", Role: "assistant", Text: "progress", TurnID: "turn"},
		&app.Message{ID: "supplement", Role: "user", Text: "extra", TurnID: "turn", NativeID: "supplement"},
		&app.Message{ID: "command", Role: "tool", TurnID: "turn", Tool: &app.Tool{Kind: "commandExecution", Status: "completed"}, Done: true},
		&app.Message{ID: "final", Role: "assistant", Text: "final", TurnID: "turn", Done: true},
	)
	if timings := desktop.conversationTimings(desktop.conversationRows()); len(timings) != 1 || timings[0] != desktop.State.Messages[0].Timing {
		t.Fatal("supplemental input hid the running task clock")
	}
	desktop.State.Messages[0].Timing.Finished = time.Now()
	desktop.State.Busy = false
	visible := desktop.visibleConversationMessages()
	if len(visible) != 3 || visible[1].ID != "supplement" || visible[2].ID != "final" || !desktop.hasTurnProcess(visible[0]) {
		t.Fatal("folding the task hid its supplements or retained process records")
	}
	desktop.expandedProcesses = map[string]bool{visible[0].ID: true}
	if len(desktop.visibleConversationMessages()) != 5 {
		t.Fatal("expanding the task did not restore its whole process")
	}
}

func TestReopeningSessionDrainsItsWaitingInput(t *testing.T) {
	backend := &queuedFollowupBackend{sent: make(chan string, 4)}
	desktop := activeFollowupApp(backend)
	updates := make(chan func(), 8)
	desktop.dispatch = func(update func()) { updates <- update }
	desktop.State.Draft = "waiting"
	desktop.send()
	owner := desktop.session
	owner.open = false
	desktop.newSession("/other")
	desktop.forSession(owner).applySessionBatch([]agent.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)}})
	if len(backend.sent) != 0 {
		t.Fatal("closed session unexpectedly submitted its next task")
	}
	desktop.selectSession(owner)
	select {
	case text := <-backend.sent:
		if text != "waiting" {
			t.Fatal(text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reopened session left queued input stuck")
	}
	pumpUpdate(t, updates)
}
