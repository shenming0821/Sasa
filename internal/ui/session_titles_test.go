package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
)

type titleCall struct {
	ctx           context.Context
	model, prompt string
	reply         chan string
}

type titleTestBackend struct {
	stubBackend
	calls chan titleCall
}

func (b *titleTestBackend) GenerateSessionTitle(ctx context.Context, model, prompt string) (string, error) {
	call := titleCall{ctx, model, prompt, make(chan string, 1)}
	b.calls <- call
	select {
	case text := <-call.reply:
		if text == "ERROR" {
			return "", errors.New("fixture failure")
		}
		return text, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (b *titleTestBackend) ListModels(context.Context) ([]agent.Model, error) {
	return []agent.Model{{Model: "cheap", DisplayName: "provider/fast"}, {Model: "hidden", Hidden: true}}, nil
}

func titleTestApp(t *testing.T) (*App, *titleTestBackend, chan func()) {
	t.Helper()
	a := New(stubBackend{}, "test", "/workspace", nil)
	b := &titleTestBackend{calls: make(chan titleCall, 8)}
	a.backends = map[backendKey]Backend{{kind: agent.Claude, runtime: "/fixture/claude"}: b}
	a.preferences.AuxiliaryRuntime, a.preferences.AuxiliaryModel = "/fixture/claude", "cheap"
	a.preferences.AutoSessionTitle = true
	a.State.ThreadID = "title-thread"
	a.State.Messages = []*app.Message{
		{ID: "u1", Role: "user", Text: "修复会话切换时的草稿恢复", Done: true},
		{ID: "a1", Role: "assistant", Text: "已修复草稿恢复并完成验证", Done: true},
	}
	queue := make(chan func(), 32)
	a.dispatch = func(fn func()) { queue <- fn }
	t.Cleanup(func() {
		a.closeAuxiliary()
		close(a.shutdown)
	})
	return a, b, queue
}

func receiveTitleCall(t *testing.T, b *titleTestBackend) titleCall {
	t.Helper()
	select {
	case call := <-b.calls:
		return call
	case <-time.After(3 * time.Second):
		t.Fatal("title call did not start")
	}
	return titleCall{}
}

func finishTitleCallback(t *testing.T, queue chan func()) {
	t.Helper()
	select {
	case fn := <-queue:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("title callback did not arrive")
	}
}

func TestAutomaticTitleScheduleAndContentBudget(t *testing.T) {
	a, b, queue := titleTestApp(t)
	now := time.Now().Add(-time.Hour)
	a.titleTurnCompleted(now)
	a.maintainTitles(now.Add(59 * time.Second))
	if a.titleJob != nil {
		t.Fatal("first title started too early")
	}
	a.maintainTitles(now.Add(time.Minute))
	call := receiveTitleCall(t, b)
	if call.model != "cheap" || !strings.Contains(call.prompt, "草稿恢复") {
		t.Fatal(call)
	}
	call.reply <- "会话草稿恢复"
	finishTitleCallback(t, queue)
	if a.title() != "会话草稿恢复" || a.saved.CustomTitle != "" || a.State.Draft != "" {
		t.Fatal(a.saved)
	}
	last := a.saved.TitleCheckedAt
	for i := 0; i < 4; i++ {
		a.titleTurnCompleted(last.Add(time.Duration(i+1) * time.Minute))
	}
	if _, ok := a.titleDue(a.session); ok {
		t.Fatal("four turns met five-turn threshold")
	}
	a.titleTurnCompleted(last.Add(6 * time.Minute))
	due, ok := a.titleDue(a.session)
	if !ok || !due.Equal(last.Add(titleCooldown)) {
		t.Fatal("cooldown ignored", due)
	}
	a.maintainTitles(last.Add(titleCooldown - time.Second))
	if a.titleJob != nil {
		t.Fatal("cooldown bypassed")
	}
	a.maintainTitles(last.Add(titleCooldown))
	call = receiveTitleCall(t, b)
	call.reply <- "会话草稿恢复"
	finishTitleCallback(t, queue)
	if a.saved.TitleCheckedTurns != 6 || a.titlePending {
		t.Fatal("unchanged title did not consume content budget")
	}
	a.maintainTitles(last.Add(24 * time.Hour))
	if a.titleJob != nil {
		t.Fatal("idle unchanged conversation was checked again")
	}
}

func TestAutomaticTitleInputResetsIdleAndOtherSessionDoesNotBlock(t *testing.T) {
	a, b, queue := titleTestApp(t)
	target := a.session
	now := time.Now()
	a.titleTurnCompleted(now)
	a.titleInputActivity(now.Add(50 * time.Second))
	due, ok := a.titleDue(target)
	if !ok || !due.Equal(now.Add(110*time.Second)) {
		t.Fatal("typing did not reset idle", due)
	}
	a.newSession("/other")
	a.State.Busy, a.State.Phase = true, "正在执行"
	a.maintainTitles(now.Add(110 * time.Second))
	call := receiveTitleCall(t, b)
	call.reply <- "独立会话标题"
	finishTitleCallback(t, queue)
	if target.title() != "独立会话标题" || a.title() == "独立会话标题" {
		t.Fatal("result crossed sessions")
	}
}

func TestTitleCancellationAndLateResults(t *testing.T) {
	for _, scenario := range []string{"disabled", "off-on", "rename", "deleted", "new-turn", "model", "thread"} {
		t.Run(scenario, func(t *testing.T) {
			a, b, queue := titleTestApp(t)
			original := a.title()
			a.titleTurnCompleted(time.Now().Add(-2 * time.Minute))
			a.maintainTitles(time.Now())
			call := receiveTitleCall(t, b)
			switch scenario {
			case "disabled", "off-on":
				previous := a.preferences
				a.preferences.AutoSessionTitle = false
				a.savePreferences(previous)
				if scenario == "off-on" {
					previous = a.preferences
					a.preferences.AutoSessionTitle = true
					a.savePreferences(previous)
				}
			case "rename":
				if err := a.saveSessionName(a.session, "手动命名"); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				a.invalidateSessionTitle(a.session)
				a.sessions = nil
			case "new-turn":
				a.titleConversationChanged(time.Now())
				a.State.Busy = true
			case "model":
				previous := a.preferences
				a.preferences.AuxiliaryModel = "other"
				a.savePreferences(previous)
			case "thread":
				a.State.ThreadID = "other-thread"
			}
			call.reply <- "迟到标题"
			finishTitleCallback(t, queue)
			if a.saved.AutoTitle != "" || a.title() == "迟到标题" {
				t.Fatal("stale result applied")
			}
			if scenario == "rename" && a.title() != "手动命名" || scenario == "disabled" && a.title() != original {
				t.Fatal("original title lost")
			}
		})
	}
}

func TestManualTitleWorksWithSwitchOffAndRestoresAutomation(t *testing.T) {
	a, b, queue := titleTestApp(t)
	store := imageStore(t, a)
	a.preferences.AutoSessionTitle = false
	a.saved.CustomTitle = "用户标题"
	a.State.Draft = "不要改变草稿"
	before := len(a.State.Messages)
	a.requestSessionTitle(a.session)
	call := receiveTitleCall(t, b)
	a.requestSessionTitle(a.session)
	if len(a.titleQueue) != 0 {
		t.Fatal("manual request duplicated")
	}
	call.reply <- "草稿恢复修复"
	finishTitleCallback(t, queue)
	if a.title() != "草稿恢复修复" || a.saved.CustomTitle != "" || a.State.Draft != "不要改变草稿" ||
		len(a.State.Messages) != before || a.saved.Unread {
		t.Fatal("manual generation changed chat", a.saved)
	}
	restored := New(stubBackend{}, "test", "/workspace", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.title() != "草稿恢复修复" || restored.preferences.AutoSessionTitle || restored.preferences.AuxiliaryModel != "cheap" ||
		restored.saved.TitleCheckedAt.IsZero() {
		t.Fatal("title/settings did not survive restart", restored.saved)
	}
}

func TestManualTitleTakesPriorityAndGlobalConcurrencyIsOne(t *testing.T) {
	a, b, queue := titleTestApp(t)
	first := a.session
	a.titleTurnCompleted(time.Now().Add(-2 * time.Minute))
	a.maintainTitles(time.Now())
	automatic := receiveTitleCall(t, b)
	a.newSession("/other")
	a.State.ThreadID = "manual-thread"
	a.State.Messages = []*app.Message{{Role: "user", Text: "手动任务", Done: true}}
	a.requestSessionTitle(a.session)
	select {
	case <-automatic.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("manual request did not cancel background title")
	}
	select {
	case <-b.calls:
		t.Fatal("started second generation before first ended")
	default:
	}
	finishTitleCallback(t, queue)
	manual := receiveTitleCall(t, b)
	manual.reply <- "手动优先"
	finishTitleCallback(t, queue)
	if a.title() != "手动优先" || first.saved.AutoTitle != "" {
		t.Fatal("priority or ownership failure")
	}
}

func TestTitleFailureRetainsOriginalAndDoesNotLoop(t *testing.T) {
	a, b, queue := titleTestApp(t)
	original := a.title()
	now := time.Now().Add(-2 * time.Minute)
	a.titleTurnCompleted(now)
	a.maintainTitles(time.Now())
	receiveTitleCall(t, b).reply <- "ERROR"
	finishTitleCallback(t, queue)
	if a.title() != original || a.auxiliaryError == "" || a.auxiliaryNotice {
		t.Fatal("failure changed title or interrupted user")
	}
	for i := 0; i < 10; i++ {
		a.maintainTitles(time.Now().Add(time.Duration(i) * time.Hour))
	}
	if a.titleJob != nil {
		t.Fatal("failure retried without new content")
	}
}

func TestTitleSwitchAndModelSelectionAreIndependent(t *testing.T) {
	a, b, queue := titleTestApp(t)
	store := imageStore(t, a)
	a.preferences.AutoSessionTitle = false
	a.preferences.AuxiliaryRuntime, a.preferences.AuxiliaryModel = "", ""
	a.selectedModel = "main-model"
	a.openSettings()
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.Click("切换自动生成会话标题"); err != nil {
		t.Fatal(err)
	}
	if a.preferences.AutoSessionTitle {
		t.Fatal("switch toggled while no model selected")
	}
	if err := tt.Click("选择辅助模型"); err != nil {
		t.Fatal(err)
	}
	finishTitleCallback(t, queue)
	tt.Frame()
	if err := tt.Click("辅助模型 provider/fast"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("切换自动生成会话标题"); err != nil {
		t.Fatal(err)
	}
	if !a.preferences.AutoSessionTitle || a.preferences.AuxiliaryModel != "cheap" || a.preferences.AuxiliaryModelName != "provider/fast" ||
		a.selectedModel != "main-model" || len(a.lastModels) != 0 {
		t.Fatal("auxiliary model leaked into chat", a.preferences)
	}
	restored := New(stubBackend{}, "test", "/workspace", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if !restored.preferences.AutoSessionTitle || restored.preferences.AuxiliaryModelName != "provider/fast" {
		t.Fatal("preference not persisted")
	}
	select {
	case <-b.calls:
		t.Fatal("enabling switch processed existing history")
	default:
	}
	if path := os.Getenv("SASA_AUXILIARY_SCREENSHOT"); path != "" {
		tt.Advance(300 * time.Millisecond)
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTitlePromptFiltersAndBoundsContent(t *testing.T) {
	messages := []*app.Message{
		{Role: "user", Text: "原始需求", Done: true},
		{Role: "assistant", Text: "正文\n```go\nCODE_SECRET\n```\n后续说明", Done: true},
		{Role: "tool", Text: "TOOL_SECRET", Done: true},
		{Role: "user", Text: "UNSENT_SECRET", Done: true, Delivery: "unsent"},
		{Role: "assistant", Text: "STREAM_SECRET"},
	}
	prompt := titlePrompt("标题", messages)
	for _, secret := range []string{"CODE_SECRET", "TOOL_SECRET", "UNSENT_SECRET", "STREAM_SECRET"} {
		if strings.Contains(prompt, secret) {
			t.Fatal("excluded content leaked", secret)
		}
	}
	if !strings.Contains(prompt, "原始需求") || !strings.Contains(prompt, "后续说明") {
		t.Fatal(prompt)
	}
	for i := 0; i < 100; i++ {
		messages = append(messages, &app.Message{Role: "assistant", Text: strings.Repeat("中", 10000), Done: true})
	}
	var payload struct {
		Title    string `json:"currentTitle"`
		Messages []struct {
			Text string `json:"text"`
		} `json:"messages"`
	}
	prompt = titlePrompt(strings.Repeat("长", 10000), messages)
	if err := json.Unmarshal([]byte(prompt), &payload); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, m := range payload.Messages {
		total += len([]rune(m.Text))
	}
	if total > titleInputRunes || len([]rune(payload.Title)) > 80 || len(prompt) > 48<<10 {
		t.Fatal("input unbounded", total, len(prompt))
	}
	for _, invalid := range []string{"", "标题\n第二行", strings.Repeat("中", 29), "# 标题", "```标题```"} {
		if _, err := validSessionTitle(invalid); err == nil {
			t.Fatal("invalid title accepted", invalid)
		}
	}
}

func TestTitleCompletionCountsOnlyFreshSuccessForBothAgents(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			a, _, _ := titleTestApp(t)
			a.kind = kind
			if kind == agent.Claude {
				a.runtimeKey = "/fixture/claude"
			}
			for i, status := range []string{"completed", "failed", "interrupted", "completed"} {
				id := fmt.Sprintf("turn-%d", i)
				a.State.Busy, a.State.TurnID = true, id
				a.State.Messages = append(a.State.Messages,
					&app.Message{ID: id + "-user", Role: "user", TurnID: id, Text: "新问题", Done: true},
					&app.Message{ID: id + "-reply", Role: "assistant", TurnID: id, Text: "已完成", Done: true},
				)
				raw, _ := json.Marshal(map[string]any{"threadId": a.State.ThreadID, "turn": map[string]any{"id": id, "status": status}})
				event := agent.Event{Method: "turn/completed", Params: raw}
				event.Session.Agent, event.Session.Runtime, event.Session.SessionID = kind, a.runtimeKey, a.State.ThreadID
				a.applySessionBatch([]agent.Event{event, event})
			}
			if a.saved.TitleCompletedTurns != 2 {
				t.Fatal("failed/interrupted/duplicate terminal counted", a.saved.TitleCompletedTurns)
			}
		})
	}
}

type titleHistoryBackend struct{ *titleTestBackend }

func (b titleHistoryBackend) ListTurns(context.Context, string, *string) (agent.TurnsPage, error) {
	return agent.TurnsPage{Data: []agent.HistoryTurn{{ID: "old-turn", Status: "completed", Items: []json.RawMessage{
		json.RawMessage(`{"id":"old-user","type":"userMessage","content":[{"type":"text","text":"修复旧会话的标题显示"}]}`),
		json.RawMessage(`{"id":"old-reply","type":"agentMessage","text":"已修复历史标题显示并验证重启恢复"}`),
	}}}}, nil
}
func (b titleHistoryBackend) ListThreads(context.Context, *string) (agent.ThreadsPage, error) {
	return agent.ThreadsPage{}, nil
}
func (b titleHistoryBackend) ResumeThread(context.Context, string) (agent.ResumedThread, error) {
	return agent.ResumedThread{}, errors.New("title generation must not resume chat")
}

func TestManualTitleForUnopenedHistoryAndSearch(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			a, b, queue := titleTestApp(t)
			store := imageStore(t, a)
			historyBackend := titleHistoryBackend{b}
			runtime := ""
			if kind == agent.Claude {
				runtime = "/fixture/claude"
				a.backends[backendKey{kind: kind, runtime: runtime}] = historyBackend
			} else {
				a.backend = historyBackend
			}
			original := a.session
			thread := agent.ThreadMeta{Agent: kind, Runtime: runtime, ID: "history-only", Cwd: "/workspace", Preview: "原历史标题"}
			a.threadList.threads = []agent.ThreadMeta{thread}
			target := a.historySession(thread)
			a.requestSessionTitle(target)
			call := receiveTitleCall(t, b)
			if !strings.Contains(call.prompt, "历史标题显示") {
				t.Fatal("did not read native prose", call.prompt)
			}
			call.reply <- "历史标题恢复"
			finishTitleCallback(t, queue)
			if a.session != original || target.open || target.loaded || target.title() != "历史标题恢复" || len(a.sessions) != 2 {
				t.Fatal("manual history generation changed selection")
			}
			a.dispatch = nil // Rendering cannot launch unrelated discovery in this fixture.
			a.historyOpen, a.rightTab, a.historyQuery = true, "history", "历史标题恢复"
			tt := native.NewTester(a.view, 1400, 800)
			if !tt.HasText("历史标题恢复") {
				t.Fatal("history display/search ignored automatic title")
			}
			restored := New(stubBackend{}, "test", "/workspace", nil)
			if err := restored.AttachStore(store); err != nil {
				t.Fatal(err)
			}
			if restored.historySession(thread).title() != "历史标题恢复" {
				t.Fatal("unopened title lost after restore")
			}
			other := thread
			other.Runtime = "different"
			if a.historySession(other).saved.AutoTitle != "" {
				t.Fatal("automatic title crossed runtime identity")
			}
		})
	}
}

func TestTitleDisableDropsPendingAndDoesNotBackfillOnEnable(t *testing.T) {
	a, b, _ := titleTestApp(t)
	now := time.Now()
	a.titleTurnCompleted(now)
	previous := a.preferences
	a.preferences.AutoSessionTitle = false
	a.savePreferences(previous)
	a.maintainTitles(now.Add(time.Hour))
	previous = a.preferences
	a.preferences.AutoSessionTitle = true
	a.savePreferences(previous)
	a.maintainTitles(now.Add(2 * time.Hour))
	select {
	case <-b.calls:
		t.Fatal("switch backfilled old content")
	default:
	}
	a.titleTurnCompleted(now.Add(3 * time.Hour))
	if _, ok := a.titleDue(a.session); !ok {
		t.Fatal("future completed turn did not re-enable scheduling")
	}
	if err := a.saveSessionName(a.session, "固定标题"); err != nil {
		t.Fatal(err)
	}
	a.titleTurnCompleted(now.Add(4 * time.Hour))
	if _, ok := a.titleDue(a.session); ok {
		t.Fatal("manual title not protected")
	}
}

func TestTitleSaveFailureAndShutdownPreserveOriginal(t *testing.T) {
	for _, scenario := range []string{"save-failure", "shutdown"} {
		t.Run(scenario, func(t *testing.T) {
			a, b, queue := titleTestApp(t)
			store := imageStore(t, a)
			original := a.title()
			a.requestSessionTitle(a.session)
			call := receiveTitleCall(t, b)
			if scenario == "shutdown" {
				a.closeAuxiliary()
			} else {
				store.Close()
				call.reply <- "不可保存标题"
			}
			finishTitleCallback(t, queue)
			if a.title() != original || a.saved.AutoTitle != "" {
				t.Fatal("failed or cancelled task overwrote original")
			}
			if scenario == "save-failure" && a.auxiliaryError == "" {
				t.Fatal("save error hidden")
			}
		})
	}
}

func TestAutomaticTitleStillRunsAfterClosedRuntimeRelease(t *testing.T) {
	a, b, queue := titleTestApp(t)
	a.backend = titleHistoryBackend{b}
	now := time.Now().Add(-2 * time.Minute)
	a.titleTurnCompleted(now)
	a.loaded, a.open, a.recoveryBlocked = false, false, true
	a.State.Messages = nil
	a.maintainTitles(time.Now())
	call := receiveTitleCall(t, b)
	if !strings.Contains(call.prompt, "历史标题显示") {
		t.Fatal("released session did not read native history")
	}
	call.reply <- "关闭会话标题"
	finishTitleCallback(t, queue)
	if a.title() != "关闭会话标题" || a.open || a.loaded {
		t.Fatal("title task reopened released session")
	}
}

func TestTitleExchangeRequiresCompleteQuestionAndReply(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	a.State.TurnID = "current"
	for _, test := range []struct {
		question, reply string
		done, want      bool
	}{
		{"修复草稿", "已完成", true, true},
		{"修复草稿", "", true, false},
		{"修复草稿", "正在处理", false, false},
		{"/model haiku", "Current model", true, false},
		{"/compact", "Summary", true, false},
		{"/frontend-design 创建页面", "已完成页面", true, true},
	} {
		a.State.Messages = []*app.Message{
			{Role: "user", TurnID: "current", Text: test.question, Done: true},
			{Role: "assistant", TurnID: "current", Text: test.reply, Done: test.done},
		}
		if got := a.hasTitleExchange(); got != test.want {
			t.Fatalf("question=%s reply=%s got=%v", test.question, test.reply, got)
		}
	}
}
