package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/storage"
)

const (
	titleFirstIdle  = time.Minute
	titleUpdateIdle = 2 * time.Minute
	titleCooldown   = 30 * time.Minute
	titleNewTurns   = 5
	titleInputRunes = 6000
)

type titleGenerator interface {
	GenerateSessionTitle(context.Context, string, string) (string, error)
}

type titleJob struct {
	target  *session
	manual  bool
	discard bool
	cancel  context.CancelFunc
}

type auxiliaryState struct {
	auxiliaryPicker         string
	auxiliaryModels         []agent.Model
	auxiliaryLoading        bool
	auxiliaryCatalogRuntime string
	auxiliaryCatalogEpoch   uint64
	auxiliaryCatalogCancel  context.CancelFunc
	auxiliaryClosed         bool
	auxiliaryError          string
	auxiliaryNotice         bool
	auxiliaryWorkers        sync.WaitGroup
	titleQueue              []*session
	titleJob                *titleJob
}

func (a *App) auxiliaryBackend() Backend {
	return a.application().backends[backendKey{kind: agent.Claude, runtime: a.application().preferences.AuxiliaryRuntime}]
}

func (a *App) titleInputActivity(now time.Time) {
	a.titleActivity = now
	a.scheduleSave()
}

func (a *App) titleConversationChanged(now time.Time) {
	a.titleInputActivity(now)
	a.titleRevision++
	root := a.application()
	if root.titleJob != nil && root.titleJob.target == a.session {
		root.titleJob.discard = true
		root.titleJob.cancel()
	}
}

func (a *App) titleTurnCompleted(now time.Time) {
	a.titleConversationChanged(now)
	root := a.application()
	if !root.preferences.AutoSessionTitle || a.saved.CustomTitle != "" {
		return
	}
	a.saved.TitleCompletedTurns++
	a.titlePending = true
}

func (a *App) hasTitleExchange() bool {
	user, reply := false, false
	for i := len(a.State.Messages) - 1; i >= 0; i-- {
		m := a.State.Messages[i]
		if m.TurnID != a.State.TurnID || !m.Done {
			continue
		}
		if m.Role == "user" && m.Delivery != "unsent" && m.Delivery != "pending" {
			command := strings.Fields(m.Text)
			if len(command) > 0 && slices.Contains([]string{"/model", "/compact", "/clear", "/help", "/status", "/permissions"}, command[0]) {
				return false
			}
			user = true
		}
		reply = reply || m.Role == "assistant" && m.Tool == nil && strings.TrimSpace(m.Text) != ""
		if user && reply {
			return true
		}
	}
	return false
}

func (a *App) invalidateSessionTitle(s *session) {
	s.titleRevision++
	s.titlePending = false
	a.titleQueue = slices.DeleteFunc(a.titleQueue, func(item *session) bool { return item == s })
	if a.titleJob != nil && a.titleJob.target == s {
		a.titleJob.discard = true
		a.titleJob.cancel()
	}
}

func (a *App) auxiliaryPreferencesChanged(previous storage.Preferences) {
	configChanged := previous.AuxiliaryRuntime != a.preferences.AuxiliaryRuntime || previous.AuxiliaryModel != a.preferences.AuxiliaryModel
	if previous.AutoSessionTitle != a.preferences.AutoSessionTitle || configChanged {
		for _, s := range a.sessions {
			s.titlePending = false
		}
		if a.titleJob != nil && (configChanged || !a.titleJob.manual) {
			a.titleJob.discard = true
			a.titleJob.cancel()
		}
		if configChanged {
			a.titleQueue = nil
		}
	}
}

func (a *App) titleDue(s *session) (time.Time, bool) {
	if !a.preferences.AutoSessionTitle || !s.titlePending || s.saved.CustomTitle != "" ||
		s.State.Busy || s.State.Disconnected || s.recovering || s.loaded && s.recoveryBlocked || s.releasing ||
		s.saved.Active || s.State.Incomplete || s.State.NeedsSnapshot || s.writerOccupied || s.reviewRequired ||
		s.submitCancel != nil || s.followupSending || len(s.pendingInputs) > 0 || s.edit != nil {
		return time.Time{}, false
	}
	if s.saved.TitleCheckedAt.IsZero() {
		return s.titleActivity.Add(titleFirstIdle), true
	}
	if s.saved.TitleCompletedTurns < s.saved.TitleCheckedTurns+titleNewTurns {
		return time.Time{}, false
	}
	return later(s.titleActivity.Add(titleUpdateIdle), s.saved.TitleCheckedAt.Add(titleCooldown)), true
}

func later(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}

