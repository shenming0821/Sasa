package ui

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/codex"
)

type Backend interface {
	StartThread(context.Context, string) (codex.Thread, error)
	Send(context.Context, string, string) (codex.Turn, error)
	Interrupt(context.Context, string, string) error
	Events() <-chan codex.Event
}

type App struct {
	State       app.State
	backend     Backend
	version     string
	win         *mygo.Window
	list        native.ListState
	md          markdown.Options
	projectOpen bool
	detailsOpen bool
	copiedID    string
	// Replaced in component tests so no task or native dialog is started.
	submit func()
}

func New(backend Backend, version, cwd string, startupErr error) *App {
	a := &App{State: app.New(cwd), backend: backend, version: strings.TrimSpace(version)}
	// MyGo measures proportional digits but renders tabular list markers.
	// Reserve room for common multi-digit markers throughout streaming.
	a.md.Theme = &markdown.Theme{ListIndent: 3, FontSize: 15, LineHeight: 1.5}
	if startupErr != nil {
		a.State.Error, a.State.Phase = startupErr.Error(), "连接失败"
		a.State.Disconnected = true
	}
	a.submit = a.send
	return a
}

func (a *App) Run() error {
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Fuyu", Width: 1000, Height: 760, MinWidth: 640, MinHeight: 480,
			Content: native.View(a.view),
		})
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if a.State.Busy {
				e.PreventDefault()
				a.State.Error = "任务仍在运行，请先停止任务，再关闭窗口。"
				a.win.Invalidate()
			}
		})
		mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
			if a.State.Busy {
				e.PreventDefault()
				a.State.Error = "任务仍在运行，请先停止任务，再退出 Fuyu。"
				a.win.Invalidate()
			}
		})
		if a.backend != nil {
			go a.receive()
		}
	})
	return mygo.App.Run()
}

func (a *App) canSend() bool {
	return a.backend != nil && !a.State.Busy && !a.State.Disconnected &&
		a.State.Cwd != "" && strings.TrimSpace(a.State.Draft) != ""
}

func (a *App) chooseDirectory(defaultPath string) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: a.win, Title: "选择工作目录", Directory: true,
		DefaultPath: defaultPath, ButtonLabel: "选择此目录",
	})
	a.win.Update(func() {
		if err != nil {
			a.State.Error = err.Error()
			return
		}
		if len(paths) > 0 && !a.State.Busy {
			next := app.New(filepath.Clean(paths[0]))
			next.Sequence = a.State.Sequence + 1
			a.State = next
			a.list = native.ListState{}
			a.projectOpen, a.detailsOpen, a.copiedID = false, false, ""
		}
	})
}

func (a *App) send() {
	if !a.canSend() {
		return
	}
	text := strings.TrimSpace(a.State.Draft)
	a.State.Begin(text)
	sequence, cwd, threadID := a.State.Sequence, a.State.Cwd, a.State.ThreadID
	go func() {
		ctx, cancel := codex.RequestContext()
		defer cancel()
		if threadID == "" {
			thread, err := a.backend.StartThread(ctx, cwd)
			if err != nil {
				a.fail(sequence, text, err)
				return
			}
			threadID = thread.Thread.ID
			a.win.Update(func() {
				if a.State.Sequence != sequence {
					return
				}
				a.State.ThreadID, a.State.Model, a.State.Cwd = threadID, thread.Model, thread.Cwd
				a.State.Policy = string(thread.ApprovalPolicy)
				a.State.Sandbox = string(thread.Sandbox)
			})
		}
		turn, err := a.backend.Send(ctx, threadID, text)
		if err != nil {
			a.fail(sequence, text, err)
			return
		}
		a.win.Update(func() {
			if a.State.Sequence == sequence && a.State.Busy {
				a.State.TurnID, a.State.Phase = turn.ID, "正在执行"
			}
		})
	}()
}

func (a *App) fail(sequence int, text string, err error) {
	a.win.Update(func() {
		if a.State.Sequence != sequence {
			return
		}
		a.State.Error, a.State.Phase = err.Error(), "请求失败"
		// An uncertain submit must be reconciled before another execution.
		a.State.Busy = false
		a.State.Disconnected = true
		if a.State.Draft == "" {
			a.State.Draft = text
		}
	})
}

func (a *App) stop() {
	a.State.Phase = "正在停止"
	threadID, turnID := a.State.ThreadID, a.State.TurnID
	go func() {
		ctx, cancel := codex.RequestContext()
		defer cancel()
		err := a.backend.Interrupt(ctx, threadID, turnID)
		if err != nil {
			a.win.Update(func() {
				if a.State.ThreadID != threadID || a.State.TurnID != turnID || !a.State.Busy {
					return
				}
				a.State.Error = "停止请求未确认：" + err.Error()
				a.State.Phase = "停止待确认"
			})
		}
	}()
}

func (a *App) receive() {
	events := a.backend.Events()
	for event := range events {
		batch := []codex.Event{event}
		timer := time.NewTimer(40 * time.Millisecond)
	collect:
		for len(batch) < 256 {
			select {
			case next, ok := <-events:
				if !ok {
					break collect
				}
				batch = append(batch, next)
			case <-timer.C:
				break collect
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		a.win.Update(func() {
			for _, e := range batch {
				a.State.Apply(e)
			}
		})
	}
}
