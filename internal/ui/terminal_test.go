package ui

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/terminal"
)

type testTerminal struct {
	closed   atomic.Bool
	hidden   atomic.Bool
	shortcut func(string)
	colors   terminal.Theme
	dark     bool
}

func (terminal *testTerminal) Present(uintptr, native.Rect, bool, bool) error {
	terminal.hidden.Store(false)
	return nil
}
func (terminal *testTerminal) Hide()                             { terminal.hidden.Store(true) }
func (terminal *testTerminal) Close()                            { terminal.closed.Store(true) }
func (terminal *testTerminal) Exited() bool                      { return terminal.closed.Load() }
func (terminal *testTerminal) SetShortcut(callback func(string)) { terminal.shortcut = callback }

func (connection *testTerminal) SetTheme(colors terminal.Theme, dark bool) error {
	connection.colors, connection.dark = colors, dark
	return nil
}

func TestThemePickerUpdatesOpenTerminals(t *testing.T) {
	app := New(stubBackend{}, "test", t.TempDir(), nil)
	t.Cleanup(app.closeWorkspaceResources)
	var connections []*testTerminal
	app.terminalFactory = func(string) (terminalConnection, error) {
		connection := &testTerminal{}
		connections = append(connections, connection)
		return connection, nil
	}
	tester := native.NewTester(app.view, 1000, 760)
	tester.SetDark(false)
	app.newTerminal()
	app.newTerminal()
	app.openSettings()
	tester.Frame()
	if err := tester.Click("设置分类 外观"); err != nil {
		t.Fatal(err)
	}
	for _, choice := range themes[1:] {
		if err := tester.Click("配色 " + choice.label); err != nil {
			t.Fatal(err)
		}
		for _, dark := range []bool{false, true} {
			tester.SetDark(dark)
			tester.Frame()
			for _, connection := range connections {
				if connection.colors != choice.paletteFor(dark).term || connection.dark != dark || connection.closed.Load() {
					t.Fatalf("open terminal did not receive %s palette in place (dark=%v)", choice.label, dark)
				}
			}
		}
	}
	if err := tester.Click("配色 默认"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	for _, connection := range connections {
		if connection.colors != (terminal.Theme{}) || !connection.dark || connection.closed.Load() {
			t.Fatal("switching back did not restore the terminal's own colors in place")
		}
	}
	if len(connections) != 2 {
		t.Fatal("changing the palette restarted a terminal")
	}
}

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

func TestEmbeddedTerminalRestoresTabsAndStartsShell(t *testing.T) {
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
	if tester.HasText("重新启动 Sasa 后，终端需要新建 shell 会话。") || calls != 1 || restored.currentTerminal() != page || page.connection == nil {
		t.Fatal("restored terminal did not start shell automatically")
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

func TestTerminalShortcutOpensQuickOpen(t *testing.T) {
	a, queue, root := p4App(t)
	var overlay *testQuickOpenWindow
	a.quickOpen.newWindow = func(opts mygo.FlyoutOptions, closed func()) quickOpenWindow {
		if opts.Parent != a.win || !opts.Focusable || opts.Width != 1280 || opts.Height != 800 {
			t.Fatalf("invalid quick-open surface options: %+v", opts)
		}
		overlay = &testQuickOpenWindow{closed: closed}
		return overlay
	}
	tt := native.NewTester(a.view, 1280, 800)
	a.terminalFactory = func(string) (terminalConnection, error) { return &testTerminal{}, nil }
	a.newTerminal()
	tt.Frame()
	connection := a.currentTerminal().connection.(*testTerminal)
	connection.shortcut("p")
	p4Wait(t, tt, queue, func() bool { return a.quickOpen.open })
	if a.quickOpen.root != root {
		t.Fatalf("quick open rooted at %q instead of %q", a.quickOpen.root, root)
	}
	tt.Frame()
	if connection.hidden.Load() || connection.closed.Load() {
		t.Fatal("quick open must keep the terminal visible and retain its shell")
	}
	if overlay == nil {
		t.Fatal("quick open did not create an independent surface")
	}
	// Native dismissal (outside click or app deactivation) restores logical
	// state and lets the terminal take focus on the next parent frame.
	a.currentTerminal().focus = false
	overlay.Destroy()
	tt.Frame()
	if a.quickOpen.open || a.quickOpen.window != nil || connection.hidden.Load() || !a.currentTerminal().focus {
		t.Fatal("terminal did not regain focus after quick open closed")
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
				if err := tester.Click("设置分类 通用"); err != nil {
					t.Fatal(err)
				}
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
