package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/editor"
	"github.com/shenming0821/Fuyu/internal/workspace"
)

type fileActionState struct {
	open                        bool
	root, path, kind, name, err string
	trash                       func(string) error
	copy                        func(string) error
}

func (a *App) fileMenu(root, path string, editing, dir bool) func(*native.Menu) {
	return a.fileMenuAt(root, path, editing, dir, nil)
}

// fileMenuAt adds a "复制路径和行号" item when line is not nil: line reports
// the line the menu opened at, which the editor's secondary click sets as
// the caret.
func (a *App) fileMenuAt(root, path string, editing, dir bool, line func() int) func(*native.Menu) {
	return func(menu *native.Menu) {
		copyText := a.fileAction.copy
		if copyText == nil {
			copyText = func(text string) error {
				mygo.Clipboard.WriteText(text)
				return nil
			}
		}
		if editing {
			menu.EditItems()
			menu.Separator()
		}
		for _, label := range []string{"复制路径", "复制相对路径"} {
			if menu.Item(label).Chosen() {
				text := path
				if label == "复制相对路径" {
					var err error
					text, err = filepath.Rel(root, path)
					if err != nil {
						a.beginFileAction(root, path, "错误")
						a.fileAction.err = err.Error()
						return
					}
				}
				if err := copyText(text); err != nil {
					a.beginFileAction(root, path, "错误")
					a.fileAction.err = "复制路径失败：" + err.Error()
				}
			}
		}
		if line != nil && menu.Item("复制路径和行号").Chosen() {
			if err := copyText(fmt.Sprintf("%s:%d", path, line()+1)); err != nil {
				a.beginFileAction(root, path, "错误")
				a.fileAction.err = "复制路径失败：" + err.Error()
			}
		}
		if dir {
			menu.Separator()
			if menu.Item("在文件夹中搜索").Chosen() {
				a.searchInFolder(root, path)
			}
		}
		menu.Separator()
		for _, kind := range []string{"重命名", "删除"} {
			if menu.Item(kind).Disabled(path == root).Chosen() {
				a.beginFileAction(root, path, kind)
			}
		}
	}
}

// searchInFolder opens the files panel content search with the folder as its
// include pattern. The folder is already inside the shown workspace (the tree
// only offers the item on its own rows).
func (a *App) searchInFolder(root, path string) {
	w := a.workspace(a.workspaceRoot(root))
	if !workspace.Contains(w.root, path) {
		return
	}
	w.searchInclude = ""
	w.searchDetails = true
	if rel, err := filepath.Rel(w.root, path); err == nil && rel != "." {
		w.searchInclude = "./" + filepath.ToSlash(rel)
	}
	w.searchFocus = true
	a.rightTab = "files"
	a.historyOpen = true
}

// renamedSearchInclude keeps a single folder include, as searchInFolder
// writes it, pointing at the folder after a rename and drops it on delete.
func renamedSearchInclude(root, include, path, target string) string {
	rel, ok := strings.CutPrefix(include, "./")
	if !ok || strings.ContainsAny(rel, "*?[{,") {
		return include
	}
	folder := filepath.Join(root, filepath.FromSlash(rel))
	if !workspace.Contains(path, folder) {
		return include
	}
	if target == "" {
		return ""
	}
	moved, err := filepath.Rel(root, renamedEditorPath(folder, path, target))
	if err != nil {
		return include
	}
	return "./" + filepath.ToSlash(moved)
}

func (a *App) beginFileAction(root, path, kind string) {
	a.fileAction.open = true
	a.fileAction.root, a.fileAction.path, a.fileAction.kind = root, path, kind
	a.fileAction.name, a.fileAction.err = filepath.Base(path), ""
}

