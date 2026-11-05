package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func TestPaneNestedLayoutAndTabMovePreservesTasks(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	store := imageStore(t, a)
	first := a.session
	first.State.Draft, first.State.Busy = "keep running", true
	left := focusedPane(a.layoutFor("/project"))
	right := a.splitPane("/project", left, "horizontal")
	a.newSessionForAgent("/project", agent.Claude, "/claude")
	second := a.session
	second.State.Draft = "claude draft"
	bottom := a.splitPane("/project", right, "vertical")
	right = paneLeaves(a.layoutFor("/project").Root)[1]
	layout := a.layoutFor("/project")
	if layout.Root.Axis != "horizontal" || layout.Root.Second.Axis != "vertical" || layout.Root.Second.Second != bottom || layout.Root.Ratio != 50 {
		t.Fatal("nested split did not preserve existing panes")
	}
	a.terminalFactory = func(string) (terminalConnection, error) { return &testTerminal{}, nil }
	t.Cleanup(a.closeWorkspaceResources)
	a.newTerminal()
	terminal := a.currentTerminal()
	if bottom.Active != terminal.id || right.Active != sessionTabID(second.id) {
		t.Fatal("content was opened in the wrong pane")
	}
	if !a.movePaneTab("/project", right, paneTabDrag{Workspace: "/project", Pane: bottom.ID, Tab: terminal.id}) {
		t.Fatal("terminal tab did not move")
	}
	if len(bottom.Tabs) != 0 || bottom.Active != "" || right.Active != terminal.id || terminal.connection.Exited() {
		t.Fatal("moving terminal closed its shell or left a duplicate")
	}
	leaves := paneLeaves(a.layouts["/project"].Root)
	if !a.movePaneTab("/project", right, paneTabDrag{Workspace: "/project", Pane: leaves[0].ID, Tab: sessionTabID(first.id)}) {
		t.Fatal("running session tab did not move")
	}
	if len(leaves) != 3 || len(leaves[0].Tabs) != 0 || len(right.Tabs) != 3 || !first.State.Busy || !first.open || first.stopOnClose || first.State.Draft != "keep running" || second.State.Draft != "claude draft" {
		t.Fatal("tab move changed resources, drafts or pane layout")
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.layouts["/project"].Focused != right.ID || !slices.Equal(paneLeaves(restored.layouts["/project"].Root)[1].Tabs, right.Tabs) {
		t.Fatal("restart lost moved tabs or focus")
	}
}

func TestPaneTabDragDropAndCancel(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			first := a.session
			a.newSession("/project")
			backup := a.session
			right := a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
			a.newSessionForAgent("/project", agent.Claude, "/claude")
			second := a.session
			left := paneLeaves(a.layouts["/project"].Root)[0]
			first.State.Draft, first.State.Busy = "preserved draft", true
			tester := native.NewTester(a.view, 1600, 1000)
			tester.SetDark(dark)
			drag := func(target *storage.PaneNode, cancel bool) {
				t.Helper()
				from, found := tester.Find(fmt.Sprintf("会话标签 %d", first.id))
				if !found {
					t.Fatal("source tab missing")
				}
				bounds := a.paneBounds[target.ID]
				x, y := from.X+30, from.Y+from.H/2
				destX, destY := bounds.X+bounds.W/2, bounds.Y+100
				tester.Press(x, y)
				tester.Move(x+15, y)
				tester.Move(destX, destY)
				if cancel {
					tester.Key(0, native.KeyEscape)
				}
				tester.Release(destX, destY)
				tester.Frame()
			}
			drag(right, true)
			if !slices.Contains(left.Tabs, sessionTabID(first.id)) || right.Active != sessionTabID(second.id) {
				t.Fatal("Escape moved a tab")
			}
			drag(left, false)
			if len(left.Tabs) != 2 || len(right.Tabs) != 1 {
				t.Fatal("same-pane drag changed ownership")
			}
			drag(right, false)
			if slices.Contains(left.Tabs, sessionTabID(first.id)) || right.Active != sessionTabID(first.id) || left.Active != sessionTabID(backup.id) || a.session != first {
				t.Fatalf("drop did not move and focus exactly one tab: left=%+v right=%+v", left, right)
			}
			if !first.State.Busy || first.stopOnClose || !first.open || first.State.Draft != "preserved draft" || len(a.sessions) != 3 {
				t.Fatal("drop stopped or recreated the session")
			}
			if a.movePaneTab("/project", left, paneTabDrag{Workspace: "/other", Pane: right.ID, Tab: sessionTabID(first.id)}) || a.movePaneTab("/project", left, paneTabDrag{Workspace: "/project", Pane: right.ID, Tab: "missing"}) {
				t.Fatal("accepted an invalid drag")
			}
			if err := tester.RightClick("会话面板 " + right.ID); err != nil {
				t.Fatal(err)
			}
			if slices.Contains(tester.Menu(), "合并到相邻面板") {
				t.Fatal("obsolete merge action is still visible")
			}
			tester.Key(0, native.KeyEscape)
			a.terminalFactory = func(string) (terminalConnection, error) { return &testTerminal{}, nil }
			t.Cleanup(a.closeWorkspaceResources)
			a.newTerminal()
			terminal := a.currentTerminal()
			tester.Frame()
			from, found := tester.Find("终端标签 " + terminal.id)
			if !found {
				t.Fatal("terminal tab missing")
			}
			bounds := a.paneBounds[left.ID]
			tester.Press(from.X+30, from.Y+15)
			tester.Move(from.X+45, from.Y+15)
			tester.Move(bounds.X+100, bounds.Y+100)
			if !a.paneTabDragging || !terminal.connection.(*testTerminal).hidden.Load() {
				t.Fatal("native terminal was not hidden during tab drag")
			}
			tester.Release(bounds.X+100, bounds.Y+100)
			tester.Frame()
			if a.currentTerminal() != terminal || terminal.connection.Exited() || a.paneTabDragging || left.Active != terminal.id {
				t.Fatal("terminal drag lost the shell or retained drag state")
			}
		})
	}
}

