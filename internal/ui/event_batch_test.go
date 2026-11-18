package ui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
)

func deliveryEvent(method, thread, turn, delta string) agent.Event {
	params, _ := json.Marshal(map[string]any{"threadId": thread, "turnId": turn, "delta": delta})
	return agent.Event{Method: method, Params: params}
}

func TestFirstContentAndInteractionsFlushWithoutTimer(t *testing.T) {
	for _, event := range []agent.Event{
		deliveryEvent("item/agentMessage/delta", "one", "turn", "first"),
		{Method: "item/started", Params: json.RawMessage(`{"threadId":"one","turnId":"turn","item":{"type":"commandExecution"}}`)},
		{Method: "item/tool/requestUserInput", ID: json.RawMessage(`"request"`)},
		{Method: "item/commandExecution/requestApproval", ID: json.RawMessage(`"request"`)},
		{Method: "turn/completed"},
		{Method: "sasa/disconnected"},
	} {
		t.Run(event.Method, func(t *testing.T) {
			delivery := eventDelivery{}
			done := make(chan []agent.Event, 1)
			shutdown := make(chan struct{})
			defer close(shutdown)
			go func() { done <- delivery.collect(event, make(chan agent.Event), make(chan time.Time), shutdown) }()
			select {
			case batch := <-done:
				if len(batch) != 1 || batch[0].Method != event.Method {
					t.Fatal("immediate event changed", batch)
				}
			case <-time.After(time.Second):
				t.Fatal("event waited for a timer that never fired")
			}
		})
	}
}

func TestLaterDeltasCoalesceAndApprovalFlushesInOrder(t *testing.T) {
	delivery := eventDelivery{}
	first := deliveryEvent("item/agentMessage/delta", "one", "turn", "first")
	if !delivery.immediate(first) || delivery.immediate(first) {
		t.Fatal("only the first content should bypass batching")
	}
	if !delivery.immediate(deliveryEvent("item/agentMessage/delta", "two", "turn", "other session")) ||
		!delivery.immediate(deliveryEvent("item/agentMessage/delta", "one", "next", "next turn")) {
		t.Fatal("first-output state crossed session or turn")
	}
	events := make(chan agent.Event, 3)
	events <- deliveryEvent("item/agentMessage/delta", "one", "turn", "third")
	events <- agent.Event{Method: "approval", ID: json.RawMessage(`1`)}
	batch := delivery.collect(deliveryEvent("item/agentMessage/delta", "one", "turn", "second"), events, make(chan time.Time), make(chan struct{}))
	if len(batch) != 3 || batch[0].Method != "item/agentMessage/delta" || batch[2].Method != "approval" {
		t.Fatal("approval reordered or delayed buffered text", batch)
	}
	tick := make(chan time.Time, 1)
	tick <- time.Time{}
	if batch := delivery.collect(first, make(chan agent.Event), tick, make(chan struct{})); len(batch) != 1 {
		t.Fatal("later text did not flush on timer")
	}
}

func TestWhitespaceDoesNotConsumeFirstVisibleOutput(t *testing.T) {
	delivery := eventDelivery{}
	if delivery.immediate(deliveryEvent("item/agentMessage/delta", "one", "turn", " \n")) ||
		delivery.immediate(agent.Event{Method: "item/started", Params: json.RawMessage(`{"item":{"type":"agentMessage","text":""}}`)}) {
		t.Fatal("empty content consumed first output")
	}
	if !delivery.immediate(deliveryEvent("item/agentMessage/delta", "one", "turn", "visible")) {
		t.Fatal("first visible output was delayed")
	}
}
