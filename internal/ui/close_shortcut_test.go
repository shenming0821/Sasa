package ui

import (
	"path/filepath"
	"testing"

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
	a := New(stubBackend{}, "test", t.TempDir(), nil)
	a.dispatch = func(work func()) { work() }
	first := a.session
	first.State.Draft, first.State.Busy = "keep draft", true
	a.newSession(first.State.Cwd)
	second := a.session
	item := closeMenuItem(t, a)
	item.Click(item, nil)
	if second.open || !first.open || a.session != first || !first.State.Busy || first.State.Draft != "keep draft" {
		t.Fatal("menu close lost the selected tab or changed the background task")
	}
	item.Click(item, nil)
	item.Click(item, nil)
	if a.open || len(a.tabs) != 0 || a.quitting || !first.State.Busy {
		t.Fatal("closing the last tab or empty workspace quit the app or stopped its task")
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