func TestPaneSnapshotMigrationRepairAndFolderSwitch(t *testing.T) {
	a := New(stubBackend{}, "test", "/first", nil)
	store := imageStore(t, a)
	first := a.session
	left := focusedPane(a.layoutFor("/first"))
	right := a.splitPane("/first", left, "horizontal")
	a.newSession("/first")
	second := a.session
	a.layouts["/first"].Root.Ratio = 37
	a.newSession("/second")
	a.splitPane("/second", focusedPane(a.layoutFor("/second")), "vertical")
	if !a.restoreFolderTab("/first") || a.session != second || a.activeTab != sessionTabID(second.id) {
		t.Fatal("folder did not restore its focused pane")
	}
	a.focusPane("/first", paneLeaves(a.layouts["/first"].Root)[0])
	if a.session != first {
		t.Fatal("left pane lost original session")
	}
	a.focusPane("/first", right)
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.layouts["/first"].Root.Ratio != 37 || restored.activeTab != sessionTabID(second.id) || len(paneLeaves(restored.layouts["/second"].Root)) != 2 {
		t.Fatal("restart lost split proportions, tabs or focus")
	}
	snapshot := a.snapshot()
	snapshot.WorkspaceLayouts = nil
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	legacy := New(stubBackend{}, "test", "", nil)
	if err := legacy.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if len(paneLeaves(legacy.layouts["/first"].Root)) != 1 || legacy.activeTab != sessionTabID(second.id) {
		t.Fatal("legacy single-pane selection did not migrate")
	}
	snapshot.WorkspaceLayouts = map[string]storage.WorkspaceLayout{
		"/first": {Root: &storage.PaneNode{ID: "broken", Axis: "vertical"}, Focused: "missing"},
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	repaired := New(stubBackend{}, "test", "", nil)
	if err := repaired.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if len(paneLeaves(repaired.layouts["/first"].Root)) != 1 || len(repaired.tabs) != len(a.tabs) {
		t.Fatal("invalid layout did not fall back while keeping tabs")
	}
}

func TestPaneMenusCreateAgentsAndRouteInput(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
				t.Fatal(err)
			}
			leftSession := a.session
			var submitted []int
			a.submit = func() { submitted = append(submitted, a.session.id) }
			tester := native.NewTester(a.view, 1600, 1000)
			tester.SetDark(dark)
			paneID := a.layouts["/project"].Focused
			if err := tester.RightClick("会话面板 " + paneID); err != nil {
				t.Fatal(err)
			}
			if err := tester.ChooseMenuItem("左右分屏"); err != nil {
				t.Fatal(err, tester.Menu())
			}
			if err := tester.Click("Claude Code"); err != nil {
				t.Fatal(err)
			}
			rightSession := a.session
			if rightSession.kind != agent.Claude || rightSession.State.Cwd != "/project" {
				t.Fatal("pane chooser did not create a Claude session in its folder")
			}
			tester.Frame()
			input, found := tester.Find("任务输入")
			if !found {
				t.Fatal("missing input")
			}
			tester.ClickAt(input.X+20, input.Y+10)
			tester.Type("left draft")
			tester.Key(0, native.KeyEnter)
			leaves := paneLeaves(a.layouts["/project"].Root)
			leftBounds, rightBounds := a.paneBounds[leaves[0].ID], a.paneBounds[leaves[1].ID]
			tester.ClickAt(input.X-leftBounds.X+rightBounds.X+20, input.Y+10)
			tester.Type("right draft")
			tester.Key(0, native.KeyEnter)
			if !reflect.DeepEqual(submitted, []int{leftSession.id, rightSession.id}) || leftSession.State.Draft != "left draft" || rightSession.State.Draft != "right draft" {
				t.Fatalf("pane callbacks crossed owners: submitted=%v left=%q right=%q", submitted, leftSession.State.Draft, rightSession.State.Draft)
			}
			if a.session != rightSession {
				t.Fatal("keyboard focus did not select its pane")
			}
			tester.Key(native.Cmd, native.KeyW)
			if rightSession.open || !leftSession.open || leftSession.State.Draft != "left draft" {
				t.Fatal("Command-W closed the wrong session")
			}
		})
	}
}

