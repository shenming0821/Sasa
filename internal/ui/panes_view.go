package ui

import (
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/storage"
)

var paneMenuIcon = icon(`<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>`)
var paneNextIcon = icon(`<path d="m9 6 6 6-6 6"/>`)
var paneDragIcon = icon(`<rect x="3" y="3" width="10" height="7" rx="2"/><path d="M3 14v5a2 2 0 0 0 2 2h7m1-9 7 3-3 1-1 3-3-7Z"/>`)

func (a *App) workspacePanes(c *native.Context, leftShown, rightShown bool) {
	if a.paneHandles == nil {
		a.paneHandles = map[string]*native.Handle{}
		a.paneBounds = map[string]native.Rect{}
		a.paneErrors = map[string]string{}
	}
	a.visibleTerms = map[string]bool{}
	a.paneTabDragging = false
	root := a.activeWorkspace
	layout := a.layoutFor(root)
	var dropped paneTabDrag
	var destination *storage.PaneNode
	var render func(*storage.PaneNode)
	render = func(node *storage.PaneNode) {
		if node.Axis != "" {
			first := func() { render(node.First) }
			second := func() { render(node.Second) }
			var split native.Element
			if node.Axis == "vertical" {
				split = native.SplitVerticalPercent(c.Key(node.ID), &node.Ratio, first, second).Fill().MinWidth(0).MinHeight(0)
			} else {
				split = native.SplitPercent(c.Key(node.ID), &node.Ratio, first, second).Fill().MinWidth(0).MinHeight(0)
			}
			a.paneBounds[node.ID] = split.Bounds()
			return
		}
		pc := c.Key(node.ID)
		if a.paneHandles[node.ID] == nil {
			a.paneHandles[node.ID] = &native.Handle{}
		}
		box := native.Column(pc).Fill().MinWidth(0).MinHeight(0).Clip().Bind(a.paneHandles[node.ID]).Label("会话面板 " + node.ID)
		if a.paneFocusPending == node.ID {
			box.Focus()
			a.paneFocusPending = ""
		}
		box.ContextMenu(a.paneMenu(root, node))
		drag, over := native.DragOver[paneTabDrag](box)
		over = over && drag.Workspace == root && drag.Pane != node.ID
		if source, ok := native.Drop[paneTabDrag](box); ok {
			dropped, destination = source, node
		}
		clicked := box.Clicked()
		if clicked {
			box.Focus()
		}
		if box.FocusWithin() && a.paneNativeFocus == "" || clicked {
			a.focusPane(root, node)
		}
		a.paneBounds[node.ID] = box.Bounds()
		box.Children(func() {
			a.withPane(root, node, func() {
				if page := a.currentTerminal(); page != nil {
					a.visibleTerms[page.id] = true
					a.terminalView(pc.Key(page.id), page, leftShown, rightShown)
				} else if document := a.currentFile(); document != nil {
					a.fileView(pc.Key(document.id), document, leftShown, rightShown)
				} else if a.open && node.Active != "" {
					a.sessionView(pc.Key(a.session.id), leftShown, rightShown)
				} else {
					a.emptyWorkspace(pc.Key("empty-workspace"), leftShown, rightShown)
				}
			})
		})
		if over {
			accent := c.Theme().Accent
			box.DrawOver(func(painter *native.Painter, rect native.Rect) {
				color := accent
				thickness := float32(3)
				painter.Fill(native.Rect{X: rect.X, Y: rect.Y, W: rect.W, H: thickness}, color, 0)
				painter.Fill(native.Rect{X: rect.X, Y: rect.Y + rect.H - thickness, W: rect.W, H: thickness}, color, 0)
				painter.Fill(native.Rect{X: rect.X, Y: rect.Y, W: thickness, H: rect.H}, color, 0)
				painter.Fill(native.Rect{X: rect.X + rect.W - thickness, Y: rect.Y, W: thickness, H: rect.H}, color, 0)
			})
		}
	}
	render(layout.Root)
	if request := a.paneClosePending; request != nil {
		a.paneClosePending = nil
		a.closePane(request.root, request.pane)
		c.AnimationFrame()
	}
	if destination != nil && a.movePaneTab(root, destination, dropped) {
		c.AnimationFrame()
	}
}

