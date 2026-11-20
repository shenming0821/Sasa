package ui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/attachment"
	"github.com/shenming0821/Sasa/internal/storage"
)

type compactBackend struct {
	followupBackend
	compacts chan string
	results  chan error
	sends    chan string
	stops    chan string
}

func newCompactBackend() *compactBackend {
	return &compactBackend{
		followupBackend: followupBackend{calls: make(chan followupCall, 8), reply: make(chan error, 8)},
		compacts:        make(chan string, 8), results: make(chan error, 8), sends: make(chan string, 8),
		stops: make(chan string, 8),
	}
}

func (backend *compactBackend) CompactThread(ctx context.Context, threadID string) error {
	backend.compacts <- threadID
	select {
	case err := <-backend.results:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (backend *compactBackend) Send(_ context.Context, _, text string) (agent.Turn, error) {
	backend.sends <- text
	return agent.Turn{ID: "next", Status: "inProgress"}, nil
}

func (backend *compactBackend) Interrupt(_ context.Context, _, turnID string) error {
	backend.stops <- turnID
	return nil
}

func compactEvent(method, turn, status string) agent.Event {
	raw, _ := json.Marshal(map[string]any{
		"threadId": "thread", "turnId": turn,
		"turn": map[string]string{"id": turn, "status": status},
		"item": map[string]string{"id": "compaction", "type": "contextCompaction"},
	})
	return agent.Event{Method: method, Params: raw}
}

func receiveCompact(t *testing.T, calls <-chan string) string {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(3 * time.Second):
		t.Fatal("runtime call missing")
		return ""
	}
}

func TestCompactCompletionInsertsForBothAgents(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			desktop := New(stubBackend{}, "test", "/project", nil)
			desktop.kind, desktop.State.Agent = kind, kind
			items := desktop.completionItems(completionQuery{kind: '/', query: "comp"})
			if len(items) != 1 || items[0].name != "/compact" {
				t.Fatal(items)
			}
			desktop.acceptCompletion(completionQuery{kind: '/'}, items[0])
			if desktop.State.Draft != "/compact " || !desktop.completion.focus {
				t.Fatal("completion did not insert editable command")
			}
		})
	}
}

func TestCompactQueueWaitsForTerminalAndBlocksSteering(t *testing.T) {
	backend := newCompactBackend()
	desktop := activeFollowupApp(backend)
	updates := make(chan func(), 16)
	desktop.dispatch = func(update func()) { updates <- update }
	desktop.State.Draft = "/compact"
	desktop.send()
	desktop.State.Draft = "after compact"
	desktop.send()
	select {
	case call := <-backend.calls:
		t.Fatal("command barrier was steered", call)
	default:
	}
	select {
	case <-backend.compacts:
		t.Fatal("compaction ran before current task finished")
	default:
	}
	desktop.applySessionBatch([]agent.Event{compactEvent("turn/completed", "turn", "completed")})
	if receiveCompact(t, backend.compacts) != "thread" {
		t.Fatal("wrong thread")
	}
	if !desktop.State.Busy || desktop.State.PendingSubmission() != nil || !desktop.State.Compacting() {
		t.Fatal("compaction must be busy without pending user delivery")
	}
	if desktop.State.Messages[len(desktop.State.Messages)-1].Delivery != "queued" {
		t.Fatal("compaction altered the following queued message")
	}
	backend.results <- nil
	pumpUpdate(t, updates)
	if !desktop.State.Busy || len(desktop.pendingInputs) != 1 {
		t.Fatal("RPC acceptance completed compaction")
	}
	desktop.applySessionBatch([]agent.Event{
		compactEvent("turn/started", "compact-turn", "inProgress"),
		compactEvent("item/started", "compact-turn", ""),
		compactEvent("item/completed", "compact-turn", ""),
	})
	select {
	case call := <-backend.calls:
		t.Fatal("following input was steered during compaction", call)
	case call := <-backend.sends:
		t.Fatal("following input was sent during compaction", call)
	default:
	}
	desktop.applySessionBatch([]agent.Event{compactEvent("turn/completed", "compact-turn", "completed")})
	if receiveCompact(t, backend.sends) != "after compact" {
		t.Fatal("queue did not resume")
	}
	pumpUpdate(t, updates)
}

