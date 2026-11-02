package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
)

type historyBackend = agent.HistoryBackend

func (a *App) recoverSession(s *session) {
	a.loadSessionHistory(s, nil)
}

// A fork already belongs to this transport. Read its returned state directly,
// preserving the fork's deferred goal instead of resuming it a second time.
func (a *App) loadSessionHistory(s *session, fork *agent.ResumedThread) {
	backend, ok := a.forSession(s).sessionBackend().(historyBackend)
	if !ok || s.State.Disconnected || s.recovering || s.releasing || s.forking || s.State.ThreadID == "" {
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
	s.historyAtTop = false
	s.historyOverflow = false
	s.State.Phase, s.State.Error = "正在核验原生历史", ""
	s.recoveryEpoch++
	epoch, threadID := s.recoveryEpoch, s.State.ThreadID
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		var resumed agent.ResumedThread
		var err error
		if fork != nil {
			resumed = *fork
		} else {
			resumed, err = backend.ResumeThread(ctx, threadID)
		}
		occupied := agent.IsActiveWriter(err, threadID)
		if occupied {
			if reader, ok := backend.(interface {
				ReadThreadMeta(context.Context, string) (agent.ThreadMeta, error)
			}); ok {
				resumed.ThreadMeta, err = reader.ReadThreadMeta(ctx, threadID)
				resumed.Cwd = resumed.ThreadMeta.Cwd
			}
		}
		var page agent.TurnsPage
		if err == nil {
			page, err = backend.ListTurns(ctx, threadID, nil)
			if err == nil {
				bound.materializeHistoryImages(&page)
			}
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
			state.Agent = s.kind.Normalized()
			state.RestoreTimings(s.saved.TurnTimings)
			state.Draft = s.State.Draft
			state.DraftImages = slices.Clone(s.State.DraftImages)
			if s.saved.Title == "" || s.saved.Title == "新会话" {
				s.saved.Title = resumed.ThreadMeta.Preview
			}
			state.Model, state.Policy, state.Sandbox = resumed.Model, string(resumed.Policy), string(resumed.Sandbox)
			state.RuntimeVersion = resumed.RuntimeVersion
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
			var controls []agent.Event
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
	var question *app.Message
	for _, message := range s.State.Messages {
		if message.Role == "user" {
			question = message
		}
		if message.ID == s.saved.Anchor {
			if question != nil && question.Timing != nil && !question.Timing.Finished.IsZero() && !s.expandedProcesses[question.ID] {
				visible := false
				for _, row := range s.conversationRows() {
					visible = visible || row.message.ID == message.ID
				}
				if !visible {
					s.saved.Anchor, s.saved.AnchorOffset = question.ID, 0
				}
			}
			break
		}
	}
	for i, row := range s.conversationRows() {
		matches := row.message.ID == s.saved.Anchor
		for _, command := range append(append([]*app.Message(nil), row.commands...), row.changes...) {
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

func (a *App) loadHistoryNearTop() {
	first, last := a.list.Visible()
	nearTop := last >= 0 && first <= 1 && (!a.list.AtEnd() || a.scroll.MaxY == 0)
	if nearTop && !a.historyAtTop && !a.restoreOffset {
		a.loadOlder()
	}
	// A failed page retries only after the user leaves and returns to the top.
	a.historyAtTop = nearTop
}

func (a *App) loadOlder() {
	s := a.session
	backend, ok := a.sessionBackend().(historyBackend)
	if !ok || s.historyCursor == nil || s.historyLoading || s.recovering {
		return
	}
	s.historyLoading = true
	cursor, epoch, threadID := s.historyCursor, s.recoveryEpoch, s.State.ThreadID
	bound := a.forSession(s)
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		page, err := backend.ListTurns(ctx, threadID, cursor)
		if err == nil {
			bound.materializeHistoryImages(&page)
		}
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
			older.RestoreTimings(s.saved.TurnTimings)
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
			s.historyAtTop = false
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

type threadSource struct {
	cursor *string
	loaded bool
}

func (a *App) threadSourceKey(thread agent.ThreadMeta) backendKey {
	if thread.Agent == "" {
		return backendKey{kind: a.kind.Normalized(), runtime: a.runtimeKey}
	}
	return backendKey{kind: thread.Agent.Normalized(), runtime: thread.Runtime}
}

func (a *App) loadThreadList() {
	a = a.forSession(a.session)
	key := agent.CacheKey{Agent: a.kind.Normalized(), Runtime: a.runtimeKey, Cwd: a.State.Cwd}
	if a.historyCacheKey != key && !a.threadsLoading {
		a.nativeThreads, a.threadsCursor, a.threadsError = nil, nil, ""
		a.threadSources = nil
		a.historyCacheKey = key
	}
	if a.threadsLoading {
		return
	}
	root := a.application()
	backends := make(map[backendKey]historyBackend)
	if backend, ok := root.backend.(historyBackend); ok {
		backends[backendKey{kind: agent.Codex}] = backend
	}
	for source, backend := range root.backends {
		if history, ok := backend.(historyBackend); ok {
			backends[source] = history
		}
	}
	missingSource := false
	for source := range backends {
		if _, exists := a.threadSources[source]; !exists {
			missingSource = true
		}
	}
	if !missingSource && (len(a.threadSources) > 0 || len(backends) <= 1 && len(a.nativeThreads) > 0) && a.threadsCursor == nil && a.threadsError == "" {
		return
	}
	sources := make([]backendKey, 0, len(backends))
	for source := range backends {
		if state := a.threadSources[source]; !state.loaded || state.cursor != nil {
			sources = append(sources, source)
		}
	}
	if len(sources) == 0 {
		return
	}
	sort.Slice(sources, func(first, second int) bool {
		if sources[first].kind != sources[second].kind {
			return sources[first].kind < sources[second].kind
		}
		return sources[first].runtime < sources[second].runtime
	})
	a.threadsLoading, a.threadsError = true, ""
	states := make(map[backendKey]threadSource, len(sources))
	for _, source := range sources {
		states[source] = a.threadSources[source]
		if len(a.threadSources) == 0 && source == (backendKey{kind: a.kind.Normalized(), runtime: a.runtimeKey}) {
			states[source] = threadSource{cursor: a.threadsCursor}
		}
	}
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		var incoming []agent.ThreadMeta
		var failures []string
		for _, source := range sources {
			page, err := backends[source].ListThreads(ctx, states[source].cursor)
			if err != nil {
				failures = append(failures, source.kind.String()+"："+err.Error())
				continue
			}
			states[source] = threadSource{cursor: page.NextCursor, loaded: true}
			for _, thread := range page.Data {
				thread.Agent, thread.Runtime = source.kind, source.runtime
				incoming = append(incoming, thread)
			}
		}
		a.update(func() {
			a.threadsLoading = false
			if key != (agent.CacheKey{Agent: a.kind.Normalized(), Runtime: a.runtimeKey, Cwd: a.State.Cwd}) {
				return
			}
			if len(failures) > 0 {
				a.threadsError = "读取历史失败：" + strings.Join(failures, "；")
			}
			threads := make([]agent.ThreadMeta, 0, len(a.nativeThreads)+len(incoming))
			positions := make(map[string]int)
			for _, batch := range [][]agent.ThreadMeta{a.nativeThreads, incoming} {
				for _, thread := range batch {
					source := a.threadSourceKey(thread)
					identity := string(source.kind) + "\x00" + source.runtime + "\x00" + thread.ID
					if i, exists := positions[identity]; exists {
						threads[i] = thread
					} else {
						positions[identity] = len(threads)
						threads = append(threads, thread)
					}
				}
			}
			a.nativeThreads = threads
			if a.threadSources == nil {
				a.threadSources = make(map[backendKey]threadSource)
			}
			a.threadsCursor = nil
			for source, state := range states {
				a.threadSources[source] = state
				if state.cursor != nil || !state.loaded {
					cursor := "pending"
					a.threadsCursor = &cursor
					if state.cursor != nil {
						a.threadsCursor = state.cursor
					}
				}
			}
			a.threadsLayoutPending = true
			if a.threadsReload {
				a.threadsReload = false
				a.loadThreadList()
			}
		})
	}()
}

func (a *App) recordClaudeHistory() {
	if a.State.ThreadID == "" {
		return
	}
	key := agent.CacheKey{Agent: agent.Claude, Runtime: a.runtimeKey, Cwd: a.State.Cwd}
	thread := agent.ThreadMeta{Agent: agent.Claude, Runtime: a.runtimeKey, ID: a.State.ThreadID, Cwd: a.State.Cwd, Preview: a.session.title(), Status: agent.ThreadStatus{Type: "notLoaded"}}
	for _, target := range a.application().sessions {
		_, includesClaude := target.threadSources[backendKey{kind: agent.Claude, runtime: a.runtimeKey}]
		if target.historyCacheKey != key && !includesClaude {
			continue
		}
		threads := []agent.ThreadMeta{thread}
		for _, existing := range target.nativeThreads {
			if existing.ID != thread.ID || a.forSession(target).threadSourceKey(existing) != (backendKey{kind: agent.Claude, runtime: a.runtimeKey}) {
				threads = append(threads, existing)
			}
		}
		target.nativeThreads = threads
		target.threadsLayoutPending = true
	}
}

func (a *App) historyBrowser(c *native.Context) {
	w := a.workspace(a.workspaceRoot(a.workspaceCwd()))
	native.Scroll(c).TrackScroll(&w.historyScroll).FillWidth().Grow(1).MinHeight(0).Children(func() {
		native.Column(c).FillWidth().Padding(8).Gap(6).Children(func() {
			count := 0
			for _, thread := range a.nativeThreads {
				sourceKey := a.threadSourceKey(thread)
				if a.threadDeleted(sourceKey.kind, sourceKey.runtime, thread.ID) {
					continue
				}
				if a.historyScope == 0 && filepath.Clean(thread.Cwd) != filepath.Clean(a.workspaceCwd()) {
					continue
				}
				target := a.historySession(thread)
				title := thread.Preview
				if thread.Name != nil && *thread.Name != "" {
					title = *thread.Name
				}
				if target.saved.CustomTitle != "" {
					title = target.saved.CustomTitle
				}
				if title == "" {
					title = thread.Cwd
				}
				if !matchesQuery(a.historyQuery, title, thread.Cwd) {
					continue
				}
				count++
				rowKey := string(sourceKey.kind) + "\x00" + sourceKey.runtime + "\x00" + thread.ID
				b := treeButton(c.Key(rowKey), "打开历史 "+thread.ID).FillWidth().Padding(12).Tooltip(sourceKey.kind.String() + " · " + thread.Cwd)
				b.ContextMenu(a.sessionMenu(target))
				b.Children(func() {
					native.Column(c).FillWidth().Gap(6).Children(func() {
						native.Row(c).FillWidth().Gap(6).AlignItems(native.Center).Children(func() {
							agentIcon(c, sourceKey.kind, 12).Label("历史 Agent " + sourceKey.kind.String())
							native.Text(c, title).FontSize(12).TextColor(c.Theme().Text).SingleLine().Shrink(1).MinWidth(0)
						})
						native.Text(c, thread.Cwd).FontSize(10).TextColor(c.Theme().TextMuted).SingleLine().Shrink(1).MinWidth(0)
					})
				})
				if b.Clicked() {
					for _, s := range a.sessions {
						if s.kind.Normalized() == sourceKey.kind && s.runtimeKey == sourceKey.runtime && s.State.ThreadID == thread.ID {
							a.selectSession(s)
							a.loadThreadList()
							return
						}
					}
					kind, runtime := sourceKey.kind, sourceKey.runtime
					source := a.session
					a.newSessionForAgent(thread.Cwd, kind, runtime)
					s := a.session
					s.kind, s.runtimeKey = kind, runtime
					s.State.Agent = kind
					s.historyCacheKey = agent.CacheKey{Agent: kind.Normalized(), Runtime: runtime, Cwd: thread.Cwd}
					if source.historyCacheKey == s.historyCacheKey {
						s.nativeThreads = slices.Clone(source.nativeThreads)
						s.threadSources = maps.Clone(source.threadSources)
						s.threadsCursor, s.threadsError = source.threadsCursor, source.threadsError
						s.threadsLayoutPending = true
					} else {
						a.loadThreadList()
					}
					s.State.ThreadID, s.loaded, s.recoveryBlocked = thread.ID, false, true
					s.saved.Title = title
					a.recoverSession(s)
				}
			}
			if count == 0 && !a.threadsLoading && a.threadsError == "" && a.threadsCursor == nil {
				native.Text(c, "已加载的历史中没有匹配会话").FontSize(12).TextColor(c.Theme().TextMuted).Padding(12)
			}
			if a.threadsError != "" {
				native.Text(c, a.threadsError).FontSize(11).TextColor(c.Theme().Danger)
				if actionButton(c, "重试").Disabled(a.threadsLoading).Clicked() {
					a.loadThreadList()
				}
			}
		})
	})
	// Measure the appended rows before deciding whether another page is needed.
	if a.threadsLayoutPending {
		a.threadsLayoutPending = false
		c.Invalidate()
		return
	}
	if a.threadsCursor != nil && !a.threadsLoading && a.threadsError == "" {
		if w.historyScroll.Bounds(c).H == 0 {
			c.Invalidate()
		} else if w.historyScroll.MaxY-w.historyScroll.Y <= 80 {
			a.loadThreadList()
		}
	}
}

func (s *session) bufferRecovery(e agent.Event) bool {
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
