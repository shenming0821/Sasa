package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	gitview "github.com/shenming0821/Sasa/internal/git"
)

func (a *App) gitRemoteAction(w *workspacePage, action string) {
	if action == "publish" && len(w.gitRemotes) > 1 && w.gitRemote == "" {
		decision := &gitDecision{title: "选择发布到哪个远程", detail: w.git.Branch}
		decision.labels, decision.actions = []string{"取消"}, []func(){nil}
		for _, remote := range w.gitRemotes {
			decision.labels = append(decision.labels, remote)
			decision.actions = append(decision.actions, func() {
				w.gitRemote = remote
				a.gitRemoteAction(w, action)
			})
		}
		w.gitDecision = decision
		return
	}
	remote := w.gitRemote
	if remote == "" && len(w.gitRemotes) > 0 {
		remote = w.gitRemotes[0]
	}
	label := map[string]string{"fetch": "获取远程更新", "pull": "拉取", "push": "推送", "publish": "发布分支"}[action]
	do := func() {
		a.gitAction(w, label, func(ctx context.Context) error { return gitview.RemoteAction(ctx, w.root, action, remote) }, nil)
	}
	if action == "pull" {
		a.gitWithSavedFiles(w, nil, do)
	} else {
		do()
	}
}

func (a *App) gitSwitchBranch(w *workspacePage, name string, create bool) {
	a.gitChangeBranch(w, name, create, "")
}

func (a *App) gitChangeBranch(w *workspacePage, name string, create bool, base string) {
	do := func() {
		a.gitAction(w, "切换分支", func(ctx context.Context) error {
			if create {
				return gitview.CreateBranchFromRef(ctx, w.root, strings.TrimSpace(name), base)
			}
			return gitview.Switch(ctx, w.root, strings.TrimSpace(name), false)
		}, nil)
	}
	// Switching changes disk contents, so unlike staging it cannot use disk
	// while an editor still has unsaved changes.
	for _, d := range a.files {
		if d.root == w.root && d.dirty() {
			a.gitWithSavedFiles(w, nil, do)
			save := w.gitDecision.actions[2]
			w.gitDecision = &gitDecision{
				title:   "切换分支前保存编辑",
				detail:  "工作区内有未保存文件。保存后由 Git 检查能否保留本地改动并切换。",
				labels:  []string{"取消", "返回编辑器", "保存后切换"},
				actions: []func(){nil, func() { a.selectFile(d) }, save},
			}
			return
		}
	}
	do()
}

func (a *App) gitDiscardGuard(w *workspacePage, entries []gitview.Entry) bool {
	for _, d := range a.files {
		rel, _ := filepath.Rel(w.root, d.path)
		for _, e := range entries {
			if d.root == w.root && d.dirty() && rel == e.Path {
				w.gitActionError = "请先保存或撤销 " + e.Path + " 的未保存编辑，再撤销磁盘改动"
				return false
			}
		}
	}
	return true
}

// Read exact versions before confirmation. A later change invalidates it.
func (a *App) gitDiscardReview(w *workspacePage, entries []gitview.Entry, done func([]gitview.Review)) {
	var reviews []gitview.Review
	a.gitAction(w, "核对撤销范围", func(ctx context.Context) error {
		for _, entry := range entries {
			review, err := gitview.ReadReview(ctx, w.root, entry, false)
			if err != nil {
				return err
			}
			reviews = append(reviews, review)
		}
		return nil
	}, func() { done(reviews) })
}

func (a *App) gitDiscardExec(w *workspacePage, entries []gitview.Entry, reviews []gitview.Review) {
	recovery := filepath.Join(a.recoveryDir(), "discarded")
	a.gitAction(w, "撤销改动", func(ctx context.Context) error {
		for _, review := range reviews {
			latest, err := gitview.ReadReview(ctx, w.root, review.Entry, false)
			if err != nil {
				return err
			}
			if latest.Token != review.Token {
				return fmt.Errorf("%s 已变化，请重新核对撤销范围", review.Entry.Path)
			}
		}
		return gitview.Discard(ctx, w.root, entries, recovery)
	}, nil)
}

