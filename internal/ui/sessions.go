package ui

import (
	"context"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/attachment"
	"github.com/shenming0821/Fuyu/internal/storage"
)

// A session owns both its protocol projection and its reading/interaction state.
// Switching the selected session never changes the owner of a pending callback.
type session struct {
	messageEditing
	conversationCache
	conversationNavigation
	nativeThreads        []agent.ThreadMeta
	threadsCursor        *string
	threadsLoading       bool
	threadsReload        bool
	threadsLayoutPending bool
	threadsError         string
	threadSources        map[backendKey]threadSource
	historyCacheKey      agent.CacheKey
	kind                 agent.AgentKind
	runtimeKey           string
	generation           agent.ProcessGeneration
	models               []agent.Model
	modelsLoading        bool
	modelsError          string
	modelsLoaded         bool
	modelsCwd            string
	modelsRuntime        string
	selectedModel        string
	modelResolving       bool
	modelResolvedCwd     string
	modelMenuOpen        bool
	modelError           string
	imagesImporting      bool
	importingImages      int
	queuedImages         []attachment.Image
	imageError           string
	id                   int
	open                 bool
	stopOnClose          bool
	State                app.State
	list                 native.ListState
	scroll               native.ScrollState
	rows                 map[string]*native.Handle
	commandGroups        map[string]*bool
	expandedProcesses    map[string]bool
	processProgress      map[string]float32
	processScrollHold    bool
	disclosureMoving     bool
	changeSummaries      map[string]cachedChangeSummary
	completion           inputCompletion
	readingContext       *native.Context
	restoreOffset        bool
	detailsOpen          bool
	copiedID             string
	snapshotPending      bool
	followResponses      map[string]bool
	saved                storage.Session
	loaded               bool
	recovering           bool
	recoveryBlocked      bool
	historyLoading       bool
	historyAtTop         bool
	historyCursor        *string
	recoveryEvents       []agent.Event
	recoveryBytes        int
	recoveryEpoch        int
	historyOverflow      bool
	reviewRequired       bool
	writerOccupied       bool
	forking              bool
	forkUncertain        bool
	submitCancel         context.CancelFunc
	submitStop           bool
	releaseAfter         time.Time
	releasing            bool
}

func (s *session) rowHandle(id string) *native.Handle {
	if s.rows == nil {
		s.rows = make(map[string]*native.Handle)
	}
	if s.rows[id] == nil {
		s.rows[id] = &native.Handle{}
	}
	return s.rows[id]
}

func (a *App) newSession(cwd string) {
	kind := a.defaultAgent.Normalized()
	runtime := ""
	if kind == agent.Claude {
		runtime = a.agentRuntime(kind)
	}
	a.newSessionForAgent(cwd, kind, runtime)
}

func (a *App) newSessionForAgent(cwd string, kind agent.AgentKind, runtime string) {
	a.nextSession++
	s := &session{
		modelsCwd: cwd, id: a.nextSession, State: app.New(cwd), loaded: true,
		kind: kind, runtimeKey: runtime,
		historyCacheKey: agent.CacheKey{Agent: kind.Normalized(), Runtime: runtime, Cwd: cwd},
	}
	if kind.Normalized() == agent.Claude {
		s.selectedModel = a.lastClaudeModels[s.runtimeKey]
		s.State.Agent = agent.Claude
		if a.forSession(s).sessionBackend() == nil {
			s.State.Disconnected = true
			s.State.Error = a.agentErrors[agent.Claude]
		}
	}
	if s.kind.Normalized() == agent.Codex && a.session != nil && a.kind.Normalized() == agent.Codex && a.runtimeKey == "" && a.State.Disconnected {
		s.State.Disconnected = true
		s.State.Phase, s.State.Error = a.State.Phase, a.State.Error
	}
	a.sessions = append(a.sessions, s)
	a.selectSession(s)
}

func (a *App) selectSession(s *session) {
	s.open = true
	s.stopOnClose = false
	a.session = s
	a.activeTab = sessionTabID(s.id)
	a.selectWorkspace(a.workspaceRoot(s.State.Cwd))
	a.ensureSessionTab(s)
	a.activatePaneTab(a.activeTab, a.activeWorkspace, false)
	a.rememberFolderTab()
	a.compactSidebar = false
	s.detailsOpen = false
	if !s.loaded && !s.releasing && (a.win != nil || a.dispatch != nil) {
		a.recoverSession(s)
	}
}

func (a *App) clearConversation() {
	if !a.canSend() {
		return
	}
	if len(a.State.DraftImages) > 0 {
		a.State.Error = "清空上下文前请移除图片附件"
		return
	}
	root, previous := a.application(), a.session
	draft := previous.State.Draft
	previous.State.Draft = ""
	if err := root.persist(); err != nil {
		previous.State.Draft = draft
		previous.State.Error = err.Error()
		return
	}
	root.nextSession++
	fresh := &session{
		id: root.nextSession, kind: previous.kind, runtimeKey: previous.runtimeKey,
		State: app.New(previous.State.Cwd), loaded: true, selectedModel: previous.selectedModel,
		modelsCwd: previous.State.Cwd, historyCacheKey: previous.historyCacheKey,
	}
	fresh.State.Agent = previous.kind
	fresh.completion.focus = true
	root.sessions = append(root.sessions, fresh)
	root.selectSession(fresh)
	root.closeSession(previous)
	_ = root.persist()
}

func (a *App) closeSession(s *session) {
	if !s.open {
		return
	}
	a.closeSessionTab(s)
	s.stopOnClose = true
	s.releaseAfter = time.Now()
	a.releaseClosedSessions(s.releaseAfter)
}

func (a *App) closeSessionTab(s *session) {
	if !s.open {
		return
	}
	id := sessionTabID(s.id)
	a.removeWorkspaceTab(id)
	s.open = false
	s.completion.focus = false
}

func (a *App) recoverSelectedSession() {
	layout := a.layoutFor(a.activeWorkspace)
	for _, pane := range paneLeaves(layout.Root) {
		for _, target := range a.sessions {
			if pane.Active == sessionTabID(target.id) && target.open && !target.loaded && !target.recovering && !target.releasing {
				a.recoverSession(target)
			}
		}
	}
}

// Capture a stable session owner on the UI thread before starting asynchronous
// work. This receiver shares transport/dispatch only, not selection or rendering.
func (a *App) forSession(s *session) *App {
	return &App{
		session: s, backend: a.backend, win: a.win,
		shutdown: a.shutdown, dispatch: a.dispatch, root: a.application(),
	}
}

func (a *App) anyBusy() bool {
	for _, s := range a.sessions {
		if s.State.Busy {
			return true
		}
	}
	return false
}

func (s *session) title() string {
	if s.saved.CustomTitle != "" {
		return s.saved.CustomTitle
	}
	if s.saved.Title != "" && s.saved.Title != "新会话" {
		return s.saved.Title
	}
	for _, m := range s.State.Messages {
		if m.Role == "user" {
			title := strings.Join(strings.Fields(m.Text), " ")
			if title == "" && len(m.Images) > 0 {
				title = "图片提问"
			}
			runes := []rune(title)
			if len(runes) > 28 {
				title = string(runes[:28]) + "…"
			}
			return title
		}
	}
	if s.saved.Title != "" {
		return s.saved.Title
	}
	return "新会话"
}
