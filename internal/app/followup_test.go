package app

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUserInputsInOneTurnKeepNativeIdentityAndTiming(t *testing.T) {
	state := New("/project")
	state.ThreadID = "thread"
	state.Begin("original")
	apply(&state, "turn/started", `{"turn":{"id":"turn"}}`)
	apply(&state, "item/completed", `{"turnId":"turn","item":{"id":"original","type":"userMessage","content":[{"type":"text","text":"original"}]}}`)
	apply(&state, "item/started", `{"turnId":"turn","item":{"id":"supplement","type":"userMessage","content":[]}}`)
	for range 2 {
		apply(&state, "item/completed", `{"turnId":"turn","item":{"id":"original","type":"userMessage","content":[{"type":"text","text":"original"}]}}`)
		apply(&state, "item/completed", `{"turnId":"turn","item":{"id":"supplement","type":"userMessage","content":[{"type":"text","text":"extra"}]}}`)
	}
	state.AcceptUserInput("supplement", "turn", "extra", nil)
	if len(state.Messages) != 2 || state.Messages[0].NativeID != "original" || state.Messages[1].NativeID != "supplement" || state.Sequence != 1 || !state.Busy {
		t.Fatal("user input events duplicated messages or changed the active task")
	}
	apply(&state, "turn/completed", `{"turn":{"id":"turn","status":"completed"}}`)
	if state.Messages[0].Timing.Finished.IsZero() || state.Messages[1].Timing != nil {
		t.Fatal("turn timing attached to supplementary input")
	}
	raw := json.RawMessage(`{"thread":{"id":"thread","turns":[{"id":"turn","status":"completed","items":[{"id":"original","type":"userMessage","content":[{"type":"text","text":"original"}]},{"id":"supplement","type":"userMessage","content":[{"type":"text","text":"extra"}]}]}]}}`)
	if err := state.Reconcile(raw); err != nil || len(state.Messages) != 2 || state.Messages[0].NativeID != "original" {
		t.Fatal("snapshot confused original and supplementary inputs", err)
	}
	state.Messages[0].Timing = nil
	state.RestoreTimings(map[string]TurnTiming{"turn": {Started: time.Now(), Finished: time.Now()}})
	if state.Messages[0].Timing == nil || state.Messages[1].Timing != nil {
		t.Fatal("history duplicated turn timing across supplementary inputs")
	}
}

func TestBeginMessageReusesBubbleAndBindsTimingPastQueuedInputs(t *testing.T) {
	state := New("/project")
	first := &Message{ID: "first", Role: "user", Text: "first", Done: true, Delivery: "queued"}
	second := &Message{ID: "second", Role: "user", Text: "second", Done: true, Delivery: "queued"}
	state.Messages = []*Message{first, second}
	state.BeginMessage("first", "first")
	state.BindTiming("turn")
	state.AcceptUserInput("native-first", "turn", "first", nil)
	if len(state.Messages) != 2 || state.Messages[0] != first || first.TurnID != "turn" || first.NativeID != "native-first" || second.Timing != nil || second.TurnID != "" {
		t.Fatal("queued inputs broke bubble reuse or timing binding")
	}
}
