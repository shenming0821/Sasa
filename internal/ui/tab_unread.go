package ui

import "strings"

func (s *session) latestReply() (id, text string) {
	for i := len(s.State.Messages) - 1; i >= 0; i-- {
		message := s.State.Messages[i]
		if message.Role == "assistant" && strings.TrimSpace(message.Text) != "" {
			return message.ID, message.Text
		}
	}
	return "", ""
}

func (a *App) readingSession(s *session) bool {
	root := a.application()
	if root.activeTab != sessionTabID(s.id) || !root.sessionViewActive() ||
		root.settingsOpen || root.viewerOpen || !s.loaded || s.recovering || s.historyLoading {
		return false
	}
	if root.windowFocused != nil {
		return root.windowFocused()
	}
	return root.win == nil || root.win.IsFocused()
}

func (a *App) markSelectedSessionRead() {
	if a.session != nil && a.saved.Unread && a.readingSession(a.session) {
		a.saved.Unread = false
	}
}
