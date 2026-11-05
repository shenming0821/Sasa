package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/fsnotify/fsnotify"
	"github.com/shenming0821/Fuyu/internal/editor"
	gitview "github.com/shenming0821/Fuyu/internal/git"
	"github.com/shenming0821/Fuyu/internal/storage"
	"github.com/shenming0821/Fuyu/internal/workspace"
)

type editorState struct {
	fileAction                                fileActionState
	fileWorkers, gitWorkers, highlightWorkers chan struct{}
	fileBackgroundWorkers                     chan struct{}
	tabs                                      []storage.Tab
	folderSelectedTabs                        map[string]string
	activeTab, activeWorkspace, rightTab      string
	files                                     map[string]*filePage
	workspaces                                map[string]*workspacePage
	fileQuitPending                           bool
	fileExitRequested                         bool
	gitQuitPending                            bool
	recoveryWarning                           string
	tabHandles                                map[string]*native.Handle
}
type workspacePage struct {
	gitWorkflow
	root, query, error, selected string
	children                     map[string][]workspace.Entry
	expanded, loading            map[string]bool
	list                         native.ListState
	searchList                   native.ListState
	contentList                  native.ListState
	historyScroll, gitScroll     native.ScrollState
	historyQuery                 string
	historyScope                 int
	directoryProgress            map[string]float32
	entries                      []treeRow
	search                       workspace.SearchResult
	contentSearch                workspace.ContentSearchResult
	searchQuery                  string
	searchLoading                bool
	searchContent                bool
	searchInclude, searchExclude string
	searchCase, searchWord       bool
	searchRegex, searchIgnored   bool
	searchCollapsed              map[string]bool
	searchFocus                  bool
	searchIndex                  int
	searchCancel                 context.CancelFunc
	generation                   int
	watcher                      *fsnotify.Watcher
	cancel                       context.CancelFunc
	git                          gitview.Status
	gitError                     string
	gitLoading                   bool
	gitRefreshPending            bool
	gitContext                   context.Context
	gitCancel                    context.CancelFunc
	gitTime                      time.Time
	gitFingerprint               string
	collapsed                    map[string]bool
	rowHandles                   map[string]*native.Handle
	focusPath                    string
	reveal                       bool
	directoryTasks               map[string]directoryTask
}
type directoryTask struct {
	ctx    context.Context
	cancel context.CancelFunc
}
type treeRow struct {
	workspace.Entry
	Depth int
	Scale float32
}
type filePage struct {
	gitDiffState
	id, root, path, kind, compare                                       string
	file                                                                editor.File
	buffer                                                              *native.TextBuffer
	loaded, loading, saving, checking, conflict, missing, focus, resync bool
	readContext                                                         context.Context
	readCancel                                                          context.CancelFunc
	checkContext                                                        context.Context
	checkCancel                                                         context.CancelFunc
	checkPending                                                        bool
	err                                                                 string
	scroll, hscroll                                                     native.ScrollState
	lines                                                               native.TextLines
	caret, anchor                                                       int
	observed                                                            native.TextSnapshot
	undo, redo                                                          []native.TextSnapshot
	savedText                                                           string
	changedAt, checkpointAt                                             time.Time
	checkpointVersion                                                   uint64
	checkpointBusy                                                      bool
	query                                                               string
	findOpen, findFocus                                                 bool
	findCase                                                            bool
	findGeneration                                                      uint64
	findCancel                                                          context.CancelFunc
	findResult                                                          string
	pendingSelection                                                    *[2]int
	pendingLocation                                                     *fileLocation
	highlights                                                          []syntaxToken
	highlightVersion                                                    uint64
	highlighting                                                        bool
	highlightCancel                                                     context.CancelFunc
	modified                                                            bool
	contentWidth                                                        float32
	checkedAt                                                           time.Time
	focusOwner                                                          *native.Handle
	saveAgain, saveForce, saveRecreate                                  bool
	saveCallbacks                                                       []func(bool)
	closeAfterCheckpoint                                                bool
	diskEpoch                                                           uint64
	overwriteBase                                                       *editor.File
	lastVisited                                                         time.Time
	pinFrame                                                            uint64
	pinY                                                                float32
	turnReview                                                          *turnReview
	comparisonSource                                                    string
	comparisonVersion                                                   uint64
	comparisonDisk                                                      editor.File
	comparisonStale                                                     bool
}

