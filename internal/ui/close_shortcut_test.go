package ui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
)

func closeMenuItem(t *testing.T, a *App) *mygo.MenuItem {
	t.Helper()
	menu := a.applicationMenu()
	item := menu.ItemByID("close-current-page")
	if item == nil || item.Role != "" || item.Accelerator != "CmdOrCtrl+W" || item.Click == nil {
		t.Fatal("close shortcut must invoke the page action instead of a native window role")
	}
	var checkRoles func([]*mygo.MenuItem)
	checkRoles = func(items []*mygo.MenuItem) {
		for _, entry := range items {
			if entry.Role == mygo.RoleClose || entry.Role == mygo.RoleFileMenu {
				t.Fatal("native window close still captures Cmd+W")
			}
			checkRoles(entry.Submenu)
		}
	}
	checkRoles(menu.Items())
	return item
}

func TestCloseMenuClosesSelectedSessionAndKeepsBackgroundTask(t *testing.T) {
	backend := &quitBackend{stops: make(chan string, 2)}
	a := New(backend, "test", t.TempDir(), nil)
	a.dispatch = func(work func()) { work() }
	first := a.session
	first.State.Draft, first.State.Busy = "keep draft", true
	first.State.ThreadID, first.State.TurnID = "first", "turn"
	a.newSession(first.State.Cwd)
	second := a.session
	item := closeMenuItem(t, a)
	item.Click(item, nil)
	if second.open || !first.open || a.session != first || !first.State.Busy || first.State.Draft != "keep draft" {
		t.Fatal("menu close lost the selected tab or changed the background task")
	}
	item.Click(item, nil)
	item.Click(item, nil)
	if a.open || len(a.tabs) != 0 || a.quitting || !first.State.Busy || !first.State.Stopping() {
		t.Fatal("closing the last tab did not keep the window and await interruption")
	}
	select {
	case stopped := <-backend.stops:
		if stopped != "first/turn" {
			t.Fatal("close stopped a different task", stopped)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("close menu did not interrupt its running session")
	}
}

func TestCloseMenuDismissesOverlayBeforeTab(t *testing.T) {
	for _, overlay := range []string{"file-action", "worktree-rename", "session-rename", "settings", "viewer", "quit-dialog"} {
		t.Run(overlay, func(t *testing.T) {
			a := New(stubBackend{}, "test", t.TempDir(), nil)
			a.dispatch = func(work func()) { work() }
			var open *bool
			switch overlay {
			case "file-action":
				open = &a.fileAction.open
			case "worktree-rename":
				open = &a.worktreeRenameOpen
			case "session-rename":
				open = &a.sessionRenameOpen
			case "settings":
				open = &a.settingsOpen
			case "viewer":
				open = &a.viewerOpen
			case "quit-dialog":
				open = &a.quitDialog
			}
			*open = true
			item := closeMenuItem(t, a)
			item.Click(item, nil)
			if !a.open || a.quitting || *open != (overlay == "quit-dialog") {
				t.Fatal("close bypassed the overlay or closed its underlying tab")
			}
		})
	}
}

func TestCloseMenuPreservesUnsavedFileOnCancel(t *testing.T) {
	a, queue, root := p4App(t)
	tester := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tester, queue, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	page := a.currentFile()
	page.buffer.Set("unsaved text")
	prompts := make(chan mygo.MessageOptions, 1)
	a.quitPrompt = func(options mygo.MessageOptions) (mygo.MessageResult, error) {
		prompts <- options
		return mygo.MessageResult{Button: 0}, nil
	}
	item := closeMenuItem(t, a)
	item.Click(item, nil)
	p4Wait(t, tester, queue, func() bool { return len(prompts) > 0 && !a.quitDialog })
	if options := <-prompts; options.Title != "关闭文件" {
		t.Fatal("shortcut did not use the existing save confirmation")
	}
	if a.currentFile() != page || !page.dirty() || page.buffer.String() != "unsaved text" || !a.open {
		t.Fatal("canceling close lost unsaved content or the chat")
	}
}

func TestCloseShortcutRemovesEmptyFocusedPane(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	a.dispatch = func(work func()) { work() }
	a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
	tester := native.NewTester(a.view, 1200, 760)
	if a.activeTab != "" {
		t.Fatal("new split pane unexpectedly owns a tab")
	}
	tester.Key(native.Cmd, native.KeyW)
	tester.Frame()
	if len(paneLeaves(a.layoutFor("/project").Root)) != 1 || !a.session.open {
		t.Fatal("Cmd+W did not remove the empty focused pane")
	}
	tester.Key(native.Cmd, native.KeyW)
	tester.Frame()
	if len(paneLeaves(a.layoutFor("/project").Root)) != 1 || len(a.folderTabs("/project")) != 0 || a.session.open {
		t.Fatal("single-pane Cmd+W did not fall back to closing its tab")
	}
	tester.Key(native.Cmd, native.KeyW)
	tester.Frame()
	if len(paneLeaves(a.layoutFor("/project").Root)) != 1 {
		t.Fatal("Cmd+W removed the last pane")
	}
}

func TestCloseShortcutClosesSingleTabPane(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	a.dispatch = func(work func()) { work() }
	first := a.session
	a.splitPane("/project", focusedPane(a.layoutFor("/project")), "horizontal")
	a.newSession("/project")
	second := a.session
	a.newSession("/project")
	third := a.session
	tester := native.NewTester(a.view, 1200, 760)
	if len(focusedPane(a.layoutFor("/project")).Tabs) != 2 {
		t.Fatal("fixture did not give the focused pane two tabs")
	}
	tester.Key(native.Cmd, native.KeyW)
	tester.Frame()
	if len(paneLeaves(a.layoutFor("/project").Root)) != 2 || third.open || !second.open {
		t.Fatal("multi-tab pane closed more than its current tab")
	}
	tester.Key(native.Cmd, native.KeyW)
	tester.Frame()
	if len(paneLeaves(a.layoutFor("/project").Root)) != 1 || second.open || !first.open || a.session != first {
		t.Fatal("single-tab pane did not close tab and pane together")
	}
}

func TestCloseShortcutAndMenuCloseTerminalTabs(t *testing.T) {
	for _, useMenu := range []bool{false, true} {
		t.Run(map[bool]string{false: "keyboard", true: "menu"}[useMenu], func(t *testing.T) {
			a := New(stubBackend{}, "test", t.TempDir(), nil)
			a.dispatch = func(work func()) { work() }
			connection := &testTerminal{}
			a.terminalFactory = func(string) (terminalConnection, error) { return connection, nil }
			t.Cleanup(a.closeWorkspaceResources)
			a.newTerminal()
			tester := native.NewTester(a.view, 1000, 760)
			if useMenu {
				item := closeMenuItem(t, a)
				item.Click(item, nil)
			} else {
				tester.Key(native.Cmd, native.KeyW)
			}
			a.terminalWorkers.Wait()
			if !connection.closed.Load() || len(a.terminals) != 0 || !a.open || a.quitting {
				t.Fatal("close did not release only the selected terminal")
			}
		})
	}
}
