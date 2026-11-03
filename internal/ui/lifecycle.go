package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

func (a *App) closeCurrentPage() {
	if a.quitDialog || a.quitting || a.quitStopping {
		return
	}
	switch {
	case a.worktreeCreate != nil:
		if !a.worktreeCreate.busy {
			a.worktreeCreate = nil
		}
	case a.fileAction.open:
		a.fileAction.open = false
	case a.worktreeRenameOpen:
		a.worktreeRenameOpen = false
	case a.sessionRenameOpen:
		a.sessionRenameOpen = false
	case a.settingsOpen:
		a.settingsOpen = false
	case a.application().viewerOpen:
		a.application().viewerOpen = false
	default:
		if page := a.currentTerminal(); page != nil {
			a.closeTerminal(page)
		} else if page := a.currentFile(); page != nil {
			a.closeFile(page)
		} else if a.open && a.activeTab != "" {
			a.closeSession(a.session)
		}
	}
}

func (a *App) autosave() {
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		select {
		case <-a.wake:
			if time.Until(deadline) > 500*time.Millisecond {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				deadline = time.Now().Add(500 * time.Millisecond)
				timer.Reset(time.Until(deadline))
			}
		case <-timer.C:
			applied := make(chan struct{})
			delay := 30 * time.Second
			work := func() {
				delay = a.maintain(time.Now())
				close(applied)
			}
			if a.dispatch != nil {
				a.dispatch(work)
			} else {
				mygo.RunOnMain(work)
			}
			select {
			case <-applied:
				deadline = time.Now().Add(delay)
				timer.Reset(delay)
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
	for _, page := range a.terminals {
		if page.connection != nil && !page.connection.Exited() {
			tasks = append(tasks, "• 终端（"+page.root+"）")
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
	for _, page := range a.terminals {
		if page.connection != nil && !page.closing {
			page.closing = true
			connection := page.connection
			a.terminalWorkers.Add(1)
			go func() {
				defer a.terminalWorkers.Done()
				connection.Close()
				a.update(func() { a.advanceQuit() })
			}()
		}
	}
}

func (a *App) installLifecycle() {
	a.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			if a.settingsOpen {
				a.settingsOpen = false
				a.win.Invalidate()
				return
			}
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
				Detail:  strings.Join(tasks, "\n") + "\n将停止 Agent 任务并等待原生终态，同时结束窗口内的终端 shell。独立后台进程仍需另行核验。",
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
		if s.State.Busy && (s.State.TurnID != "" || s.submitCancel != nil) && !a.quitSent[key] {
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