func sessionTabID(id int) string { return fmt.Sprintf("session-%d", id) }
func (a *App) workspaceRoot(cwd string) string {
	a = a.application()
	if cwd == "" {
		return ""
	}
	if w := a.worktreeFor(cwd); w != nil {
		return w.Path
	}
	cwd = filepath.Clean(cwd)
	if root := a.workspaceAliases[cwd]; root != "" {
		return root
	}
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return cwd
	}
	if a.workspaceAliases == nil {
		a.workspaceAliases = map[string]string{}
	}
	root := a.workspaceAliases[resolved]
	if root == "" {
		root = cwd
	}
	a.workspaceAliases[cwd], a.workspaceAliases[resolved] = root, root
	return root
}
func (a *App) ensureSessionTab(s *session) {
	id := sessionTabID(s.id)
	for i := range a.tabs {
		if a.tabs[i].ID == id {
			a.tabs[i].Workspace = a.workspaceRoot(s.State.Cwd)
			return
		}
	}
	a.tabs = append(a.tabs, storage.Tab{ID: id, Kind: "session", SessionID: s.id, Workspace: a.workspaceRoot(s.State.Cwd)})
}
func (a *App) removeWorkspaceTab(id string) {
	previous := a.folderTabs(a.activeWorkspace)
	if a.layouts != nil {
		for _, layout := range a.layouts {
			for _, pane := range paneLeaves(layout.Root) {
				if slices.Contains(pane.Tabs, id) {
					previous = nil
					for _, other := range pane.Tabs {
						for _, tab := range a.tabs {
							if tab.ID == other {
								previous = append(previous, tab)
							}
						}
					}
					pane.Tabs = slices.DeleteFunc(pane.Tabs, func(other string) bool { return other == id })
					if pane.Active == id {
						pane.Active = ""
					}
					break
				}
			}
		}
	}
	a.tabs = slices.DeleteFunc(a.tabs, func(t storage.Tab) bool { return t.ID == id })
	a.selectAfterTabClose(id, previous)
}
func (a *App) currentFile() *filePage { return a.files[a.activeTab] }
func (a *App) workspaceCwd() string {
	if page := a.currentTerminal(); page != nil {
		return page.root
	}
	if a.currentFile() != nil || a.isEmptyTab(a.activeTab) || !a.open || a.activeTab == "" {
		if a.activeWorkspace != "" {
			return a.activeWorkspace
		}
	}
	if a.State.Cwd != "" {
		return a.State.Cwd
	}
	return a.activeWorkspace
}
func (a *App) workspace(root string) *workspacePage {
	if a.workspaces == nil {
		a.workspaces = map[string]*workspacePage{}
	}
	w := a.workspaces[root]
	if w == nil {
		w = &workspacePage{root: root, children: map[string][]workspace.Entry{}, expanded: map[string]bool{root: true}, loading: map[string]bool{}, collapsed: map[string]bool{}}
		a.workspaces[root] = w
	}
	return w
}
func (a *App) selectWorkspace(root string) {
	root = a.workspaceRoot(root)
	a.rememberFolderTab()
	if a.activeWorkspace == root {
		return
	}
	if a.activeWorkspace != "" {
		old := a.workspace(a.activeWorkspace)
		old.historyQuery, old.historyScope = a.historyQuery, a.historyScope
		if old.searchCancel != nil {
			old.searchCancel()
			old.searchCancel = nil
			old.generation++
			old.searchLoading = false
			old.searchQuery = ""
		}
		for path, task := range old.directoryTasks {
			task.cancel()
			delete(old.directoryTasks, path)
			old.loading[path] = false
		}
		a.cancelGitRefresh(old)
		for _, d := range a.files {
			if d.root == old.root {
				a.cancelFileRead(d)
				d.cancelFind()
			}
		}
	}
	a.activeWorkspace = root
	w := a.workspace(root)
	a.historyQuery, a.historyScope = w.historyQuery, w.historyScope
	if a.canAsync() {
		a.recoverSelectedSession()
	}
}
func (a *App) canAsync() bool { return a.win != nil || a.dispatch != nil }
func (a *App) openFile(root, path string) {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if !workspace.Contains(root, path) {
		return
	}
	// Keep the workspace's path spelling (/var and /private/var may alias).
	// Resolve only for identity, never to move the displayed file outside its root.
	if resolved, e := filepath.EvalSymlinks(path); e == nil {
		for _, existing := range a.files {
			if existing.root != root || existing.kind != "file" {
				continue
			}
			target, err := filepath.EvalSymlinks(existing.path)
			info, statErr := os.Lstat(path)
			existingInfo, existingErr := os.Lstat(existing.path)
			if err == nil && statErr == nil && existingErr == nil &&
				info.Mode()&os.ModeSymlink == 0 && existingInfo.Mode()&os.ModeSymlink == 0 && target == resolved {
				a.selectFile(existing)
				return
			}
		}
	}
	id := "file-" + editor.RecoveryID(root, path)
	if a.files == nil {
		a.files = map[string]*filePage{}
	}
	d := a.files[id]
	if d == nil {
		d = &filePage{id: id, root: root, path: path, kind: "file", resync: true}
		a.files[id] = d
		a.tabs = append(a.tabs, storage.Tab{ID: id, Kind: "file", Workspace: root, Path: path})
	}
	a.selectFile(d)
	if !d.loaded && !d.loading {
		a.loadFile(d)
	}
}
func (a *App) selectFile(d *filePage) {
	d.lastVisited = time.Now()
	a.activeTab = d.id
	a.selectWorkspace(d.root)
	a.activatePaneTab(d.id, a.activeWorkspace, true)
	d.focus = true
	d.focusOwner = nil
	d.resync = true
	a.compactSidebar = false
	w := a.workspace(d.root)
	a.startWatcher(w)
	if w.watcher != nil {
		_ = w.watcher.Add(filepath.Dir(d.path))
	}
	w.selected = d.path
	w.reveal = true
	for p := filepath.Dir(d.path); workspace.Contains(d.root, p); p = filepath.Dir(p) {
		w.expanded[p] = true
		if p == d.root {
			break
		}
	}
	if d.loaded && d.kind == "file" {
		a.checkFile(d)
	}
}
func (a *App) loadFile(d *filePage) {
	if d.turnReview != nil {
		a.setTurnReviewFile(d)
		return
	}
	a.readFile(d, false)
}
func (a *App) reloadFile(d *filePage) {
	if d.saving || d.checkpointBusy {
		d.err = "请等待文件保存或恢复副本写入完成后重新载入"
		return
	}
	a.readFile(d, true)
}
func (a *App) readFile(d *filePage, discard bool) {
	if !a.canAsync() || d.loading {
		return
	}
	d.diskEpoch++
	epoch := d.diskEpoch
	var version uint64
	if d.buffer != nil {
		version = d.buffer.Version()
	}
	d.loading = true
	ctx, cancel := a.projectContext(5 * time.Second)
	d.readContext, d.readCancel = ctx, cancel
	a.runFileTaskContext(ctx, func() {
		defer cancel()
		var f editor.File
		err := ctx.Err()
		if err == nil {
			f, err = editor.Read(d.path)
		}
		a.update(func() {
			if a.files[d.id] != d || d.diskEpoch != epoch || d.readContext != ctx {
				return
			}
			d.readContext, d.readCancel = nil, nil
			d.loading = false
			if errors.Is(err, context.Canceled) {
				return
			}
			d.err = ""
			if err != nil {
				d.err = err.Error()
				return
			}
			if discard && d.buffer != nil && d.buffer.Version() != version {
				d.err = "读取期间出现新的编辑，请重新确认载入"
				return
			}
			if discard {
				if !a.clearRecovery(d) {
					return
				}
			}
			d.file = f
			d.cancelFind()
			if d.highlightCancel != nil {
				d.highlightCancel()
			}
			d.buffer = native.NewTextBuffer(f.Text)
			d.savedText = f.Text
			d.observed = d.buffer.Snapshot()
			d.loaded = true
			d.modified, d.conflict, d.missing = false, false, false
			d.undo, d.redo = nil, nil
			d.checkpointVersion = 0
			d.highlights = nil
			d.highlightVersion = 0
			d.resync = true
			a.requestHighlight(d)
		})
	})
}
func (d *filePage) dirty() bool {
	return d.loaded && d.kind == "file" && d.buffer != nil && (d.buffer.Version() != d.observed.Version() || d.modified)
}
func (d *filePage) observe() {
	if d.buffer == nil {
		return
	}
	now := d.buffer.Snapshot()
	if now.Version() == d.observed.Version() {
		return
	}
	if d.highlightCancel != nil {
		d.highlightCancel()
	}
	d.cancelFind()
	if now.ByteLen() > editor.EditBytes && d.kind == "file" {
		d.buffer.Restore(d.observed)
		d.observed = d.buffer.Snapshot()
		d.err = "编辑超过 4 MiB，已拒绝本次输入；请先保存或拆分文件"
		d.resync = true
		return
	}
	d.undo = append(d.undo, d.observed)
	if len(d.undo) > 128 {
		d.undo = d.undo[len(d.undo)-128:]
	}
	total := 0
	for i := len(d.undo) - 1; i >= 0; i-- {
		total += d.undo[i].ByteLen()
		if total > 8<<20 {
			d.undo = d.undo[i+1:]
			break
		}
	}
	d.redo = nil
	d.observed = now
	d.modified = true
	d.changedAt = time.Now()
}
func (d *filePage) undoEdit(redo bool) {
	d.observe()
	from, to := &d.undo, &d.redo
	if redo {
		from, to = &d.redo, &d.undo
	}
	if len(*from) == 0 {
		return
	}
	d.cancelFind()
	if d.highlightCancel != nil {
		d.highlightCancel()
	}
	*to = append(*to, d.buffer.Snapshot())
	n := len(*from) - 1
	d.buffer.Restore((*from)[n])
	*from = (*from)[:n]
	d.observed = d.buffer.Snapshot()
	d.modified = d.observed.String() != d.savedText
	d.changedAt = time.Now()
	d.resync = true
}
func (a *App) saveFile(d *filePage, force, recreate bool, done func(bool)) {
	if d.buffer == nil || d.file.Reason != "" {
		if done != nil {
			done(false)
		}
		return
	}
	if done != nil {
		d.saveCallbacks = append(d.saveCallbacks, done)
	}
	if d.saving || d.checkpointBusy {
		d.saveAgain = true
		d.saveForce = force
		d.saveRecreate = recreate
		return
	}
	d.saveAgain = false
	a.observeFile(d)
	base := d.file
	if force {
		if d.overwriteBase == nil {
			d.err = "请先核对并确认当前磁盘版本"
			a.finishFileSave(d, false)
			return
		}
		base = *d.overwriteBase
		d.overwriteBase = nil
	}
	text := d.buffer.Snapshot().String()
	a.cancelFileRead(d)
	d.diskEpoch++
	d.saving = true
	d.err = ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Keep both the previous disk bytes and local text recoverable before replacement.
		if dir := a.recoveryDir(); dir != "" {
			r := editor.Recovery{ID: editor.RecoveryID(d.root, d.path), Workspace: d.root, Path: d.path, Text: text, Base: base}
			if e := editor.WriteRecovery(dir, r); e != nil {
				a.update(func() {
					d.saving = false
					d.err = "保存恢复副本：" + e.Error()
					a.finishFileSave(d, false)
				})
				return
			}
		}
		result, err := editor.Save(ctx, base, text, recreate)
		a.update(func() {
			d.saving = false
			if err != nil {
				d.err = err.Error()
				d.conflict = errors.Is(err, editor.ErrConflict)
				a.finishFileSave(d, false)
				return
			}
			d.file = result
			d.savedText = text
			d.modified = d.buffer.String() != text
			d.conflict, d.missing = false, false
			d.err = ""
			if !d.dirty() {
				if !a.clearRecovery(d) {
					a.finishFileSave(d, false)
					return
				}
			}
			a.refreshGit(a.workspace(d.root))
			if d.saveAgain && d.dirty() {
				a.saveFile(d, false, false, nil)
			} else {
				a.finishFileSave(d, true)
			}
		})
	}()
}

