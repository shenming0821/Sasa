package ui

import (
	"fmt"
	"math"
	"slices"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/storage"
)

type paneState struct {
	layouts           map[string]storage.WorkspaceLayout
	paneSerial        int
	emptyTabSerial    int
	renderingPane     *storage.PaneNode
	paneHandles       map[string]*native.Handle
	paneBounds        map[string]native.Rect
	paneErrors        map[string]string
	visibleTerms      map[string]bool
	paneFocusPending  string
	workspaceAliases  map[string]string
	paneNativeFocus   string
	paneTabDragging   bool
	paneClosing       map[string]bool
	paneClosePending  *paneCloseRequest
	tabReorderPending *paneTabReorder
}

type paneCloseRequest struct {
	root string
	pane *storage.PaneNode
}

type paneTabDrag struct {
	Workspace string
	Pane      string
	Tab       string
}

type paneTabReorder struct {
	pane   *storage.PaneNode
	source paneTabDrag
	index  int
}

func paneLeaves(node *storage.PaneNode) []*storage.PaneNode {
	if node == nil {
		return nil
	}
	if node.Axis == "" {
		return []*storage.PaneNode{node}
	}
	return append(paneLeaves(node.First), paneLeaves(node.Second)...)
}

func (a *App) newPane() *storage.PaneNode {
	a.paneSerial++
	return &storage.PaneNode{ID: fmt.Sprintf("pane-%d", a.paneSerial)}
}

func (a *App) layoutFor(root string) storage.WorkspaceLayout {
	if a.layouts == nil {
		a.layouts = map[string]storage.WorkspaceLayout{}
	}
	layout := a.layouts[root]
	if layout.Root == nil {
		layout.Root = a.newPane()
		layout.Focused = layout.Root.ID
		a.layouts[root] = layout
	}
	valid := map[string]bool{}
	for _, tab := range a.folderTabs(root) {
		valid[tab.ID] = true
	}
	seen := map[string]bool{}
	leaves := paneLeaves(layout.Root)
	focused := leaves[0]
	for _, pane := range leaves {
		if pane.ID == layout.Focused {
			focused = pane
		}
		pane.Tabs = slices.DeleteFunc(pane.Tabs, func(id string) bool {
			if !valid[id] || seen[id] {
				return true
			}
			seen[id] = true
			return false
		})
		if !slices.Contains(pane.Tabs, pane.Active) {
			pane.Active = ""
			if len(pane.Tabs) > 0 {
				pane.Active = pane.Tabs[0]
			}
		}
	}
	for _, tab := range a.folderTabs(root) {
		if !seen[tab.ID] {
			focused.Tabs = append(focused.Tabs, tab.ID)
		}
	}
	if focused.Active == "" && len(focused.Tabs) > 0 {
		focused.Active = focused.Tabs[0]
	}
	layout.Focused = focused.ID
	a.layouts[root] = layout
	return layout
}

func focusedPane(layout storage.WorkspaceLayout) *storage.PaneNode {
	for _, pane := range paneLeaves(layout.Root) {
		if pane.ID == layout.Focused {
			return pane
		}
	}
	return nil
}

func (a *App) setPaneSelection(pane *storage.PaneNode) {
	a.activeTab = pane.Active
	for _, target := range a.sessions {
		if sessionTabID(target.id) == pane.Active {
			a.session = target
			break
		}
	}
}

func (a *App) focusPane(root string, pane *storage.PaneNode) {
	a.paneNativeFocus = ""
	a.selectWorkspace(root)
	layout := a.layoutFor(root)
	if !slices.Contains(paneLeaves(layout.Root), pane) {
		return
	}
	layout.Focused = pane.ID
	a.layouts[root] = layout
	a.setPaneSelection(pane)
	a.rememberFolderTab()
}

func (a *App) activatePaneTab(id, root string, move bool) {
	a.paneNativeFocus = ""
	alreadyPlaced := false
	if existing, ok := a.layouts[root]; ok {
		for _, pane := range paneLeaves(existing.Root) {
			if slices.Contains(pane.Tabs, id) {
				alreadyPlaced = true
				break
			}
		}
	}
	layout := a.layoutFor(root)
	target := focusedPane(layout)
	if a.renderingPane != nil && slices.Contains(paneLeaves(layout.Root), a.renderingPane) {
		target = a.renderingPane
	}
	for _, pane := range paneLeaves(layout.Root) {
		if !slices.Contains(pane.Tabs, id) {
			continue
		}
		if !move && alreadyPlaced {
			target = pane
		} else if pane != target {
			pane.Tabs = slices.DeleteFunc(pane.Tabs, func(other string) bool { return other == id })
			if pane.Active == id {
				pane.Active = ""
				if len(pane.Tabs) > 0 {
					pane.Active = pane.Tabs[0]
				}
			}
		}
	}
	if (!alreadyPlaced || move) && !a.isEmptyTab(id) && a.isEmptyTab(target.Active) {
		emptyID := target.Active
		target.Tabs = slices.DeleteFunc(target.Tabs, func(other string) bool { return other == id })
		if index := slices.Index(target.Tabs, emptyID); index >= 0 {
			target.Tabs[index] = id
		} else {
			target.Tabs = append(target.Tabs, id)
		}
		a.tabs = slices.DeleteFunc(a.tabs, func(tab storage.Tab) bool { return tab.ID == emptyID })
		for folder, selected := range a.folderSelectedTabs {
			if selected == emptyID {
				delete(a.folderSelectedTabs, folder)
			}
		}
	} else if !slices.Contains(target.Tabs, id) {
		target.Tabs = append(target.Tabs, id)
	}
	target.Active = id
	layout.Focused = target.ID
	a.layouts[root] = layout
}

