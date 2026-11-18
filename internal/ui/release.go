package ui

import (
	"github.com/egoist/mygo"
	"github.com/shenming0821/Sasa/internal/release"
)

func (a *App) startReleaseControl() (func(), error) {
	return release.StartControl(func() { mygo.RunOnMain(mygo.App.Quit) })
}

func (a *App) SetRelease(info release.Info, ready func() error) {
	a.releaseTitle = info.WindowTitle()
	a.startupReady = ready
}

func (a *App) windowTitle() string {
	if a.releaseTitle != "" {
		return a.releaseTitle
	}
	return "Sasa"
}
