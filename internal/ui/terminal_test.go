package ui

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/terminal"
)

type testTerminal struct {
	closed   atomic.Bool
	hidden   atomic.Bool
	shortcut func(string)
}

func (terminal *testTerminal) Present(uintptr, native.Rect, bool, bool) error { return nil }
func (terminal *testTerminal) Hide()                                          { terminal.hidden.Store(true) }
func (terminal *testTerminal) Close()                                         { terminal.closed.Store(true) }
func (terminal *testTerminal) Exited() bool                                   { return terminal.closed.Load() }
func (terminal *testTerminal) SetShortcut(callback func(string))              { terminal.shortcut = callback }

func TestEmbeddedTerminalTabsKeepWorkspaceAndSession(t *testing.T) {
	for _, width := range []int{480, 1000} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/dark=%t", width, dark), func(t *testing.T) {
				app := New(stubBackend{}, "test", t.TempDir(), nil)
				original := app.session
				original.State.Draft, original.State.Busy = "keep draft", true
				cwd := original.State.Cwd
				var started []string
				var connections []*testTerminal
				app.terminalFactory = func(root string) (terminalConnection, error) {
					started = append(started, root)
					connection := &testTerminal{}
					connections = append(connections, connection)
					return connection, nil
				}
				t.Cleanup(app.closeWorkspaceResources)
				tester := native.NewTester(app.view, width, 760)
				tester.SetDark(dark)
				button, found := tester.Find("打开终端")
				claude, _ := tester.Find("使用 Claude Code 新建会话")
				if !found || button.X < claude.X+claude.W || button.X+button.W > float32(width) || button.Y != claude.Y {
					t.Fatal("terminal toolbar button missing or clipped", button)
				}
				if err := tester.Click("打开终端"); err != nil {
					t.Fatal(err)
				}
				page := app.currentTerminal()
				if page == nil || page.root != cwd || page.engine != terminal.Ghostty || len(app.sessions) != 1 || app.session != original || original.State.Draft != "keep draft" || !original.State.Busy {
					t.Fatal("embedded terminal changed Agent session or directory")
				}
				if _, found := tester.Find("终端标签 " + page.id); !found {
					t.Fatal("terminal did not open in the main tab bar")
				}
				app.preferences.Terminal = "xterm"
				if err := tester.Click("打开终端"); err != nil {
					t.Fatal(err)
				}
				second := app.currentTerminal()
				if second == page || second.root != cwd || second.engine != terminal.Ghostty || len(started) != 2 {
					t.Fatal("new terminal did not use current workspace and selected engine")
				}
				if err := tester.Click("终端标签 " + page.id); err != nil {
					t.Fatal(err)
				}
				if app.currentTerminal() != page || len(started) != 2 {
					t.Fatal("switching terminal restarted shell")
				}
				if err := tester.Click("关闭终端标签 " + page.id); err != nil {
					t.Fatal(err)
				}
				app.terminalWorkers.Wait()
				if !connections[0].closed.Load() || connections[1].closed.Load() || len(app.terminals) != 1 {
					t.Fatal("closing terminal affected another shell")
				}
			})
		}
	}
}

