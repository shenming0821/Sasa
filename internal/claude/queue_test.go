package claude

import (
	"encoding/json"
	"testing"

	"github.com/shenming0821/Sasa/internal/agent"
)

func TestQueuePreservesApprovalAndTerminalWhenDeltasOverflow(t *testing.T) {
	queue := eventQueue{}
	for range agent.MaxQueueEvents {
		if !queue.push(agent.Event{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"delta":"text"}`)}) {
			t.Fatal("display overflow disconnected the client")
		}
	}
	for _, event := range []agent.Event{
		{Method: "item/commandExecution/requestApproval", ID: json.RawMessage(`"permission"`)},
		{Method: "turn/completed"},
	} {
		if !queue.push(event) {
			t.Fatal("control event was lost")
		}
	}
	if !queue.gap || queue.overflow || len(queue.pending) > agent.MaxQueueEvents || queue.bytes > agent.MaxQueueBytes {
		t.Fatal("queue did not remain bounded with a visible gap")
	}
	seen := map[string]bool{}
	for _, event := range queue.pending {
		seen[event.Method] = true
	}
	if !seen["turn/completed"] || !seen["item/commandExecution/requestApproval"] {
		t.Fatal("approval or terminal removed")
	}
}

func TestQueueControlOverflowFailsVisibly(t *testing.T) {
	queue := eventQueue{}
	for range agent.MaxQueueEvents {
		if !queue.push(agent.Event{Method: "item/started"}) {
			t.Fatal("premature overflow")
		}
	}
	if queue.push(agent.Event{Method: "turn/completed"}) || !queue.overflow || len(queue.pending) != 1 || queue.pending[0].Method != "sasa/disconnected" {
		t.Fatal("control overflow failed silently")
	}
}