func (a *App) paneTabs() []storage.Tab {
	if a.renderingPane == nil {
		return a.folderTabs(a.activeWorkspace)
	}
	var tabs []storage.Tab
	for _, id := range a.renderingPane.Tabs {
		for _, tab := range a.tabs {
			if tab.ID == id {
				tabs = append(tabs, tab)
				break
			}
		}
	}
	return tabs
}

func (a *App) withPane(root string, pane *storage.PaneNode, build func()) {
	previous := a.renderingPane
	a.renderingPane = pane
	a.setPaneSelection(pane)
	defer func() {
		a.renderingPane = previous
		if current := focusedPane(a.layoutFor(a.activeWorkspace)); current != nil {
			a.setPaneSelection(current)
		}
	}()
	build()
}

func (a *App) paneTextKey(callback func(native.TextKey) bool) func(native.TextKey) bool {
	pane, root := a.renderingPane, a.activeWorkspace
	if pane == nil {
		return callback
	}
	return func(key native.TextKey) (handled bool) {
		a.focusPane(root, pane)
		a.withPane(root, pane, func() { handled = callback(key) })
		return
	}
}

func (a *App) panePaste(callback func(string) bool) func(string) bool {
	pane, root := a.renderingPane, a.activeWorkspace
	if pane == nil {
		return callback
	}
	return func(text string) (handled bool) {
		a.focusPane(root, pane)
		a.withPane(root, pane, func() { handled = callback(text) })
		return
	}
}

func (a *App) splitPane(root string, pane *storage.PaneNode, axis string) *storage.PaneNode {
	layout := a.layoutFor(root)
	if !slices.Contains(paneLeaves(layout.Root), pane) {
		return nil
	}
	first := *pane
	second := a.newPane()
	branch := a.newPane()
	*pane = storage.PaneNode{ID: branch.ID, Axis: axis, Ratio: 50, First: &first, Second: second}
	layout.Focused = second.ID
	a.layouts[root] = layout
	a.newEmptyTab(root, second)
	a.paneFocusPending = second.ID
	return second
}

func (a *App) movePaneTab(root string, target *storage.PaneNode, source paneTabDrag) bool {
	if source.Workspace != root {
		return false
	}
	layout := a.layoutFor(root)
	leaves := paneLeaves(layout.Root)
	if !slices.Contains(leaves, target) {
		return false
	}
	var origin *storage.PaneNode
	for _, pane := range leaves {
		if pane.ID == source.Pane && slices.Contains(pane.Tabs, source.Tab) {
			origin = pane
			break
		}
	}
	if origin == nil || origin == target {
		return false
	}
	a.focusPane(root, target)
	a.activatePaneTab(source.Tab, root, true)
	a.setPaneSelection(target)
	a.paneFocusPending = target.ID
	delete(a.paneErrors, target.ID)
	a.syncFolderTabOrder(root)
	return true
}