func TestEmbeddedTerminalRestoresTabsWithoutStartingShell(t *testing.T) {
	app := New(stubBackend{}, "test", t.TempDir(), nil)
	store := imageStore(t, app)
	app.terminalFactory = func(string) (terminalConnection, error) { return &testTerminal{}, nil }
	t.Cleanup(app.closeWorkspaceResources)
	app.preferences.Terminal = "xterm"
	app.newTerminal()
	first := app.currentTerminal()
	if err := app.persist(); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	page := restored.currentTerminal()
	if page == nil || page.id != first.id || page.root != first.root || page.engine != terminal.Ghostty || page.connection != nil {
		t.Fatal("restored terminal lost metadata or restarted shell")
	}
	calls := 0
	restored.terminalFactory = func(root string) (terminalConnection, error) { calls++; return &testTerminal{}, nil }
	t.Cleanup(restored.closeWorkspaceResources)
	tester := native.NewTester(restored.view, 1000, 760)
	if !tester.HasText("重新启动 Fuyu 后，终端需要新建 shell 会话。") || calls != 0 {
		t.Fatal("terminal restoration hides shell boundary")
	}
	if err := tester.Click("启动终端"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || restored.currentTerminal() != page {
		t.Fatal("restored terminal failed to start")
	}
	restored.newTerminal()
	if restored.currentTerminal().id == page.id {
		t.Fatal("restored terminal ID was reused")
	}
}

func TestNativeTerminalHidesBehindOtherTabsAndOverlays(t *testing.T) {
	app := New(stubBackend{}, "test", t.TempDir(), nil)
	app.terminalFactory = func(string) (terminalConnection, error) { return &testTerminal{}, nil }
	t.Cleanup(app.closeWorkspaceResources)
	app.newTerminal()
	page := app.currentTerminal()
	connection := page.connection.(*testTerminal)
	app.hideInactiveTerminals()
	if connection.hidden.Load() {
		t.Fatal("active terminal hidden")
	}
	app.settingsOpen = true
	app.hideInactiveTerminals()
	if !connection.hidden.Load() {
		t.Fatal("native terminal covered settings")
	}
	app.settingsOpen = false
	connection.hidden.Store(false)
	app.activeTab = "other"
	app.hideInactiveTerminals()
	if !connection.hidden.Load() || connection.closed.Load() {
		t.Fatal("switching tabs must hide the view and retain its shell")
	}
}

func TestEmbeddedTerminalStartupErrorAndRetry(t *testing.T) {
	app := New(stubBackend{}, "test", t.TempDir(), nil)
	app.closeSession(app.session)
	app.activeWorkspace = t.TempDir()
	cwd := app.activeWorkspace
	app.terminalFactory = func(root string) (terminalConnection, error) {
		if root != cwd {
			t.Fatal("empty workspace directory lost")
		}
		return nil, errors.New("shell unavailable")
	}
	tester := native.NewTester(app.view, 1000, 760)
	if err := tester.Click("打开终端"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("启动终端失败：shell unavailable") {
		t.Fatal("terminal failure hidden")
	}
	app.terminalFactory = func(string) (terminalConnection, error) { return &testTerminal{}, nil }
	t.Cleanup(app.closeWorkspaceResources)
	if err := tester.Click("启动终端"); err != nil {
		t.Fatal(err)
	}
	if app.currentTerminal().connection == nil || app.currentTerminal().err != "" {
		t.Fatal("terminal retry failed")
	}
}

func TestGeneralTerminalPreferencePersistence(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{640, 480}, {1000, 760}} {
			t.Run(fmt.Sprintf("%t/%v", dark, size), func(t *testing.T) {
				app := New(stubBackend{}, "test", "/workspace", nil)
				store := imageStore(t, app)
				app.openSettings()
				tester := native.NewTester(app.view, size[0], size[1])
				tester.SetDark(dark)
				for _, choice := range terminal.Choices {
					if err := tester.Click("默认终端 " + terminal.Label(app.preferences.Terminal)); err != nil {
						t.Fatal(err)
					}
					bounds, found := tester.Find("默认终端选项 " + choice.Label)
					if !found || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > float32(size[0]) || bounds.Y+bounds.H > float32(size[1]) {
						t.Fatal("terminal option clipped", bounds)
					}
					if err := tester.Click("默认终端选项 " + choice.Label); err != nil {
						t.Fatal(err)
					}
					restored := New(stubBackend{}, "test", "/workspace", nil)
					if err := restored.AttachStore(store); err != nil {
						t.Fatal(err)
					}
					if app.preferences.Terminal != choice.ID || restored.preferences.Terminal != choice.ID {
						t.Fatal("terminal choice not persisted")
					}
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
