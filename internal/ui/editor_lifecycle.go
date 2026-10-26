package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
)

func (a *App) hasUnsavedFiles() bool {
	for _, d := range a.files {
		if d.dirty() || d.saving || d.checkpointBusy {
			return true
		}
	}
	return false
}
func (a *App) requestFileExit() {
	if a.quitDialog || a.fileQuitPending {
		return
	}
	if a.hasGitWrites() {
		a.fileExitRequested = true
		return
	}
	var dirty []*filePage
	var names []string
	for _, d := range a.files {
		if d.saving || d.checkpointBusy {
			a.fileExitRequested = true
			return
		}
		if d.dirty() {
			dirty = append(dirty, d)
			names = append(names, filepath.Base(d.path))
		}
	}
	a.fileExitRequested = false
	if len(dirty) == 0 {
		if a.persist() == nil {
			a.quitting = true
			a.quitApp()
		}
		return
	}
	a.quitDialog = true
	a.fileQuitPending = true
	go func() {
		r, err := a.quitPrompt(mygo.MessageOptions{Parent: a.win, Type: mygo.MessageWarning, Title: "退出 Fuyu", Message: fmt.Sprintf("有 %d 个文件尚未保存", len(dirty)), Detail: strings.Join(names, "\n"), Buttons: []string{"取消", "不保存并退出", "全部保存并退出"}, DefaultButton: 2, CancelButton: 0})
		a.update(func() {
			a.quitDialog = false
			if err != nil || r.Button == 0 {
				a.fileQuitPending = false
				return
			}
			if r.Button == 1 {
				for _, d := range dirty {
					if !a.clearRecovery(d) {
						a.fileQuitPending = false
						a.selectFile(d)
						return
					}
				}
				a.fileQuitPending = false
				if a.persist() == nil {
					a.quitting = true
					a.quitApp()
				}
				return
			}
			var saveNext func(int)
			saveNext = func(i int) {
				if i >= len(dirty) {
					a.fileQuitPending = false
					if a.hasUnsavedFiles() {
						a.storageError = "保存期间有新的编辑，请核对后再次退出"
						return
					}
					if a.persist() == nil {
						a.quitting = true
						a.quitApp()
					}
					return
				}
				d := dirty[i]
				a.saveFile(d, false, false, func(ok bool) {
					if !ok || d.dirty() {
						a.fileQuitPending = false
						a.selectFile(d)
						return
					}
					saveNext(i + 1)
				})
			}
			saveNext(0)
		})
	}()
}

func (a *App) advanceFileExit() {
	if !a.fileExitRequested || a.quitDialog || a.fileQuitPending {
		return
	}
	if a.hasGitWrites() {
		return
	}
	for _, d := range a.files {
		if d.saving || d.checkpointBusy {
			return
		}
	}
	a.requestFileExit()
}

func (a *App) fileExitNotice(c *native.Context) {
	if a.gitQuitPending {
		native.Text(c, "正在等待 Git 操作完成后退出…").FontSize(11).TextColor(c.Theme().TextMuted).Padding(8, 14)
	}
	if a.recoveryWarning != "" {
		native.Row(c).FillWidth().Padding(8, 14).Gap(8).AlignItems(native.Center).Children(func() {
			native.Text(c, a.recoveryWarning).FontSize(11).TextColor(c.Theme().Danger).MaxLines(3).Grow(1)
			if actionButton(c, "查看恢复目录").Clicked() && a.win != nil {
				if err := mygo.Shell.OpenPath(a.recoveryDir()); err != nil {
					a.recoveryWarning = err.Error()
				}
			}
		})
	}
	if !a.fileExitRequested {
		return
	}
	native.Row(c).FillWidth().Padding(8, 14).Gap(8).AlignItems(native.Center).Background(c.Theme().Surface).Children(func() {
		native.Text(c, "正在等待文件保存完成…").FontSize(11).TextColor(c.Theme().TextMuted).Grow(1)
		if actionButton(c, "取消退出").Clicked() {
			a.fileExitRequested = false
		}
	})
}
