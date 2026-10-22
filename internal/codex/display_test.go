package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOversizedFinalOutputKeepsTerminalIdentityInQueue(t *testing.T) {
	params, _ := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{
		"id": "tool", "type": "commandExecution", "status": "failed", "exitCode": 9, "aggregatedOutput": strings.Repeat("中", 2*1024*1024),
	}})
	e := (Event{Method: "item/completed", Params: params}).ForDisplay()
	var p struct {
		Item struct {
			ID, Status, AggregatedOutput string
			ExitCode                     int
		}
	}
	if err := json.Unmarshal(e.Params, &p); err != nil {
		t.Fatal(err)
	}
	if !e.Truncated || len(e.Params) > MaxQueueBytes || p.Item.ID != "tool" || p.Item.Status != "failed" || p.Item.ExitCode != 9 || len(p.Item.AggregatedOutput) > MaxToolOutputBytes || !utf8.ValidString(p.Item.AggregatedOutput) {
		t.Fatal("final status lost or output unbounded")
	}
}