func (a *App) nextTitleDue() (time.Time, bool) {
	if a.titleJob != nil || a.preferences.AuxiliaryModel == "" || a.auxiliaryBackend() == nil || a.quitting || a.quitStopping {
		return time.Time{}, false
	}
	if a.titleSendPreparing() {
		return time.Time{}, false
	}
	var due time.Time
	for _, s := range a.sessions {
		if candidate, ok := a.titleDue(s); ok && (due.IsZero() || candidate.Before(due)) {
			due = candidate
		}
	}
	return due, !due.IsZero()
}

func (a *App) maintainTitles(now time.Time) {
	if a.auxiliaryClosed || a.quitting || a.quitStopping || a.titleJob != nil || !a.canAsync() {
		return
	}
	// User-initiated sends already in preparation take precedence. Other active
	// conversations do not prevent an idle conversation from getting a title.
	if a.titleSendPreparing() {
		return
	}
	for len(a.titleQueue) > 0 {
		s := a.titleQueue[0]
		a.titleQueue = a.titleQueue[1:]
		if slices.Contains(a.sessions, s) && !s.State.Busy && !s.recovering && !s.releasing {
			a.startSessionTitle(s, true, now)
			return
		}
	}
	if a.preferences.AuxiliaryModel == "" || a.auxiliaryBackend() == nil {
		return
	}
	for _, s := range a.sessions {
		if due, ok := a.titleDue(s); ok && !now.Before(due) {
			a.startSessionTitle(s, false, now)
			return
		}
	}
}

func (a *App) titleSendPreparing() bool {
	for _, s := range a.sessions {
		if s.submitCancel != nil || s.followupSending || s.State.Phase == "正在发送" {
			return true
		}
	}
	return false
}

func (a *App) requestSessionTitle(s *session) {
	a = a.application()
	if a.preferences.AuxiliaryModel == "" || a.auxiliaryBackend() == nil {
		a.openSettings()
		a.settingsSection = "通用"
		a.settingsError = "请先选择 Claude Code 配置和辅助模型。"
		return
	}
	if s == nil || s.State.Busy || s.recovering || s.releasing || s.State.ThreadID == "" || a.threadDeleted(s.kind, s.runtimeKey, s.State.ThreadID) {
		return
	}
	if s.id == 0 {
		for _, existing := range a.sessions {
			if existing.kind.Normalized() == s.kind.Normalized() && existing.runtimeKey == s.runtimeKey && existing.State.ThreadID == s.State.ThreadID {
				s = existing
				break
			}
		}
		if s.id == 0 {
			// Reuse the existing history import and persistence path. The
			// original title remains protected until generation succeeds.
			if err := a.saveSessionName(s, s.title()); err != nil {
				a.auxiliaryError = err.Error()
				return
			}
		}
	}
	if a.titleJob != nil && a.titleJob.target == s && a.titleJob.manual || slices.Contains(a.titleQueue, s) {
		return
	}
	if a.titleJob != nil && !a.titleJob.manual {
		a.titleJob.discard = true
		a.titleJob.cancel()
	}
	a.titleQueue = append(a.titleQueue, s)
	a.maintainTitles(time.Now())
	a.scheduleSave()
}