func (a *App) emptyPane(c *native.Context) {
	pane, root := a.renderingPane, a.activeWorkspace
	if pane == nil {
		return
	}
	theme := c.Theme()
	native.Column(c).FillWidth().Grow(1).MinHeight(0).Center().Padding(24).Children(func() {
		native.Column(c).FillWidth().MaxWidth(360).MinWidth(0).Children(func() {
			native.Text(c, "选择面板内容").FillWidth().TextAlign(native.Center).FontSize(22).FontWeight(600).TextColor(theme.Text)
			native.Text(c, "新建会话或终端").FillWidth().TextAlign(native.Center).FontSize(12).TextColor(theme.TextMuted).Margin(8, 0, 24, 0)
			native.Column(c).FillWidth().Radius(12).Border(1, theme.Border).Clip().Children(func() {
				for index, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
					if index > 0 {
						native.Box(c).FillWidth().Height(1).Background(theme.Border).Margin(0, 16, 0, 58)
					}
					label, glyph, color := "新建 Codex 会话", codexAgentIcon, theme.Text
					if kind == agent.Claude {
						label, glyph, color = "新建 Claude 会话", claudeAgentIcon, native.Hex("#D97757")
					}
					if emptyPaneAction(c.Key(string(kind)), label, glyph, color).Clicked() {
						a.focusPane(root, pane)
						if err := a.newAgentSession(kind); err != nil {
							a.paneErrors[pane.ID] = err.Error()
						} else {
							delete(a.paneErrors, pane.ID)
						}
					}
				}
				native.Box(c).FillWidth().Height(1).Background(theme.Border).Margin(0, 16, 0, 58)
				if emptyPaneAction(c.Key("terminal"), "新建终端", launchTerminalIcon, theme.TextMuted).Clicked() {
					a.focusPane(root, pane)
					a.newTerminal()
					delete(a.paneErrors, pane.ID)
				}
			})
			if message := a.paneErrors[pane.ID]; message != "" {
				native.Text(c, message).FillWidth().FontSize(12).TextColor(theme.Danger).Margin(12, 0, 0, 0)
			}
			native.Row(c).FillWidth().AlignItems(native.Center).Justify(native.Center).Gap(6).Margin(20, 0, 0, 0).Children(func() {
				native.Icon(c, paneDragIcon).Size(14, 14).Shrink(0).TextColor(theme.TextMuted)
				native.Text(c, "也可以将其他面板的页签拖到这里").FontSize(11).TextColor(theme.TextMuted).MinWidth(0).Shrink(1)
			})
		})
	})
}

func emptyPaneAction(c *native.Context, label string, glyph *native.SVG, color native.Color) native.Element {
	theme := c.Theme()
	button := native.ButtonBase(c).Label(label).FillWidth().MinWidth(0).Height(56).Padding(0, 14)
	buttonFace(c, button, theme.Background, theme.SurfaceHover, theme.SurfacePressed, 0, 0)
	button.Children(func() {
		native.Row(c).Fill().AlignItems(native.Center).Gap(12).Children(func() {
			native.Box(c).Size(30, 30).Shrink(0).Radius(8).Background(theme.Surface).Center().Children(func() {
				native.Icon(c, glyph).Size(19, 19).TextColor(color)
			})
			native.Text(c, label).FontSize(14).FontWeight(500).TextColor(theme.Text).Grow(1).MinWidth(0).SingleLine()
			native.Icon(c, paneNextIcon).Size(14, 14).Shrink(0).TextColor(theme.TextMuted)
		})
	})
	return button
}
