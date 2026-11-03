package ui

import (
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func (a *App) threadDeleted(kind agent.AgentKind, runtime, id string) bool {
	for _, s := range a.application().sessions {
		if s.kind.Normalized() == kind.Normalized() && s.runtimeKey == runtime && slices.Contains(s.saved.ReplacedThreads, id) {
			return true
		}
	}
	return id != "" && slices.Contains(a.application().deletedThreads, storage.DeletedThread{Agent: kind.Normalized(), Runtime: runtime, ThreadID: id})
}

func (a *App) canDeleteSession(target *session) bool {
	return target != nil && !a.quitting && !a.quitDialog && !target.State.Busy && !target.recovering && !target.releasing && !target.editPreparing && !target.editSending &&
		!target.historyLoading && !target.forking && !target.forkUncertain && !target.reviewRequired &&
		!target.saved.Active && !(target.loaded && target.recoveryBlocked) && !target.imagesImporting && target.submitCancel == nil
}

func (a *App) sessionMenu(target *session) func(*native.Menu) {
	return func(menu *native.Menu) {
		if menu.Item("重命名").Disabled(target == nil || a.quitting || a.quitDialog).Chosen() {
			a.beginSessionRename(target)
		}
		if menu.Item("删除会话").Disabled(!a.canDeleteSession(target)).Chosen() {
			a.confirmDeleteSession(target)
		}
	}
}

func (a *App) historySession(thread agent.ThreadMeta) *session {
	key := a.threadSourceKey(thread)
	for _, target := range a.sessions {
		if target.kind.Normalized() == key.kind && target.runtimeKey == key.runtime && target.State.ThreadID == thread.ID {
			return target
		}
	}
	target := &session{kind: key.kind, runtimeKey: key.runtime, State: app.New(thread.Cwd)}
	target.State.ThreadID = thread.ID
	target.saved.Title = thread.Preview
	if thread.Name != nil && *thread.Name != "" {
		target.saved.Title = *thread.Name
	}
	target.State.Busy = thread.Status.Type == "active"
	return target
}

func (a *App) confirmDeleteSession(target *session) {
	if !a.canDeleteSession(target) {
		return
	}
	a.quitDialog = true
	identity := storage.DeletedThread{Agent: target.kind.Normalized(), Runtime: target.runtimeKey, ThreadID: target.State.ThreadID}
	go func() {
		result, err := a.quitPrompt(mygo.MessageOptions{Parent: a.win, Type: mygo.MessageWarning,
			Title: "删除会话", Message: "删除“" + target.title() + "”？",
			Detail:  "将从 Fuyu 删除此会话及草稿，关闭对应标签，并从历史列表隐藏。此操作无法在 Fuyu 中撤销。项目文件和 CLI 原始记录保留。",
			Buttons: []string{"删除会话", "取消"}, DefaultButton: 0, CancelButton: 1})
		a.update(func() {
			a.quitDialog = false
			if err != nil {
				a.sessionDeleteError = "打开删除确认失败：" + err.Error()
				return
			}
			if result.Button == 0 {
				if identity != (storage.DeletedThread{Agent: target.kind.Normalized(), Runtime: target.runtimeKey, ThreadID: target.State.ThreadID}) {
					a.sessionDeleteError = "会话已变化，请核对后重新删除。"
					return
				}
				a.deleteSession(target)
			}
		})
	}()
}

func (a *App) deleteSession(target *session) {
	if target == nil {
		return
	}
	if target.id == 0 {
		for _, existing := range a.sessions {
			if existing.kind.Normalized() == target.kind.Normalized() && existing.runtimeKey == target.runtimeKey && existing.State.ThreadID == target.State.ThreadID {
				target = existing
				break
			}
		}
	}
	if !a.canDeleteSession(target) || (target.id != 0 && !slices.Contains(a.sessions, target)) {
		a.sessionDeleteError = "会话正在运行、恢复或等待核验，请处理完成后再删除。"
		return
	}
	snapshot := a.snapshot()
	snapshot.Sessions = slices.DeleteFunc(snapshot.Sessions, func(item storage.Session) bool { return item.ID == target.id })
	snapshot.OpenTabs = slices.DeleteFunc(snapshot.OpenTabs, func(id int) bool { return id == target.id })
	snapshot.WorkspaceTabs = slices.DeleteFunc(snapshot.WorkspaceTabs, func(tab storage.Tab) bool { return tab.Kind == "session" && tab.SessionID == target.id })
	if target.State.ThreadID != "" && !a.threadDeleted(target.kind, target.runtimeKey, target.State.ThreadID) {
		snapshot.DeletedThreads = append(snapshot.DeletedThreads, storage.DeletedThread{Agent: target.kind.Normalized(), Runtime: target.runtimeKey, ThreadID: target.State.ThreadID})
	}
	for _, id := range target.saved.ReplacedThreads {
		snapshot.DeletedThreads = append(snapshot.DeletedThreads, storage.DeletedThread{Agent: target.kind.Normalized(), Runtime: target.runtimeKey, ThreadID: id})
	}
	if snapshot.Selected == target.id {
		snapshot.Selected = 0
	}
	if snapshot.ActiveTabID == sessionTabID(target.id) {
		snapshot.ActiveTabID = ""
	}
	if a.store != nil {
		if err := a.store.DeleteSession(snapshot, target.id); err != nil {
			a.sessionDeleteError = fmt.Sprintf("删除会话失败，原会话已保留：%v", err)
			return
		}
	}
	a.deletedThreads = snapshot.DeletedThreads
	a.sessionDeleteError = ""
	a.closeSessionTab(target)
	a.sessions = slices.DeleteFunc(a.sessions, func(item *session) bool { return item == target })
	if a.session == target {
		if len(a.sessions) > 0 {
			a.session = a.sessions[len(a.sessions)-1]
		} else {
			activeFile := a.currentFile()
			activeTab := a.activeTab
			a.newSession(target.State.Cwd)
			if activeFile != nil {
				for _, tab := range a.tabs {
					if tab.ID == activeTab {
						a.selectWorkspaceTab(tab)
						break
					}
				}
			}
		}
	}
	a.lastSaved = nil
	a.persist()
	if a.canAsync() {
		a.releaseClosedSession(target, time.Now())
	}
}
