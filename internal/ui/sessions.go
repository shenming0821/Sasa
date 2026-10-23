package ui

import (
	"fmt"
	"path/filepath"
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
	State           app.State
	list            native.ListState
	scroll          native.ScrollState
	rows            map[string]*native.Handle
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
	a.session = s
	s.projectOpen, s.detailsOpen = false, false
	if !s.loaded && a.win != nil {
		a.recoverSession(s)
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

func (a *App) sidebar(c *native.Context) {
	t := c.Theme()
	native.Column(c).Width(204).FillHeight().Background(t.Surface).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border.Alpha(0.5)).Children(func() {
		native.Row(c).FillWidth().Height(46).Padding(0, 12).AlignItems(native.Center).Children(func() {
			native.Text(c, "Fuyu").FontSize(14).Bold()
			native.Spacer(c)
			b := quietButton(c, "新建会话").Size(30, 30)
			b.Children(func() { native.Icon(c, newIcon).Size(17, 17) })
			if b.Clicked() {
				a.newSession(a.State.Cwd)
			}
		})
		native.Scroll(c).FillWidth().Grow(1).Children(func() {
			native.Column(c).FillWidth().Padding(8).Gap(4).Children(func() {
				for i := len(a.sessions) - 1; i >= 0; i-- {
					s := a.sessions[i]
					b := quietButton(c.Key(s.id), fmt.Sprintf("切换会话 %d", s.id)).FillWidth().Padding(10).Radius(8)
					if a.session == s {
						b.Background(t.SurfacePressed)
					}
					b.Children(func() {
						native.Column(c).FillWidth().Gap(5).Children(func() {
							native.Text(c, s.title()).FontSize(13).TextColor(t.Text).SingleLine()
							status := filepath.Base(s.State.Cwd)
							if s.State.Cwd == "" {
								status = "选择工作目录"
							}
							if pending := len(s.State.PendingInteractions()); pending > 0 {
								status += fmt.Sprintf(" · %d 个待办", pending)
							} else if s.State.Busy || s.State.Disconnected {
								status += " · " + s.State.Phase
							}
							native.Text(c, status).FontSize(11).TextColor(t.TextMuted).SingleLine()
						})
					})
					if b.Clicked() {
						a.selectSession(s)
					}
				}
			})
		})
		b := actionButton(c, "浏览 Codex 历史").FillWidth().Margin(8)
		if b.Clicked() {
			a.historyOpen = !a.historyOpen
			if a.historyOpen && len(a.nativeThreads) == 0 {
				a.loadThreadList()
			}
		}
		if a.historyOpen {
			a.historyBrowser(c)
		}
	})
}
