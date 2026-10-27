package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

func (a *App) autosave() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			applied := make(chan struct{})
			a.update(func() {
				a.advanceQuit()
				if a.gitQuitPending && !a.hasGitWrites() {
					a.gitQuitPending = false
					mygo.App.Quit()
				}
				a.advanceFileExit()
				a.checkpointFiles()
				a.trimEditorCaches()
				if d := a.currentFile(); d != nil && d.loaded && d.kind == "file" && time.Since(d.checkedAt) > 2*time.Second {
					a.checkFile(d)
				}
				if a.activeWorkspace != "" && (a.historyOpen && a.rightTab == "git" || a.currentFile() != nil && a.currentFile().kind == "diff") {
					w := a.workspace(a.activeWorkspace)
					if time.Since(w.gitTime) > 5*time.Second {
						a.refreshGit(w)
					}
				}
				if !a.projectsLoading && time.Since(a.projectsRefreshed) >= 30*time.Second {
					a.refreshProjects()
				}
				close(applied)
			})
			select {
			case <-applied:
			case <-a.shutdown:
				return
			}
		case <-a.shutdown:
			return
		}
	}
}

func (a *App) activeTasks() []string {
	var tasks []string
	for _, s := range a.sessions {
		if a.quitTaskPending(s) {
			tasks = append(tasks, fmt.Sprintf("• %s（%s）", s.title(), s.State.Cwd))
		}
	}
	return tasks
}

func (a *App) quitTaskPending(s *session) bool {
	return s.State.Busy || (a.quitTargets[s.id] && (s.recoveryBlocked || s.State.Disconnected))
}

func (a *App) beginQuit() {
	a.quitStopping, a.quitDeadline = true, time.Now().Add(10*time.Second)
	a.quitSent, a.quitTargets = make(map[string]bool), make(map[int]bool)
	for _, s := range a.sessions {
		if s.State.Busy {
			a.quitTargets[s.id] = true
		}
	}
}

func (a *App) installLifecycle() {
	a.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			if a.viewerOpen {
				a.viewerOpen = false
				a.win.Invalidate()
				return
			}
			a.persist()
			for _, d := range a.files {
				d.changedAt = time.Time{}
			}
			a.checkpointFiles()
			a.win.Hide()
		}
	})
	mygo.App.OnActivate(func(visible bool) {
		if !visible && !a.quitting {
			a.win.Show()
		}
	})
	mygo.App.Dock.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "打开 Fuyu", Click: func(*mygo.MenuItem, *mygo.Window) { a.win.Show() }},
	}))
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if a.quitting {
			return
		}
		if a.hasGitWrites() {
			e.PreventDefault()
			a.gitQuitPending = true
			a.win.Show()
			a.win.Invalidate()
			return
		}
		if err := a.persist(); err != nil {
			e.PreventDefault()
			a.win.Show()
			a.win.Invalidate()
			return
		}
		tasks := a.activeTasks()
		if len(tasks) == 0 {
			if a.hasUnsavedFiles() {
				e.PreventDefault()
				a.requestFileExit()
				return
			}
			a.quitting = true
			return
		}
		e.PreventDefault()
		if a.quitDialog || a.quitStopping {
			return
		}
		a.quitDialog = true
		a.win.Show()
		go func() {
			result, err := a.quitPrompt(mygo.MessageOptions{
				Parent: a.win, Type: mygo.MessageWarning, Title: "退出 Fuyu",
				Message: fmt.Sprintf("仍有 %d 个任务正在运行，要退出吗？", len(tasks)),
				Detail:  strings.Join(tasks, "\n") + "\n将请求停止这些任务并等待原生终态，再关闭共享 Codex 连接。独立后台进程仍需另行核验。",
				Buttons: []string{"取消", "停止任务并退出"}, DefaultButton: 0, CancelButton: 0,
			})
			a.update(func() {
				a.quitDialog = false
				if err != nil {
					a.State.Error = "退出确认失败：" + err.Error()
					return
				}
				if result.Button == 1 {
					a.beginQuit()
					a.advanceQuit()
				}
			})
		}()
	})
}

func (a *App) advanceQuit() {
	if !a.quitStopping {
		return
	}
	if a.hasGitWrites() {
		return
	}
	if len(a.activeTasks()) == 0 {
		a.quitStopping = false
		if a.hasUnsavedFiles() {
			a.requestFileExit()
			return
		}
		if a.persist() == nil {
			a.quitting = true
			a.quitApp()
		}
		return
	}
	for _, s := range a.sessions {
		key := fmt.Sprintf("%d/%s", s.id, s.State.TurnID)
		if s.State.Busy && s.State.TurnID != "" && !a.quitSent[key] {
			a.quitSent[key] = true
			a.forSession(s).stop()
		}
	}
	if time.Now().Before(a.quitDeadline) || a.quitDialog {
		return
	}
	a.quitStopping, a.quitDialog = false, true
	tasks := strings.Join(a.activeTasks(), "\n")
	go func() {
		result, err := a.quitPrompt(mygo.MessageOptions{
			Parent: a.win, Type: mygo.MessageWarning, Title: "停止尚未确认",
			Message: "部分任务未在 10 秒内确认停止。",
			Detail:  tasks + "\n强制退出会关闭共享连接及其自有进程组，不能确认独立后台命令已退出；重开后将保留待核验状态。",
			Buttons: []string{"返回核验", "强制退出"}, CancelButton: 0, DefaultButton: 0,
		})
		a.update(func() {
			a.quitDialog = false
			if err == nil && result.Button == 1 {
				a.requestFileExit()
			}
		})
	}()
}
