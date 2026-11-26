package ui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
	"github.com/shenming0821/Sasa/internal/attachment"
	"github.com/shenming0821/Sasa/internal/storage"
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
		message := desktop.State.Messages[1]
		status := desktop.pendingInputs[0].Status
		desktop.beginMessageEdit(message)
		if status == "failed" && (desktop.edit == nil || desktop.edit.Text != "supplement") {
			t.Fatal("rejected content could not be edited in place")
		}
		if status == "uncertain" && desktop.edit != nil {
			t.Fatal("unverified content became replayable")
		}
		if desktop.State.Draft != "" || len(desktop.pendingInputs) != 1 || len(desktop.State.Messages) != 2 {
			t.Fatal("editing moved failed content into the main composer")
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

type modelSteerBackend struct {
	*modelCaptureBackend
}

func (*modelSteerBackend) Steer(context.Context, string, string, string, string, []agent.Skill, []attachment.Image) error {
	return errors.New("a message for a different model must start a new turn")
}

func TestFollowupWithDifferentModelStartsNextTurn(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			capture := &modelCaptureBackend{request: make(chan modelRequest, 1)}
			var backend Backend = stubBackend{}
			if kind == agent.Codex {
				backend = &modelSteerBackend{modelCaptureBackend: capture}
			}
			desktop := activeFollowupApp(backend)
			if kind == agent.Claude {
				if err := desktop.RegisterBackend(kind, "fixture", capture); err != nil {
					t.Fatal(err)
				}
				desktop.kind, desktop.State.Agent, desktop.runtimeKey = kind, kind, "fixture"
			}
			store := imageStore(t, desktop)
			updates := make(chan func(), 8)
			desktop.dispatch = func(update func()) { updates <- update }
			desktop.activeOptions.Model, desktop.State.Model = "provider/one", "provider/one"
			desktop.selectedModel, desktop.selectedEffort = "provider/two", "high"
			desktop.State.Draft = "next task"
			desktop.send()
			if len(desktop.pendingInputs) != 1 || desktop.pendingInputs[0].Status != "queued" ||
				desktop.pendingInputs[0].TurnID != "" || desktop.followupSending || len(capture.request) != 0 {
				t.Fatal("message was submitted to the active turn", desktop.pendingInputs)
			}
			desktop.selectedModel, desktop.selectedEffort = "provider/three", "low"
			desktop.rememberModel(desktop.selectedModel)
			event := agent.Event{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)}
			if kind == agent.Claude {
				event.Session = agent.SessionKey{Agent: kind, Runtime: "fixture", SessionID: "thread"}
			}
			desktop.applySessionBatch([]agent.Event{event})
			select {
			case request := <-capture.request:
				if request.options.Model != "provider/two" || request.options.Effort != "high" {
					t.Fatal("next turn lost the submitted options", request.options)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("queued message did not start a new turn")
			}
			snapshot, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Sessions) != 1 || snapshot.Sessions[0].SelectedModel != "provider/three" || snapshot.Sessions[0].SelectedEffort != "low" {
				t.Fatal("sending the queued message overwrote the saved model preference")
			}
			pumpUpdate(t, updates)
			if !desktop.State.Busy || desktop.State.TurnID != "model-turn" || desktop.activeOptions.Model != "provider/two" ||
				desktop.selectedModel != "provider/three" || desktop.lastModels[modelPreferenceKey(kind, desktop.runtimeKey)] != "provider/three" {
				t.Fatal("the turn acknowledgement overwrote the next model selection")
			}
		})
	}
}

func TestFollowupWithChangedEffortOrSpeedStartsNextTurn(t *testing.T) {
	for _, options := range []agent.TurnOptions{
		{Model: "same", Effort: "high", ServiceTier: "default"},
		{Model: "same", Effort: "medium", ServiceTier: "priority"},
	} {
		t.Run(options.Effort+"-"+options.ServiceTier, func(t *testing.T) {
			capture := &modelCaptureBackend{request: make(chan modelRequest, 1)}
			desktop := activeFollowupApp(&modelSteerBackend{capture})
			updates := make(chan func(), 8)
			desktop.dispatch = func(update func()) { updates <- update }
			desktop.activeOptions = agent.TurnOptions{Model: "same", Effort: "medium", ServiceTier: "default"}
			desktop.selectedModel, desktop.selectedEffort, desktop.selectedTier = options.Model, options.Effort, options.ServiceTier
			desktop.State.Draft = "next task"
			desktop.send()
			if len(desktop.pendingInputs) != 1 || desktop.pendingInputs[0].TurnID != "" || desktop.followupSending {
				t.Fatal("follow-up attached to active turn", desktop.pendingInputs)
			}
			desktop.selectedEffort, desktop.selectedTier = "low", "default"
			desktop.applySessionBatch([]agent.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)}})
			pumpUpdate(t, updates)
			if request := <-capture.request; request.options != options || desktop.activeOptions != options {
				t.Fatal("next turn lost the submitted options", request.options, desktop.activeOptions)
			}
		})
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

