package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/attachment"
	"github.com/shenming0821/Fuyu/internal/power"
	"github.com/shenming0821/Fuyu/internal/project"
	"github.com/shenming0821/Fuyu/internal/storage"
)

type Backend = agent.Backend

type App struct {
	paneState
	taskErrorState
	startupState
	maintenanceState
	completionCatalogState
	settingsState
	terminalState
	worktreeRenameState
	branchCatalogState
	worktreeCreate *worktreeCreation
	sessionRenameState
	quickOpen          quickOpenState
	imageCache         map[string]*imagePreview
	imageLoading       bool
	imageClock         uint64
	viewerOpen         bool
	viewerImage        attachment.Image
	viewerZoom         float32
	viewerScroll       native.ScrollState
	readImageClipboard func() (transfer.Data, error)
	*session
	editorState
	backend            Backend
	backends           map[backendKey]Backend
	modelCatalogs      map[agent.CacheKey]*modelCatalogEntry
	agentErrors        map[agent.AgentKind]string
	defaultAgent       agent.AgentKind
	lastClaudeModels   map[string]string
	modelEfforts       map[string]string
	version            string
	win                *mygo.Window
	md                 markdown.Options
	sessions           []*session
	deletedThreads     []storage.DeletedThread
	sessionDeleteError string
	repositories       []project.Repository
	projectOrder       []string
	hiddenProjects     map[string]bool
	projectsLoading    bool
	projectsError      string
	projectErrors      map[string]string
	projectsRefreshed  time.Time
	nextSession        int
	sidebarOpen        bool
	sidebarWidth       float32
	rightPanelWidth    float32
	compactSidebar     bool
	sessionQuery       string
	tabsSelected       string
	collapsedProjects  map[string]bool
	historyQuery       string
	historyScope       int
	threadList         threadList
	root               *App
	store              *storage.Store
	lastSaved          []byte
	storageError       string
	historyOpen        bool
	quitting           bool
	quitDialog         bool
	quitStopping       bool
	quitDeadline       time.Time
	quitSent           map[string]bool
	quitTargets        map[int]bool
	quitApp            func()
	quitPrompt         func(mygo.MessageOptions) (mygo.MessageResult, error)
	releaseTitle       string
	startupReady       func() error
	// Replaced in component tests so no task or native dialog is started.
	submit   func()
	respond  func(*app.Interaction, any)
	dispatch func(func())
	shutdown chan struct{}
}

func New(backend Backend, version, cwd string, startupErr error) *App {
	a := &App{
		backend: backend, version: strings.TrimSpace(version), shutdown: make(chan struct{}), sidebarOpen: true,
		maintenanceState: maintenanceState{wake: make(chan struct{}, 1)},
		quitApp:          mygo.App.Quit, quitPrompt: mygo.Dialog.Message, sidebarWidth: 240,
	}
	a.preferences = defaultPreferences()
	a.rightTab = "history"
	a.newSession(cwd)
	// MyGo measures proportional digits but renders tabular list markers.
	// Reserve room for common multi-digit markers throughout streaming;
	// the local Markdown patch caps unordered marker columns at 1.4 em.
	a.md.Theme = &markdown.Theme{ListIndent: 3, FontSize: 15, LineHeight: 1.5, Font: textFont, CodeFont: codeFont}
	a.md.Theme.Headings[1].Size = 1.2
	if startupErr != nil {
		a.State.Error, a.State.Phase = "连接 Agent 失败，请核对可执行路径和配置后重新启动 Fuyu："+startupErr.Error(), "连接失败"
		a.State.Disconnected = true
	}
	a.submit = a.send
	a.respond = a.answer
	return a
}

