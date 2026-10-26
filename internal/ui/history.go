package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/codex"
)

type historyBackend interface {
	ResumeThread(context.Context, string) (codex.ResumedThread, error)
	ListTurns(context.Context, string, *string) (codex.TurnsPage, error)
	ListThreads(context.Context, *string) (codex.ThreadsPage, error)
}

func (a *App) recoverSession(s *session) {
	a.loadSessionHistory(s, nil)
}

// A fork already belongs to this transport. Read its returned state directly,
// preserving the fork's deferred goal instead of resuming it a second time.
func (a *App) loadSessionHistory(s *session, fork *codex.ResumedThread) {
	backend, ok := a.backend.(historyBackend)
	if !ok || s.State.Disconnected || s.recovering || s.forking || s.State.ThreadID == "" {
		return
	}
	if s.loaded && (!s.recoveryBlocked || s.writerOccupied) {
		for _, saved := range a.application().snapshot().Sessions {
			if saved.ID == s.id {
				s.saved = saved
				break
			}
		}
	}
	bound := a.forSession(s)
	s.recovering, s.recoveryBlocked = true, true
	s.historyOverflow = false
	s.State.Phase, s.State.Error = "正在核验原生历史", ""
	s.recoveryEpoch++
	epoch, threadID := s.recoveryEpoch, s.State.ThreadID
	go func() {
		ctx, cancel := codex.RequestContext()
		defer cancel()
		var resumed codex.ResumedThread
		var err error
		if fork != nil {
			resumed = *fork
		} else {
			resumed, err = backend.ResumeThread(ctx, threadID)
		}
		occupied := codex.IsActiveWriter(err, threadID)
		if occupied {
			if reader, ok := backend.(interface {
				ReadThreadMeta(context.Context, string) (codex.ThreadMeta, error)
			}); ok {
				resumed.ThreadMeta, err = reader.ReadThreadMeta(ctx, threadID)
				resumed.Cwd = resumed.ThreadMeta.Cwd
			}
		}
		var page codex.TurnsPage
		if err == nil {
			page, err = backend.ListTurns(ctx, threadID, nil)
		}
		bound.update(func() {
			if s.recoveryEpoch != epoch {
				return
			}
			s.recovering = false
			s.writerOccupied = occupied
			if err != nil {
				if occupied {
					s.State.Error, s.State.Phase = "历史暂时无法读取："+err.Error(), "已在另一个应用中打开"
				} else {
					s.State.Error, s.State.Phase = "恢复失败，可重试核验："+err.Error(), "恢复失败"
				}
				s.recoveryEvents, s.recoveryBytes = nil, 0
				return
			}
			if resumed.ThreadMeta.ID != threadID || resumed.Cwd == "" {
				s.State.Error = "原生恢复的会话标识或目录不完整，请核验记录。"
				return
			}
			slices.Reverse(page.Data)
			state, unfinished, projectErr := app.History(resumed.Cwd, threadID, page.Data)
			if projectErr != nil {
				s.State.Error = "历史投影失败：" + projectErr.Error()
				return
			}
			state.Draft = s.State.Draft
			if s.saved.Title == "" || s.saved.Title == "新会话" {
				s.saved.Title = resumed.ThreadMeta.Preview
			}
			state.Model, state.Policy, state.Sandbox = resumed.Model, string(resumed.Policy), string(resumed.Sandbox)
			s.State, s.loaded, s.historyCursor = state, true, page.NextCursor
			// On a new transport an active/unfinished snapshot is evidence to
			// investigate, not authority to replay a turn or revive an approval.
			s.reviewRequired = s.saved.Active && s.saved.TurnID == ""
			unconfirmed := false
			if s.saved.Active && s.saved.TurnID != "" {
				unconfirmed = true
				for _, turn := range page.Data {
					if turn.ID == s.saved.TurnID {
						unconfirmed = false
					}
				}
			}
			s.recoveryBlocked = unfinished || resumed.ThreadMeta.Status.Type != "idle" ||
				s.historyOverflow || unconfirmed || s.reviewRequired || occupied
			if s.recoveryBlocked {
				s.State.Phase = "旧任务待核验"
				s.State.Notice = "已恢复历史，但旧任务尚无确定终态；无法重新附着旧 stdio 或恢复旧审批。核验前暂停发送。"
			}
			if s.reviewRequired && !unfinished && resumed.ThreadMeta.Status.Type == "idle" {
				s.State.Notice = "上次发送未取得任务 ID，结果不能自动确认。请检查已恢复历史，确认后再继续；不会自动重发。"
			} else {
				s.reviewRequired = false
			}
			for _, m := range s.State.Messages {
				if m.Tool != nil {
					m.Tool.Expanded = s.saved.Expanded[m.ID]
				}
			}
			events := s.recoveryEvents
			s.recoveryEvents, s.recoveryBytes = nil, 0
			if occupied {
				s.State.Phase, s.State.Notice = "已在另一个应用中打开", ""
				s.restoreReading()
				if s.restoreOffset && !s.hasSavedAnchor() && s.historyCursor != nil {
					bound.loadOlder()
				}
				return
			}
			var controls []codex.Event
			for _, e := range events {
				switch e.Method {
				case "item/agentMessage/delta", "item/commandExecution/outputDelta", "item/fileChange/outputDelta":
					// A page may already contain a buffered delta. Keep control
					// events and reconcile authoritative items instead of doubling text.
					s.State.NeedsSnapshot, s.State.Incomplete = true, true
				default:
					controls = append(controls, e)
				}
			}
			bound.applySessionBatch(controls)
			s.restoreReading()
			if s.restoreOffset && !s.hasSavedAnchor() && s.historyCursor != nil {
				bound.loadOlder()
			}
		})
	}()
}

