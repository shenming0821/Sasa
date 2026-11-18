package ui

import (
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
)

func TestReplyCopyWaitsForTurnAndCombinesSegments(t *testing.T) {
	a := New(nil, "test", "/workspace", nil)
	first := &app.Message{ID: "first", Role: "assistant", TurnID: "turn", Text: "先查找资料。", Done: true}
	last := &app.Message{ID: "last", Role: "assistant", TurnID: "turn", Text: "最终结论。", Done: true}
	a.State.Messages = []*app.Message{
		{ID: "user", Role: "user", Text: "问题", Done: true}, first,
		{ID: "search", Role: "tool", TurnID: "turn", Done: true, Tool: &app.Tool{Kind: "webSearch", Status: "completed"}}, last,
	}
	a.State.Busy, a.State.TurnID = true, "turn"
	tester := native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Children(func() {
			for _, message := range a.State.Messages {
				a.message(c, message)
			}
		})
	}, 700, 700)
	if _, found := tester.Find("复制回复"); found {
		t.Fatal("copy visible before turn completion")
	}
	a.State.Busy = false
	tester.Frame()
	if a.replyCopyText(first) != "" || a.replyCopyText(last) != "先查找资料。\n\n最终结论。" {
		t.Fatal("copy should belong only to the final segment and include all reply text")
	}
	if err := tester.Click("复制回复"); err != nil {
		t.Fatal(err)
	}
	if tester.Clipboard() != "先查找资料。\n\n最终结论。" || a.copiedID != last.ID {
		t.Fatal("copy did not include the whole turn")
	}
	last.Done = false
	if a.replyCopyText(first) != "" || a.replyCopyText(last) != "" {
		t.Fatal("unfinished reply must not expose a copy action")
	}
}

func TestReplyCopyKeepsTurnsSeparate(t *testing.T) {
	for _, ids := range []bool{false, true} {
		a := New(nil, "test", "/workspace", nil)
		first := &app.Message{ID: "first", Role: "assistant", Text: "上一轮", Done: true}
		last := &app.Message{ID: "last", Role: "assistant", Text: "当前轮", Done: true}
		if ids {
			first.TurnID, last.TurnID = "old", "current"
		}
		a.State.Messages = []*app.Message{first, {ID: "user", Role: "user", Text: "下一问", Done: true}, last}
		a.State.Busy, a.State.TurnID = true, "current"
		if a.replyCopyText(first) != "上一轮" || a.replyCopyText(last) != "" {
			t.Fatalf("turn isolation failed, ids=%v", ids)
		}
		a.State.Busy = false
		if a.replyCopyText(last) != "当前轮" {
			t.Fatal("copy mixed adjacent turns")
		}
		if ids {
			a.State.Messages = []*app.Message{first, last}
			if a.replyCopyText(first) != "上一轮" || a.replyCopyText(last) != "当前轮" {
				t.Fatal("history turn IDs did not separate replies")
			}
		}
	}
}
