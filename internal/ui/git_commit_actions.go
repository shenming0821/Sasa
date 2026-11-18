// Portions adapted from Mayoi, Copyright (c) 2026 chencheng.
// Licensed under MIT; see THIRD_PARTY_NOTICES/Mayoi-LICENSE.
package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

func (a *App) gitPreviewCommit(w *workspacePage, cm gitview.CommitInfo, keep bool) {
	id := "commit-" + w.root + "-" + cm.Hash
	existed := a.files[id] != nil
	wasPreview := w.gitPreview == id
	if w.gitPreview != "" && w.gitPreview != id {
		if d := a.files[w.gitPreview]; d != nil {
			a.cancelFileRead(d)
			delete(a.files, w.gitPreview)
			a.removeWorkspaceTab(w.gitPreview)
		}
	}
	a.gitOpenCommit(w, cm)
	if keep {
		w.gitPreview = ""
	} else if !existed || wasPreview {
		w.gitPreview = id
	}
	// The history keeps keyboard focus while its preview changes.
	if d := a.currentFile(); d != nil && !keep {
		d.focus = false
	}
}
func (a *App) gitCompareCommits(w *workspacePage, from, to string) {
	if !a.canAsync() {
		return
	}
	w.gitCompareRequest++
	request, root := w.gitCompareRequest, w.root
	ctx, cancel := a.projectContext(10 * time.Second)
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		left, err := gitview.ResolveCommit(ctx, root, from)
		right := ""
		if err == nil {
			right, err = gitview.ResolveCommit(ctx, root, to)
		}
		a.update(func() {
			if request != w.gitCompareRequest || a.activeWorkspace != root || a.workspaces[root] != w {
				return
			}
			if err != nil {
				w.gitActionError = "比较失败：" + err.Error()
				return
			}
			label := left[:min(7, len(left))] + " → " + right[:min(7, len(right))]
			info := gitview.CommitInfo{Hash: left + ".." + right, Subject: label}
			a.gitOpenCommit(w, info)
		})
	})
}
func (a *App) gitCommitMenu(c *native.Context, w *workspacePage, cm gitview.CommitInfo, m *native.Menu, action *func()) {
	moving := w.gitBusy != "" || w.gitPreparing || w.gitOperation != "" || a.gitQuitPending || a.quitStopping
	head := cm.Hash == w.git.Head
	in := w.gitAncestors[cm.Hash]
	item := func(label string, disabled bool, run func()) {
		if m.Item(label).Disabled(disabled).Chosen() {
			*action = run
		}
	}
	item("打开更改", false, func() { a.gitPreviewCommit(w, cm, true) })
	m.Separator()
	item("检出…", moving || head, func() { a.gitCheckoutCommit(w, cm) })
	item("合并到 "+w.git.Branch+"…", moving || in, func() { a.gitConfirmCommitOp(w, cm, "merge", "合并提交") })
	item("将 "+w.git.Branch+" 变基到此提交…", moving || in, func() { a.gitConfirmCommitOp(w, cm, "rebase", "变基") })
	m.Separator()
	item("在此创建标签…", moving, func() { w.gitRefPrompt = &gitRefPrompt{hash: cm.Hash, tag: true} })
	item("在此创建分支…", moving, func() { w.gitRefPrompt = &gitRefPrompt{hash: cm.Hash} })
	m.Separator()
	item("将 "+w.git.Branch+" 重置到此提交…", moving || head, func() { a.gitResetCommit(w, cm) })
	item("还原此提交…", moving, func() { a.gitConfirmCommitOp(w, cm, "revert", "还原提交") })
	item("拣选此提交", moving || in, func() { a.gitRunCommitOp(w, cm, "cherry-pick", "拣选提交") })
	if w.gitOperation != "" {
		m.Separator()
		op := w.gitOperation
		item("继续 "+op, w.gitBusy != "" || len(gitEntries(w, "冲突")) > 0, func() {
			a.gitWithSavedFiles(w, nil, func() {
				a.gitAction(w, "继续 "+op, func(ctx context.Context) error { return gitview.ContinueOperation(ctx, w.root, op, false) }, nil)
			})
		})
		item("中止 "+op, w.gitBusy != "", func() { a.gitAbort(w) })
	}
	m.Separator()
	item("复制提交 Hash", false, func() { c.WriteClipboard(cm.Hash) })
	item("复制提交消息", false, func() {
		text := cm.Subject
		if cm.Body != "" {
			text += "\n\n" + cm.Body
		}
		c.WriteClipboard(text)
	})
	for _, ref := range cm.Refs {
		if ref.Kind != gitview.RefHead {
			name := ref.Name
			item("复制 “"+name+"”", false, func() { c.WriteClipboard(name) })
		}
	}
	if head {
		m.Separator()
		item("撤销最后提交", moving, func() { a.gitConfirmCommitOp(w, cm, "undo", "撤销最后提交") })
	}
	m.Separator()
	item("创建补丁…", false, func() { a.gitCommitPatch(w, cm, true) })
	item("复制更改（补丁）", false, func() { a.gitCommitPatch(w, cm, false) })
	m.Separator()
	item("从 HEAD 比较到此处", head, func() { a.gitCompareCommits(w, "HEAD", cm.Hash) })
	item("从此处比较到 HEAD", head, func() { a.gitCompareCommits(w, cm.Hash, "HEAD") })
	item("选择用于比较", false, func() { w.gitCompareSelected = cm.Hash })
	if w.gitCompareSelected != "" {
		selected := w.gitCompareSelected
		item("与所选提交比较（"+selected[:min(7, len(selected))]+"）", selected == cm.Hash, func() { a.gitCompareCommits(w, selected, cm.Hash) })
	}
	m.Separator()
	item("在远端打开提交", false, func() { a.gitCommitRemote(c, w, cm) })
}
func (a *App) gitConfirmCommitOp(w *workspacePage, cm gitview.CommitInfo, op, title string) {
	detail := cm.Short() + " · " + cm.Subject
	if op == "rebase" {
		detail += "\n将当前分支独有提交重新应用到目标提交上。"
	}
	if op == "undo" {
		detail += "\n撤销提交，文件改动保留在暂存区。"
	}
	if op == "revert" {
		detail += "\n创建反向提交。"
		if len(cm.Parents) > 1 {
			detail += "合并提交以第一父提交为主线。"
		}
	}
	w.gitDecision = &gitDecision{title: title, detail: detail, labels: []string{"取消", title}, actions: []func(){nil, func() { a.gitRunCommitOp(w, cm, op, title) }}}
}
func (a *App) gitRunCommitOp(w *workspacePage, cm gitview.CommitInfo, op, label string) {
	next := func() {
		a.gitAction(w, label, func(ctx context.Context) error {
			return gitview.CommitOperation(ctx, w.root, op, cm.Hash, len(cm.Parents) > 1)
		}, nil)
	}
	a.gitWithSavedFiles(w, nil, next)
	// Disk-changing actions require saving edits, rather than silently leaving
	// an editor buffer based on the old branch's contents.
	if w.gitDecision != nil && w.gitDecision.title == "这些文件有未保存编辑" {
		save := w.gitDecision.actions[2]
		w.gitDecision.labels = []string{"取消", "保存后继续"}
		w.gitDecision.actions = []func(){nil, save}
	}
}
func (a *App) gitCheckoutCommit(w *workspacePage, cm gitview.CommitInfo) {
	var branches []string
	for _, ref := range cm.Refs {
		if ref.Kind == gitview.RefBranch {
			branches = append(branches, ref.Name)
		}
	}
	if len(branches) == 1 {
		a.gitSwitchBranch(w, branches[0], false)
		return
	}
	a.gitConfirmCommitOp(w, cm, "checkout", "以 detached HEAD 检出")
}
func (a *App) gitResetCommit(w *workspacePage, cm gitview.CommitInfo) {
	w.gitDecision = &gitDecision{title: "重置到 " + cm.Short(), detail: "Soft 保留暂存；Mixed 保留文件改动并取消暂存；Hard 丢弃已跟踪文件的暂存和工作区改动。", labels: []string{"取消", "Soft", "Mixed", "Hard…"}, actions: []func(){nil, func() { a.gitRunCommitOp(w, cm, "soft", "重置 Soft") }, func() { a.gitRunCommitOp(w, cm, "mixed", "重置 Mixed") }, func() {
		w.gitDecision = &gitDecision{title: "确认 Hard 重置", detail: "已跟踪文件的未提交改动将被丢弃。", labels: []string{"取消", "Hard 重置"}, actions: []func(){nil, func() { a.gitRunCommitOp(w, cm, "hard", "重置 Hard") }}}
	}}}
}
func (a *App) gitRefPromptView(c *native.Context, w *workspacePage) {
	draft := w.gitRefPrompt
	if draft == nil {
		return
	}
	title := "在此创建分支"
	if draft.tag {
		title = "在此创建标签"
	}
	native.Column(c).FillWidth().Padding(8, 12).Gap(6).Children(func() {
		native.Text(c, title+" · "+draft.hash[:7]).FontSize(12).Bold()
		native.TextInput(c, &draft.name).Label("引用名称").Placeholder("名称").FontSize(12).FillWidth()
		if draft.err != "" {
			native.Text(c, draft.err).FontSize(11).TextColor(c.Theme().Danger)
		}
		native.Row(c).Gap(6).Children(func() {
			if actionButton(c, "取消创建引用").Clicked() {
				w.gitRefPrompt = nil
			}
			if actionButton(c, "确认创建引用").Disabled(strings.TrimSpace(draft.name) == "" || w.gitBusy != "").Clicked() {
				a.gitAction(w, title, func(ctx context.Context) error {
					return gitview.CreateRef(ctx, w.root, draft.name, draft.hash, draft.tag)
				}, func() { w.gitRefPrompt = nil })
			}
		})
	})
}
func (a *App) gitCommitPatch(w *workspacePage, cm gitview.CommitInfo, save bool) {
	if !a.canAsync() {
		return
	}
	ctx, cancel := a.projectContext(10 * time.Second)
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		patch, err := gitview.CommitPatch(ctx, w.root, cm.Hash)
		a.update(func() {
			if err != nil {
				w.gitActionError = err.Error()
				return
			}
			if !save {
				mygo.Clipboard.WriteText(patch)
				w.gitNotice = "已复制补丁"
				return
			}
			path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.win, DefaultPath: cm.Short() + ".patch"})
			if err != nil {
				w.gitActionError = err.Error()
				return
			}
			if path == "" {
				return
			}
			ctx, cancel := a.projectContext(10 * time.Second)
			a.runGitTaskContext(ctx, func() {
				defer cancel()
				err := os.WriteFile(path, []byte(patch), 0600)
				a.update(func() {
					if err != nil {
						w.gitActionError = fmt.Sprintf("保存补丁失败：%v", err)
					} else {
						w.gitNotice = "补丁已保存"
					}
				})
			})
		})
	})
}
func (a *App) gitCommitRemote(c *native.Context, w *workspacePage, cm gitview.CommitInfo) {
	if !a.canAsync() {
		return
	}
	ctx, cancel := a.projectContext(10 * time.Second)
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		address, err := gitview.CommitRemoteURL(ctx, w.root, cm)
		a.update(func() {
			if err != nil {
				w.gitActionError = err.Error()
				return
			}
			c.OpenURLThen(address, func(err error) {
				if err != nil {
					a.update(func() { w.gitActionError = err.Error() })
				}
			})
		})
	})
}