func TestInterruptedUnconsumedFollowupSettlesWithoutButtonFlicker(t *testing.T) {
	backend := &deliveryBackend{reads: make(chan struct{}, 8)}
	desktop := activeFollowupApp(backend)
	updates := make(chan func(), 8)
	desktop.dispatch = func(update func()) { updates <- update }
	desktop.pendingInputs = []storage.PendingInput{{ID: "supplement", Text: "change direction", TurnID: "turn", Status: "accepted"}}
	desktop.restorePendingMessages()
	message := desktop.State.Messages[1]
	desktop.State.Messages = append(desktop.State.Messages, &app.Message{ID: "reply", Role: "assistant", Text: "old direction", TurnID: "turn", Done: true})
	desktop.State.Draft = "keep typing"
	desktop.applySessionBatch([]agent.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)}})
	if desktop.canSend() {
		t.Fatal("send enabled before delivery verification")
	}
	pumpUpdate(t, updates)
	if len(desktop.pendingInputs) != 1 || desktop.pendingInputs[0].Status != "paused" {
		t.Fatal("unconsumed follow-up did not become editable", desktop.pendingInputs)
	}
	if desktop.State.Messages[len(desktop.State.Messages)-1] != message || message.TurnID != "" {
		t.Fatal("unconsumed question did not move after the stopped turn")
	}
	if desktop.State.Draft != "keep typing" {
		t.Fatal("verification overwrote the new draft")
	}
	tester := native.NewTester(func(c *native.Context) { desktop.composer(c) }, 640, 320)
	baseline := tester.Image()
	for frame := 0; frame < 8; frame++ {
		desktop.applySessionBatch(nil)
		tester.Advance(200 * time.Millisecond)
		if !desktop.canSend() || desktop.snapshotPending || desktop.State.NeedsSnapshot {
			t.Fatal("settled follow-up restarted verification and toggled send availability")
		}
		button, found := tester.Find("发送")
		if !found || tester.Image().RGBAAt(int(button.X+5), int(button.Y+17)) != baseline.RGBAAt(int(button.X+5), int(button.Y+17)) {
			t.Fatal("send button changed appearance after verification settled")
		}
	}
	desktop.State.Draft = ""
	desktop.beginMessageEdit(message)
	if desktop.edit == nil || desktop.edit.Text != "change direction" || desktop.State.Draft != "" || len(desktop.pendingInputs) != 1 || len(desktop.State.Messages) != 3 {
		t.Fatal("stopped follow-up could not be edited in place")
	}
}

type followupSnapshotBackend struct {
	*followupBackend
	snapshot json.RawMessage
	err      error
}

func (backend *followupSnapshotBackend) ReadThread(context.Context, string) (json.RawMessage, error) {
	return backend.snapshot, backend.err
}

func TestFollowupSnapshotRequiresCompleteDeliveryEvidence(t *testing.T) {
	for _, example := range []struct {
		name, snapshot string
		err            error
		consumed       bool
		incomplete     bool
	}{
		{name: "absent", snapshot: `{"thread":{"id":"thread","turns":[{"id":"turn","status":"completed","items":[]}]}}`},
		{name: "consumed", consumed: true, snapshot: `{"thread":{"id":"thread","turns":[{"id":"turn","status":"interrupted","items":[{"id":"native-supplement","type":"userMessage","content":[{"type":"text","text":"supplement"}]}]}]}}`},
		{name: "partial", incomplete: true, snapshot: `{"thread":{"id":"thread","turns":[{"id":"turn","status":"interrupted","itemsView":"summary","items":[]}]}}`},
		{name: "missing items", incomplete: true, snapshot: `{"thread":{"id":"thread","turns":[{"id":"turn","status":"interrupted"}]}}`},
		{name: "read failure", incomplete: true, err: errors.New("unavailable")},
	} {
		t.Run(example.name, func(t *testing.T) {
			backend := &followupSnapshotBackend{followupBackend: &followupBackend{}, snapshot: json.RawMessage(example.snapshot), err: example.err}
			desktop := activeFollowupApp(backend)
			updates := make(chan func(), 8)
			desktop.dispatch = func(update func()) { updates <- update }
			desktop.pendingInputs = []storage.PendingInput{{ID: "supplement", Text: "supplement", TurnID: "turn", Status: "accepted"}}
			desktop.restorePendingMessages()
			message := desktop.State.Messages[1]
			desktop.applySessionBatch([]agent.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)}})
			pumpUpdate(t, updates)
			desktop.State.Draft = "new draft"
			if desktop.State.Incomplete != example.incomplete || desktop.canSend() == example.incomplete {
				t.Fatal("send availability did not follow snapshot certainty")
			}
			if example.consumed {
				if len(desktop.pendingInputs) != 0 || message.NativeID != "native-supplement" || message.TurnID != "turn" {
					t.Fatal("consumed input was marked for replay or lost native identity")
				}
			} else {
				status := "paused"
				if example.incomplete {
					status = "accepted"
				}
				if len(desktop.pendingInputs) != 1 || desktop.pendingInputs[0].Status != status {
					t.Fatal("snapshot discarded or incorrectly settled pending input")
				}
			}
		})
	}
}