// movePaneTabAt inserts the dragged tab into target at index: a reorder when
// it already belongs there, otherwise a positioned move from its pane. A drop
// past the last tab reuses movePaneTab's append behaviour.
func (a *App) movePaneTabAt(root string, target *storage.PaneNode, source paneTabDrag, index int) bool {
	if source.Workspace != root {
		return false
	}
	layout := a.layoutFor(root)
	leaves := paneLeaves(layout.Root)
	if !slices.Contains(leaves, target) {
		return false
	}
	var origin *storage.PaneNode
	for _, pane := range leaves {
		if pane.ID == source.Pane && slices.Contains(pane.Tabs, source.Tab) {
			origin = pane
			break
		}
	}
	if origin == nil {
		return false
	}
	index = max(0, min(index, len(target.Tabs)))
	if origin == target {
		from := slices.Index(target.Tabs, source.Tab)
		if from < 0 || from == index || from+1 == index {
			return false
		}
		target.Tabs = slices.Delete(target.Tabs, from, from+1)
		if from < index {
			index--
		}
		target.Tabs = slices.Insert(target.Tabs, index, source.Tab)
		target.Active = source.Tab
		a.focusPane(root, target)
		a.syncFolderTabOrder(root)
		return true
	}
	if index == len(target.Tabs) {
		return a.movePaneTab(root, target, source)
	}
	origin.Tabs = slices.DeleteFunc(origin.Tabs, func(id string) bool { return id == source.Tab })
	if origin.Active == source.Tab {
		origin.Active = ""
		if len(origin.Tabs) > 0 {
			origin.Active = origin.Tabs[0]
		}
	}
	if !a.isEmptyTab(source.Tab) && a.isEmptyTab(target.Active) {
		emptyID := target.Active
		if at := slices.Index(target.Tabs, emptyID); at >= 0 {
			target.Tabs = slices.Delete(target.Tabs, at, at+1)
			if at < index {
				index--
			}
		}
		a.tabs = slices.DeleteFunc(a.tabs, func(tab storage.Tab) bool { return tab.ID == emptyID })
		for folder, selected := range a.folderSelectedTabs {
			if selected == emptyID {
				delete(a.folderSelectedTabs, folder)
			}
		}
	}
	index = min(index, len(target.Tabs))
	target.Tabs = slices.Insert(target.Tabs, index, source.Tab)
	target.Active = source.Tab
	a.focusPane(root, target)
	a.paneFocusPending = target.ID
	delete(a.paneErrors, target.ID)
	a.syncFolderTabOrder(root)
	return true
}

// syncFolderTabOrder rewrites the flat order of root's tabs to match the
// panes' displayed order, so neighbour selection, restore and the saved
// workspace tab order agree with the tab strips.
func (a *App) syncFolderTabOrder(root string) {
	var order []string
	for _, pane := range paneLeaves(a.layoutFor(root).Root) {
		order = append(order, pane.Tabs...)
	}
	byID := make(map[string]storage.Tab, len(a.tabs))
	for _, tab := range a.tabs {
		byID[tab.ID] = tab
	}
	next := 0
	for i := range a.tabs {
		if next >= len(order) {
			return
		}
		if folder, valid := a.tabFolder(a.tabs[i]); !valid || folder != root {
			continue
		}
		if tab, ok := byID[order[next]]; ok {
			a.tabs[i] = tab
		}
		next++
	}
}

func (a *App) paneMenu(root string, pane *storage.PaneNode) func(*native.Menu) {
	return func(menu *native.Menu) {
		bounds := a.paneBounds[pane.ID]
		if menu.Item("左右分屏").Disabled(bounds.W < 640 || bounds.H < 220).Chosen() {
			a.splitPane(root, pane, "horizontal")
		}
		if menu.Item("上下分屏").Disabled(bounds.H < 440 || bounds.W < 320).Chosen() {
			a.splitPane(root, pane, "vertical")
		}
		if len(paneLeaves(a.layoutFor(root).Root)) == 1 {
			return
		}
		menu.Separator()
		if menu.Item("关闭窗口").Chosen() {
			a.paneClosePending = &paneCloseRequest{root: root, pane: pane}
		}
	}
}

func (a *App) closePane(root string, pane *storage.PaneNode) {
	if a.quitDialog || a.paneClosing[pane.ID] || !slices.Contains(paneLeaves(a.layoutFor(root).Root), pane) {
		return
	}
	if a.paneClosing == nil {
		a.paneClosing = map[string]bool{}
	}
	a.paneClosing[pane.ID] = true
	a.focusPane(root, pane)
	tabs := slices.Clone(pane.Tabs)
	var closeNext func()
	closeNext = func() {
		if !slices.Contains(paneLeaves(a.layoutFor(root).Root), pane) {
			delete(a.paneClosing, pane.ID)
			return
		}
		if len(tabs) == 0 {
			delete(a.paneClosing, pane.ID)
			a.removePane(root, pane)
			return
		}
		id := tabs[0]
		tabs = tabs[1:]
		if !slices.Contains(pane.Tabs, id) {
			closeNext()
			return
		}
		done := func(closed bool) {
			if closed {
				closeNext()
			} else {
				delete(a.paneClosing, pane.ID)
			}
		}
		if page := a.files[id]; page != nil {
			a.closeFileWithResult(page, done)
			return
		}
		if page := a.terminals[id]; page != nil {
			a.closeTerminalWithResult(page, done)
			return
		}
		for _, target := range a.sessions {
			if sessionTabID(target.id) == id {
				a.closeSession(target)
				closeNext()
				return
			}
		}
		a.removeWorkspaceTab(id)
		closeNext()
	}
	closeNext()
}

