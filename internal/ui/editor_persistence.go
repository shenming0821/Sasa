package ui

import (
	"fmt"
	"path/filepath"
	"slices"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/editor"
	"github.com/shenming0821/Sasa/internal/storage"
	"github.com/shenming0821/Sasa/internal/terminal"
)

func (a *App) restoreWorkspaceTabs(snapshot storage.Snapshot, cwd string) error {
	a.tabs = nil
	a.files = map[string]*filePage{}
	a.terminals = map[string]*terminalPage{}
	a.activeTab = ""
	a.activeWorkspace = a.workspaceRoot(snapshot.ActiveWorkspace)
	for _, tab := range snapshot.WorkspaceTabs {
		a.workspaceRoot(tab.Workspace)
	}
	a.folderSelectedTabs = map[string]string{}
	a.rightTab = snapshot.RightTab
	if a.rightTab == "" {
		a.rightTab = "history"
		if len(snapshot.Sessions) == 0 {
			a.rightTab = "files"
		}
	}
	if a.rightTab != "files" && a.rightTab != "history" && a.rightTab != "git" {
		return fmt.Errorf("未知右侧面板 %s，保留原设置", a.rightTab)
	}
	for root, paths := range snapshot.ExpandedFiles {
		w := a.workspace(root)
		for _, path := range paths {
			w.expanded[path] = true
		}
	}
	for root, message := range snapshot.GitDrafts {
		a.workspace(root).gitMessage = message
	}
	if snapshot.WorkspaceTabs == nil {
		for _, s := range a.sessions {
			if s.open {
				a.ensureSessionTab(s)
			}
		}
		if a.open {
			a.activeTab = sessionTabID(a.session.id)
		}
	} else {
		for _, s := range a.sessions {
			s.open = false
		}
		seen := map[string]bool{}
		for _, tab := range snapshot.WorkspaceTabs {
			if tab.ID == "" || seen[tab.ID] {
				continue
			}
			seen[tab.ID] = true
			switch tab.Kind {
			case "session":
				for _, s := range a.sessions {
					if s.id == tab.SessionID && !s.isBlankUnbound() {
						s.open = true
						a.tabs = append(a.tabs, tab)
						if tab.ID == snapshot.ActiveTabID {
							a.session = s
						}
					}
				}
			case "empty":
				if tab.Workspace != "" && !filepath.IsAbs(tab.Workspace) {
					return fmt.Errorf("新标签页路径损坏，保留原设置")
				}
				a.tabs = append(a.tabs, tab)
			case "terminal":
				if !filepath.IsAbs(tab.Workspace) {
					return fmt.Errorf("终端标签路径损坏，保留原设置")
				}
				tab.TerminalEngine = terminal.Normalize(tab.TerminalEngine)
				a.tabs = append(a.tabs, tab)
				a.terminals[tab.ID] = &terminalPage{id: tab.ID, root: tab.Workspace, engine: tab.TerminalEngine, focus: true}
			case "file", "diff":
				if !filepath.IsAbs(tab.Workspace) || !filepath.IsAbs(tab.Path) {
					return fmt.Errorf("文件标签路径损坏，保留原设置")
				}
				a.tabs = append(a.tabs, tab)
				a.files[tab.ID] = &filePage{id: tab.ID, kind: tab.Kind, compare: tab.Compare, root: tab.Workspace, path: tab.Path, caret: tab.Caret, anchor: tab.Caret, resync: true, scroll: native.ScrollState{Y: tab.ScrollY}, preview: tab.Kind == "file" && tab.Preview}
			default:
				return fmt.Errorf("未知标签类型 %s，保留原设置", tab.Kind)
			}
		}
		a.activeTab = snapshot.ActiveTabID
		if !slices.ContainsFunc(a.tabs, func(tab storage.Tab) bool { return tab.ID == a.activeTab }) {
			a.activeTab = ""
			if tabs := a.folderTabs(a.workspaceRoot(a.activeWorkspace)); len(tabs) > 0 {
				a.activeTab = tabs[0].ID
				for _, tab := range tabs {
					if tab.ID == snapshot.FolderSelectedTabs[a.activeWorkspace] {
						a.activeTab = tab.ID
					}
				}
			} else if snapshot.ActiveWorkspace == "" && snapshot.ActiveTabID != "" && len(a.tabs) > 0 {
				a.activeTab = a.tabs[0].ID
			}
		}
	}
	if cwd != "" {
		a.session.open = true
		a.ensureSessionTab(a.session)
		a.activeTab = sessionTabID(a.session.id)
	}
	for _, tab := range a.tabs {
		if tab.ID == a.activeTab {
			a.activeWorkspace, _ = a.tabFolder(tab)
			if tab.Kind == "session" {
				for _, target := range a.sessions {
					if target.id == tab.SessionID {
						a.session = target
					}
				}
			}
		}
	}
	if a.activeWorkspace == "" && a.session != nil && a.session.open {
		a.activeWorkspace = a.workspaceRoot(a.State.Cwd)
	}
	recoveries, err := editor.Recoveries(a.recoveryDir())
	if err != nil {
		a.recoveryWarning = err.Error()
	}
	for _, r := range recoveries {
		id := "file-" + editor.RecoveryID(r.Workspace, r.Path)
		d := a.files[id]
		if d == nil {
			d = &filePage{id: id, root: r.Workspace, path: r.Path, kind: "file", resync: true}
			a.files[id] = d
			a.tabs = append(a.tabs, storage.Tab{ID: id, Kind: "file", Workspace: r.Workspace, Path: r.Path})
		}
		d.file = r.Base
		d.file.Path = r.Path
		d.file.Text = r.DiskText
		d.buffer = native.NewTextBuffer(r.Text)
		d.observed = d.buffer.Snapshot()
		d.savedText = r.DiskText
		d.modified = true
		d.preview = false
		d.loaded = true
		d.conflict = true
		d.err = "已恢复未保存编辑，请核对磁盘版本后保存"
		if a.activeTab == "" && (a.activeWorkspace == "" || a.workspaceRoot(r.Workspace) == a.activeWorkspace) {
			a.activeTab = id
			a.activeWorkspace = a.workspaceRoot(r.Workspace)
		}
	}
	for _, tab := range a.tabs {
		if folder, valid := a.tabFolder(tab); valid {
			if a.folderSelectedTabs[folder] == "" || snapshot.FolderSelectedTabs[folder] == tab.ID {
				a.folderSelectedTabs[folder] = tab.ID
			}
		}
	}
	a.rememberFolderTab()
	a.restorePaneLayouts(snapshot.WorkspaceLayouts)
	return nil
}
func (a *App) snapshotWorkspaceTabs(s *storage.Snapshot) {
	a.syncActivePane()
	s.WorkspaceTabs = []storage.Tab{}
	s.ActiveTabID = a.activeTab
	s.ActiveWorkspace = a.activeWorkspace
	s.RightTab = a.rightTab
	s.ExpandedFiles = map[string][]string{}
	s.GitDrafts = map[string]string{}
	s.FolderSelectedTabs = map[string]string{}
	a.rememberFolderTab()
	s.WorkspaceLayouts = map[string]storage.WorkspaceLayout{}
	for root := range a.layouts {
		layout := a.layoutFor(root)
		layout.Root = validPaneCopy(layout.Root, map[string]bool{}, 0)
		s.WorkspaceLayouts[root] = layout
	}
	for _, tab := range a.tabs {
		if tab.Kind == "session" {
			valid := false
			for _, session := range a.sessions {
				if session.id == tab.SessionID && session.open {
					valid = true
					tab.Workspace = a.workspaceRoot(session.State.Cwd)
				}
			}
			if !valid {
				continue
			}
		} else if d := a.files[tab.ID]; d != nil {
			if d.turnReview != nil {
				if s.ActiveTabID == d.id {
					s.ActiveTabID = sessionTabID(a.session.id)
				}
				continue
			}
			tab.Caret = d.caret
			tab.ScrollY = d.scroll.Y
			tab.Preview = d.preview && !d.dirty()
		}
		s.WorkspaceTabs = append(s.WorkspaceTabs, tab)
		if folder, valid := a.tabFolder(tab); valid {
			if s.FolderSelectedTabs[folder] == "" || a.folderSelectedTabs[folder] == tab.ID {
				s.FolderSelectedTabs[folder] = tab.ID
			}
		}
	}
	if len(s.WorkspaceTabs) == 0 {
		s.ActiveTabID = ""
	}
	for root, w := range a.workspaces {
		if w.gitMessage != "" {
			s.GitDrafts[root] = w.gitMessage
		}
		var paths []string
		for path, expanded := range w.expanded {
			if expanded {
				paths = append(paths, path)
			}
		}
		slices.Sort(paths)
		s.ExpandedFiles[root] = paths
	}
}
