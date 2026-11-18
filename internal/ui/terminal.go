package ui

import (
	"fmt"
	"strings"
	"sync"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/storage"
	"github.com/shenming0821/Sasa/internal/terminal"
)

var launchTerminalIcon = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 9 3 3-3 3m6 0h4"/>`)

type terminalConnection interface {
	Present(uintptr, native.Rect, bool, bool) error
	Hide()
	Close()
	Exited() bool
	SetShortcut(func(string))
}

type terminalState struct {
	terminals       map[string]*terminalPage
	terminalSerial  int
	terminalFactory func(string) (terminalConnection, error)
	terminalWorkers sync.WaitGroup
}

type terminalPage struct {
	id, root, engine string
	connection       terminalConnection
	err              string
	focus            bool
	closing          bool
	layout           terminalLayout
}

type terminalLayout struct {
	bounds                        native.Rect
	width, height, sidebar, right float32
	left, history, settings       bool
}

func (a *App) hideInactiveTerminals() {
	for _, page := range a.terminals {
		visible := a.activeTab == page.id
		if a.visibleTerms != nil {
			visible = a.visibleTerms[page.id]
		}
		if page.connection != nil && (!visible || a.paneTabDragging || a.settingsOpen || a.viewerOpen || a.compactSidebar || a.worktreeRenameOpen || a.sessionRenameOpen || a.fileAction.open || a.worktreeCreate != nil) {
			page.connection.Hide()
			if a.activeTab == page.id {
				page.focus = true
			}
		}
	}
}

func (a *App) currentTerminal() *terminalPage { return a.terminals[a.activeTab] }

func (a *App) terminalButton(c *native.Context) {
	button := quietIconButton(c, "打开终端").Size(30, 30).Tooltip("在当前工作区新建终端（" + terminal.Label(a.preferences.Terminal) + "）")
	button.Children(func() { native.Icon(c, launchTerminalIcon).Size(20, 20) })
	if button.Clicked() {
		a.newTerminal()
	}
}

func (a *App) newTerminal() {
	root := a.application()
	if root.terminals == nil {
		root.terminals = map[string]*terminalPage{}
	}
	for {
		root.terminalSerial++
		if root.terminals[fmt.Sprintf("terminal-%d", root.terminalSerial)] == nil {
			break
		}
	}
	page := &terminalPage{id: fmt.Sprintf("terminal-%d", root.terminalSerial), root: a.workspaceCwd(), engine: terminal.Normalize(root.preferences.Terminal), focus: true}
	root.terminals[page.id] = page
	root.tabs = append(root.tabs, storage.Tab{ID: page.id, Kind: "terminal", Workspace: page.root, TerminalEngine: page.engine})
	root.selectTerminal(page)
	root.startTerminal(page)
}

func (a *App) selectTerminal(page *terminalPage) {
	a.activeTab = page.id
	a.selectWorkspace(page.root)
	a.activatePaneTab(page.id, a.activeWorkspace, false)
	page.focus = true
	a.compactSidebar = false
}

func (a *App) startTerminal(page *terminalPage) {
	if page.connection != nil || page.closing {
		return
	}
	factory := a.terminalFactory
	if factory == nil {
		factory = func(cwd string) (terminalConnection, error) { return terminal.Start(cwd) }
	}
	connection, err := factory(page.root)
	if err != nil {
		page.err = "启动终端失败：" + err.Error()
		return
	}
	page.connection, page.err = connection, ""
	connection.SetShortcut(func(key string) {
		a.update(func() {
			if a.terminals[page.id] != page || key == "exit" || a.visibleTerms != nil && !a.visibleTerms[page.id] {
				return
			}
			a.selectTerminal(page)
			switch strings.ToLower(key) {
			case "focus":
				page.focus = false
				a.paneNativeFocus = page.id
			case "w":
				a.closeCurrentPage()
			case ",":
				a.openSettings()
			case "t":
				a.newTerminal()
			case "p":
				a.openQuickOpen()
			}
		})
	})
}

func (a *App) terminalView(c *native.Context, page *terminalPage, leftShown, rightShown bool) {
	native.Column(c).Grow(1).FillHeight().MinWidth(0).Background(c.Theme().Background).Children(func() {
		a.sessionTabs(c, leftShown, rightShown)
		if page.closing {
			native.Text(c, "正在关闭终端…").Padding(16).FontSize(12)
			return
		}
		if page.connection == nil && page.err == "" {
			a.startTerminal(page)
		}
		if page.connection == nil {
			native.Column(c).Fill().Padding(20).Gap(12).Children(func() {
				native.Text(c, page.err).FontSize(12)
				if actionButton(c, "启动终端").Clicked() {
					a.startTerminal(page)
				}
			})
			return
		}
		if a.win == nil {
			native.Text(c, "终端 "+page.root).FontSize(12)
			return
		}
		host := native.Box(c.Key("native-terminal")).Label("工作区终端 " + page.root).Grow(1).FillWidth().MinHeight(0)
		width, height := c.Size()
		layout := terminalLayout{host.Bounds(), width, height, a.sidebarWidth, a.rightPanelWidth, leftShown, rightShown, a.settingsOpen}
		if layout != page.layout {
			page.layout = layout
			c.AnimationFrame()
		}
		bounds := host.Bounds()
		if bounds.W == 0 || bounds.H == 0 {
			c.AnimationFrame()
			return
		}
		if a.paneTabDragging || a.settingsOpen || a.viewerOpen || a.compactSidebar || a.worktreeRenameOpen || a.sessionRenameOpen || a.fileAction.open || a.worktreeCreate != nil {
			return
		}
		focus := page.focus && a.paneIsFocused() && !a.quickOpen.open
		if focus {
			a.paneNativeFocus = page.id
		}
		if err := page.connection.Present(a.win.NativeHandle(), bounds, c.Theme().Dark, focus); err != nil {
			page.err = "无法显示终端：" + err.Error()
			page.connection.Close()
			page.connection = nil
			c.AnimationFrame()
			return
		}
		page.focus = false
	})
}

func (a *App) closeTerminal(page *terminalPage) {
	a.closeTerminalWithResult(page, func(bool) {})
}

func (a *App) closeTerminalWithResult(page *terminalPage, done func(bool)) {
	if page.closing {
		done(false)
		return
	}
	a.finishCloseTerminal(page)
	done(true)
}

func (a *App) finishCloseTerminal(page *terminalPage) {
	page.closing = true
	if page.connection != nil {
		page.connection.Hide()
	}
	a.removeWorkspaceTab(page.id)
	delete(a.terminals, page.id)
	if page.connection != nil {
		a.terminalWorkers.Add(1)
		go func() {
			defer a.terminalWorkers.Done()
			page.connection.Close()
		}()
	}
}