func (a *App) finishFileSave(d *filePage, ok bool) {
	d.saveAgain = false
	callbacks := d.saveCallbacks
	d.saveCallbacks = nil
	for _, callback := range callbacks {
		callback(ok)
	}
	if d.closeAfterCheckpoint && a.files[d.id] == d {
		d.closeAfterCheckpoint = false
		a.closeFile(d)
	}
}
func (a *App) checkFile(d *filePage) {
	if d.checking {
		d.checkPending = true
		return
	}
	if d.saving || !d.loaded || !a.canAsync() {
		return
	}
	d.checkPending = false
	d.checking = true
	epoch := d.diskEpoch
	path := d.path
	d.checkedAt = time.Now()
	ctx, cancel := a.projectContext(5 * time.Second)
	d.checkContext, d.checkCancel = ctx, cancel
	a.runBackgroundFileTaskContext(ctx, func() {
		defer cancel()
		var f editor.File
		err := ctx.Err()
		if err == nil {
			f, err = editor.Read(path)
		}
		a.updateIfChanged(func() bool {
			if a.files[d.id] != d || d.checkContext != ctx {
				return false
			}
			d.checkContext, d.checkCancel = nil, nil
			d.checking = false
			defer func() {
				if d.checkPending && !errors.Is(err, context.Canceled) {
					a.checkFile(d)
				}
			}()
			if epoch != d.diskEpoch || errors.Is(err, context.Canceled) {
				return false
			}
			if err != nil {
				changed := d.err != err.Error()
				d.missing = errors.Is(err, os.ErrNotExist)
				d.err = err.Error()
				return changed
			}
			changed := d.missing
			d.missing = false
			for _, comparison := range a.files {
				if comparison.comparisonSource == d.id && comparison.loaded && comparison.comparisonDisk.Changed(f) {
					changed = changed || !comparison.comparisonStale
					comparison.comparisonStale = true
				}
			}
			if !d.file.Changed(f) {
				return changed
			}
			a.observeFile(d)
			if d.dirty() {
				changed = changed || !d.conflict
				d.conflict = true
				return changed
			}
			d.file = f
			d.cancelFind()
			d.savedText = f.Text
			if d.highlightCancel != nil {
				d.highlightCancel()
			}
			d.buffer.Set(f.Text)
			d.observed = d.buffer.Snapshot()
			d.modified = false
			d.resync = true
			d.undo = nil
			d.redo = nil
			a.requestHighlight(d)
			return true
		})
	})
}
func (a *App) recoveryDir() string {
	if a.store == nil {
		return ""
	}
	return filepath.Join(a.store.Directory(), "editor-recovery")
}
func (a *App) clearRecovery(d *filePage) bool {
	if dir := a.recoveryDir(); dir != "" {
		if err := editor.RemoveRecovery(dir, editor.RecoveryID(d.root, d.path)); err != nil {
			d.err = err.Error()
			return false
		}
	}
	// A later close/exit step may fail, leaving this draft open and dirty.
	// Allow its next checkpoint to recreate the successfully removed copy.
	d.checkpointVersion = 0
	return true
}
func (a *App) checkpointFiles() {
	if a.quitDialog || a.fileQuitPending {
		return
	}
	for _, d := range a.files {
		if d.kind != "file" || !d.loaded {
			continue
		}
		a.observeFile(d)
		if !d.dirty() || d.checkpointBusy || d.saving || d.checkpointVersion == d.buffer.Version() {
			continue
		}
		if time.Since(d.changedAt) < time.Second && time.Since(d.checkpointAt) < 5*time.Second {
			continue
		}
		dir := a.recoveryDir()
		if dir == "" {
			continue
		}
		r := editor.Recovery{ID: editor.RecoveryID(d.root, d.path), Workspace: d.root, Path: d.path, Text: d.buffer.String(), Base: d.file, Version: d.buffer.Version()}
		d.checkpointBusy = true
		go func(d *filePage, r editor.Recovery) {
			err := editor.WriteRecovery(dir, r)
			a.update(func() {
				d.checkpointBusy = false
				if err != nil {
					d.err = "草稿恢复副本保存失败：" + err.Error()
				} else {
					d.checkpointAt = time.Now()
					d.checkpointVersion = r.Version
				}
				if d.saveAgain {
					a.saveFile(d, d.saveForce, d.saveRecreate, nil)
				} else if d.closeAfterCheckpoint {
					d.closeAfterCheckpoint = false
					a.closeFile(d)
				}
			})
		}(d, r)
	}
}
func (a *App) loadDirectory(w *workspacePage, path string) {
	if w.loading[path] || !a.canAsync() {
		return
	}
	w.loading[path] = true
	ctx, cancel := a.projectContext(5 * time.Second)
	if w.directoryTasks == nil {
		w.directoryTasks = map[string]directoryTask{}
	}
	w.directoryTasks[path] = directoryTask{ctx: ctx, cancel: cancel}
	a.runBackgroundFileTaskContext(ctx, func() {
		defer cancel()
		var entries []workspace.Entry
		err := ctx.Err()
		if err == nil {
			entries, err = workspace.ReadDir(ctx, w.root, path)
		}
		if err == nil {
			workspace.MarkIgnored(ctx, w.root, entries)
		}
		a.update(func() {
			if w.directoryTasks[path].ctx != ctx {
				return
			}
			delete(w.directoryTasks, path)
			w.loading[path] = false
			if err != nil {
				var pathErr *os.PathError
				if os.IsNotExist(err) && errors.As(err, &pathErr) &&
					pathErr.Path != w.root && workspace.Contains(w.root, pathErr.Path) && workspace.Contains(pathErr.Path, path) {
					a.forgetDirectory(w, pathErr.Path)
					if w.error == err.Error() {
						w.error = ""
					}
					return
				}
				w.error = err.Error()
				return
			}
			w.error = ""
			w.children[path] = entries
			// Reconcile persisted descendants against this actual directory listing.
			directories := make(map[string]bool, len(entries))
			for _, entry := range entries {
				if entry.Dir && !entry.Link {
					directories[entry.Path] = true
				}
			}
			for expanded := range w.expanded {
				if expanded == path || !workspace.Contains(path, expanded) {
					continue
				}
				relative, _ := filepath.Rel(path, expanded)
				name, _, _ := strings.Cut(relative, string(filepath.Separator))
				child := filepath.Join(path, name)
				if !directories[child] {
					a.forgetDirectory(w, child)
				}
			}
		})
	})
}

