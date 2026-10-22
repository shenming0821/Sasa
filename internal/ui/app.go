package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	submit          func()
	respond         func(*app.Interaction, any)
	shutdown        chan struct{}
	snapshotPending bool
	followResponses map[string]bool
}

func New(backend Backend, version, cwd string, startupErr error) *App {
	a := &App{State: app.New(cwd), backend: backend, version: strings.TrimSpace(version), shutdown: make(chan struct{})}
	// MyGo measures proportional digits but renders tabular list markers.
	// Reserve room for common multi-digit markers throughout streaming.
	a.md.Theme = &markdown.Theme{ListIndent: 3, FontSize: 15, LineHeight: 1.5}
	if startupErr != nil {
		a.State.Error, a.State.Phase = "连接 Codex 失败，请核对可执行路径和配置后重新启动 Fuyu："+startupErr.Error(), "连接失败"
		a.State.Disconnected = true
	}
	a.submit = a.send
	a.respond = a.answer
	return a
}

func (a *App) Run() error {
	defer close(a.shutdown)
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
				a.fail(sequence, text, err, false)
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
			a.fail(sequence, text, err, true)
			return
		}
		a.win.Update(func() {
			if a.State.Sequence == sequence && a.State.Busy {
				a.State.TurnID, a.State.Phase = turn.ID, "正在执行"
			}
		})
	}()
}

func (a *App) fail(sequence int, text string, err error, turnSubmitted bool) {
	a.win.Update(func() {
		a.applySubmissionFailure(sequence, text, err, turnSubmitted)
	})
}

func (a *App) applySubmissionFailure(sequence int, text string, err error, turnSubmitted bool) {
	if a.State.Sequence != sequence {
		return
	}
	var rpc *codex.RPCError
	rejectedStart := !turnSubmitted && errors.As(err, &rpc) && !a.State.Disconnected
	a.State.Busy = false
	if rejectedStart {
		// No turn/start was attempted, so retrying cannot duplicate execution.
		a.State.Error, a.State.Phase = "创建会话失败，可稍后重新发送："+err.Error(), "会话创建失败"
	} else {
		a.State.Error, a.State.Phase = "请求结果未确认，请重新启动 Fuyu 并核验原生记录："+err.Error(), "请求待核验"
		a.State.Disconnected = true
	}
	if a.State.Draft == "" {
		a.State.Draft = text
	}
}

func (a *App) stop() {
	a.State.BeginStop()
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
		applied := make(chan struct{})
		a.win.Update(func() {
			defer close(applied)
			a.applyBatch(batch)
		})
		// Window.Update is asynchronous. Bound outstanding UI batches as well as
		// the protocol queue, so an unresponsive renderer cannot accumulate work.
		select {
		case <-applied:
		case <-a.shutdown:
			return
		}
	}
}

func (a *App) applyBatch(batch []codex.Event) {
	follow := false
	for _, e := range batch {
		a.State.Apply(e)
		if e.Method == "serverRequest/resolved" {
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			if json.Unmarshal(e.Params, &p) == nil && a.followResponses[string(p.RequestID)] {
				delete(a.followResponses, string(p.RequestID))
				follow = true
			}
		}
	}
	if !a.State.Busy || a.State.Disconnected {
		clear(a.followResponses)
	}
	// Answering is an explicit request to continue this workflow. Position its
	// next step after the form shrinks; unrelated stream updates keep the anchor.
	if follow {
		a.list.ScrollToEnd()
	}
	a.reconcileIfNeeded()
}

func (a *App) answer(r *app.Interaction, result any) {
	if a.State.Disconnected || !a.State.Busy || a.State.Stopping() {
		return
	}
	response, err := r.Submit(result)
	if err != nil {
		return
	}
	backend, ok := a.backend.(interface {
		Respond(json.RawMessage, any) error
	})
	if !ok {
		r.Status, r.Error = "发送失败", "当前连接不支持交互响应，请停止任务。"
		return
	}
	if a.followResponses == nil {
		a.followResponses = make(map[string]bool)
	}
	a.followResponses[string(r.Request.ID)] = true
	go func() {
		err := backend.Respond(r.Request.ID, response)
		a.win.Update(func() {
			if r.Status != "提交中" {
				return
			}
			if err != nil {
				delete(a.followResponses, string(r.Request.ID))
				r.Status, r.Error = "发送待核验", "响应未确认，请停止任务并核验结果："+err.Error()
				a.State.Error = r.Error
			} else {
				r.Status = "已响应"
			}
		})
	}()
}

func (a *App) reconcileIfNeeded() {
	if !a.State.NeedsSnapshot || a.State.Busy || a.State.Disconnected || a.snapshotPending {
		return
	}
	backend, ok := a.backend.(interface {
		ReadThread(context.Context, string) (json.RawMessage, error)
	})
	if !ok {
		a.State.Notice = "部分事件未保留，当前连接无法读取原生记录，请核验结果。"
		return
	}
	threadID, sequence := a.State.ThreadID, a.State.Sequence
	a.snapshotPending = true
	a.State.NeedsSnapshot = false
	go func() {
		ctx, cancel := codex.RequestContext()
		defer cancel()
		raw, err := backend.ReadThread(ctx, threadID)
		a.win.Update(func() {
			a.snapshotPending = false
			if a.State.ThreadID != threadID || a.State.Sequence != sequence {
				return
			}
			if err != nil {
				a.State.Notice = "原生记录核验失败，结果可能不完整：" + err.Error()
				return
			}
			if err := a.State.Reconcile(raw); err != nil {
				a.State.Notice = fmt.Sprintf("原生记录无法校准：%v；结果可能不完整。", err)
			}
		})
	}()
}