func TestPaneSessionNavigationAndFileTarget(t *testing.T) {
	a, queue, root := p4App(t)
	first := a.session
	first.State.Draft, first.State.Busy = "busy draft", true
	a.newSession(root)
	second := a.session
	left := focusedPane(a.layoutFor(root))
	right := a.splitPane(root, left, "horizontal")
	tester := native.NewTester(a.view, 1600, 1000)
	source := paneLeaves(a.layouts[root].Root)[0]
	if !a.movePaneTab(root, right, paneTabDrag{Workspace: root, Pane: source.ID, Tab: sessionTabID(first.id)}) {
		t.Fatal("session did not move to target pane")
	}
	tester.Frame()
	leaves := paneLeaves(a.layouts[root].Root)
	if slices.Contains(leaves[0].Tabs, sessionTabID(first.id)) || right.Active != sessionTabID(first.id) || !first.State.Busy || first.stopOnClose {
		t.Fatalf("moving session changed the task or left a duplicate: left=%+v right=%+v busy=%t stopped=%t", leaves[0], right, first.State.Busy, first.stopOnClose)
	}
	if err := tester.Click(fmt.Sprintf("切换会话 %d", second.id)); err != nil {
		t.Fatal(err)
	}
	if a.layouts[root].Focused != leaves[0].ID {
		t.Fatal("selecting an open session did not reveal its pane")
	}
	a.openFile(root, root+"/internal/main.go")
	p4Wait(t, tester, queue, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	file := a.currentFile()
	if leaves[0].Active != file.id || right.Active != sessionTabID(first.id) {
		t.Fatal("file opening changed another pane")
	}
	a.focusPane(root, right)
	a.openFile(root, file.path)
	if right.Active != file.id || slices.Contains(leaves[0].Tabs, file.id) {
		t.Fatal("opening existing file did not move its single view to the focused pane")
	}
	tester.Frame()
	from, found := tester.Find("文件标签 " + file.path)
	if !found {
		t.Fatal("file tab is missing")
	}
	bounds := a.paneBounds[leaves[0].ID]
	tester.Press(from.X+30, from.Y+15)
	tester.Move(from.X+45, from.Y+15)
	tester.Move(bounds.X+100, bounds.Y+100)
	tester.Release(bounds.X+100, bounds.Y+100)
	tester.Frame()
	if a.currentFile() != file || leaves[0].Active != file.id || slices.Contains(right.Tabs, file.id) {
		t.Fatal("dragging a file tab recreated its view or lost ownership")
	}
}

func TestPaneEmptyContentLayout(t *testing.T) {
	for _, width := range []int{640, 1000, 1600} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/dark=%t", width, dark), func(t *testing.T) {
				a := New(stubBackend{}, "test", "/project", nil)
				pane := a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
				tester := native.NewTester(a.view, width, 760)
				tester.SetDark(dark)
				bounds := a.paneBounds[pane.ID]
				if !tester.HasText("新标签页") || !tester.HasText("project") {
					t.Fatal("empty pane is missing its new tab or folder name")
				}
				var first native.Rect
				for index, label := range []string{"Codex", "Claude Code", "终端"} {
					button, found := tester.Find(label)
					if !found || button.W < 240 || button.H != 56 || button.X < bounds.X || button.X+button.W > bounds.X+bounds.W {
						t.Fatalf("%s is missing or clipped: button=%+v pane=%+v", label, button, bounds)
					}
					if index == 0 {
						first = button
					} else if button.X != first.X || button.W != first.W || button.Y != first.Y+float32(index*57) {
						t.Fatalf("content actions are not uniformly aligned: first=%+v current=%+v", first, button)
					}
				}
				centerDelta := first.X + first.W/2 - bounds.X - bounds.W/2
				if centerDelta < -1 || centerDelta > 1 || !tester.HasText("也可以将其他面板的页签拖到这里") || tester.HasText("选择已有会话") {
					t.Fatal("empty content is not centered or retains the removed chooser")
				}
				if err := tester.RightClick("会话面板 " + pane.ID); err != nil {
					t.Fatal(err)
				}
				if slices.Contains(tester.Menu(), "选择已有会话") {
					t.Fatal("removed chooser is still in the pane menu")
				}
			})
		}
	}
}