func TestStoppedFollowupSettlesAfterLateSteerAcknowledgement(t *testing.T) {
	backend := &followupSnapshotBackend{
		followupBackend: &followupBackend{calls: make(chan followupCall, 1), reply: make(chan error, 1)},
		snapshot:        json.RawMessage(`{"thread":{"id":"thread","turns":[{"id":"turn","status":"interrupted","items":[]}]}}`),
	}
	desktop := activeFollowupApp(backend)
	updates := make(chan func(), 8)
	desktop.dispatch = func(update func()) { updates <- update }
	desktop.State.Draft = "supplement"
	desktop.send()
	receiveFollowup(t, backend.calls)
	desktop.applySessionBatch([]agent.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)}})
	pumpUpdate(t, updates)
	if desktop.pendingInputs[0].Status != "sending" {
		t.Fatal("in-flight steer settled before its response")
	}
	backend.reply <- nil
	pumpUpdate(t, updates)
	pumpUpdate(t, updates)
	if desktop.followupSending || desktop.pendingInputs[0].Status != "paused" {
		t.Fatal("late steer response left input accepted indefinitely")
	}
}

func TestRestoredUnconsumedFollowupBecomesEditable(t *testing.T) {
	backend := recoveredBackend("interrupted")
	desktop := New(backend, "test", "/first", nil)
	desktop.State.ThreadID = "native"
	desktop.pendingInputs = []storage.PendingInput{{ID: "pending", Text: "saved supplement", TurnID: "old-turn", Status: "uncertain"}}
	desktop.State.Draft = "saved draft"
	updates := make(chan func(), 8)
	desktop.dispatch = func(update func()) { updates <- update }
	desktop.recoverSession(desktop.session)
	pumpUpdate(t, updates)
	if len(desktop.pendingInputs) != 1 || desktop.pendingInputs[0].Status != "paused" || desktop.State.Draft != "saved draft" {
		t.Fatal("history recovery lost or failed to settle pending input")
	}
	last := desktop.State.Messages[len(desktop.State.Messages)-1]
	if last.ID != "pending" || last.TurnID != "" || desktop.snapshotPending || desktop.State.NeedsSnapshot {
		t.Fatal("history recovery did not leave the question at the end")
	}
}

func TestUnmatchedNativeInputDoesNotBecomeReplayable(t *testing.T) {
	desktop := activeFollowupApp(stubBackend{})
	desktop.pendingInputs = []storage.PendingInput{{ID: "pending", Text: "supplement", TurnID: "turn", Status: "accepted", KnownIDs: []string{"original"}}}
	desktop.restorePendingMessages()
	err := desktop.settleInputsFromHistory([]agent.HistoryTurn{{
		ID: "turn", Status: "interrupted", Items: []json.RawMessage{
			json.RawMessage(`{"id":"native-supplement","type":"userMessage","content":[{"type":"text","text":"supplement"}]}`),
		},
	}})
	if err == nil || desktop.pendingInputs[0].Status != "accepted" {
		t.Fatal("unmatched native input was treated as absent")
	}
}

