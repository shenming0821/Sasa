package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
)

func doubleClickTabBlank(t *testing.T, tester *native.Tester, paneID string) {
	t.Helper()
	tester.Frame()
	tester.Frame()
	blank, found := tester.Find("新建标签页空白处 " + paneID)
	if !found || blank.W < 8 || blank.H < 8 {
		t.Fatalf("tab bar has no usable blank area: %+v", blank)
	}
	tester.Advance(time.Second)
	tester.ClickAt(blank.X+blank.W/2, blank.Y+blank.H/2)
	tester.ClickAt(blank.X+blank.W/2, blank.Y+blank.H/2)
	tester.Frame()
}

func TestTabBlankDoubleClickAppendsEmptyTabInItsPane(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			first := a.session
			first.State.Draft, first.State.Busy = "keep draft", true
			right := a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
			a.newSessionForAgent("/project", agent.Claude, "/claude")
			second := a.session
			tester := native.NewTester(a.view, 1800, 900)
			tester.SetDark(dark)
			left := paneLeaves(a.layoutFor("/project").Root)[0]
			a.focusPane("/project", left)
			tester.Frame()
			blank, _ := tester.Find("新建标签页空白处 " + right.ID)
			tester.ClickAt(blank.X+blank.W/2, blank.Y+blank.H/2)
			if len(a.tabs) != 2 {
				t.Fatal("single click created a tab")
			}
			doubleClickTabBlank(t, tester, right.ID)
			if len(a.tabs) != 3 || len(right.Tabs) != 2 || len(left.Tabs) != 1 || !a.isEmptyTab(right.Active) || a.activeTab != right.Active {
				t.Fatalf("double click did not append and select in its pane: tabs=%+v right=%+v", a.tabs, right)
			}
			if !first.open || !first.State.Busy || first.State.Draft != "keep draft" || !second.open || len(a.sessions) != 2 || len(a.terminals) != 0 {
				t.Fatal("empty tab changed existing resources or started content")
			}
			if !tester.HasText("选择面板内容") {
				t.Fatal("empty tab did not show the content picker")
			}
			if err := tester.Click(fmt.Sprintf("会话标签 %d", second.id)); err != nil {
				t.Fatal(err)
			}
			if a.session != second || a.activeTab != sessionTabID(second.id) || len(right.Tabs) != 2 {
				t.Fatal("switching back lost a tab or selected another session")
			}
		})
	}
}

func TestTabBlankDoubleClickPreservesImplicitEmptyTabAndRestores(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	store := imageStore(t, a)
	a.closeSession(a.session)
	pane := focusedPane(a.layoutFor("/project"))
	tester := native.NewTester(a.view, 1400, 900)
	doubleClickTabBlank(t, tester, pane.ID)
	if len(a.tabs) != 2 || len(pane.Tabs) != 2 || !a.isEmptyTab(pane.Active) {
		t.Fatal("double click replaced the existing implicit empty tab")
	}
	selected := a.activeTab
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if len(restored.tabs) != 2 || restored.activeTab != selected || !slices.Equal(focusedPane(restored.layoutFor("/project")).Tabs, pane.Tabs) {
		t.Fatal("restart lost empty tabs, order or selection")
	}
	restored.newEmptyTab("/project", focusedPane(restored.layoutFor("/project")))
	if len(restored.tabs) != 3 || restored.activeTab == selected {
		t.Fatal("creating after restart reused an existing tab ID")
	}
	restored.closeCurrentPage()
	if len(restored.tabs) != 2 || !restored.isEmptyTab(restored.activeTab) {
		t.Fatal("closing empty tab did not select its neighbor")
	}
}

func TestEmptyTabContentCreationReplacesSelectedTab(t *testing.T) {
	for _, label := range []string{"新建 Codex 会话", "新建 Claude 会话", "新建终端"} {
		t.Run(label, func(t *testing.T) {
			a := New(stubBackend{}, "test", "/background", nil)
			if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
				t.Fatal(err)
			}
			a.terminalFactory = func(string) (terminalConnection, error) { return &testTerminal{}, nil }
			t.Cleanup(a.closeWorkspaceResources)
			pane := focusedPane(a.layoutFor("/project"))
			a.newEmptyTab("/project", pane)
			emptyID := a.activeTab
			tester := native.NewTester(a.view, 1400, 900)
			if err := tester.Click(label); err != nil {
				t.Fatal(err)
			}
			if a.isEmptyTab(emptyID) || len(pane.Tabs) != 2 || a.isEmptyTab(a.activeTab) {
				t.Fatal("choosing content did not replace exactly the selected empty tab")
			}
			if a.workspaceCwd() != "/project" {
				t.Fatal("content was created in the background session directory")
			}
		})
	}
}

func TestEmptyTabUsesItsWorkspaceInsteadOfBackgroundSession(t *testing.T) {
	a := New(stubBackend{}, "test", "/background", nil)
	a.newEmptyTab("/project", focusedPane(a.layoutFor("/project")))
	if a.workspaceCwd() != "/project" || a.State.Cwd != "/background" {
		t.Fatal("empty tab used the background session directory")
	}
}

func TestTabBlankDoubleClickPreservesFileAndTerminal(t *testing.T) {
	a, queue, root := p4App(t)
	tester := native.NewTester(a.view, 1600, 900)
	a.openFile(root, filepath.Join(root, "README.md"))
	p4Wait(t, tester, queue, func() bool { return a.currentFile().loaded })
	document := a.currentFile()
	a.terminalFactory = func(string) (terminalConnection, error) { return &testTerminal{}, nil }
	t.Cleanup(a.closeWorkspaceResources)
	a.newTerminal()
	terminal := a.currentTerminal()
	pane := focusedPane(a.layoutFor(root))
	doubleClickTabBlank(t, tester, pane.ID)
	if a.files[document.id] != document || a.terminals[terminal.id] != terminal || terminal.connection.Exited() || !slices.Contains(pane.Tabs, document.id) || !slices.Contains(pane.Tabs, terminal.id) {
		t.Fatal("new empty tab closed a file or terminal")
	}
}