func TestPaneCloseShortcutFollowsBlankAreaFocus(t *testing.T) {
	for _, useMenu := range []bool{false, true} {
		t.Run(fmt.Sprintf("menu=%t", useMenu), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			a.dispatch = func(work func()) { work() }
			leftSession := a.session
			right := a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
			a.newSession("/project")
			rightSession := a.session
			tester := native.NewTester(a.view, 1600, 1000)
			tester.Frame()
			leaves := paneLeaves(a.layouts["/project"].Root)
			leftBounds := a.paneBounds[leaves[0].ID]
			rightBounds := a.paneBounds[right.ID]
			tester.ClickAt(leftBounds.X+20, leftBounds.Y+100)
			tester.Frame()
			if a.layouts["/project"].Focused != leaves[0].ID {
				t.Fatalf("left click did not focus pane: left=%+v right=%+v", leftBounds, rightBounds)
			}
			tester.ClickAt(rightBounds.X+20, rightBounds.Y+100)
			tester.Frame()
			if a.layouts["/project"].Focused != right.ID {
				t.Fatal("clicking right pane blank area did not retain its focus")
			}
			if !tester.Focused("会话面板 " + right.ID) {
				t.Fatal("blank area click did not move keyboard focus to the right pane")
			}
			if useMenu {
				item := closeMenuItem(t, a)
				item.Click(item, nil)
				tester.Frame()
			} else {
				tester.Key(native.Cmd, native.KeyW)
			}
			if rightSession.open || !leftSession.open || len(paneLeaves(a.layouts["/project"].Root)) != 1 {
				t.Fatal("close action did not close only the focused right pane")
			}
		})
	}
}

func TestPaneCloseUsesFocusedTabSelection(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	leftSession := a.session
	right := a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
	a.newSession("/project")
	firstRight := a.session
	a.newSession("/project")
	secondRight := a.session
	a.activeTab, a.session = sessionTabID(leftSession.id), leftSession
	a.closeCurrentPage()
	if !leftSession.open || !firstRight.open || secondRight.open || right.Active != sessionTabID(firstRight.id) {
		t.Fatal("close action used a stale rendered tab instead of the focused pane selection")
	}
}