func TestPendingMessageEditIconEditsInPlace(t *testing.T) {
	for _, status := range []string{"paused", "queued", "failed"} {
		t.Run(status, func(t *testing.T) {
			desktop := activeFollowupApp(stubBackend{})
			desktop.State.Busy = false
			desktop.State.Draft = "keep main draft"
			desktop.pendingInputs = []storage.PendingInput{{ID: "pending", Text: "edit this question", Status: status}}
			desktop.restorePendingMessages()
			message := desktop.State.Messages[1]
			tester := native.NewTester(desktop.view, 700, 500)
			for _, label := range []string{"已暂停发送", "继续发送", "取回编辑", "移除消息"} {
				if _, found := tester.Find(label); found || tester.HasText(label) {
					t.Fatalf("pending message still exposed %s", label)
				}
			}
			if reason := desktop.messageEditReason(message); reason != "" {
				t.Fatal("pending edit icon is disabled", reason)
			}
			bubble, found := tester.Find("edit this question")
			if !found {
				t.Fatal("pending question missing")
			}
			tester.Move(bubble.X+4, bubble.Y+4)
			if err := tester.Click("编辑消息"); err != nil {
				t.Fatal(err)
			}
			tester.Frame()
			if desktop.State.Draft != "keep main draft" || len(desktop.pendingInputs) != 1 || desktop.State.Messages[1] != message || desktop.edit == nil || !desktop.canSendMessageEdit() {
				t.Fatal("edit icon did not keep editing at the original message")
			}
			if !tester.Focused("编辑消息内容") {
				t.Fatal("inline editor did not receive focus")
			}
			if tester.HasText("发送后将替换此处及后续对话") {
				t.Fatal("local input editor showed history replacement instructions")
			}
			tester.Key(native.Cmd, native.KeyA)
			tester.Type("changed question")
			if desktop.edit.Text != "changed question" || desktop.State.Draft != "keep main draft" || message.Text != "edit this question" {
				t.Fatal("inline changes modified the main draft or original question")
			}
			tester.Key(0, native.KeyEscape)
			if desktop.edit != nil || message.Text != "edit this question" || desktop.State.Draft != "keep main draft" || desktop.pendingInputs[0].Status != "paused" {
				t.Fatal("cancel did not preserve the original question and main draft")
			}
		})
	}
}

func TestPendingMessageEditPreservesUncertainDelivery(t *testing.T) {
	for _, example := range []struct{ status, draft string }{
		{status: "sending"},
		{status: "accepted"},
		{status: "uncertain"},
	} {
		desktop := activeFollowupApp(stubBackend{})
		desktop.State.Busy = false
		desktop.State.Draft = example.draft
		desktop.pendingInputs = []storage.PendingInput{{ID: "pending", Text: "question", Status: example.status}}
		desktop.restorePendingMessages()
		message := desktop.State.Messages[1]
		if desktop.messageEditReason(message) == "" {
			t.Fatal("editing allowed before existing draft or delivery was settled")
		}
		desktop.beginMessageEdit(message)
		if desktop.State.Draft != example.draft || len(desktop.pendingInputs) != 1 {
			t.Fatal("edit discarded draft or delivery evidence")
		}
	}
}

func TestPendingMessageInlineEditSendsWithoutReplacingHistory(t *testing.T) {
	for _, suffix := range []string{"", " changed"} {
		t.Run("suffix="+suffix, func(t *testing.T) {
			backend := &queuedFollowupBackend{sent: make(chan string, 1)}
			desktop := activeFollowupApp(backend)
			desktop.State.Busy = false
			desktop.State.Draft = "main draft"
			desktop.pendingInputs = []storage.PendingInput{
				{ID: "earlier", Text: "leave paused", Status: "paused"},
				{ID: "pending", Text: "question", Status: "paused"},
			}
			desktop.restorePendingMessages()
			message := desktop.State.Messages[2]
			updates := make(chan func(), 8)
			desktop.dispatch = func(update func()) { updates <- update }
			desktop.beginMessageEdit(message)
			desktop.edit.Text += suffix
			desktop.sendMessageEdit()
			pumpUpdate(t, updates)
			if sent := <-backend.sent; sent != "question"+suffix {
				t.Fatal("inline question was not sent", sent)
			}
			if desktop.State.Draft != "main draft" || desktop.State.ThreadID != "thread" || desktop.State.Messages[2] != message || message.Text != "question"+suffix || desktop.edit != nil {
				t.Fatal("inline send replaced history, the message, or the main draft")
			}
			if len(desktop.pendingInputs) != 1 || desktop.pendingInputs[0].ID != "earlier" || desktop.pendingInputs[0].Status != "paused" {
				t.Fatal("sending the selected question replayed another pending message")
			}
		})
	}
}

func TestPendingMessageInlineEditPreservesChangesWhenSaveFails(t *testing.T) {
	backend := &queuedFollowupBackend{sent: make(chan string, 1)}
	desktop := activeFollowupApp(backend)
	desktop.State.Busy = false
	desktop.State.Draft = "main draft"
	desktop.pendingInputs = []storage.PendingInput{{ID: "pending", Text: "question", Status: "paused"}}
	desktop.restorePendingMessages()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	desktop.store = store
	store.Close()
	message := desktop.State.Messages[1]
	desktop.beginMessageEdit(message)
	desktop.edit.Text = "changed question"
	desktop.sendMessageEdit()
	if desktop.edit == nil || desktop.edit.Text != "changed question" || desktop.editError == "" || desktop.State.Draft != "main draft" || message.Text != "question" || desktop.pendingInputs[0].Status != "paused" || len(backend.sent) != 0 {
		t.Fatal("failed save lost inline changes or submitted the question")
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