func (a *App) gitDiscard(w *workspacePage, entries []gitview.Entry) {
	if len(entries) == 0 || w.gitBusy != "" || !a.gitDiscardGuard(w, entries) {
		return
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Path)
	}
	a.gitDiscardReview(w, entries, func(reviews []gitview.Review) {
		w.gitDecision = &gitDecision{
			title:  fmt.Sprintf("撤销 %d 个文件的改动？", len(entries)),
			detail: strings.Join(names, "\n") + "\n已跟踪文件恢复至暂存版本；未跟踪文件移至 Sasa 回收目录。",
			labels: []string{"取消", "撤销改动"},
			actions: []func(){nil, func() {
				a.gitDiscardExec(w, entries, reviews)
			}},
		}
	})
}

// gitDiscardArmDelay is how long a discard button stays in its confirm state.
var gitDiscardArmDelay = 4 * time.Second

func (a *App) gitDiscardButton(w *workspacePage, key string, entries []gitview.Entry) {
	if w.gitArmed != nil && w.gitArmed.key == key && len(w.gitArmed.reviews) > 0 {
		a.gitDiscardConfirm(w, w.gitArmed)
		return
	}
	a.gitDiscardArm(w, key, entries)
}

func (a *App) gitDiscardArm(w *workspacePage, key string, entries []gitview.Entry) {
	if len(entries) == 0 || w.gitBusy != "" || !a.gitDiscardGuard(w, entries) {
		return
	}
	armed := &gitArm{key: key}
	w.gitArmed = armed
	a.gitDiscardReview(w, entries, func(reviews []gitview.Review) {
		if w.gitArmed == armed {
			armed.reviews = reviews
		}
	})
	time.AfterFunc(gitDiscardArmDelay, func() {
		a.update(func() {
			if w.gitArmed == armed {
				w.gitArmed = nil
			}
		})
	})
}

func (a *App) gitDiscardConfirm(w *workspacePage, armed *gitArm) {
	if w.gitBusy != "" || w.gitArmed != armed {
		return
	}
	w.gitArmed = nil
	var entries []gitview.Entry
	for _, review := range armed.reviews {
		entries = append(entries, review.Entry)
	}
	if !a.gitDiscardGuard(w, entries) {
		return
	}
	a.gitDiscardExec(w, entries, armed.reviews)
}

func (a *App) gitAbort(w *workspacePage) {
	op := w.gitOperation
	for _, d := range a.files {
		if d.root == w.root && d.dirty() {
			w.gitActionError = "请先保存或撤销未保存编辑，再中止 " + op
			return
		}
	}
	w.gitDecision = &gitDecision{
		title: "中止 " + op + "？", detail: "Git 将恢复操作前状态；尚未提交的冲突解决结果可能被覆盖。",
		labels: []string{"取消", "中止操作"}, actions: []func(){nil, func() {
			a.gitAction(w, "中止 "+op, func(ctx context.Context) error { return gitview.ContinueOperation(ctx, w.root, op, true) }, nil)
		}},
	}
}

func (a *App) gitToCodex(w *workspacePage, entries []gitview.Entry, staged bool, intent string, hunk *gitview.Hunk) {
	if !a.canAsync() {
		return
	}
	ctx, cancel := a.projectContext(10 * time.Second)
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		var prompt strings.Builder
		fmt.Fprintf(&prompt, "%s。工作区：%s\n请仅围绕以下选定改动处理。\n", intent, w.root)
		for _, e := range entries {
			fmt.Fprintf(&prompt, "\n文件：%s\n", e.Path)
			var patch string
			var err error
			if hunk != nil {
				patch = hunk.Patch
			} else {
				patch, err = gitview.Diff(ctx, w.root, e, staged)
			}
			if err != nil || prompt.Len()+len(patch) > 256<<10 {
				a.update(func() { w.gitActionError = "选定差异读取失败或超过 256 KiB，请缩小选择范围" })
				return
			}
			prompt.WriteString("```diff\n" + patch + "\n```\n")
		}
		text := prompt.String()
		a.update(func() {
			var target *session
			for _, s := range a.sessions {
				if a.workspaceRoot(s.State.Cwd) == w.root && !s.State.Busy && !s.writerOccupied {
					target = s
					break
				}
			}
			if target == nil {
				a.newSession(w.root)
				target = a.session
			} else {
				a.selectSession(target)
			}
			if strings.TrimSpace(target.State.Draft) != "" {
				target.State.Draft += "\n\n"
			}
			target.State.Draft += text
			target.completion.focus = true
			w.gitNotice = "已放入会话输入框，确认后发送"
		})
	})
}