func (a *App) Run() error {
	var readyErr error
	stopControl := func() {}
	defer func() { stopControl() }()
	if a.store != nil {
		mygo.App.SetPath(mygo.PathUserData, a.store.Directory())
	}
	a.powerController = power.New()
	defer a.powerController.Close()
	if baseline := a.preferences.KeepAwakeBaseline; baseline != nil {
		a.powerController.RestoreBaseline(power.Baseline{AC: baseline.AC, Battery: baseline.Battery})
	}
	if mode, err := a.powerController.Sync(); err != nil {
		a.settingsError = "读取系统电源配置失败：" + err.Error()
	} else {
		a.preferences.KeepAwake = mode
	}
	defer a.catalogWrites.Wait()
	stopStartup := func() {}
	defer func() { stopStartup() }()
	defer close(a.shutdown)
	defer a.closeWorkspaceResources()
	mygo.App.WhenReady(func() {
		a.applySystemTheme()
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title: a.windowTitle(), Width: 1000, Height: 760, MinWidth: 640, MinHeight: 480,
			StateKey: "main",
			Content:  native.View(a.view),
		})
		a.installLifecycle()
		a.installSettingsMenu()
		a.installNotificationActions()
		a.loadCompletionDiskCache()
		a.refreshProjects()
		if a.backend != nil {
			go a.receive()
			if a.historyOpen {
				a.openThreadList()
			}
		}
		for key, backend := range a.backends {
			go a.receiveBackend(backend, key)
		}
		a.recoverSelectedSession()
		stopStartup = a.startRuntimeLoading()
		go a.autosave()
		stop, err := a.startReleaseControl()
		if err == nil {
			stopControl = stop
			if a.startupReady != nil {
				err = a.startupReady()
			}
		}
		if err != nil {
			readyErr = err
			a.quitting = true
			mygo.App.Quit()
		}
	})
	if err := mygo.App.Run(); err != nil {
		return err
	}
	return readyErr
}

func (a *App) canSend() bool {
	return a.edit == nil && a.canSubmitTask() && (strings.TrimSpace(a.State.Draft) != "" || len(a.State.DraftImages) > 0)
}

func (a *App) canSubmitTask() bool {
	if a.State.PendingSubmission() != nil && (a.snapshotPending || a.State.NeedsSnapshot || a.State.Incomplete) {
		return false
	}
	return a.open && a.sessionBackend() != nil && !a.releasing && !a.editPreparing && !a.application().fileQuitPending && !a.application().quitStopping && !a.State.Busy && !a.State.Disconnected && !a.recovering && !a.recoveryBlocked && !a.writerOccupied && !a.forking &&
		!a.imagesImporting && a.State.Cwd != ""
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
	a.composerFocus = true
	text := strings.TrimSpace(a.State.Draft)
	if (len(a.State.DraftImages) == 0 || a.isClearCommand(text)) && a.runSlashCommand(text) {
		return
	}
	a.sendInput(text, slices.Clone(a.State.DraftImages), false)
}