// Forget navigation state only; open documents and unsaved buffers are retained.
func (a *App) forgetDirectory(w *workspacePage, path string) {
	if path == w.root || !workspace.Contains(w.root, path) {
		return
	}
	for child, task := range w.directoryTasks {
		if workspace.Contains(path, child) {
			task.cancel()
			delete(w.directoryTasks, child)
		}
	}
	for _, state := range []map[string]bool{w.expanded, w.loading} {
		for child := range state {
			if workspace.Contains(path, child) {
				delete(state, child)
			}
		}
	}
	for child := range w.children {
		if workspace.Contains(path, child) {
			delete(w.children, child)
		}
	}
	for child := range w.directoryProgress {
		if workspace.Contains(path, child) {
			delete(w.directoryProgress, child)
		}
	}
	for child := range w.rowHandles {
		if workspace.Contains(path, child) {
			delete(w.rowHandles, child)
		}
	}
	parent := filepath.Dir(path)
	if entries, ok := w.children[parent]; ok {
		w.children[parent] = slices.DeleteFunc(entries, func(e workspace.Entry) bool { return e.Path == path })
	}
	if workspace.Contains(path, w.selected) {
		w.selected = parent
	}
	if workspace.Contains(path, w.focusPath) {
		w.focusPath = ""
	}
}