func (a *App) startSessionTitle(s *session, manual bool, now time.Time) {
	generator, ok := a.auxiliaryBackend().(titleGenerator)
	if !ok {
		a.auxiliaryError = "当前 Claude Code 连接不支持辅助模型调用。"
		a.auxiliaryNotice = manual
		s.titlePending = false
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	job := &titleJob{target: s, manual: manual, cancel: cancel}
	a.titleJob = job
	a.auxiliaryError = ""
	a.auxiliaryNotice = false
	s.titlePending = false
	revision, thread, runtime, kind := s.titleRevision, s.State.ThreadID, s.runtimeKey, s.kind
	original := s.title()
	prompt := titlePrompt(original, s.State.Messages)
	loaded := s.loaded
	history, _ := a.forSession(s).sessionBackend().(historyBackend)
	config := a.preferences
	if !manual {
		// A failed attempt also consumes this content's budget. Retry only after
		// new turns meet the same cooldown, never in the maintenance loop.
		s.saved.TitleCheckedAt = now
		s.saved.TitleCheckedTurns = s.saved.TitleCompletedTurns
		a.persist()
	}
	a.auxiliaryWorkers.Add(1)
	go func() {
		defer a.auxiliaryWorkers.Done()
		defer cancel()
		var text string
		var err error
		if !loaded {
			if history == nil {
				err = fmt.Errorf("当前连接不支持读取会话正文")
			} else {
				var page agent.TurnsPage
				page, err = history.ListTurns(ctx, thread, nil)
				if err == nil {
					slices.Reverse(page.Data)
					var state app.State
					var unfinished bool
					state, unfinished, err = app.History("", thread, page.Data)
					if unfinished && err == nil {
						err = fmt.Errorf("会话尚有未结束的任务，请完成后重试")
					}
					if err == nil {
						prompt = titlePrompt(original, state.Messages)
					}
				}
			}
		}
		if err == nil && prompt == "" {
			err = fmt.Errorf("会话暂无可用于生成标题的问答")
		}
		if err == nil {
			text, err = generator.GenerateSessionTitle(ctx, config.AuxiliaryModel, prompt)
		}
		if err == nil {
			text, err = validSessionTitle(text)
		}
		select {
		case <-a.shutdown:
			return
		default:
		}
		cancelled := errors.Is(ctx.Err(), context.Canceled)
		if ctx.Err() != nil && err == nil {
			err = ctx.Err()
		}
		a.update(func() {
			if a.titleJob != job {
				return
			}
			a.titleJob = nil
			current := a.preferences
			valid := !cancelled && !job.discard && !a.auxiliaryClosed && !a.quitting && !a.quitStopping && slices.Contains(a.sessions, s) &&
				s.titleRevision == revision && s.State.ThreadID == thread && s.runtimeKey == runtime && s.kind == kind &&
				!s.State.Busy && current.AuxiliaryRuntime == config.AuxiliaryRuntime && current.AuxiliaryModel == config.AuxiliaryModel &&
				(manual || current.AutoSessionTitle && s.saved.CustomTitle == "")
			if valid && err != nil {
				a.auxiliaryError = "生成会话标题失败：" + err.Error()
				a.auxiliaryNotice = manual
			}
			if valid && err == nil {
				previous := s.saved
				s.saved.AutoTitle = text
				if manual {
					s.saved.CustomTitle = ""
				}
				s.saved.TitleCheckedAt = time.Now()
				s.saved.TitleCheckedTurns = s.saved.TitleCompletedTurns
				if err := a.persist(); err != nil {
					s.saved = previous
					a.auxiliaryError = "保存自动标题失败：" + err.Error()
					a.auxiliaryNotice = manual
				}
			}
			a.maintainTitles(time.Now())
		})
	}()
}

func (a *App) closeAuxiliary() {
	a.auxiliaryClosed = true
	if a.auxiliaryCatalogCancel != nil {
		a.auxiliaryCatalogCancel()
	}
	if a.titleJob != nil {
		a.titleJob.discard = true
		a.titleJob.cancel()
	}
	a.auxiliaryWorkers.Wait()
}

func validSessionTitle(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > 28 || strings.ContainsFunc(text, unicode.IsControl) ||
		strings.HasPrefix(text, "#") || strings.Contains(text, "```") {
		return "", fmt.Errorf("模型返回的标题须为不超过28个字符的单行文字")
	}
	return text, nil
}

// titlePrompt takes a bounded sample of the original question and recent
// completed prose. Tool output, drafts, images and fenced code are excluded.
func titlePrompt(original string, messages []*app.Message) string {
	type entry struct {
		Role string `json:"role"`
		Text string `json:"text"`
	}
	var entries []entry
	var firstQuestion string
	for _, m := range messages {
		if m.Role == "user" && m.Done && m.Delivery != "unsent" && m.Delivery != "pending" {
			firstQuestion = titleProse(m.Text, 600)
			break
		}
	}
	budget := titleInputRunes - len([]rune(firstQuestion))
	for i := len(messages) - 1; i >= max(0, len(messages)-64) && budget > 0; i-- {
		m := messages[i]
		if (m.Role != "user" && m.Role != "assistant") || !m.Done || m.Tool != nil || m.Interaction != nil ||
			m.Delivery == "unsent" || m.Delivery == "pending" {
			continue
		}
		text := titleProse(m.Text, min(budget, 1000))
		if text == "" {
			continue
		}
		budget -= len([]rune(text))
		entries = append(entries, entry{m.Role, text})
		if len(entries) == 12 {
			break
		}
	}
	if len(entries) == 0 {
		return ""
	}
	slices.Reverse(entries)
	if firstQuestion != "" && !slices.ContainsFunc(entries, func(e entry) bool { return e.Role == "user" && strings.HasPrefix(e.Text, firstQuestion) }) {
		entries = append([]entry{{"user", firstQuestion}}, entries...)
	}
	raw, _ := json.Marshal(struct {
		Title    string  `json:"currentTitle"`
		Messages []entry `json:"messages"`
	}{runePrefix(original, 80), entries})
	return string(raw)
}

func runePrefix(text string, limit int) string {
	count := 0
	for index := range text {
		if count == limit {
			return text[:index]
		}
		count++
	}
	return text
}

func titleProse(text string, limit int) string {
	var lines []string
	code := false
	for _, line := range strings.Split(runePrefix(text, 4000), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") || strings.HasPrefix(strings.TrimSpace(line), "~~~") {
			code = !code
			continue
		}
		if !code {
			lines = append(lines, line)
		}
	}
	return runePrefix(strings.Join(strings.Fields(strings.Join(lines, "\n")), " "), limit)
}
