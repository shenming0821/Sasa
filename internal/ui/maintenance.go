package ui

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/shenming0821/Fuyu/internal/agent"
)

type maintenanceState struct {
	wake        chan struct{}
	savePending bool
	saveDue     time.Time
	trimmedAt   time.Time
}

func (a *App) updateIfChanged(change func() bool) {
	work := func() {
		if change() {
			a.scheduleSave()
			if a.win != nil {
				a.win.Invalidate()
			}
		}
	}
	if a.dispatch != nil {
		a.dispatch(work)
	} else {
		mygo.RunOnMain(work)
	}
}

func (a *App) scheduleSave() {
	a = a.application()
	if a.store != nil && !a.savePending {
		a.savePending = true
		a.saveDue = time.Now().Add(time.Second)
	}
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *App) maintain(now time.Time) time.Duration {
	storageError := a.storageError
	a.advanceQuit()
	if a.gitQuitPending && !a.hasGitWrites() {
		a.gitQuitPending = false
		mygo.App.Quit()
	}
	a.advanceFileExit()
	a.checkpointFiles()
	a.releaseClosedSessions(now)
	if now.Sub(a.trimmedAt) >= 30*time.Second {
		a.trimEditorCaches()
		a.trimmedAt = now
	}
	if d := a.currentFile(); d != nil && d.loaded && d.kind == "file" && now.Sub(d.checkedAt) >= 2*time.Second {
		a.checkFile(d)
	}
	if a.activeWorkspace != "" {
		w := a.workspace(a.activeWorkspace)
		if a.gitWorkspaceVisible(w) && now.Sub(w.gitTime) >= 5*time.Second {
			a.refreshGit(w)
		}
	}
	if !a.projectsLoading && now.Sub(a.projectsRefreshed) >= 30*time.Second && a.canAsync() {
		a.refreshProjects()
	}
	a.prewarmCompletionSkills()
	a.prewarmModels()
	if a.savePending && !now.Before(a.saveDue) {
		a.persist()
	}
	if storageError != a.storageError && a.win != nil {
		a.win.Invalidate()
	}
	if a.State.Busy && a.win != nil {
		a.win.Invalidate()
	}
	return a.maintenanceDelay(now)
}

func (a *App) maintenanceDelay(now time.Time) time.Duration {
	delay := 30 * time.Second
	if a.State.Busy {
		delay = time.Second
	}
	if a.savePending {
		delay = min(delay, max(100*time.Millisecond, a.saveDue.Sub(now)))
	}
	if a.quitStopping || a.fileQuitPending || a.fileExitRequested || a.gitQuitPending {
		delay = min(delay, 250*time.Millisecond)
	}
	for _, d := range a.files {
		if d.dirty() {
			delay = min(delay, 500*time.Millisecond)
			break
		}
	}
	for _, s := range a.sessions {
		if s.canReleaseRuntime() {
			if _, ok := a.forSession(s).sessionBackend().(agent.IdleThreadReleaser); ok {
				delay = min(delay, max(100*time.Millisecond, s.releaseAfter.Sub(now)))
			}
		}
	}
	if d := a.currentFile(); d != nil && d.kind == "file" {
		delay = min(delay, 2*time.Second)
	}
	if w := a.workspaces[a.activeWorkspace]; w != nil && a.gitWorkspaceVisible(w) {
		delay = min(delay, 5*time.Second)
	}
	return delay
}