func (s *session) restoreReading() {
	if s.saved.AtEnd || s.saved.Anchor == "" {
		s.list.ScrollToEnd()
		return
	}
	for i, row := range s.conversationRows() {
		matches := row.message.ID == s.saved.Anchor
		for _, command := range row.commands {
			matches = matches || command.ID == s.saved.Anchor
		}
		if matches {
			if row.message.ID != s.saved.Anchor {
				s.saved.Anchor, s.saved.AnchorOffset = row.message.ID, 0
			}
			s.list.ScrollTo(i, native.Start)
			s.restoreOffset = true
			return
		}
	}
	// A saved anchor can be on an older native page. Keep its identity while
	// paging backwards, so recovery never silently substitutes the latest row.
	s.State.Notice += " 正在加载阅读位置所在的更早历史。"
	s.restoreOffset = s.historyCursor != nil
	s.list.ScrollTo(0, native.Start)
}

func (s *session) hasSavedAnchor() bool {
	for _, m := range s.State.Messages {
		if m.ID == s.saved.Anchor {
			return true
		}
	}
	return false
}

func (s *session) restoreRowOffset(c *native.Context) {
	s.readingContext = c
	if !s.restoreOffset {
		return
	}
	handle := s.rows[s.saved.Anchor]
	if handle != nil {
		list, row := s.list.Handle.Bounds(c), handle.Bounds(c)
		if list.H > 0 && row.H > 0 {
			s.scroll.Y += s.saved.AnchorOffset - (list.Y - row.Y)
			s.restoreOffset = false
		}
	}
	c.Invalidate()
}

func (a *App) loadOlder() {
	s := a.session
	backend, ok := a.backend.(historyBackend)
	if !ok || s.historyCursor == nil || s.historyLoading || s.recovering {
		return
	}
	s.historyLoading = true
	cursor, epoch, threadID := s.historyCursor, s.recoveryEpoch, s.State.ThreadID
	bound := a.forSession(s)
	go func() {
		ctx, cancel := codex.RequestContext()
		defer cancel()
		page, err := backend.ListTurns(ctx, threadID, cursor)
		bound.update(func() {
			s.historyLoading = false
			if epoch != s.recoveryEpoch {
				return
			}
			if err != nil {
				s.State.Error = "加载更早历史失败：" + err.Error()
				return
			}
			slices.Reverse(page.Data)
			older, _, err := app.History(s.State.Cwd, threadID, page.Data)
			if err != nil {
				s.State.Error = err.Error()
				return
			}
			seen := make(map[string]bool)
			for _, m := range s.State.Messages {
				seen[m.ID] = true
			}
			var prepend []*app.Message
			for _, m := range older.Messages {
				if !seen[m.ID] {
					if m.Tool != nil {
						m.Tool.Expanded = s.saved.Expanded[m.ID]
					}
					prepend = append(prepend, m)
				}
			}
			displayBytes := 0
			for _, list := range [][]*app.Message{prepend, s.State.Messages} {
				for _, m := range list {
					raw, _ := json.Marshal(m)
					displayBytes += len(raw)
				}
			}
			if len(prepend)+len(s.State.Messages) > app.MaxMessages || displayBytes > app.MaxDisplayBytes {
				s.State.Notice = "当前会话显示已达到 1000 条或 8 MiB 上限；原生记录仍完整保留。"
				s.historyCursor = nil
				s.restoreOffset = false
				return
			}
			s.State.Messages = append(prepend, s.State.Messages...)
			s.historyCursor = page.NextCursor
			if s.saved.Anchor != "" {
				for _, m := range prepend {
					if m.ID == s.saved.Anchor {
						s.restoreReading()
						break
					}
				}
			}
			if s.restoreOffset && !s.hasSavedAnchor() {
				if s.historyCursor != nil {
					bound.loadOlder()
				} else {
					s.restoreOffset = false
					s.State.Notice = "原生历史已加载，但原阅读条目已不存在；已保留可用历史。"
				}
			}
		})
	}()
}

