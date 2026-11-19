package ui

import (
	"fmt"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/storage"
)

func TestTabSelectionKeepsTitleAndCloseButtonGeometry(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			a.session.saved.CustomTitle = "会话标题"
			a.tabs = append(a.tabs,
				storage.Tab{ID: "file", Kind: "file", Workspace: "/project", Path: "/project/main.go"},
				storage.Tab{ID: "diff", Kind: "diff", Workspace: "/project", Path: "/project/changes.go", Compare: "更改"},
				storage.Tab{ID: "terminal", Kind: "terminal", Workspace: "/project"},
				storage.Tab{ID: "empty", Kind: "empty", Workspace: "/project"},
			)
			pane := focusedPane(a.layoutFor("/project"))
			tester := native.NewTester(func(c *native.Context) {
				a.withPane("/project", pane, func() {
					native.Column(c).Fill().Children(func() { a.sessionTabs(c, true, true) })
				})
			}, 1200, 80)
			tester.SetDark(dark)
			geometry := func() map[string]native.Rect {
				result := map[string]native.Rect{}
				for _, tab := range a.tabs {
					label := a.tabTitle(tab)
					bounds, found := tester.Find(label)
					if !found {
						t.Fatalf("missing title %q", label)
					}
					result[label] = bounds
				}
				for _, label := range []string{"关闭会话标签 1", "关闭文件标签 /project/main.go", "关闭文件标签 /project/changes.go", "关闭终端标签 terminal", "关闭新标签页 empty"} {
					bounds, found := tester.Find(label)
					if !found {
						t.Fatalf("missing close button %q", label)
					}
					result[label] = bounds
				}
				return result
			}
			before := geometry()
			for _, tab := range a.tabs[1:] {
				pane.Active, a.activeTab = tab.ID, tab.ID
				tester.Frame()
				tester.Frame()
				for label, after := range geometry() {
					if after != before[label] {
						t.Fatalf("selecting %s moved %q: before=%+v after=%+v", tab.Kind, label, before[label], after)
					}
				}
			}
		})
	}
}