func (a *App) removePane(root string, pane *storage.PaneNode) {
	layout := a.layoutFor(root)
	leaves := paneLeaves(layout.Root)
	index := slices.Index(leaves, pane)
	if index < 0 || len(pane.Tabs) != 0 {
		return
	}
	if len(leaves) > 1 {
		target := leaves[max(0, index-1)]
		if index == 0 {
			target = leaves[1]
		}
		var remove func(*storage.PaneNode) *storage.PaneNode
		remove = func(node *storage.PaneNode) *storage.PaneNode {
			if node == pane {
				return nil
			}
			if node.Axis == "" {
				return node
			}
			node.First, node.Second = remove(node.First), remove(node.Second)
			if node.First == nil {
				return node.Second
			}
			if node.Second == nil {
				return node.First
			}
			return node
		}
		layout.Root = remove(layout.Root)
		layout.Focused = target.ID
		delete(a.paneHandles, pane.ID)
		delete(a.paneBounds, pane.ID)
		delete(a.paneErrors, pane.ID)
	}
	a.layouts[root] = layout
	if a.activeWorkspace == root {
		a.focusPane(root, focusedPane(layout))
		a.paneFocusPending = layout.Focused
	}
}

func (a *App) focusVisiblePane(c *native.Context) {
	if a.layouts == nil || a.paneFocusPending != "" || a.paneNativeFocus != "" {
		return
	}
	layout := a.layoutFor(a.activeWorkspace)
	for _, pane := range paneLeaves(layout.Root) {
		if handle := a.paneHandles[pane.ID]; handle != nil && handle.FocusWithin(c) && pane.ID != layout.Focused {
			a.focusPane(a.activeWorkspace, pane)
			return
		}
	}
}

func (a *App) focusPressedPane(element native.Element) {
	if a.renderingPane != nil && element.Pressed() {
		a.focusPane(a.activeWorkspace, a.renderingPane)
	}
}

func (a *App) paneIsFocused() bool {
	return a.renderingPane == nil || a.layouts[a.activeWorkspace].Focused == a.renderingPane.ID
}

func (a *App) syncActivePane() {
	if a.renderingPane != nil || a.activeTab == "" {
		return
	}
	for _, tab := range a.tabs {
		if tab.ID == a.activeTab {
			if root, valid := a.tabFolder(tab); valid {
				if root != a.activeWorkspace && (root != "" || a.activeWorkspace == "") {
					if previous, exists := a.layouts[a.activeWorkspace]; exists && len(a.folderTabs(a.activeWorkspace)) == 0 && a.layouts[root].Root == nil {
						a.layouts[root] = previous
						delete(a.layouts, a.activeWorkspace)
					}
					a.selectWorkspace(root)
				}
				current := focusedPane(a.layoutFor(root))
				if current.Active != tab.ID {
					a.activatePaneTab(tab.ID, root, false)
				}
			}
			return
		}
	}
}

func validPaneCopy(node *storage.PaneNode, seen map[string]bool, depth int) *storage.PaneNode {
	if node == nil || depth > 32 || node.ID == "" || seen[node.ID] {
		return nil
	}
	seen[node.ID] = true
	copy := *node
	copy.Tabs = slices.Clone(node.Tabs)
	if node.Axis == "" {
		copy.First, copy.Second = nil, nil
		return &copy
	}
	if node.Axis != "horizontal" && node.Axis != "vertical" {
		return nil
	}
	copy.First = validPaneCopy(node.First, seen, depth+1)
	copy.Second = validPaneCopy(node.Second, seen, depth+1)
	if copy.First == nil || copy.Second == nil {
		return nil
	}
	if math.IsNaN(float64(copy.Ratio)) || math.IsInf(float64(copy.Ratio), 0) || copy.Ratio <= 0 || copy.Ratio >= 100 {
		copy.Ratio = 50
	}
	copy.Tabs, copy.Active = nil, ""
	return &copy
}

func (a *App) restorePaneLayouts(saved map[string]storage.WorkspaceLayout) {
	a.layouts = map[string]storage.WorkspaceLayout{}
	seen := map[string]bool{}
	for root, layout := range saved {
		layout.Root = validPaneCopy(layout.Root, seen, 0)
		if layout.Root != nil {
			a.layouts[a.workspaceRoot(root)] = layout
		}
	}
	for id := range seen {
		var serial int
		if _, err := fmt.Sscanf(id, "pane-%d", &serial); err == nil {
			a.paneSerial = max(a.paneSerial, serial)
		}
	}
	for _, tab := range a.tabs {
		if root, valid := a.tabFolder(tab); valid {
			a.layoutFor(root)
		}
	}
	layout := a.layoutFor(a.activeWorkspace)
	if len(saved) == 0 {
		pane := focusedPane(layout)
		if slices.Contains(pane.Tabs, a.activeTab) {
			pane.Active = a.activeTab
		}
	} else {
		a.setPaneSelection(focusedPane(layout))
	}
}