func (a *App) sendInput(text string, images []attachment.Image, edited bool) {
	if !a.canSubmitTask() || strings.TrimSpace(text) == "" && len(images) == 0 {
		return
	}
	if len(images) > 0 {
		if _, ok := a.sessionBackend().(imageSender); !ok {
			a.State.Error = "当前连接不支持图片输入"
			return
		}
		for _, image := range images {
			if _, err := attachment.Read(image); err != nil {
				a.State.Error = err.Error()
				return
			}
		}
	}
	modelBackend, supportsModels := a.sessionBackend().(modelSender)
	if a.selectedModel != "" && !supportsModels {
		a.modelError = "当前连接不支持切换模型"
		return
	}
	selectedModel := a.selectedModel
	selectedEffort, selectedTier := a.sendTurnOptions()
	skills := a.draftSkills(text)
	draft, draftImages := a.State.Draft, a.State.DraftImages
	if edited {
		a.removeEditPlaceholder()
	}
	a.State.DraftImages = images
	a.State.Begin(text)
	if edited {
		a.State.Draft, a.State.DraftImages = draft, draftImages
		message := a.State.Messages[len(a.State.Messages)-1]
		message.Edited = true
		a.edit.ID, a.edit.Submitted = message.ID, true
		a.editSending = true
	}
	if err := a.persist(); err != nil {
		a.abortBeforeTurn(text, err)
		return
	}
	if !edited {
		a.processScrollHold = false
		a.editScrollHold = 0
		a.list.FollowEnd = true
		a.list.ScrollToEnd()
	}
	sequence, cwd, threadID := a.State.Sequence, a.State.Cwd, a.State.ThreadID
	ctx, cancel := agent.RequestContext()
	a.submitCancel = cancel
	a.submitStop = false
	go func() {
		defer cancel()
		model := selectedModel
		if supportsModels && model == "" {
			var err error
			model, err = modelBackend.DefaultModel(ctx, cwd)
			if err != nil {
				a.update(func() {
					if a.State.Sequence == sequence {
						a.submitCancel = nil
						a.submitStop = false
						a.State.Busy = false
						a.State.FinishTiming()
						a.State.RejectSubmission()
						a.State.Error, a.State.Phase = "读取默认模型失败，本次未发送："+err.Error(), "未发送"
						if errors.Is(err, context.Canceled) {
							a.State.Error, a.State.Phase = "", "已取消"
						}
						a.editSendFailed()
						if !edited && a.State.Draft == "" && len(a.State.DraftImages) == 0 {
							a.State.Draft = text
						}
						a.restoreSubmittedImages(text)
					}
				})
				return
			}
		}
		if err := ctx.Err(); err != nil {
			a.fail(sequence, text, &agent.NotSubmittedError{Err: err}, false)
			return
		}
		if threadID == "" {
			thread, err := a.sessionBackend().StartThread(ctx, cwd)
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
		if err := ctx.Err(); err != nil {
			a.fail(sequence, text, &agent.NotSubmittedError{Err: err}, false)
			return
		}
		var turn agent.Turn
		var err error
		if supportsModels {
			if sender, ok := modelBackend.(optionSender); ok {
				turn, err = sender.SendWithOptions(ctx, threadID, text, skills, images,
					agent.TurnOptions{Model: model, Effort: selectedEffort, ServiceTier: selectedTier})
			} else {
				turn, err = modelBackend.SendWithModel(ctx, threadID, text, skills, images, model)
			}
		} else if sender, ok := a.sessionBackend().(imageSender); ok && len(images) > 0 {
			turn, err = sender.SendWithImages(ctx, threadID, text, skills, images)
		} else if sender, ok := a.sessionBackend().(interface {
			SendWithSkills(context.Context, string, string, []agent.Skill) (agent.Turn, error)
		}); ok && len(skills) > 0 {
			turn, err = sender.SendWithSkills(ctx, threadID, text, skills)
		} else {
			turn, err = a.sessionBackend().Send(ctx, threadID, text)
		}
		if err != nil {
			a.fail(sequence, text, err, true)
			return
		}
		a.update(func() {
			if a.State.Sequence == sequence {
				a.submitCancel = nil
				if edited && a.edit != nil {
					a.edit.SubmittedTurnID = turn.ID
					if a.kind.Normalized() == agent.Claude {
						a.edit = nil
						a.editSending = false
					}
				}
				if a.kind.Normalized() == agent.Claude {
					a.rememberClaudeModel(model)
				}
				if model != "" && a.kind.Normalized() != agent.Claude {
					a.State.Model = model
				}
				if a.State.Busy {
					stopping := a.State.Stopping()
					a.State.BindTiming(turn.ID)
					a.State.TurnID = turn.ID
					if stopping && a.submitStop {
						a.stop()
					} else if !stopping {
						a.State.Phase = "正在执行"
					}
				}
			}
		})
	}()
}

func (a *App) abortBeforeTurn(text string, err error) {
	a.State.FinishTiming()
	a.State.Busy = false
	a.State.RejectSubmission()
	a.State.Error, a.State.Phase = "会话状态未能保存，本次未发送："+err.Error(), "未发送"
	a.editSendFailed()
	if !a.lastMessageEdited() && a.State.Draft == "" && len(a.State.DraftImages) == 0 {
		a.State.Draft = text
	}
	a.restoreSubmittedImages(text)
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
	a.State.FinishTiming()
	var rpc *agent.RPCError
	var notSubmitted *agent.NotSubmittedError
	a.submitCancel = nil
	a.submitStop = false
	rejectedStart := !turnSubmitted && errors.As(err, &rpc) && !a.State.Disconnected
	a.State.Busy = false
	if errors.As(err, &notSubmitted) {
		a.State.RejectSubmission()
		a.State.Error, a.State.Phase = "本次未发送："+err.Error(), "未发送"
		if errors.Is(err, context.Canceled) {
			a.State.Error, a.State.Phase = "", "已取消"
		}
	} else if rejectedStart {
		a.State.RejectSubmission()
		// No turn/start was attempted, so retrying cannot duplicate execution.
		a.State.Error, a.State.Phase = "创建会话失败，可稍后重新发送："+err.Error(), "会话创建失败"
	} else {
		a.State.Error, a.State.Phase = "请求结果未确认，请重新启动 Fuyu 并核验原生记录："+err.Error(), "请求待核验"
		a.State.Disconnected = true
	}
	a.editSendFailed()
	if !a.lastMessageEdited() && a.State.Draft == "" && len(a.State.DraftImages) == 0 {
		a.State.Draft = text
	}
	a.restoreSubmittedImages(text)
}

func (a *App) stop() {
	a = a.forSession(a.session)
	if !a.State.Busy || a.State.Stopping() && !a.submitStop || a.sessionBackend() == nil || a.State.TurnID == "" && a.submitCancel == nil {
		return
	}
	a.State.BeginStop()
	threadID, turnID := a.State.ThreadID, a.State.TurnID
	if turnID == "" && a.submitCancel != nil {
		a.submitStop = true
		a.submitCancel()
		return
	}
	a.submitStop = false
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		err := a.sessionBackend().Interrupt(ctx, threadID, turnID)
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
	a.receiveBackend(a.backend, backendKey{kind: agent.Codex})
}

func (a *App) receiveBackend(backend Backend, key backendKey) {
	events := backend.Events()
	delivery := eventDelivery{}
	for {
		var event agent.Event
		select {
		case next, ok := <-events:
			if !ok {
				return
			}
			event = next
		case <-a.shutdown:
			return
		}
		timer := time.NewTimer(40 * time.Millisecond)
		batch := delivery.collect(event, events, timer.C, a.shutdown)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if len(batch) == 0 {
			return
		}
		for i := range batch {
			batch[i].Session.Agent = key.kind
			batch[i].Session.Runtime = key.runtime
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

func (a *App) applyBatch(batch []agent.Event) {
	for _, s := range a.sessions {
		a.forSession(s).applySessionBatch(batch)
	}
	a.releaseClosedSessions(time.Now())
	for _, event := range batch {
		if event.Method == "turn/completed" || event.Method == "fuyu/disconnected" {
			a.persist()
			break
		}
	}
}

func (a *App) applySessionBatch(batch []agent.Event) {
	follow := false
	for _, e := range batch {
		if a.kind.Normalized() == agent.Claude && e.Session.Agent == agent.Claude && e.Session.Runtime == a.runtimeKey && e.Session.SessionID == a.State.ThreadID && e.Generation > a.generation {
			a.generation = e.Generation
		}
		if !a.session.accepts(e) {
			continue
		}
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
		a.finishMessageEdit()
		if e.Method == "turn/completed" && wasBusy && !a.State.Busy && !a.State.Disconnected {
			delete(a.application().quitTargets, a.session.id)
			a.notifyCompletion(e)
			a.recordThreadHistory()
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
	backend, ok := a.sessionBackend().(interface {
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
	backend, ok := a.sessionBackend().(interface {
		ReadThread(context.Context, string) (json.RawMessage, error)
	})
	if !ok {
		a.State.Incomplete = true
		a.State.Notice = "当前连接无法读取原生记录，请核验本轮结果及消息是否送达。"
		return
	}
	threadID, sequence := a.State.ThreadID, a.State.Sequence
	a.snapshotPending = true
	a.State.NeedsSnapshot = false
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		raw, err := backend.ReadThread(ctx, threadID)
		a.update(func() {
			a.snapshotPending = false
			if a.State.ThreadID != threadID || a.State.Sequence != sequence {
				return
			}
			if err != nil {
				a.State.Incomplete = true
				a.State.Notice = "原生记录核验失败，结果可能不完整：" + err.Error()
				return
			}
			if err := a.State.Reconcile(raw); err != nil {
				a.State.Incomplete = true
				a.State.Notice = fmt.Sprintf("原生记录无法校准：%v；结果可能不完整。", err)
			}
			a.finishMessageEdit()
		})
	}()
}

func (a *App) update(fn func()) {
	wrapped := func() {
		fn()
		a.scheduleSave()
	}
	if a.dispatch != nil {
		a.dispatch(wrapped)
		return
	}
	a.win.Update(wrapped)
}
