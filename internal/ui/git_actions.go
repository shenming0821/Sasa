package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
	"github.com/shenming0821/Fuyu/internal/storage"
)

type gitDecision struct {
	title, detail string
	labels        []string
	actions       []func()
}

type gitCommitTree struct {
	expanded, loaded, loading bool
	entries                   []gitview.Entry
	err                       string
}

type gitWorkflow struct {
	gitMessage, gitBusy, gitNotice, gitActionError string
	gitSelection                                   map[string]bool
	gitAnchor                                      string
	gitDecision                                    *gitDecision
	gitBranches                                    []gitview.Branch
	gitRemotes                                     []string
	gitRemote                                      string
	gitOperation                                   string
	gitBranchPicker                                bool
	gitBranchName                                  string
	gitBranchBase                                  string
	gitHistory                                     []gitview.CommitInfo
	gitHistoryLoading, gitHistoryLoaded            bool
	gitHistoryMore                                 bool
	gitHistoryError                                string
	gitHistoryTime                                 time.Time
	gitHistoryScroll                               native.ScrollState
	gitCommitTrees                                 map[string]*gitCommitTree
	gitStats                                       map[string]gitview.Review
	gitStatsLoading                                bool
	gitStatsFingerprint                            string
	gitGenerating                                  bool
	gitPreparing                                   bool
	gitAfterSave                                   bool
	gitGenerateContext                             context.Context
	gitGenerateCancel                              context.CancelFunc
}

type gitDiffState struct {
	gitConflict                           *gitConflictState
	gitReview                             *gitview.Review
	gitHunk                               int
	gitInitialCenter                      [2]bool
	gitSplit                              bool
	gitLeft, gitRight                     *native.TextBuffer
	gitLeftScroll                         native.ScrollState
	gitRightScroll                        native.ScrollState
	gitLeftHScroll                        native.ScrollState
	gitRightHScroll                       native.ScrollState
	gitLeftWidth                          float32
	gitRightWidth                         float32
	gitSplitY                             float32
	gitSplitCaret                         [2]int
	gitCommit                             string
	gitCommitInfo                         gitview.CommitInfo
	gitCommitFiles                        []gitview.Entry
	gitCommitFile                         int
	gitRows                               []diffCodeRow
	gitPairs                              []diffCodePair
	gitNote                               string
	gitLeftHighlights, gitRightHighlights []syntaxToken
}

func gitKey(group, path string) string { return group + "\x00" + path }

func gitEntries(w *workspacePage, group string) []gitview.Entry {
	var entries []gitview.Entry
	for _, e := range w.git.Entries {
		if group == "冲突" && e.Conflict || group == "更改" && e.Unstaged() || group == "已暂存的更改" && e.Staged() {
			entries = append(entries, e)
		}
	}
	return entries
}

