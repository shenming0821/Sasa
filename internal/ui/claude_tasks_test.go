package ui

import (
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
)

func TestSubagentCommandsKeepTheirParentGroups(t *testing.T) {
	desktop := New(stubBackend{}, "test", "/project", nil)
	for index, parent := range []string{"", "parent-a", "parent-b"} {
		desktop.State.Messages = append(desktop.State.Messages, &app.Message{
			ID: []string{"main", "child-a", "child-b"}[index], Role: "tool", TurnID: "turn", Done: true,
			Tool: &app.Tool{Kind: "commandExecution", Status: "completed", Command: "printf child", ParentToolID: parent},
		})
	}
	rows := desktop.conversationRows()
	if len(rows) != 3 {
		t.Fatalf("different parent commands merged into %d rows", len(rows))
	}
	tester := native.NewTester(desktop.view, 1000, 760)
	tester.Frame()
	if !tester.HasText("子任务 · 执行了命令") {
		t.Fatal("child command ownership was not visible")
	}
	if caption := toolCaption(&app.Tool{Kind: "dynamicToolCall", Name: "Read", ParentToolID: "parent-a"}); caption != "子任务 · Read" {
		t.Fatal("child tool caption lost ownership", caption)
	}
}