func (a *App) toggleFileDirectory(w *workspacePage, path string) {
	w.expanded[path] = !w.expanded[path]
	if w.expanded[path] {
		a.loadDirectory(w, path)
		if w.watcher != nil {
			_ = w.watcher.Add(path)
		}
		return
	}
	if workspace.Contains(path, w.selected) {
		w.selected = path
	}
}
func (a *App) startWatcher(w *workspacePage) {
	if w.watcher != nil || !a.canAsync() {
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.error = err.Error()
		return
	}
	w.watcher = watcher
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	_ = watcher.Add(w.root)
	a.runGitTaskContext(ctx, func() {
		if ctx.Err() != nil {
			return
		}
		metadataCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		dirs, err := gitview.MetadataDirectories(metadataCtx, w.root)
		if err == nil {
			for _, dir := range dirs {
				// Closed watchers reject Add; a workspace change cannot leak a watcher.
				_ = watcher.Add(dir)
			}
		}
	})
	go func() {
		pending := map[string]bool{}
		timer := time.NewTimer(time.Hour)
		timer.Stop()
		defer timer.Stop()
		var tick <-chan time.Time
		for {
			select {
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if len(pending) < 4096 {
					pending[ev.Name] = true
				} else {
					pending[""] = true
				}
				if tick == nil {
					timer.Reset(250 * time.Millisecond)
					tick = timer.C
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				a.update(func() { w.error = "文件监听：" + err.Error() })
			case <-tick:
				tick = nil
				if len(pending) > 0 {
					paths := pending
					pending = map[string]bool{}
					a.update(func() { a.applyWorkspaceChanges(w, paths) })
				}
			case <-ctx.Done():
				return
			case <-a.shutdown:
				return
			}
		}
	}()
}
func (a *App) closeWorkspaceResources() {
	for _, page := range a.terminals {
		if page.connection != nil {
			page.connection.Close()
		}
	}
	a.terminalWorkers.Wait()
	for _, d := range a.files {
		a.cancelFileRead(d)
		d.cancelFind()
		if d.highlightCancel != nil {
			d.highlightCancel()
		}
	}
	for _, w := range a.workspaces {
		if w.gitGenerateCancel != nil {
			w.gitGenerateCancel()
		}
		a.cancelGitRefresh(w)
		for _, task := range w.directoryTasks {
			task.cancel()
		}
		if w.searchCancel != nil {
			w.searchCancel()
		}
		if w.cancel != nil {
			w.cancel()
		}
		if w.watcher != nil {
			w.watcher.Close()
		}
	}
}

func (a *App) cancelFileRead(d *filePage) {
	d.checkPending = false
	if d.readCancel != nil {
		d.readCancel()
		d.readContext, d.readCancel = nil, nil
		d.loading = false
	}
	if d.checkCancel != nil {
		d.checkCancel()
		d.checkContext, d.checkCancel = nil, nil
		d.checking = false
	}
}

func (a *App) cancelGitRefresh(w *workspacePage) {
	w.gitRefreshPending = false
	if w.gitCancel != nil {
		w.gitCancel()
		w.gitContext, w.gitCancel = nil, nil
		w.gitLoading = false
	}
}

func (a *App) gitWorkspaceVisible(w *workspacePage) bool {
	d := a.currentFile()
	return a.historyOpen && a.rightTab == "git" && a.workspaceRoot(a.workspaceCwd()) == w.root ||
		d != nil && d.kind == "diff" && d.root == w.root && d.compare != "外部更改"
}

func (a *App) refreshGit(w *workspacePage) {
	if w.gitLoading || w.gitBusy != "" || w.gitPreparing || !a.canAsync() {
		return
	}
	w.gitLoading = true
	w.gitRefreshPending = false
	a.refreshBranchCatalog(w.root)
	ctx, cancel := a.projectContext(5 * time.Second)
	w.gitContext, w.gitCancel = ctx, cancel
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		var s gitview.Status
		var remotes []string
		var operation string
		err := ctx.Err()
		if err == nil {
			s, err = gitview.Read(ctx, w.root)
			if err == nil {
				remotes, _ = gitview.Remotes(ctx, w.root)
				operation = gitview.InProgress(ctx, w.root)
			}
		}
		a.update(func() {
			if w.gitContext != ctx {
				return
			}
			w.gitContext, w.gitCancel = nil, nil
			w.gitLoading = false
			defer func() {
				if w.gitRefreshPending && a.gitWorkspaceVisible(w) && !errors.Is(err, context.Canceled) {
					a.refreshGit(w)
				}
			}()
			if errors.Is(err, context.Canceled) {
				return
			}
			w.gitTime = time.Now()
			if err != nil {
				w.gitError = err.Error()
				if d := a.currentFile(); d != nil && d.kind == "diff" && d.root == w.root && d.turnReview == nil {
					d.err = err.Error()
				}
				return
			}
			w.gitRemotes, w.gitOperation = remotes, operation
			a.applyGitStatus(w, s)
		})
	})
}

