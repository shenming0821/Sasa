package ui

import (
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func (a *App) tabFolder(tab storage.Tab) (string, bool) {
	if tab.Kind == "session" {
		for _, target := range a.sessions {
			if target.id == tab.SessionID && target.open {
				return a.workspaceRoot(target.State.Cwd), true
			}
		}
		return "", false
	}
	return a.workspaceRoot(tab.Workspace), true
}

func (a *App) folderTabs(root string) []storage.Tab {
	var tabs []storage.Tab
	for _, tab := range a.tabs {
		if folder, valid := a.tabFolder(tab); valid && folder == root {
			tabs = append(tabs, tab)
		}
	}
	return tabs
}

func (a *App) rememberFolderTab() {
	for _, tab := range a.tabs {
		if folder, valid := a.tabFolder(tab); valid && tab.ID == a.activeTab {
			if a.folderSelectedTabs == nil {
				a.folderSelectedTabs = map[string]string{}
			}
			a.folderSelectedTabs[folder] = tab.ID
			return
		}
	}
}

func (a *App) refreshFolderSelection() {
	for _, tab := range a.tabs {
		if folder, valid := a.tabFolder(tab); valid && tab.ID == a.activeTab {
			a.selectWorkspace(folder)
			return
		}
	}
	a.selectWorkspace(a.activeWorkspace)
}

func (a *App) restoreFolderTab(root string) bool {
	tabs := a.folderTabs(root)
	for _, tab := range tabs {
		if tab.ID == a.folderSelectedTabs[root] {
			a.selectWorkspaceTab(tab)
			return true
		}
	}
	if len(tabs) > 0 {
		a.selectWorkspaceTab(tabs[0])
		return true
	}
	return false
}

func (a *App) selectAfterTabClose(id string, previous []storage.Tab) {
	for folder, selected := range a.folderSelectedTabs {
		if selected == id {
			delete(a.folderSelectedTabs, folder)
		}
	}
	if a.activeTab != id {
		return
	}
	a.activeTab = ""
	index := 0
	for position, tab := range previous {
		if tab.ID == id {
			index = position
			break
		}
	}
	if index > 0 {
		a.selectWorkspaceTab(previous[index-1])
	} else if len(previous) > 1 {
		a.selectWorkspaceTab(previous[1])
	}
}

func (a *App) folderTabIndicator(c *native.Context, root string, left float32) {
	active := len(a.folderTabs(root)) > 0
	native.Box(c.Key("folder-tabs-indicator")).Absolute().Left(left).Top(0).Bottom(0).Width(8).AlignItems(native.Center).Justify(native.Center).Children(func() {
		if active {
			native.Box(c).Label("有打开的页签 "+root).Size(6, 6).Radius(3).Background(c.Theme().Success)
		}
	})
}
