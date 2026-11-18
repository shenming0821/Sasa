package app

import (
	"encoding/json"
	"testing"

	"github.com/shenming0821/Sasa/internal/agent"
)

func TestCompactionItemLiveAndHistory(t *testing.T) {
	state := New("/project")
	state.ThreadID = "thread"
	apply(&state, "turn/started", `{"threadId":"thread","turn":{"id":"compact"}}`)
	apply(&state, "item/started", `{"threadId":"thread","turnId":"compact","item":{"id":"item","type":"contextCompaction"}}`)
	if len(state.Messages) != 1 || state.Messages[0].Text != "正在压缩上下文" {
		t.Fatal(state.Messages)
	}
	apply(&state, "item/completed", `{"threadId":"thread","turnId":"compact","item":{"id":"item","type":"contextCompaction"}}`)
	if !state.Busy || state.Messages[0].Text != "上下文已压缩" {
		t.Fatal("item completion ended turn or lost result")
	}
	apply(&state, "turn/completed", `{"threadId":"thread","turn":{"id":"compact","status":"completed"}}`)
	history, unfinished, err := History("/project", "thread", []agent.HistoryTurn{{
		ID: "compact", Status: "completed", Items: []json.RawMessage{json.RawMessage(`{"id":"item","type":"contextCompaction"}`)},
	}})
	if err != nil || unfinished || len(history.Messages) != 1 || history.Messages[0].Text != state.Messages[0].Text || history.Busy {
		t.Fatal("history did not preserve compaction result", history, err)
	}
}

func TestInterruptedCompactionDoesNotKeepRunningCaption(t *testing.T) {
	state := New("/project")
	state.ThreadID = "thread"
	apply(&state, "turn/started", `{"threadId":"thread","turn":{"id":"compact"}}`)
	apply(&state, "item/started", `{"threadId":"thread","turnId":"compact","item":{"id":"item","type":"contextCompaction"}}`)
	apply(&state, "turn/completed", `{"threadId":"thread","turn":{"id":"compact","status":"interrupted"}}`)
	if state.Messages[0].Text != "上下文压缩未完成" || !state.Messages[0].Done {
		t.Fatal(state.Messages[0])
	}
}