func (a *App) applyGitStatus(w *workspacePage, status gitview.Status) {
	w.gitError = ""
	w.gitTime = time.Now()
	w.gitFingerprint = fmt.Sprintf("%+v", status)
	w.gitStatsFingerprint = ""
	w.git = status
	a.applyCurrentBranch(w)
	if d := a.currentFile(); d != nil && d.kind == "diff" && d.root == w.root && d.compare != "外部更改" && d.turnReview == nil && !strings.HasPrefix(d.compare, "提交 ") {
		rel, _ := filepath.Rel(w.root, d.path)
		found := false
		for _, entry := range status.Entries {
			if entry.Path == rel {
				a.loadGitDiff(d, entry, d.compare == "已暂存")
				found = true
				break
			}
		}
		if !found {
			d.gitReview = nil
			d.buffer = native.NewTextBuffer("没有差异")
			d.file = editor.File{Path: d.path, Reason: "只读差异"}
			d.loaded = true
			d.err = ""
		}
	}
}
func (a *App) openGitDiff(w *workspacePage, e gitview.Entry, staged bool) {
	kind := "更改"
	if staged {
		kind = "已暂存"
	}
	id := "diff-" + editor.RecoveryID(w.root, e.Path+kind)
	if a.files == nil {
		a.files = map[string]*filePage{}
	}
	d := a.files[id]
	if d == nil {
		d = &filePage{id: id, root: w.root, path: filepath.Join(w.root, e.Path), kind: "diff", compare: kind}
		a.files[id] = d
		a.tabs = append(a.tabs, storage.Tab{ID: id, Kind: "diff", Workspace: w.root, Path: d.path, Compare: kind})
	}
	a.selectFile(d)
	a.loadGitDiff(d, e, staged)
}
func (a *App) loadGitDiff(d *filePage, e gitview.Entry, staged bool) {
	if d.loading || !a.canAsync() {
		return
	}
	d.loading = true
	ctx, cancel := a.projectContext(5 * time.Second)
	d.readContext, d.readCancel = ctx, cancel
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		var review gitview.Review
		err := ctx.Err()
		if err == nil {
			review, err = gitview.ReadReview(ctx, d.root, e, staged)
		}
		a.update(func() {
			if a.files[d.id] != d || d.readContext != ctx {
				return
			}
			d.readContext, d.readCancel = nil, nil
			d.loading = false
			if errors.Is(err, context.Canceled) {
				return
			}
			if err != nil {
				d.err = err.Error()
				return
			}
			text := review.Patch
			if text == "" {
				text = "没有差异"
			}
			a.gitSetDiffText(d, text, &review)
		})
	})
}
func (a *App) flattenTree(w *workspacePage) []treeRow {
	var rows []treeRow
	var walk func(string, int, float32)
	walk = func(path string, depth int, scale float32) {
		for _, entry := range w.children[path] {
			rows = append(rows, treeRow{Entry: entry, Depth: depth, Scale: scale})
			progress, animating := w.directoryProgress[entry.Path]
			if !animating {
				progress = 1
				if !w.expanded[entry.Path] {
					progress = 0
				}
			}
			if entry.Dir && (w.expanded[entry.Path] || progress > 0) {
				walk(entry.Path, depth+1, scale*progress)
			}
		}
	}
	walk(w.root, 0, 1)
	return rows
}
func treeAnimationSize(w *workspacePage, path string) int {
	count := 0
	var visit func(string)
	visit = func(path string) {
		for _, e := range w.children[path] {
			count++
			if count > 200 {
				return
			}
			if e.Dir && (w.expanded[e.Path] || w.directoryProgress[e.Path] > 0) {
				visit(e.Path)
			}
		}
	}
	visit(path)
	return count
}