func TestPaneEmptyTabCloseAndContentReplacement(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	leftSession := a.session
	right := a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
	tester := native.NewTester(a.view, 1600, 1000)
	if err := tester.Click("关闭新标签页 " + right.Active); err != nil {
		t.Fatal(err)
	}
	if len(paneLeaves(a.layouts["/project"].Root)) != 1 || !leftSession.open {
		t.Fatal("closing empty tab did not preserve the neighboring pane")
	}
	a.closeSession(leftSession)
	tester.Frame()
	if tester.HasText("新标签页") || tester.HasText("选择面板内容") || tester.HasText("从一个清晰的任务开始") {
		t.Fatal("closing the last tab did not leave a blank page")
	}
	pane := focusedPane(a.layoutFor("/project"))
	doubleClickTabBlank(t, tester, pane.ID)
	if !tester.HasText("新标签页") || !tester.HasText("选择面板内容") {
		t.Fatal("double click did not open an empty tab")
	}
	if err := tester.Click("关闭新标签页 " + a.activeTab); err != nil {
		t.Fatal(err)
	}
	if tester.HasText("新标签页") || tester.HasText("选择面板内容") || len(paneLeaves(a.layouts["/project"].Root)) != 1 {
		t.Fatal("closing the final empty tab did not return to the blank page")
	}
	doubleClickTabBlank(t, tester, pane.ID)
	if err := tester.Click("Codex"); err != nil {
		t.Fatal(err)
	}
	if tester.HasText("选择面板内容") || a.activeTab != sessionTabID(a.session.id) || !a.session.open {
		t.Fatal("creating content did not replace the empty tab")
	}
}

func TestPaneEmptyContentCreationFailureAndRetry(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	pane := a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
	tester := native.NewTester(a.view, 1200, 760)
	if err := tester.Click("Claude Code"); err != nil {
		t.Fatal(err)
	}
	if !a.isEmptyTab(pane.Active) || a.paneErrors[pane.ID] == "" || !tester.HasText(a.paneErrors[pane.ID]) || len(a.sessions) != 1 {
		t.Fatal("creation failure lost the empty pane or error")
	}
	if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("Claude Code"); err != nil {
		t.Fatal(err)
	}
	if a.session.kind != agent.Claude || a.session.State.Cwd != "/project" || a.paneErrors[pane.ID] != "" || pane.Active != sessionTabID(a.session.id) {
		t.Fatal("retry failed to create content in its original pane")
	}
}

type paneStopBackend struct {
	stubBackend
	stopped chan string
}

func (backend *paneStopBackend) Interrupt(_ context.Context, thread, turn string) error {
	backend.stopped <- thread + "/" + turn
	return nil
}

func TestPaneCloseStopsOnlyItsSession(t *testing.T) {
	backend := &paneStopBackend{stopped: make(chan string, 2)}
	a := New(backend, "test", "/project", nil)
	a.dispatch = func(func()) {}
	first := a.session
	first.State.ThreadID, first.State.TurnID, first.State.Busy = "first", "turn", true
	first.State.Messages = []*app.Message{{ID: "first-message", Role: "assistant", Text: "first pane history", Done: true}}
	a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
	a.newSession("/project")
	second := a.session
	second.State.ThreadID, second.State.TurnID, second.State.Busy = "second", "turn", true
	second.State.Messages = []*app.Message{{ID: "second-message", Role: "assistant", Text: "second pane history", Done: true}}
	tester := native.NewTester(a.view, 1600, 1000)
	if err := tester.RightClick("会话面板 " + focusedPane(a.layoutFor("/project")).ID); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(tester.Menu(), "在整个布局下方分屏") {
		t.Fatal("removed whole-layout action is still present")
	}
	if err := tester.ChooseMenuItem("关闭窗口"); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-backend.stopped:
		if id != "second/turn" {
			t.Fatal("stopped another pane", id)
		}
	case <-time.After(time.Second):
		t.Fatal("closing a busy session did not request interruption")
	}
	if !first.open || !first.State.Busy || second.open || !second.State.Busy {
		t.Fatal("close changed another task or claimed interruption before its terminal event")
	}
	if len(paneLeaves(a.layoutFor("/project").Root)) != 1 {
		t.Fatal("closed pane was not removed")
	}
}

func TestPaneMenuOmitsCloseForLastPane(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	tester := native.NewTester(a.view, 1000, 760)
	if err := tester.RightClick("会话面板 " + focusedPane(a.layoutFor("/project")).ID); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(tester.Menu(), "关闭窗口") {
		t.Fatal("last pane still offers the no-op close item")
	}
	pane := a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
	tester.Frame()
	if err := tester.RightClick("会话面板 " + pane.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tester.Menu(), "关闭窗口") {
		t.Fatal("split pane lost its close item")
	}
}