func gitPaths(entries []gitview.Entry, staged bool) []string {
	var paths []string
	for _, e := range entries {
		paths = append(paths, e.Path)
		if staged && e.Original != "" {
			paths = append(paths, e.Original)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

func isGitIndexAction(label string) bool {
	return label == "暂存" || label == "取消暂存"
}

func (a *App) gitAction(w *workspacePage, label string, fn func(context.Context) error, done func()) {
	if w.gitBusy != "" || w.gitPreparing || a.gitQuitPending && !w.gitAfterSave || a.quitStopping || a.fileQuitPending || !a.canAsync() {
		return
	}
	a.cancelGitRefresh(w)
	w.gitBusy, w.gitActionError, w.gitNotice = label, "", ""
	indexOnly := isGitIndexAction(label)
	ctx, cancel := a.projectContext(2 * time.Minute)
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		var status gitview.Status
		var statusErr error
		err := ctx.Err()
		if err == nil {
			err = fn(ctx)
		}
		if indexOnly {
			status, statusErr = gitview.Read(ctx, w.root)
		}
		a.update(func() {
			w.gitBusy = ""
			if err != nil {
				w.gitActionError = label + "失败：" + err.Error()
			} else {
				if !indexOnly {
					w.gitNotice = label + "完成"
				}
				w.gitSelection = nil
				if done != nil {
					done()
				}
			}
			if indexOnly {
				if statusErr != nil {
					w.gitError = statusErr.Error()
					a.refreshGit(w)
				} else {
					a.applyGitStatus(w, status)
				}
				return
			}
			w.gitHistoryLoaded = false
			w.gitStatsFingerprint = ""
			a.refreshGit(w)
			for _, d := range a.files {
				if d.root == w.root && d.kind == "file" && d.loaded {
					a.checkFile(d)
				}
			}
			if !a.projectsLoading {
				a.refreshProjects()
			}
		})
	})
}

func (a *App) gitWithSavedFiles(w *workspacePage, paths []string, next func()) {
	var dirty []*filePage
	var names []string
	for _, d := range a.files {
		if d.root != w.root || !d.dirty() {
			continue
		}
		rel, _ := filepath.Rel(w.root, d.path)
		if paths == nil || slices.Contains(paths, rel) {
			dirty = append(dirty, d)
			names = append(names, rel)
		}
	}
	if len(dirty) == 0 {
		next()
		return
	}
	var saveNext func(int)
	saveNext = func(i int) {
		if i == len(dirty) {
			w.gitPreparing = false
			w.gitAfterSave = true
			next()
			w.gitAfterSave = false
			return
		}
		a.saveFile(dirty[i], false, false, func(ok bool) {
			if ok && !dirty[i].dirty() {
				saveNext(i + 1)
			} else {
				w.gitPreparing = false
				w.gitActionError = "文件未成功保存，Git 操作尚未执行"
			}
		})
	}
	w.gitDecision = &gitDecision{
		title:   "这些文件有未保存编辑",
		detail:  strings.Join(names, "\n") + "\n保存不会自动更新已有暂存内容。",
		labels:  []string{"取消", "使用磁盘版本", "保存后继续"},
		actions: []func(){nil, next, func() { w.gitPreparing = true; saveNext(0) }},
	}
}

func (a *App) gitStage(w *workspacePage, entries []gitview.Entry, staged bool) {
	if len(entries) == 0 || w.gitBusy != "" {
		return
	}
	paths := gitPaths(entries, staged)
	if staged {
		a.gitAction(w, "取消暂存", func(ctx context.Context) error { return gitview.Unstage(ctx, w.root, paths) }, nil)
		return
	}
	a.gitWithSavedFiles(w, paths, func() {
		a.gitAction(w, "暂存", func(ctx context.Context) error {
			for _, entry := range entries {
				if entry.Conflict {
					if err := gitview.MarkResolved(ctx, w.root, entry.Path); err != nil {
						return err
					}
				}
			}
			return gitview.Stage(ctx, w.root, paths)
		}, nil)
	})
}

func (a *App) gitCommitChanges(w *workspacePage, all bool) {
	if strings.TrimSpace(w.gitMessage) == "" || w.gitBusy != "" {
		return
	}
	message := w.gitMessage
	paths := gitPaths(gitEntries(w, "更改"), false)
	if all && len(paths) == 0 {
		return
	}
	a.gitWithSavedFiles(w, nil, func() {
		var hash string
		a.gitAction(w, "提交", func(ctx context.Context) error {
			if all {
				if err := gitview.Stage(ctx, w.root, paths); err != nil {
					return err
				}
			}
			var err error
			hash, err = gitview.Commit(ctx, w.root, message)
			return err
		}, func() {
			w.gitNotice = "已提交 " + hash[:min(8, len(hash))] + " · " + strings.SplitN(message, "\n", 2)[0]
			if w.gitMessage == message {
				w.gitMessage = ""
			}
		})
	})
}

func (a *App) gitChosen(w *workspacePage, group string, entry gitview.Entry) []gitview.Entry {
	if !w.gitSelection[gitKey(group, entry.Path)] {
		return []gitview.Entry{entry}
	}
	var result []gitview.Entry
	for _, e := range gitEntries(w, group) {
		if w.gitSelection[gitKey(group, e.Path)] {
			result = append(result, e)
		}
	}
	return result
}

func (a *App) gitSelect(w *workspacePage, group string, entry gitview.Entry, mods native.Modifiers) bool {
	key := gitKey(group, entry.Path)
	if w.gitSelection == nil {
		w.gitSelection = map[string]bool{}
	}
	if mods&native.Shift != 0 {
		entries := gitEntries(w, group)
		first, last := -1, -1
		for i, e := range entries {
			if gitKey(group, e.Path) == w.gitAnchor {
				first = i
			}
			if e.Path == entry.Path {
				last = i
			}
		}
		if first < 0 {
			first = last
		}
		for i := min(first, last); i >= 0 && i <= max(first, last); i++ {
			w.gitSelection[gitKey(group, entries[i].Path)] = true
		}
		return false
	}
	if mods&(native.Cmd|native.Ctrl) != 0 {
		w.gitSelection[key] = !w.gitSelection[key]
		w.gitAnchor = key
		return false
	}
	w.gitSelection = map[string]bool{key: true}
	w.gitAnchor = key
	return true
}

func (a *App) gitLoadHistory(w *workspacePage, more bool) {
	if w.gitHistoryLoading || !a.canAsync() || w.gitBusy != "" {
		return
	}
	skip := 0
	if more {
		skip = len(w.gitHistory)
	}
	w.gitHistoryLoading = true
	ctx, cancel := a.projectContext(5 * time.Second)
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		items, err := gitview.History(ctx, w.root, 20, skip)
		a.update(func() {
			w.gitHistoryLoading, w.gitHistoryLoaded = false, true
			w.gitHistoryTime = time.Now()
			if err != nil {
				w.gitHistoryError = err.Error()
				return
			}
			w.gitHistoryError = ""
			if !more {
				w.gitHistory = nil
			}
			w.gitHistory = append(w.gitHistory, items...)
			w.gitHistoryMore = len(items) == 20
			if !more {
				trees := make(map[string]*gitCommitTree)
				for _, item := range items {
					if tree := w.gitCommitTrees[item.Hash]; tree != nil {
						trees[item.Hash] = tree
					}
				}
				w.gitCommitTrees = trees
			}
		})
	})
}

