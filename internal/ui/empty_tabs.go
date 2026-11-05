package ui

import (
	"fmt"
	"slices"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func (a *App) isEmptyTab(id string) bool {
	return slices.ContainsFunc(a.tabs, func(tab storage.Tab) bool { return tab.ID == id && tab.Kind == "empty" })
}

func (a *App) newEmptyTab(root string, pane *storage.PaneNode) {
	root = a.workspaceRoot(root)
	a.focusPane(root, pane)
	for {
		a.emptyTabSerial++
		id := fmt.Sprintf("empty-%d", a.emptyTabSerial)
		if !slices.ContainsFunc(a.tabs, func(tab storage.Tab) bool { return tab.ID == id }) {
			tab := storage.Tab{ID: id, Kind: "empty", Workspace: root}
			a.tabs = append(a.tabs, tab)
			a.selectWorkspaceTab(tab)
			return
		}
	}
}

func (a *App) emptyTab(c *native.Context, tab storage.Tab) {
	pane, root := a.renderingPane, a.activeWorkspace
	theme := c.Theme()
	row := native.Row(c).Width(168).Shrink(0).FillHeight().Padding(0, 4, 0, 0).AlignItems(native.Center)
	if tab.ID == a.activeTab {
		activeTabFace(c, row)
		if tab.ID != "" && a.tabsSelected != tab.ID {
			row.ScrollIntoView()
		}
	}
	row.Children(func() {
		label, closeLabel := "新标签页", "关闭新标签页"
		if tab.ID != "" {
			label += " " + tab.ID
			closeLabel += " " + tab.ID
		}
		button := native.ButtonBase(c).Label(label).Grow(1).MinWidth(0).FillHeight().Padding(0, 12)
		if tab.ID != "" {
			button.Drag(paneTabDrag{Workspace: root, Pane: pane.ID, Tab: tab.ID})
			if button.Dragging() {
				a.paneTabDragging = true
			}
		}
		button.Children(func() {
			native.Text(c, "新标签页").FontSize(12).TextColor(theme.Text).SingleLine()
		})
		if button.Clicked() {
			if tab.ID == "" {
				a.focusPane(root, pane)
			} else {
				a.selectWorkspaceTab(tab)
			}
		}
		close := native.ButtonBase(c).Label(closeLabel).Size(24, 24).Radius(8).Padding(6)
		close.Children(func() { native.Icon(c, tabCloseIcon).Size(13, 13).TextColor(theme.TextMuted) })
		if close.Clicked() {
			if tab.ID == "" || len(pane.Tabs) == 1 && len(paneLeaves(a.layoutFor(root).Root)) > 1 {
				a.focusPane(root, pane)
				a.closeCurrentPage()
			} else {
				a.removeWorkspaceTab(tab.ID)
			}
		}
	})
}
