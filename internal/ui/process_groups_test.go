package ui

import (
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
)

func TestInterleavedCommandsAndChangesShareRowsUntilBody(t *testing.T) {
	for _, tools := range []string{"commandExecution", "dynamicToolCall"} {
		t.Run(tools, func(t *testing.T) {
			a := New(nil, "test", "/workspace", nil)
			command := func(id string) *app.Message {
				m := commandMessage(id, "completed")
				if tools == "dynamicToolCall" {
					m = readMessage(id)
				}
				return m
			}
			one, two, three := command("one"), command("two"), command("three")
			two.Tool.Status = "failed"
			first := changeMessage("first", "turn", "a.go", "@@ -1 +1 @@\n-old\n+middle\n")
			second := changeMessage("second", "turn", "a.go", "@@ -1 +1 @@\n-middle\n+final\n")
			third := changeMessage("third", "turn", "b.go", "@@ -1 +1 @@\n-old\n+new\n")
			a.State.Messages = []*app.Message{one, first, two, second, {ID: "placeholder", Role: "assistant"}, three, third}
			rows := a.conversationRows()
			if len(rows) != 2 || rows[0].message != one || len(rows[0].commands) != 3 || rows[1].message != first || len(rows[1].changes) != 3 {
				t.Fatalf("interleaved operations were split: %+v", rows)
			}
			if caption, failed, _ := commandSummary(rows[0].commands, false); !failed || caption != "执行了命令" {
				t.Fatal("merged command group should omit failure summary and preserve failure state", caption)
			}
			summary := a.summarizeChanges(rows[1].changes)
			if len(summary.Files) != 2 || summary.Added != 2 || summary.Deleted != 2 {
				t.Fatalf("repeated file edits were not combined: %+v", summary)
			}
			for child, anchor := range map[string]string{"three": "one", "second": "first"} {
				a.saved.Anchor, a.saved.AnchorOffset = child, 35
				a.restoreReading()
				if a.saved.Anchor != anchor || a.saved.AnchorOffset != 0 {
					t.Fatalf("lost saved anchor for %s: %+v", child, a.saved)
				}
			}
			*a.commandGroupOpen("one") = true
			body := &app.Message{ID: "body", Role: "assistant", Text: "接下来验证", TurnID: "turn"}
			a.State.Messages = append(a.State.Messages, body, command("four"), changeMessage("fourth", "turn", "c.go", "@@ -1 +1 @@\n-old\n+new\n"))
			rows = a.conversationRows()
			if len(rows) != 4 || rows[1].message != body || len(rows[2].commands) != 1 || len(rows[3].changes) != 4 || !*a.commandGroupOpen("one") {
				t.Fatalf("turn edits did not collect into the trailing card: %+v", rows)
			}
		})
	}
}

func TestInterleavedProcessGroupsKeepBoundaries(t *testing.T) {
	for _, boundary := range []string{"body", "user", "approval", "turn", "parent", "failed-edit", "running-edit", "declined-edit"} {
		t.Run(boundary, func(t *testing.T) {
			a := New(nil, "test", "/workspace", nil)
			first := changeMessage("first", "turn", "a.go", "@@ -1 +1 @@\n-a\n+b\n")
			next := changeMessage("next", "turn", "b.go", "@@ -1 +1 @@\n-a\n+b\n")
			command := commandMessage("after", "completed")
			var middle *app.Message
			switch boundary {
			case "body", "user":
				role := "assistant"
				if boundary == "user" {
					role = "user"
				}
				middle = &app.Message{ID: "boundary", Role: role, Text: "正文", TurnID: "turn"}
			case "approval":
				middle = &app.Message{ID: "boundary", Role: "interaction", Interaction: &app.Interaction{}}
			case "turn":
				next.TurnID, command.TurnID = "next", "next"
			case "parent":
				next.Tool.ParentToolID, command.Tool.ParentToolID = "child", "child"
			default:
				middle = changeMessage("boundary", "turn", "failed.go", "")
				middle.Tool.Status = map[string]string{"failed-edit": "failed", "running-edit": "inProgress", "declined-edit": "declined"}[boundary]
			}
			a.State.Messages = []*app.Message{commandMessage("before", "completed"), first}
			if middle != nil {
				a.State.Messages = append(a.State.Messages, middle)
			}
			a.State.Messages = append(a.State.Messages, next, command)
			rows := a.conversationRows()
			if boundary == "user" {
				// A user message ends the turn: each side keeps its own trailing card.
				if len(rows) != 5 || len(rows[0].commands) != 1 || len(rows[1].changes) != 1 || rows[1].changes[0] != first || rows[2].message != middle || len(rows[3].commands) != 1 || len(rows[4].changes) != 1 || rows[4].changes[0] != next {
					t.Fatalf("user boundary did not split summaries: %+v", rows)
				}
				return
			}
			want := 3
			if middle != nil {
				want++
			}
			// Successful edits always collect into one card at the end;
			// other records keep their own rows and boundaries.
			last := rows[len(rows)-1]
			if len(rows) != want || len(rows[0].commands) != 1 || len(rows[want-2].commands) != 1 || len(last.changes) != 2 || last.changes[0] != first || last.changes[1] != next {
				t.Fatalf("%s boundary kept edits inline: %+v", boundary, rows)
			}
			if middle != nil && rows[1].message != middle {
				t.Fatalf("%s boundary lost its own row: %+v", boundary, rows)
			}
		})
	}
}

func TestInterleavedProcessGroupsRenderOnceAndRefresh(t *testing.T) {
	a := New(nil, "test", "/workspace", nil)
	one := commandMessage("one", "completed")
	two := commandMessage("two", "failed")
	first := changeMessage("first", "turn", "a.go", "@@ -1 +1 @@\n-old\n+new\n")
	second := changeMessage("second", "turn", "b.go", "@@ -1 +1 @@\n-old\n+new\n")
	a.State.Messages = []*app.Message{one, first}
	tt := native.NewTester(a.view, 900, 900)
	a.State.Messages = append(a.State.Messages, two, second)
	tt.Frame()
	commands, cards := 0, 0
	for _, text := range tt.Texts() {
		if text == "执行了命令" {
			commands++
		}
		if text == "已编辑 2 个文件" {
			cards++
		}
	}
	if commands != 1 || cards != 1 {
		t.Fatalf("expected one command row and one edit card: %v", tt.Texts())
	}
	// A restored session projects the same groups from native message order.
	restored := New(nil, "test", "/workspace", nil)
	restored.State.Messages = a.State.Messages
	rows := restored.conversationRows()
	if len(rows) != 2 || len(rows[0].commands) != 2 || len(rows[1].changes) != 2 {
		t.Fatalf("restored grouping differs: %+v", rows)
	}
}
