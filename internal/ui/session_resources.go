package ui

import (
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
)

func (s *session) canReleaseRuntime() bool {
	if s.open || !s.loaded || s.releasing || s.editPreparing || s.editSending ||
		s.State.ThreadID == "" || s.State.TurnID == "" || s.State.Busy || s.State.Disconnected ||
		s.recovering || s.recoveryBlocked || s.historyLoading || s.forking ||
		s.forkUncertain || s.reviewRequired || s.writerOccupied || s.imagesImporting ||
		s.submitCancel != nil || s.followupSending || s.snapshotPending || s.State.Incomplete || s.State.NeedsSnapshot ||
		len(s.State.PendingInteractions()) != 0 {
		return false
	}
	for _, message := range s.State.Messages {
		if tool := message.Tool; tool != nil && (tool.Status == "inProgress" || tool.AwaitingFinal) &&
			!(s.stopOnClose && s.State.Phase == "已中断") {
			return false
		}
	}
	return true
}

func (a *App) releaseClosedSessions(now time.Time) {
	if a.quitting || a.quitStopping || !a.canAsync() {
		return
	}
	for _, s := range a.sessions {
		if s.stopOnClose && !s.open && s.loaded && !s.recovering && !s.writerOccupied &&
			s.State.Busy && !s.State.Disconnected && !s.State.Stopping() {
			a.forSession(s).stop()
		}
		if !now.Before(s.releaseAfter) {
			a.releaseClosedSession(s, now)
		}
	}
}

func (a *App) releaseClosedSession(s *session, now time.Time) {
	if !s.canReleaseRuntime() {
		return
	}
	bound := a.forSession(s)
	backend, ok := bound.sessionBackend().(agent.IdleThreadReleaser)
	if !ok {
		return
	}
	// Capture reading position and folded tools before changing loaded state.
	// A storage failure must keep the live owner available.
	if err := a.persist(); err != nil {
		s.releaseAfter = now.Add(time.Minute)
		return
	}
	s.releasing = true
	s.releaseAfter = now.Add(time.Minute)
	threadID, epoch := s.State.ThreadID, s.recoveryEpoch
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		released, err := backend.ReleaseIdleThread(ctx, threadID)
		bound.update(func() {
			s.releasing = false
			if s.State.ThreadID != threadID || s.recoveryEpoch != epoch || s.State.Disconnected {
				return
			}
			if !released {
				return
			}
			// An uncertain response can mean unsubscribe succeeded. Require a
			// fresh resume before sending; retain the existing display and draft.
			s.loaded, s.recoveryBlocked = false, true
			if s.open {
				a.recoverSession(s)
			} else if err == nil {
				s.State.Messages = nil
				s.conversationCache = conversationCache{}
				s.conversationNavigation = conversationNavigation{}
				s.rows, s.commandGroups, s.changeSummaries = nil, nil, nil
				s.streamedMessages = nil
				s.expandedProcesses = nil
				s.processProgress = nil
				s.processScrollHold = false
				s.disclosureMoving = false
				s.readingContext = nil
				s.list, s.scroll = native.ListState{}, native.ScrollState{}
			}
		})
	}()
}
