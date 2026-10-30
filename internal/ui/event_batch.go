package ui

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

type eventTurn struct {
	thread, turn string
	generation   agent.ProcessGeneration
}

type eventDelivery struct {
	visible map[eventTurn]bool
}

func (d *eventDelivery) immediate(event agent.Event) bool {
	if len(event.ID) > 0 || event.Err != nil {
		return true
	}
	switch event.Method {
	case "turn/started", "turn/completed", "fuyu/disconnected", "fuyu/eventGap", "serverRequest/resolved", "error":
		return true
	}
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Delta    string `json:"delta"`
		Item     struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
	}
	if json.Unmarshal(event.Params, &p) != nil {
		return false
	}
	content := event.Method == "item/agentMessage/delta" && strings.TrimSpace(p.Delta) != "" ||
		(event.Method == "item/started" || event.Method == "item/completed") &&
			(p.Item.Type == "agentMessage" && strings.TrimSpace(p.Item.Text) != "" ||
				p.Item.Type != "" && p.Item.Type != "agentMessage" && p.Item.Type != "reasoning")
	if !content {
		return false
	}
	if p.ThreadID == "" {
		p.ThreadID = event.Session.SessionID
	}
	key := eventTurn{p.ThreadID, p.TurnID, event.Generation}
	if d.visible[key] {
		return false
	}
	if d.visible == nil || len(d.visible) >= agent.MaxQueueEvents {
		d.visible = make(map[eventTurn]bool)
	}
	d.visible[key] = true
	return true
}

// Urgent events flush all preceding deltas in order. Subsequent text still
// coalesces, and the receiver continues to wait for one UI callback at a time.
func (d *eventDelivery) collect(first agent.Event, events <-chan agent.Event, tick <-chan time.Time, shutdown <-chan struct{}) []agent.Event {
	batch := []agent.Event{first}
	if d.immediate(first) {
		return batch
	}
	for len(batch) < 256 {
		select {
		case next, ok := <-events:
			if !ok {
				return batch
			}
			batch = append(batch, next)
			if d.immediate(next) {
				return batch
			}
		case <-tick:
			return batch
		case <-shutdown:
			return nil
		}
	}
	return batch
}
