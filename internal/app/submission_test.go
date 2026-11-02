package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shenming0821/Fuyu/internal/attachment"
)

func TestInterruptedSubmissionReconcilesDelivery(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		for _, edited := range []bool{false, true} {
			state := New("/project")
			state.ThreadID = "thread"
			image := attachment.Image{Path: "/original.png"}
			state.DraftImages = []attachment.Image{image}
			state.Begin("say ok")
			apply(&state, "turn/started", `{"threadId":"thread","turn":{"id":"turn"}}`)
			if edited {
				state.Draft = "new task"
				state.DraftImages = []attachment.Image{{Path: "/new.png"}}
			}
			apply(&state, "turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)
			if !state.NeedsSnapshot || state.Busy || state.PendingSubmission() == nil {
				t.Fatal("interruption claimed delivery before checking native input")
			}
			items := []json.RawMessage{}
			if delivered {
				items = append(items, json.RawMessage(`{"id":"native-user","type":"userMessage","content":[{"type":"text","text":"say ok"}]}`))
			}
			raw, _ := json.Marshal(map[string]any{"thread": map[string]any{"id": "thread", "turns": []any{map[string]any{"id": "turn", "status": "interrupted", "items": items}}}})
			if err := state.Reconcile(raw); err != nil {
				t.Fatal(err)
			}
			if len(state.Messages) != 1 || state.PendingSubmission() != nil {
				t.Fatal("delivery check duplicated the prompt or left it unresolved")
			}
			if delivered {
				if state.Messages[0].Delivery != "sent" || !edited && state.Draft != "" {
					t.Fatal("accepted input was restored for duplicate execution")
				}
			} else if state.Messages[0].Delivery != "unsent" || !strings.Contains(state.Notice, "未发送") {
				t.Fatal("missing input still appears sent")
			} else if !edited && (state.Draft != "say ok" || len(state.DraftImages) != 1 || state.DraftImages[0] != image) {
				t.Fatal("unsent text or image was lost")
			}
			if edited && (state.Draft != "new task" || state.DraftImages[0].Path != "/new.png") {
				t.Fatal("delivery check overwrote a newer draft")
			}
			if !delivered {
				state.Begin("say ok")
				if strings.Contains(state.Notice, "未发送") {
					t.Fatal("resubmission retained the previous unsent notice")
				}
			}
		}
	}
}

func TestSubmissionRequiresNativeUserItem(t *testing.T) {
	state := New("/project")
	state.ThreadID = "thread"
	state.Begin("say ok")
	apply(&state, "turn/started", `{"threadId":"thread","turn":{"id":"turn"}}`)
	if state.PendingSubmission() == nil {
		t.Fatal("turn/start was treated as input acknowledgement")
	}
	apply(&state, "item/started", `{"threadId":"thread","turnId":"turn","item":{"id":"native-user","type":"userMessage","content":[{"type":"text","text":"say ok"}]}}`)
	apply(&state, "turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)
	if state.PendingSubmission() != nil || state.NeedsSnapshot || state.Messages[0].Delivery != "sent" {
		t.Fatal("native input acknowledgement was ignored")
	}
}

func TestIncompleteSubmissionSnapshotKeepsUncertainty(t *testing.T) {
	for _, raw := range []string{
		`{"thread":{"id":"thread","turns":[{"id":"turn","status":"interrupted"}]}}`,
		`{"thread":{"id":"thread","turns":[{"id":"turn","status":"interrupted","itemsView":"summary","items":[]}]}}`,
		`{"thread":{"id":"thread","turns":[{"id":"turn","status":"interrupted","items":[{}]}]}}`,
		`{"thread":{"id":"thread","turns":[{"id":"other","status":"interrupted","items":[]}]}}`,
	} {
		state := New("/project")
		state.ThreadID = "thread"
		state.Begin("say ok")
		apply(&state, "turn/started", `{"threadId":"thread","turn":{"id":"turn"}}`)
		if err := state.Reconcile(json.RawMessage(raw)); err == nil || state.PendingSubmission() == nil || state.Draft != "" {
			t.Fatalf("incomplete snapshot falsely resolved delivery: %s", raw)
		}
	}
}