func (a *App) fileActionView(c *native.Context) {
	state := &a.fileAction
	native.Modal(c.Key("file-action"), &state.open, func() {
		if c.Shortcut(native.Cmd, native.KeyW) {
			state.open = false
		}
		width, _ := c.Size()
		native.Column(c).Width(min(width-64, 440)).Gap(12).Children(func() {
			native.Text(c, state.kind).FontSize(16).Bold()
			native.Text(c, state.path).FontSize(11).TextColor(c.Theme().TextMuted).FillWidth().MaxLines(3)
			submit := false
			if state.kind == "重命名" {
				input := native.TextInput(c, &state.name).Label("文件名称").FillWidth().AutoFocus()
				submit = input.Submitted() && !input.Composing()
			} else if state.kind == "删除" {
				native.Text(c, "移入废纸篓？目录中的内容也会移入废纸篓。未保存的编辑保留在标签中，可重新创建文件。").FontSize(12).FillWidth()
			}
			if state.err != "" {
				native.Text(c, state.err).FontSize(12).TextColor(c.Theme().Danger).FillWidth()
			}
			native.Row(c).FillWidth().Gap(8).Justify(native.End).Children(func() {
				if actionButton(c, "取消文件操作").Clicked() {
					state.open = false
				}
				if state.kind != "错误" && (actionButton(c, "确认"+state.kind).Clicked() || submit) {
					var err error
					if state.kind == "重命名" {
						err = a.renameEditorPath(state.root, state.path, state.name)
					} else {
						err = a.deleteEditorPath(state.root, state.path)
					}
					if err != nil {
						state.err = err.Error()
					} else {
						state.open = false
					}
				}
			})
		})
	})
}