func TestPaneCloseDirtyFileCancelAndSave(t *testing.T) {
	a, queue, root := p4App(t)
	left := focusedPane(a.layoutFor(root))
	right := a.splitPane(root, left, "horizontal")
	tester := native.NewTester(a.view, 1200, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tester, queue, func() bool { return a.currentFile().loaded })
	page := a.currentFile()
	page.buffer.Set("saved when closing pane")
	page.observe()
	a.quitPrompt = func(mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 0}, nil
	}
	a.closePane(root, right)
	p4Wait(t, tester, queue, func() bool { return !a.quitDialog })
	if a.files[page.id] != page || len(paneLeaves(a.layoutFor(root).Root)) != 2 || a.paneClosing[right.ID] {
		t.Fatal("cancel did not preserve the pane and dirty file")
	}
	a.quitPrompt = func(mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 2}, nil
	}
	a.closePane(root, right)
	p4Wait(t, tester, queue, func() bool { return len(paneLeaves(a.layoutFor(root).Root)) == 1 })
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "saved when closing pane" || a.files[page.id] != nil || !a.session.open {
		t.Fatal("pane close did not save file or changed another pane", err)
	}
	a.closePane(root, focusedPane(a.layoutFor(root)))
	if len(paneLeaves(a.layoutFor(root).Root)) != 1 || len(a.folderTabs(root)) != 0 {
		t.Fatal("last pane did not remain empty")
	}
}