func TestCompactEarlyEventsAndFailurePauseQueue(t *testing.T) {
	for _, status := range []string{"completed", "failed", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			backend := newCompactBackend()
			desktop := activeFollowupApp(backend)
			desktop.State.Busy = false
			updates := make(chan func(), 16)
			desktop.dispatch = func(update func()) { updates <- update }
			desktop.State.Draft = "/compact"
			desktop.send()
			receiveCompact(t, backend.compacts)
			desktop.applySessionBatch([]agent.Event{compactEvent("turn/started", "compact-turn", "inProgress")})
			desktop.State.Draft = "later"
			desktop.send()
			desktop.applySessionBatch([]agent.Event{compactEvent("turn/completed", "compact-turn", status)})
			backend.results <- nil
			if status == "completed" {
				receiveCompact(t, backend.sends)
				pumpUpdate(t, updates)
				pumpUpdate(t, updates)
			} else {
				pumpUpdate(t, updates)
				if desktop.State.Busy || len(desktop.pendingInputs) != 1 || desktop.pendingInputs[0].Status != "paused" {
					t.Fatal("failed compaction did not pause queue")
				}
			}
		})
	}
}

func TestCompactValidationPreservesDraftAndImages(t *testing.T) {
	for _, test := range []struct {
		name, text string
		images     []attachment.Image
		empty      bool
	}{
		{name: "parameters", text: "/compact keep focus"},
		{name: "images", text: "/compact", images: []attachment.Image{{Path: "/not-read.png"}}},
		{name: "empty", text: "/compact", empty: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			desktop := activeFollowupApp(newCompactBackend())
			desktop.State.Busy = false
			if test.empty {
				desktop.State.ThreadID = ""
			}
			desktop.State.Draft, desktop.State.DraftImages = test.text, test.images
			desktop.send()
			if desktop.State.Error == "" || desktop.State.Draft != test.text || len(desktop.State.DraftImages) != len(test.images) || desktop.State.Busy {
				t.Fatal("invalid command lost input or started a task")
			}
		})
	}
}

func TestCompactRejectedOrUncertainDoesNotRetry(t *testing.T) {
	for _, problem := range []error{&agent.RPCError{Code: -32601, Message: "unsupported"}, errors.New("connection lost")} {
		t.Run(problem.Error(), func(t *testing.T) {
			backend := newCompactBackend()
			desktop := activeFollowupApp(backend)
			desktop.State.Busy = false
			updates := make(chan func(), 8)
			desktop.dispatch = func(update func()) { updates <- update }
			desktop.State.Draft = "/compact"
			desktop.send()
			receiveCompact(t, backend.compacts)
			desktop.State.Draft = "later"
			desktop.send()
			backend.results <- problem
			pumpUpdate(t, updates)
			if desktop.State.Busy || len(desktop.pendingInputs) != 1 || desktop.pendingInputs[0].Status != "paused" {
				t.Fatal("failure did not stop queue")
			}
			var rpc *agent.RPCError
			if desktop.State.Disconnected == errors.As(problem, &rpc) {
				t.Fatal("known rejection and uncertain result were not distinguished")
			}
		})
	}
}