func (a *App) loadThreadList() {
	backend, ok := a.backend.(historyBackend)
	if !ok || a.threadsLoading {
		return
	}
	a.threadsLoading, a.threadsError = true, ""
	cursor := a.threadsCursor
	go func() {
		ctx, cancel := codex.RequestContext()
		defer cancel()
		page, err := backend.ListThreads(ctx, cursor)
		a.update(func() {
			a.threadsLoading = false
			if err != nil {
				a.threadsError = "读取历史失败：" + err.Error()
				return
			}
			a.nativeThreads = append(a.nativeThreads, page.Data...)
			a.threadsCursor = page.NextCursor
		})
	}()
}

func (a *App) historyBrowser(c *native.Context) {
	w := a.workspace(a.workspaceRoot(a.workspaceCwd()))
	native.Scroll(c).TrackScroll(&w.historyScroll).FillWidth().Grow(1).MinHeight(0).Children(func() {
		native.Column(c).FillWidth().Padding(8).Gap(6).Children(func() {
			count := 0
			for _, thread := range a.nativeThreads {
				if a.historyScope == 0 && filepath.Clean(thread.Cwd) != filepath.Clean(a.workspaceCwd()) {
					continue
				}
				title := thread.Preview
				if thread.Name != nil && *thread.Name != "" {
					title = *thread.Name
				}
				if title == "" {
					title = thread.Cwd
				}
				if !matchesQuery(a.historyQuery, title, thread.Cwd) {
					continue
				}
				count++
				b := treeButton(c.Key(thread.ID), "打开历史 "+thread.ID).FillWidth().Padding(12).Tooltip(thread.Cwd)
				b.Children(func() {
					native.Column(c).FillWidth().Gap(6).Children(func() {
						native.Text(c, title).FontSize(12).TextColor(c.Theme().Text).SingleLine().Shrink(1).MinWidth(0)
						native.Text(c, thread.Cwd).FontSize(10).TextColor(c.Theme().TextMuted).SingleLine().Shrink(1).MinWidth(0)
					})
				})
				if b.Clicked() {
					for _, s := range a.sessions {
						if s.State.ThreadID == thread.ID {
							a.selectSession(s)
							return
						}
					}
					a.newSession(thread.Cwd)
					s := a.session
					s.State.ThreadID, s.loaded, s.recoveryBlocked = thread.ID, false, true
					s.saved.Title = title
					a.recoverSession(s)
				}
			}
			if count == 0 && !a.threadsLoading && a.threadsError == "" {
				native.Text(c, "已加载的历史中没有匹配会话").FontSize(12).TextColor(c.Theme().TextMuted).Padding(12)
			}
			if a.threadsError != "" {
				native.Text(c, a.threadsError).FontSize(11).TextColor(c.Theme().Danger)
			}
			if a.threadsCursor != nil || a.threadsError != "" || len(a.nativeThreads) == 0 {
				label := "加载更多会话"
				if a.threadsLoading {
					label = "正在加载…"
				}
				if actionButton(c, label).Disabled(a.threadsLoading).Clicked() {
					a.loadThreadList()
				}
			}
		})
	})
}

func (s *session) bufferRecovery(e codex.Event) bool {
	if !s.recovering || e.Method == "fuyu/disconnected" || e.Method == "fuyu/eventGap" {
		return false
	}
	var p struct {
		ThreadID string `json:"threadId"`
	}
	if json.Unmarshal(e.Params, &p) != nil || p.ThreadID != s.State.ThreadID {
		return false
	}
	if s.recoveryBytes+len(e.Params) > 2*1024*1024 || len(s.recoveryEvents) >= 1024 {
		s.historyOverflow = true
		s.State.Notice = fmt.Sprintf("恢复期间事件超过缓存上限，请重新核验 %s。", s.State.ThreadID)
		return true
	}
	s.recoveryEvents = append(s.recoveryEvents, e)
	s.recoveryBytes += len(e.Params)
	return true
}