func TestPaneCloseNestedLayoutRestoresRemainingPanes(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	store := imageStore(t, a)
	right := a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
	bottom := a.splitPane("/project", right, "vertical")
	layout := a.layoutFor("/project")
	layout.Root.Ratio = 35
	upper := layout.Root.Second.First
	a.closePane("/project", bottom)
	layout = a.layoutFor("/project")
	if layout.Root.Ratio != 35 || layout.Root.Second != upper || layout.Focused != upper.ID || len(paneLeaves(layout.Root)) != 2 {
		t.Fatal("closing nested pane lost its sibling, ratio or focus")
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.layouts["/project"].Root.Ratio != 35 || len(paneLeaves(restored.layouts["/project"].Root)) != 2 {
		t.Fatal("restart restored a closed pane or lost ratio")
	}
}

func TestPaneCloseTerminalClosesShellWithoutPrompt(t *testing.T) {
	a, _, root := p4App(t)
	pane := a.splitPane(root, focusedPane(a.layoutFor(root)), "vertical")
	connection := &testTerminal{}
	a.terminalFactory = func(string) (terminalConnection, error) { return connection, nil }
	a.newTerminal()
	page := a.currentTerminal()
	// A non-nil window exercises the native-app branch that previously
	// displayed the confirmation; a headless test already skipped it.
	a.win = &mygo.Window{}
	t.Cleanup(func() { a.win = nil })
	a.quitPrompt = func(mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{}, fmt.Errorf("terminal close must not prompt")
	}
	a.closePane(root, pane)
	a.terminalWorkers.Wait()
	if !connection.Exited() || a.terminals[page.id] != nil || len(paneLeaves(a.layoutFor(root).Root)) != 1 || a.quitDialog {
		t.Fatal("terminal close did not finish shell and pane directly")
	}
}

func TestPaneTerminalVisibilityAndRestart(t *testing.T) {
	a := New(stubBackend{}, "test", t.TempDir(), nil)
	root := a.activeWorkspace
	store := imageStore(t, a)
	a.terminalFactory = func(string) (terminalConnection, error) { return &testTerminal{}, nil }
	t.Cleanup(a.closeWorkspaceResources)
	a.newTerminal()
	first := a.currentTerminal()
	a.splitPane(root, focusedPane(a.layoutFor(root)), "vertical")
	a.newTerminal()
	second := a.currentTerminal()
	tester := native.NewTester(a.view, 1400, 1000)
	tester.Frame()
	if !a.visibleTerms[first.id] || !a.visibleTerms[second.id] || first.connection.(*testTerminal).hidden.Load() || second.connection.(*testTerminal).hidden.Load() {
		t.Fatal("an unfocused but visible terminal was hidden")
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	for _, page := range restored.terminals {
		if page.connection != nil {
			t.Fatal("restoration restarted a shell")
		}
	}
	if len(paneLeaves(restored.layouts[root].Root)) != 2 || restored.currentTerminal().id != second.id {
		t.Fatal("restart lost terminal panes or selection")
	}
	a.newSession(root)
	tester.Frame()
	if !second.connection.(*testTerminal).hidden.Load() || first.connection.(*testTerminal).hidden.Load() {
		t.Fatal("switching a pane tab hid another pane's terminal")
	}
}

func TestPaneCanonicalDirectoryKeepsLayout(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	a := New(stubBackend{}, "test", alias, nil)
	store := imageStore(t, a)
	first := a.session
	a.splitPane(alias, focusedPane(a.layoutFor(alias)), "horizontal")
	a.newSession(alias)
	second := a.session
	first.State.Cwd, second.State.Cwd = root, root
	a.syncActivePane()
	if a.activeWorkspace != alias || len(paneLeaves(a.layouts[alias].Root)) != 2 || len(a.folderTabs(alias)) != 2 {
		t.Fatal("runtime canonical paths separated panes from their folder")
	}
	if len(a.projectGroups()) != 1 {
		t.Fatal("canonical path created duplicate navigation groups")
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.activeWorkspace != alias || len(paneLeaves(restored.layouts[alias].Root)) != 2 || len(restored.folderTabs(alias)) != 2 {
		t.Fatal("restart did not retain the layout's directory identity")
	}
}

func TestPaneDividerDragAndKeyboardResize(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	left := focusedPane(a.layoutFor("/project"))
	a.splitPane("/project", left, "horizontal")
	tester := native.NewTester(a.view, 1600, 1000)
	tester.Frame()
	layout := a.layouts["/project"]
	leaves := paneLeaves(layout.Root)
	leftBounds := a.paneBounds[leaves[0].ID]
	dividerX, dividerY := leftBounds.X+leftBounds.W+.5, leftBounds.Y+100
	tester.Move(10, 10)
	idle := tester.Image().RGBAAt(int(dividerX), int(dividerY))
	tester.Move(dividerX, dividerY)
	if tester.Image().RGBAAt(int(dividerX), int(dividerY)) != idle {
		t.Fatal("pane divider highlighted on hover")
	}
	tester.Press(dividerX, dividerY)
	if tester.Image().RGBAAt(int(dividerX), int(dividerY)) != idle {
		t.Fatal("pane divider highlighted while held")
	}
	tester.Move(dividerX+100, dividerY)
	tester.Release(dividerX+100, dividerY)
	tester.Frame()
	if layout.Root.Ratio <= 50 || a.paneBounds[leaves[0].ID].W <= leftBounds.W+50 {
		t.Fatalf("drag did not resize pane: ratio=%v before=%v after=%v", layout.Root.Ratio, leftBounds, a.paneBounds[leaves[0].ID])
	}
	before := layout.Root.Ratio
	tester.Key(0, native.KeyRight)
	if layout.Root.Ratio <= before {
		t.Fatal("focused divider did not support keyboard resizing")
	}
}

func TestPaneStartupRecoversVisibleSessionsAfterRuntimeReady(t *testing.T) {
	a := New(nil, "test", "/project", nil)
	store := imageStore(t, a)
	first := a.session
	first.State.ThreadID = "first"
	a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
	a.newSession("/project")
	second := a.session
	second.State.ThreadID = "second"
	a.newSession("/project")
	third := a.session
	third.State.ThreadID = "hidden"
	a.selectSession(second)
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	backend := &lazyTabsBackend{requests: make(chan string, 8)}
	backend.pages = []agent.TurnsPage{{}}
	restored := New(backend, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	queue := make(chan func(), 8)
	restored.dispatch = func(callback func()) { queue <- callback }
	tester := native.NewTester(restored.view, 1600, 1000)
	if len(backend.requests) != 0 {
		t.Fatal("rendering started protocol work before runtime readiness")
	}
	restored.recoverSelectedSession()
	pumpUpdate(t, queue)
	pumpUpdate(t, queue)
	tester.Frame()
	requested := []string{<-backend.requests, <-backend.requests}
	slices.Sort(requested)
	if !reflect.DeepEqual(requested, []string{"first", "second"}) || restored.sessions[2].loaded || len(backend.requests) != 0 {
		t.Fatal("startup did not limit recovery to visible pane sessions", requested)
	}
}