func (a *App) gitLoadCommitTree(w *workspacePage, info gitview.CommitInfo, tree *gitCommitTree) {
	if tree.loading || tree.loaded || !a.canAsync() {
		return
	}
	tree.loading, tree.err = true, ""
	ctx, cancel := a.projectContext(5 * time.Second)
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		entries, err := gitview.CommitFiles(ctx, w.root, info.Hash)
		a.update(func() {
			if w.gitCommitTrees[info.Hash] != tree {
				return
			}
			tree.loading = false
			if err != nil {
				tree.err = err.Error()
				return
			}
			tree.entries, tree.loaded = entries, true
		})
	})
}

func (a *App) gitOpenCommit(w *workspacePage, info gitview.CommitInfo, path ...string) {
	id := "commit-" + w.root + "-" + info.Hash
	if a.files == nil {
		a.files = map[string]*filePage{}
	}
	d := a.files[id]
	if d == nil {
		d = &filePage{id: id, root: w.root, path: filepath.Join(w.root, "提交 "+info.Hash[:8]), kind: "diff", compare: "提交 " + info.Hash, gitDiffState: gitDiffState{gitCommit: info.Hash}}
		a.files[id] = d
		a.tabs = append(a.tabs, storage.Tab{ID: id, Kind: "diff", Workspace: w.root, Path: d.path, Compare: d.compare})
	}
	a.selectFile(d)
	d.gitCommitInfo = info
	if len(path) > 0 {
		a.cancelFileRead(d)
	}
	a.gitLoadCommit(d, info, path...)
}

