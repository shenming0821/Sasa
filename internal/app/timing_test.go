package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
)

func TestTimingCoversSubmissionAndFreezesAtTerminal(t *testing.T) {
	for _, terminal := range []string{"completed", "interrupted", "failed"} {
		t.Run(terminal, func(t *testing.T) {
			state := New("/tmp")
			state.ThreadID = "thread"
			state.Begin("task")
			timing := state.Messages[0].Timing
			timing.Started = time.Now().Add(-time.Minute)
			state.Apply(agent.Event{Method: "turn/started", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn"}}`)})
			if state.Messages[0].TurnID != "turn" || time.Since(timing.Started) < time.Minute {
				t.Fatal("start event reset the submission clock or lost its turn")
			}
			params, _ := json.Marshal(map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": terminal}})
			state.Apply(agent.Event{Method: "turn/completed", Params: params})
			finished := timing.Finished
			state.FinishTiming()
			if finished.IsZero() || !finished.Equal(timing.Finished) {
				t.Fatal("terminal did not freeze elapsed time")
			}
			state.Begin("next")
			if state.Messages[1].Timing == timing || state.Messages[1].Timing.Started.Before(finished) {
				t.Fatal("next request reused the previous clock")
			}
		})
	}
}

func TestTimingDisconnectAndMissingStart(t *testing.T) {
	for _, method := range []string{"turn/completed", "sasa/disconnected"} {
		state := New("/tmp")
		state.Begin("task")
		state.Apply(agent.Event{Method: method, Params: json.RawMessage(`{"turn":{"id":"turn","status":"completed"}}`)})
		if state.Messages[0].Timing.Finished.IsZero() {
			t.Fatalf("%s left the clock running", method)
		}
		if method == "turn/completed" && state.Messages[0].TurnID != "turn" {
			t.Fatal("completion without start lost timing ownership")
		}
	}
}
