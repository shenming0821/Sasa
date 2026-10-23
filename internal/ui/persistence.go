package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func (a *App) application() *App {
	if a.root != nil {
		return a.root
	}
	return a
}

func (a *App) AttachStore(store *storage.Store) error {
	snapshot, err := store.Load()
	if err != nil {
		return err
	}
	a.store = store
	if len(snapshot.Sessions) == 0 {
		return a.persist()
	}
	cwd := a.State.Cwd
	startupError, disconnected := a.State.Error, a.State.Disconnected
	a.sessions = nil
	a.sidebarOpen = snapshot.SidebarOpen
	if snapshot.SidebarWidth >= 200 && snapshot.SidebarWidth <= 320 {
		a.sidebarWidth = snapshot.SidebarWidth
	}
	a.historyOpen, a.collapsedProjects = snapshot.HistoryOpen, snapshot.CollapsedProjects
	for _, saved := range snapshot.Sessions {
		s := &session{id: saved.ID, State: app.New(saved.Cwd), saved: saved, loaded: saved.ThreadID == ""}
		s.State.ThreadID, s.State.Draft = saved.ThreadID, saved.Draft
		s.completion.cwd = saved.Cwd
		s.completion.chosen = make(map[string]codex.Skill)
		for name, path := range saved.DraftSkills {
			s.completion.chosen[name] = codex.Skill{Name: name, Path: path, Enabled: true}
		}
		if saved.ThreadID != "" {
			s.State.Phase, s.recoveryBlocked = "等待恢复", true
		} else if saved.Active {
			s.reviewRequired, s.recoveryBlocked = true, true
			s.State.Phase = "上次发送待核验"
			s.State.Notice = "上次会话创建或发送尚未确认，且没有原生会话 ID。请浏览 Codex 历史核验后确认继续；不会自动重发。"
		}
		if disconnected {
			s.State.Error, s.State.Disconnected = startupError, true
		}
		a.sessions = append(a.sessions, s)
		a.nextSession = max(a.nextSession, saved.ID)
		if saved.ID == snapshot.Selected {
			a.session = s
		}
	}
	found := a.session != nil && slices.Contains(a.sessions, a.session)
	if cwd != "" && (!found || a.State.Cwd != cwd) {
		found = false
		for i := len(a.sessions) - 1; i >= 0; i-- {
			if a.sessions[i].State.Cwd == cwd {
				a.session, found = a.sessions[i], true
				break
			}
		}
	}
	if !found || (cwd != "" && a.State.Cwd != cwd) {
		if cwd != "" {
			a.newSession(cwd)
		} else {
			a.session = a.sessions[len(a.sessions)-1]
		}
	}
	a.session.open = true
	return nil
}

func (a *App) snapshot() storage.Snapshot {
	result := storage.Snapshot{
		Selected: a.session.id, SidebarOpen: a.sidebarOpen, SidebarWidth: a.sidebarWidth,
		HistoryOpen: a.historyOpen, CollapsedProjects: maps.Clone(a.collapsedProjects),
	}
	for _, s := range a.sessions {
		item := s.saved
		item.ID, item.ThreadID, item.Cwd = s.id, s.State.ThreadID, s.State.Cwd
		item.Draft, item.Title = s.State.Draft, s.title()
		item.DraftSkills = make(map[string]string)
		for _, skill := range s.draftSkills(s.State.Draft) {
			item.DraftSkills[skill.Name] = skill.Path
		}
		if s.loaded && !s.recovering {
			if !s.recoveryBlocked {
				item.TurnID = s.State.TurnID
			}
			if !s.writerOccupied {
				item.Active = s.State.Busy || s.recoveryBlocked
			}
			item.Expanded = maps.Clone(s.saved.Expanded)
			if item.Expanded == nil {
				item.Expanded = make(map[string]bool)
			}
			for key, open := range s.commandGroups {
				item.Expanded[key] = *open
			}
			for _, m := range s.State.Messages {
				if m.Tool != nil {
					item.Expanded[m.ID] = m.Tool.Expanded
				}
			}
			if first, last := s.list.Visible(); last >= first && !s.restoreOffset {
				item.AtEnd, item.ScrollY = s.list.AtEnd(), s.scroll.Y
				messages := s.displayMessages()
				if first < len(messages) {
					item.Anchor = messages[first].ID
					if handle := s.rows[item.Anchor]; handle != nil && s.readingContext != nil {
						list, row := s.list.Handle.Bounds(s.readingContext), handle.Bounds(s.readingContext)
						if row.H > 0 {
							item.AnchorOffset = list.Y - row.Y
						}
					}
				}
			}
		}
		result.Sessions = append(result.Sessions, item)
	}
	return result
}

func (s *session) displayMessages() []*app.Message {
	var messages []*app.Message
	for _, row := range s.conversationRows() {
		messages = append(messages, row.message)
	}
	return messages
}

func (a *App) persist() error {
	a = a.application()
	if a.store == nil {
		return nil
	}
	snapshot := a.snapshot()
	raw, err := json.Marshal(snapshot)
	if err == nil && bytes.Equal(raw, a.lastSaved) {
		return nil
	}
	if err == nil {
		err = a.store.Save(snapshot)
	}
	if err != nil {
		a.storageError = fmt.Sprintf("保存会话失败，退出前请重试：%v", err)
		return err
	}
	a.storageError, a.lastSaved = "", raw
	for i, s := range a.sessions {
		s.saved = snapshot.Sessions[i]
	}
	return nil
}
