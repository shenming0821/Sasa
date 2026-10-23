package ui

import (
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/storage"
)

// A session owns both its protocol projection and its reading/interaction state.
// Switching the selected session never changes the owner of a pending callback.
type session struct {
	id              int
	open            bool
	State           app.State
	list            native.ListState
	scroll          native.ScrollState
	rows            map[string]*native.Handle
	commandGroups   map[string]*bool
	completion      inputCompletion
	readingContext  *native.Context
	restoreOffset   bool
	projectOpen     bool
	detailsOpen     bool
	copiedID        string
	snapshotPending bool
	followResponses map[string]bool
	saved           storage.Session
	loaded          bool
	recovering      bool
	recoveryBlocked bool
	historyLoading  bool
	historyCursor   *string
	recoveryEvents  []codex.Event
	recoveryBytes   int
	recoveryEpoch   int
	historyOverflow bool
	reviewRequired  bool
	writerOccupied  bool
	forking         bool
	forkUncertain   bool
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
	a.nextSession++
	s := &session{id: a.nextSession, State: app.New(cwd), loaded: true}
	if a.session != nil && a.State.Disconnected {
		s.State.Disconnected = true
		s.State.Phase, s.State.Error = a.State.Phase, a.State.Error
	}
	a.sessions = append(a.sessions, s)
	a.selectSession(s)
}

func (a *App) selectSession(s *session) {
	s.open = true
	a.session = s
	a.compactSidebar = false
	s.projectOpen, s.detailsOpen = false, false
	if !s.loaded && (a.win != nil || a.dispatch != nil) {
		a.recoverSession(s)
	}
}

// Closing a tab leaves its draft and live protocol owner available in history.
func (a *App) closeSession(s *session) {
	if !s.open {
		return
	}
	s.open = false
	s.completion.focus = false
	if a.session != s {
		return
	}
	index := 0
	for i, candidate := range a.sessions {
		if candidate == s {
			index = i
			break
		}
	}
	for i := index - 1; i >= 0; i-- {
		if a.sessions[i].open {
			a.selectSession(a.sessions[i])
			return
		}
	}
	for _, candidate := range a.sessions[index+1:] {
		if candidate.open {
			a.selectSession(candidate)
			return
		}
	}
	a.newSession(s.State.Cwd)
}

func (a *App) recoverSelectedSession() {
	if !a.loaded {
		a.recoverSession(a.session)
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
	if s.saved.Title != "" && s.saved.Title != "新会话" {
		return s.saved.Title
	}
	for _, m := range s.State.Messages {
		if m.Role == "user" {
			title := strings.Join(strings.Fields(m.Text), " ")
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