func (a *App) editorPathPages(root, path string) ([]*filePage, error) {
	if a.quitDialog || a.hasGitWrites() {
		return nil, fmt.Errorf("请等待当前文件或 Git 操作完成后重试")
	}
	if !filepath.IsAbs(root) || !workspace.Contains(root, path) || path == root {
		return nil, fmt.Errorf("只能操作工作区内的文件或子目录")
	}
	for _, state := range a.workspaces {
		if workspace.Contains(path, state.root) {
			return nil, fmt.Errorf("目标包含已打开的工作区：%s", state.root)
		}
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil || !workspace.Contains(resolvedRoot, parent) {
		return nil, fmt.Errorf("目标父目录位于工作区之外")
	}
	if _, err := os.Lstat(path); err != nil {
		return nil, err
	}
	var pages []*filePage
	for _, page := range a.files {
		if workspace.Contains(path, page.path) {
			if page.saving || page.checkpointBusy || page.loading || page.checking {
				return nil, fmt.Errorf("请等待文件读写完成后重试：%s", page.path)
			}
			if page.kind == "file" {
				pages = append(pages, page)
			}
		}
	}
	return pages, nil
}

func renamedEditorPath(path, source, target string) string {
	rel, _ := filepath.Rel(source, path)
	return filepath.Join(target, rel)
}

func (a *App) renameEditorPath(root, path, name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("请输入有效的单个文件或目录名称")
	}
	pages, err := a.editorPathPages(root, path)
	if err != nil {
		return err
	}
	target := filepath.Join(filepath.Dir(path), name)
	if target == path {
		return nil
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("目标名称已存在：%s", target)
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, page := range a.files {
		if page.kind == "file" && workspace.Contains(target, page.path) {
			return fmt.Errorf("目标路径已有打开的标签，请先处理该标签：%s", page.path)
		}
	}
	var recoveries []editor.Recovery
	if dir := a.recoveryDir(); dir != "" {
		for _, page := range pages {
			if page.kind != "file" || !page.dirty() {
				continue
			}
			next := renamedEditorPath(page.path, path, target)
			base := page.file
			base.Path = next
			if page.path != path {
				parent, err := filepath.EvalSymlinks(filepath.Dir(path))
				if err != nil {
					return err
				}
				relative, _ := filepath.Rel(path, filepath.Dir(page.path))
				base.Stamp.Parent = filepath.Join(parent, name, relative)
			}
			recovery := editor.Recovery{ID: editor.RecoveryID(page.root, next), Workspace: page.root, Path: next, Text: page.buffer.String(), Base: base}
			if err := editor.WriteRecovery(dir, recovery); err != nil {
				for _, previous := range recoveries {
					_ = editor.RemoveRecovery(dir, previous.ID)
				}
				return fmt.Errorf("保存重命名后的草稿：%w", err)
			}
			recoveries = append(recoveries, recovery)
		}
	}
	if err := editor.Rename(path, target); err != nil {
		for _, recovery := range recoveries {
			_ = editor.RemoveRecovery(a.recoveryDir(), recovery.ID)
		}
		return fmt.Errorf("重命名失败：%w", err)
	}
	var cleanupErr error
	for _, page := range pages {
		old := page.path
		oldID := page.id
		a.cancelFileRead(page)
		page.diskEpoch++
		page.path = renamedEditorPath(old, path, target)
		page.id = "file-" + editor.RecoveryID(page.root, page.path)
		delete(a.files, oldID)
		a.files[page.id] = page
		for _, layout := range a.layouts {
			for _, pane := range paneLeaves(layout.Root) {
				for index, id := range pane.Tabs {
					if id == oldID {
						pane.Tabs[index] = page.id
					}
				}
				if pane.Active == oldID {
					pane.Active = page.id
				}
			}
		}
		if a.activeTab == oldID {
			a.activeTab = page.id
		}
		for folder, selected := range a.folderSelectedTabs {
			if selected == oldID {
				a.folderSelectedTabs[folder] = page.id
			}
		}
		for index := range a.tabs {
			if a.tabs[index].ID == oldID {
				a.tabs[index].ID, a.tabs[index].Path = page.id, page.path
			}
		}
		for _, comparison := range a.files {
			if comparison.comparisonSource == oldID {
				comparison.comparisonSource = page.id
			}
		}
		page.file.Path = page.path
		if parent, err := filepath.EvalSymlinks(filepath.Dir(page.path)); err == nil {
			page.file.Stamp.Parent = parent
		}
		page.overwriteBase = nil
		page.focusOwner = nil
		page.highlightVersion, page.contentWidth = 0, 0
		if dir := a.recoveryDir(); dir != "" && page.kind == "file" {
			if err := editor.RemoveRecovery(dir, editor.RecoveryID(page.root, old)); err != nil {
				cleanupErr = fmt.Errorf("已重命名，清理旧草稿失败：%w", err)
			}
		}
	}
	a.refreshEditorPath(path, target)
	if err := a.persist(); err != nil {
		a.fileAction.kind = "错误"
		return fmt.Errorf("文件已重命名，保存标签状态失败：%w", err)
	}
	if cleanupErr != nil {
		a.fileAction.kind = "错误"
	}
	return cleanupErr
}

func (a *App) deleteEditorPath(root, path string) error {
	pages, err := a.editorPathPages(root, path)
	if err != nil {
		return err
	}
	trash := a.fileAction.trash
	if trash == nil {
		trash = mygo.Shell.TrashItem
	}
	if dir := a.recoveryDir(); dir != "" {
		for _, page := range pages {
			if page.dirty() {
				recovery := editor.Recovery{ID: editor.RecoveryID(page.root, page.path), Workspace: page.root, Path: page.path, Text: page.buffer.String(), Base: page.file}
				if err := editor.WriteRecovery(dir, recovery); err != nil {
					return fmt.Errorf("保存删除前的草稿：%w", err)
				}
			}
		}
	}
	if err := trash(path); err != nil {
		return fmt.Errorf("移入废纸篓失败：%w", err)
	}
	for _, page := range pages {
		if page.kind == "file" && page.dirty() {
			page.missing = true
			page.err = "文件已移入废纸篓，未保存的编辑仍保留"
		} else {
			a.finishCloseFile(page)
		}
	}
	a.refreshEditorPath(path, "")
	if err := a.persist(); err != nil {
		a.fileAction.kind = "错误"
		return fmt.Errorf("文件已移入废纸篓，保存标签状态失败：%w", err)
	}
	return nil
}

func (a *App) refreshEditorPath(path, target string) {
	for _, state := range a.workspaces {
		if !workspace.Contains(state.root, path) {
			continue
		}
		var expanded []string
		for child, open := range state.expanded {
			if target != "" && open && workspace.Contains(path, child) {
				expanded = append(expanded, renamedEditorPath(child, path, target))
			}
		}
		a.forgetDirectory(state, path)
		for _, child := range expanded {
			state.expanded[child] = true
		}
		if target != "" {
			state.selected = target
			state.reveal = true
		}
		if state.searchCancel != nil {
			state.searchCancel()
		}
		state.searchInclude = renamedSearchInclude(state.root, state.searchInclude, path, target)
		state.generation++
		state.searchLoading = false
		state.searchQuery = ""
		state.error = ""
		parent := filepath.Dir(path)
		if task, pending := state.directoryTasks[parent]; pending {
			task.cancel()
			delete(state.directoryTasks, parent)
			state.loading[parent] = false
		}
		a.loadDirectory(state, parent)
		a.cancelGitRefresh(state)
		a.refreshGit(state)
	}
}