func (w *workspacePage) contentQuery() workspace.ContentQuery {
	return workspace.ContentQuery{
		Text: w.query, MatchCase: w.searchCase, WholeWord: w.searchWord, Regex: w.searchRegex,
		Include: w.searchInclude, Exclude: w.searchExclude, SearchIgnored: w.searchIgnored,
	}
}

// searchKey is the identity of the currently wanted search: mode, query text
// and, for content search, its options and file patterns.
func (w *workspacePage) searchKey() string {
	if !w.searchContent {
		return "files\x00" + w.query
	}
	return fmt.Sprintf("content\x00%#v", w.contentQuery())
}

func (a *App) searchFiles(w *workspacePage) {
	key := w.searchKey()
	if key == w.searchQuery || !a.canAsync() {
		return
	}
	w.searchQuery = key
	if w.searchCancel != nil {
		w.searchCancel()
		w.searchCancel = nil
	}
	w.error = ""
	w.generation++
	generation := w.generation
	if strings.TrimSpace(w.query) == "" {
		w.search = workspace.SearchResult{}
		w.contentSearch = workspace.ContentSearchResult{}
		w.searchCollapsed = nil
		w.searchIndex = 0
		w.searchLoading = false
		w.reveal = false
		return
	}
	w.searchLoading = true
	query, content, options := w.query, w.searchContent, w.contentQuery()
	ctx, cancel := a.projectContext(5 * time.Second)
	w.searchCancel = cancel
	a.runBackgroundFileTaskContext(ctx, func() {
		defer cancel()
		var names workspace.SearchResult
		var matches workspace.ContentSearchResult
		err := ctx.Err()
		if err == nil {
			if content {
				matches, err = workspace.SearchContent(ctx, w.root, options)
			} else {
				names, err = workspace.Search(ctx, w.root, query)
			}
		}
		a.update(func() {
			if generation != w.generation {
				return
			}
			w.searchLoading = false
			w.searchCancel = nil
			w.search = names
			w.contentSearch = matches
			w.searchCollapsed = nil
			w.searchIndex = 0
			if err != nil {
				w.error = err.Error()
			}
		})
	})
}
