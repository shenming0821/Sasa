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
	"github.com/shenming0821/Fuyu/internal/storage"
)

type Backend interface {
	StartThread(context.Context, string) (codex.Thread, error)
	Send(context.Context, string, string) (codex.Turn, error)
	Interrupt(context.Context, string, string) error
	Events() <-chan codex.Event
}

type App struct {
	*session
	backend           Backend
	version           string
	win               *mygo.Window
	md                markdown.Options
	sessions          []*session
	nextSession       int
	sidebarOpen       bool
	sidebarWidth      float32
	compactSidebar    bool
	sessionQuery      string
	tabsSelected      int
	collapsedProjects map[string]bool
	historyQuery      string
	historyScope      int
	root              *App
	store             *storage.Store
	lastSaved         []byte
	storageError      string
	historyOpen       bool
	nativeThreads     []codex.ThreadMeta
	threadsCursor     *string
	threadsLoading    bool
	threadsError      string
	quitting          bool
	quitDialog        bool
	quitStopping      bool
	quitDeadline      time.Time
	quitSent          map[string]bool
	quitTargets       map[int]bool
	quitApp           func()
	quitPrompt        func(mygo.MessageOptions) (mygo.MessageResult, error)
	// Replaced in component tests so no task or native dialog is started.
	submit   func()
	respond  func(*app.Interaction, any)
	dispatch func(func())
	shutdown chan struct{}
}

func New(backend Backend, version, cwd string, startupErr error) *App {
	a := &App{
		backend: backend, version: strings.TrimSpace(version), shutdown: make(chan struct{}), sidebarOpen: true,
		quitApp: mygo.App.Quit, quitPrompt: mygo.Dialog.Message, sidebarWidth: 240,
	}
	a.newSession(cwd)
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
		a.installLifecycle()
		if a.backend != nil {
			go a.receive()
			if a.historyOpen {
				a.loadThreadList()
			}
			for _, s := range a.sessions {
				if !s.loaded {
					a.recoverSession(s)
				}
			}
		}
		go a.autosave()
	})
	return mygo.App.Run()
}

func (a *App) canSend() bool {
	return a.backend != nil && !a.application().quitStopping && !a.State.Busy && !a.State.Disconnected && !a.recovering && !a.recoveryBlocked && !a.writerOccupied && !a.forking &&
		a.State.Cwd != "" && strings.TrimSpace(a.State.Draft) != ""
}

func (a *App) chooseDirectory(owner *session, defaultPath string) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: a.win, Title: "选择工作目录", Directory: true,
		DefaultPath: defaultPath, ButtonLabel: "选择此目录",
	})
	a.update(func() {
		if err != nil {
			owner.State.Error = err.Error()
			return
		}
		if len(paths) > 0 {
			cwd := filepath.Clean(paths[0])
			if owner.State.ThreadID == "" && !owner.State.Busy && len(owner.State.Messages) == 0 {
				owner.State.Cwd = cwd
			} else {
				a.newSession(cwd)
			}
		}
	})
}

func (a *App) send() {
	a = a.forSession(a.session)
	if !a.canSend() {
		return
	}
	text := strings.TrimSpace(a.State.Draft)
	a.State.Begin(text)
	if err := a.persist(); err != nil {
		a.abortBeforeTurn(text, err)
		return
	}
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
			installed := make(chan struct{})
			var checkpointErr error
			a.update(func() {
				defer close(installed)
				if a.State.Sequence != sequence {
					return
				}
				a.State.ThreadID, a.State.Model, a.State.Cwd = threadID, thread.Model, thread.Cwd
				a.State.Policy = string(thread.ApprovalPolicy)
				a.State.Sandbox = string(thread.Sandbox)
				checkpointErr = a.persist()
				if checkpointErr != nil {
					a.abortBeforeTurn(text, checkpointErr)
				}
			})
			// Register the owner before turn/start can emit events for this thread.
			select {
			case <-installed:
			case <-a.shutdown:
				return
			}
			if checkpointErr != nil {
				return
			}
		}
		turn, err := a.backend.Send(ctx, threadID, text)
		if err != nil {
			a.fail(sequence, text, err, true)
			return
		}
		a.update(func() {
			if a.State.Sequence == sequence && a.State.Busy {
				a.State.TurnID, a.State.Phase = turn.ID, "正在执行"
			}
		})
	}()
}

func (a *App) abortBeforeTurn(text string, err error) {
	a.State.Busy = false
	a.State.Error, a.State.Phase = "会话状态未能保存，本次未发送："+err.Error(), "未发送"
	if a.State.Draft == "" {
		a.State.Draft = text
	}
}

func (a *App) fail(sequence int, text string, err error, turnSubmitted bool) {
	a.update(func() {
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
	a = a.forSession(a.session)
	a.State.BeginStop()
	threadID, turnID := a.State.ThreadID, a.State.TurnID
	go func() {
		ctx, cancel := codex.RequestContext()
		defer cancel()
		err := a.backend.Interrupt(ctx, threadID, turnID)
		if err != nil {
			a.update(func() {
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
		a.update(func() {
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
	for _, s := range a.sessions {
		a.forSession(s).applySessionBatch(batch)
	}
}

func (a *App) applySessionBatch(batch []codex.Event) {
	follow := false
	for _, e := range batch {
		if a.recovering && e.Method == "fuyu/eventGap" {
			a.historyOverflow = true
			continue
		}
		if a.session.bufferRecovery(e) {
			continue
		}
		if a.writerOccupied && e.Method != "fuyu/disconnected" {
			continue
		}
		if e.Method == "fuyu/disconnected" {
			a.recoveryEpoch++
			a.recovering, a.recoveryBlocked = false, true
			a.recoveryEvents, a.recoveryBytes = nil, 0
		}
		if e.Method == "fuyu/eventGap" && a.State.ThreadID == "" {
			continue
		}
		wasBusy := a.State.Busy
		a.State.Apply(e)
		if e.Method == "turn/completed" && wasBusy && !a.State.Busy && !a.State.Disconnected {
			delete(a.application().quitTargets, a.session.id)
		}
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
	a = a.forSession(a.session)
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
		a.update(func() {
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
	a = a.forSession(a.session)
	if !a.State.NeedsSnapshot || a.State.Busy || a.State.Disconnected || a.snapshotPending || a.writerOccupied {
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
		a.update(func() {
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

func (a *App) update(fn func()) {
	wrapped := func() {
		fn()
		a.persist()
	}
	if a.dispatch != nil {
		a.dispatch(wrapped)
		return
	}
	a.win.Update(wrapped)
}