func TestCompactBusyStatePersistsForRecovery(t *testing.T) {
	backend := newCompactBackend()
	desktop := activeFollowupApp(backend)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	desktop.store = store
	desktop.State.Busy = false
	updates := make(chan func(), 8)
	desktop.dispatch = func(update func()) { updates <- update }
	desktop.State.Draft = "/compact"
	desktop.send()
	receiveCompact(t, backend.compacts)
	snapshot, err := store.Load()
	if err != nil || len(snapshot.Sessions) != 1 || !snapshot.Sessions[0].Active || snapshot.Sessions[0].ThreadID != "thread" {
		t.Fatal("in-flight compaction not checkpointed", snapshot, err)
	}
	backend.results <- nil
	pumpUpdate(t, updates)
	restored := New(stubBackend{}, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if !restored.session.recoveryBlocked || restored.State.ThreadID != "thread" {
		t.Fatal("restored compaction was not gated by native recovery")
	}
	select {
	case <-backend.compacts:
		t.Fatal("restoration replayed compaction")
	default:
	}
}

func TestCompactNativeMenuSelection(t *testing.T) {
	desktop := New(stubBackend{}, "test", "/project", nil)
	tester := native.NewTester(desktop.view, 640, 480)
	tester.Click("任务输入")
	tester.Type("/comp")
	if _, ok := tester.Find("候选 /compact"); !ok {
		t.Fatal("compact candidate missing")
	}
	tester.Key(0, native.KeyTab)
	if desktop.State.Draft != "/compact " || desktop.State.Busy {
		t.Fatal("selecting completion must only fill draft")
	}
}

func TestCompactionRendersAsCommandStatus(t *testing.T) {
	desktop := activeFollowupApp(stubBackend{})
	desktop.State.Apply(compactEvent("item/started", "turn", ""))
	message := desktop.State.Messages[len(desktop.State.Messages)-1]
	tester := native.NewTester(func(c *native.Context) { desktop.message(c, message) }, 600, 200)
	tester.SetPreferences(native.Preferences{ReduceMotion: true})
	status, ok := tester.Find("上下文压缩 " + message.ID)
	if !ok || status.H != 30 || !tester.HasText("正在压缩上下文") {
		t.Fatalf("running command status missing: %v %v", status, tester.Texts())
	}

	desktop.State.Apply(compactEvent("item/completed", "turn", ""))
	desktop.State.Apply(compactEvent("turn/completed", "turn", "completed"))
	tester.Frame()
	completed, ok := tester.Find("上下文压缩 " + message.ID)
	if !ok || completed != status || !tester.HasText("上下文已压缩") || tester.HasText("正在压缩上下文") {
		t.Fatalf("completed status did not replace running status in place: %v %v", completed, tester.Texts())
	}
	if _, ok := tester.Find("复制回复"); ok {
		t.Fatal("compaction had a reply action")
	}
	found := false
	for _, row := range desktop.conversationRows() {
		if row.message == message {
			found = row.processRecord() && row.processID == ""
		}
	}
	if !found {
		t.Fatal("completed compaction must remain visible when the turn is collapsed")
	}
}

func TestCompactCommandMatching(t *testing.T) {
	for text, want := range map[string]bool{"/compact": true, " /compact\tkeep ": true, "/compaction": false, "please /compact": false} {
		if isCompactCommand(text) != want {
			t.Fatal(text)
		}
	}
}

func TestCompactQueuesDuringThreadInitialization(t *testing.T) {
	backend := newCompactBackend()
	desktop := activeFollowupApp(backend)
	desktop.State.ThreadID, desktop.State.TurnID = "", ""
	desktop.State.Draft = "/compact"
	desktop.send()
	if desktop.State.Error != "" || len(desktop.pendingInputs) != 1 || desktop.State.Draft != "" {
		t.Fatal("initializing task was treated as empty context")
	}
	select {
	case <-backend.compacts:
		t.Fatal("compacted before thread initialization")
	case <-backend.calls:
		t.Fatal("command was steered during initialization")
	default:
	}
}

func TestCompactUnsupportedBackendPreservesInput(t *testing.T) {
	desktop := activeFollowupApp(stubBackend{})
	desktop.State.Busy = false
	desktop.State.Draft = "/compact"
	desktop.send()
	if desktop.State.Error != "当前连接不支持压缩上下文" || desktop.State.Draft != "/compact" || desktop.State.Busy {
		t.Fatal("unsupported backend consumed command")
	}
}

func TestCompactStopBeforeTurnIdentity(t *testing.T) {
	backend := newCompactBackend()
	desktop := activeFollowupApp(backend)
	desktop.State.Busy = false
	updates := make(chan func(), 8)
	desktop.dispatch = func(update func()) { updates <- update }
	desktop.State.Draft = "/compact"
	desktop.send()
	receiveCompact(t, backend.compacts)
	backend.results <- nil
	pumpUpdate(t, updates)
	desktop.stop()
	if !desktop.submitStop || !desktop.State.Stopping() {
		t.Fatal("stop was lost before native turn ID")
	}
	desktop.applySessionBatch([]agent.Event{compactEvent("turn/started", "compact-turn", "inProgress")})
	if receiveCompact(t, backend.stops) != "compact-turn" {
		t.Fatal("stop did not target compaction turn")
	}
	desktop.applySessionBatch([]agent.Event{compactEvent("turn/completed", "compact-turn", "interrupted")})
	if desktop.State.Busy || desktop.submitCancel != nil || desktop.submitStop {
		t.Fatal("compaction stop did not settle")
	}
}

func TestClaudeCompactInstructionsUseExistingSend(t *testing.T) {
	backend := newCompactBackend()
	desktop := activeFollowupApp(stubBackend{})
	if err := desktop.RegisterBackend(agent.Claude, "/claude", backend); err != nil {
		t.Fatal(err)
	}
	desktop.kind, desktop.runtimeKey, desktop.State.Agent = agent.Claude, "/claude", agent.Claude
	desktop.State.Busy = false
	updates := make(chan func(), 8)
	desktop.dispatch = func(update func()) { updates <- update }
	desktop.State.Draft = "/compact 保留当前目标"
	desktop.send()
	if receiveCompact(t, backend.sends) != "/compact 保留当前目标" {
		t.Fatal("Claude compaction instructions changed")
	}
	pumpUpdate(t, updates)
	select {
	case <-backend.compacts:
		t.Fatal("Claude used Codex compaction RPC")
	default:
	}
	desktop.State.Draft = "later"
	desktop.send()
	select {
	case call := <-backend.calls:
		t.Fatal("input steered during Claude compaction", call)
	default:
	}
}