func (a *App) gitLoadCommit(d *filePage, info gitview.CommitInfo, path ...string) {
	if d.loading || !a.canAsync() {
		return
	}
	d.loading = true
	fileIndex := d.gitCommitFile
	ctx, cancel := a.projectContext(5 * time.Second)
	d.readContext, d.readCancel = ctx, cancel
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		entries, err := gitview.CommitFiles(ctx, d.root, info.Hash)
		var text string
		selected := min(max(0, fileIndex), max(0, len(entries)-1))
		if err == nil && len(path) > 0 {
			selected = slices.IndexFunc(entries, func(entry gitview.Entry) bool { return entry.Path == path[0] })
			if selected < 0 {
				err = fmt.Errorf("此提交不包含文件 %s", path[0])
			}
		}
		if err == nil && len(entries) > 0 {
			text, err = gitview.CommitDiff(ctx, d.root, info.Hash, entries[selected])
		}
		review := gitview.ParsePatch(text)
		if err == nil && len(entries) > 0 {
			gitview.ReadReviewBase(ctx, d.root, info.Hash+"^:", entries[selected], &review)
			review.Entry = entries[selected]
		}
		a.update(func() {
			if a.files[d.id] != d || d.readContext != ctx {
				return
			}
			d.loading, d.readContext, d.readCancel = false, nil, nil
			if err != nil {
				d.err = err.Error()
				return
			}
			d.gitCommitFiles, d.gitCommitInfo, d.gitCommitFile = entries, info, selected
			if text == "" {
				text = "此提交没有文件差异"
			}
			a.gitSetDiffText(d, text, &review)
		})
	})
}

func (a *App) gitSetDiffText(d *filePage, text string, review *gitview.Review) {
	d.err = ""
	previous := d.gitReview
	d.gitReview = review
	if review != nil && (!d.loaded || previous == nil || previous.Entry.Path != review.Entry.Path) {
		d.gitHunk = 0
		d.gitInitialCenter = [2]bool{len(review.Hunks) > 0, len(review.Hunks) > 0}
	}
	rows, pairs := diffCodeRows(review)
	d.gitRows, d.gitPairs = rows, pairs
	if len(rows) > 0 {
		text = diffRowsText(rows)
	} else if review != nil && review.Binary {
		text = "二进制文件内容发生变化"
	} else if review != nil && strings.HasPrefix(text, "diff --git ") {
		text = "文件路径、权限或类型发生变化，无文本内容差异"
	}
	if d.loaded && d.buffer != nil && d.buffer.String() == text && previous != nil && review != nil && previous.Patch == review.Patch && previous.BeforeText == review.BeforeText && previous.BeforeKnown == review.BeforeKnown {
		return
	}
	if d.highlightCancel != nil {
		d.highlightCancel()
	}
	d.contentWidth, d.highlightVersion = 0, 0
	d.gitLeftWidth, d.gitRightWidth = 0, 0
	d.highlights = nil
	d.gitLeftHighlights, d.gitRightHighlights = nil, nil
	d.buffer = native.NewTextBuffer(text)
	d.file.Path, d.file.Text, d.file.Reason = d.path, text, "只读差异"
	d.observed = d.buffer.Snapshot()
	d.loaded, d.resync = true, true
	d.gitLeft, d.gitRight = nil, nil
}

func (a *App) gitRefreshStats(w *workspacePage) {
	if w.gitStatsLoading || w.gitStatsFingerprint == w.gitFingerprint || !a.canAsync() || w.gitBusy != "" {
		return
	}
	w.gitStatsLoading = true
	fingerprint := w.gitFingerprint
	entries := slices.Clone(w.git.Entries)
	ctx, cancel := a.projectContext(10 * time.Second)
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		stats := map[string]gitview.Review{}
		for _, entry := range entries {
			if ctx.Err() != nil || len(stats) >= 100 {
				break
			}
			for _, staged := range []bool{false, true} {
				if staged && !entry.Staged() || !staged && !entry.Unstaged() {
					continue
				}
				patch, err := gitview.Diff(ctx, w.root, entry, staged)
				if err == nil {
					// Keep counts only; storing every full patch would duplicate buffers.
					review := gitview.ParsePatch(patch)
					stats[gitKey(fmt.Sprint(staged), entry.Path)] = gitview.Review{Added: review.Added, Deleted: review.Deleted, Binary: review.Binary}
				}
			}
		}
		a.update(func() {
			w.gitStatsLoading = false
			if w.gitFingerprint == fingerprint {
				w.gitStats, w.gitStatsFingerprint = stats, fingerprint
			}
		})
	})
}

func (a *App) hasGitWrites() bool {
	for _, w := range a.workspaces {
		if w.gitBusy != "" || w.gitPreparing {
			return true
		}
	}
	return false
}
