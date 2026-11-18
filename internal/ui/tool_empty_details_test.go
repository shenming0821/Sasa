package ui

import (
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
)

func TestToolWithoutDetailsHasNoDisclosure(t *testing.T) {
	for _, kind := range []string{"webSearch", "mcpToolCall", "dynamicToolCall"} {
		t.Run(kind, func(t *testing.T) {
			a := New(nil, "test", "/workspace", nil)
			tool := &app.Tool{Kind: kind, Name: "search", Status: "inProgress", Input: " \n", Expanded: true}
			tester := native.NewTester(func(c *native.Context) { a.toolDisclosure(c, tool) }, 500, 500)
			if _, found := tester.Find("工具记录 search箭头"); found || tester.HasText("暂无详情") || !tester.HasText("执行中") {
				t.Fatal("empty tool exposed a disclosure or lost its status", tester.Texts())
			}
			tool.Status, tool.Input, tool.Expanded = "completed", `{"query":"资料"}`, false
			tester.Frame()
			if _, found := tester.Find("工具记录 search箭头"); !found {
				t.Fatal("new details did not restore disclosure")
			}
			if err := tester.Click("工具记录 search"); err != nil {
				t.Fatal(err)
			}
			tester.Advance(300 * time.Millisecond)
			if !tool.Expanded || !tester.HasText("输入") {
				t.Fatal("available details did not expand", tester.Texts())
			}
			tool.Input, tool.Output = "", ""
			tester.Frame()
			if _, found := tester.Find("工具记录 search箭头"); found || tester.HasText("输入") {
				t.Fatal("stale expanded state kept empty details visible")
			}
		})
	}
}
