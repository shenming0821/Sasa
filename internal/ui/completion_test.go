package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func completionTestApp() *App {
	a := New(stubBackend{}, "test", "/tmp", nil)
	a.completion.cwd, a.completion.loaded = "/tmp", true
	a.completion.cacheKey, a.completion.checked = a.completionCacheKey(), time.Now()
	a.completion.skills = []codex.Skill{
		{Name: "frontend-design", Path: "/tmp/skills/frontend-design/SKILL.md", Description: "Design interfaces", Enabled: true},
		{Name: "disabled", Path: "/tmp/disabled/SKILL.md", Description: "disabled skill", Enabled: false},
	}
	return a
}

func TestComposerSlashCompletionDoesNotSubmitTask(t *testing.T) {
	a := completionTestApp()
	sent := 0
	a.submit = func() { sent++ }
	tt := native.NewTester(a.view, 640, 480)
	tt.Click("任务输入")
	tt.Type("/")
	if _, ok := tt.Find("候选 /new"); !ok {
		t.Fatal("slash menu missing")
	}
	panel, _ := tt.Find("输入候选")
	editor, _ := tt.Find("任务输入")
	if panel.Y < 0 || panel.Y+panel.H > editor.Y || panel.X < 0 || panel.X+panel.W > 640 {
		t.Fatalf("panel outside/overlapping input: %+v %+v", panel, editor)
	}
	tt.Type("sta")
	if _, ok := tt.Find("候选 /new"); ok {
		t.Fatal("slash filtering ignored")
	}
	tt.Key(0, native.KeyEnter)
	if sent != 0 || a.State.Draft != "" || !a.detailsOpen {
		t.Fatal("slash command submitted as a task")
	}
}

func TestComposerSkillCompletionKeyboardEscapeAndClick(t *testing.T) {
	a := completionTestApp()
	sent := 0
	a.submit = func() { sent++ }
	tt := native.NewTester(a.view, 640, 480)
	tt.Click("任务输入")
	tt.Type("$")
	if _, ok := tt.Find("候选 frontend-design"); !ok {
		t.Fatal("skill menu missing")
	}
	if _, ok := tt.Find("候选 disabled"); ok {
		t.Fatal("disabled skill offered")
	}
	tt.Key(0, native.KeyEscape)
	if _, ok := tt.Find("输入候选"); ok {
		t.Fatal("Escape did not dismiss")
	}
	tt.Type("front")
	if _, ok := tt.Find("候选 frontend-design"); !ok {
		t.Fatal("editing did not reopen menu")
	}
	tt.Key(0, native.KeyTab)
	if sent != 0 || a.State.Draft != "$frontend-design " || len(a.draftSkills(a.State.Draft)) != 1 {
		t.Fatalf("skill not inserted as a reference: %q", a.State.Draft)
	}
	tt.Type("do design")
	if a.State.Draft != "$frontend-design do design" {
		t.Fatalf("typing lost focus/caret: %q", a.State.Draft)
	}
	a.State.Draft = ""
	tt.Frame()
	tt.Type("/sk")
	if err := tt.Click("候选 /skills"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.State.Draft != "$" {
		t.Fatal("skills slash did not open skill selector")
	}
	if err := tt.Click("候选 frontend-design"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("next")
	if a.State.Draft != "$frontend-design next" {
		t.Fatalf("mouse selection lost input focus: %q", a.State.Draft)
	}
}

func TestCompletionQueryUsesCaretAndKeepsOtherText(t *testing.T) {
	for _, text := range []string{"/tmp/file", "hello /status", "cost$front", "$$front"} {
		if _, ok := queryCompletion(text, len([]rune(text)), len([]rune(text))); ok {
			t.Fatalf("unexpected completion %q", text)
		}
	}
	text := "中文 $front suffix"
	caret := len([]rune("中文 $front"))
	q, ok := queryCompletion(text, caret, caret)
	if !ok || q.kind != '$' || q.query != "front" {
		t.Fatal("unicode/caret query failed")
	}
	a := completionTestApp()
	a.State.Draft = text
	a.acceptCompletion(q, completionItem{skill: &a.completion.skills[0]})
	if a.State.Draft != "中文 $frontend-design  suffix" {
		t.Fatal(a.State.Draft)
	}
	if len(a.draftSkills("$frontend-design-other")) != 0 {
		t.Fatal("prefix skill activated")
	}
	if len(a.draftSkills("$frontend-design $frontend-design")) != 1 {
		t.Fatal("duplicate skill references")
	}
}

type skillTestBackend struct {
	stubBackend
	request chan string
	result  chan []codex.SkillsEntry
	sent    chan []codex.Skill
}

func (b *skillTestBackend) ListSkills(ctx context.Context, cwd string) ([]codex.SkillsEntry, error) {
	b.request <- cwd
	select {
	case data := <-b.result:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (b *skillTestBackend) SendWithSkills(_ context.Context, _, _ string, skills []codex.Skill) (codex.Turn, error) {
	b.sent <- skills
	return codex.Turn{ID: "turn", Status: "inProgress"}, nil
}

func TestSkillLoadingAndSubmissionStayWithTheirSession(t *testing.T) {
	b := &skillTestBackend{request: make(chan string, 1), result: make(chan []codex.SkillsEntry, 1), sent: make(chan []codex.Skill, 1)}
	a := New(b, "test", "/one", nil)
	jobs := make(chan func(), 4)
	a.dispatch = func(fn func()) { jobs <- fn }
	first := a.session
	a.loadCompletionSkills()
	select {
	case cwd := <-b.request:
		if cwd != "/one" {
			t.Fatal(cwd)
		}
	case <-time.After(time.Second):
		t.Fatal("no skills request")
	}
	a.newSession("/two")
	b.result <- []codex.SkillsEntry{{Cwd: "/one", Skills: []codex.Skill{{Name: "design", Path: "/one/SKILL.md", Enabled: true}}}}
	select {
	case job := <-jobs:
		job()
	case <-time.After(time.Second):
		t.Fatal("no skills result")
	}
	if len(first.completion.skills) != 1 || len(a.completion.skills) != 0 {
		t.Fatal("skills crossed sessions")
	}
	a.selectSession(first)
	a.State.ThreadID = "native"
	a.State.Draft = "$design task"
	a.completion.chosen = map[string]codex.Skill{"design": first.completion.skills[0]}
	a.send()
	select {
	case skills := <-b.sent:
		if len(skills) != 1 || skills[0].Path != "/one/SKILL.md" {
			t.Fatal(skills)
		}
	case <-time.After(time.Second):
		t.Fatal("explicit skill input not sent")
	}
	select {
	case job := <-jobs:
		job()
	case <-time.After(time.Second):
		t.Fatal("no turn result")
	}
	if !strings.Contains(a.State.Phase, "执行") {
		t.Fatal(a.State.Phase)
	}
}

func TestClaudeSlashCompletionLoadsRuntimeSkills(t *testing.T) {
	b := &commandTestBackend{request: make(chan string, 1), result: make(chan []agent.Command, 1)}
	a := New(stubBackend{}, "test", "/claude-work", nil)
	if err := a.RegisterBackend(agent.Claude, "/claude", b); err != nil {
		t.Fatal(err)
	}
	a.newSession("/claude-work")
	a.session.kind, a.session.runtimeKey = agent.Claude, "/claude"
	a.session.State.Agent = agent.Claude
	jobs := make(chan func(), 4)
	a.dispatch = func(fn func()) { jobs <- fn }
	sent := 0
	a.submit = func() { sent++ }
	tt := native.NewTester(a.view, 640, 480)
	tt.Click("任务输入")
	tt.Type("/")
	select {
	case cwd := <-b.request:
		if cwd != "/claude-work" {
			t.Fatal(cwd)
		}
	case <-time.After(time.Second):
		t.Fatal("typing / did not read Claude commands")
	}
	b.result <- []agent.Command{
		{Name: "frontend-design", Description: "设计界面", Source: "Claude Code", Cwd: "/claude-work", IsSkill: true},
		{Name: "compact", Description: "压缩上下文", Source: "Claude Code", Cwd: "/claude-work"},
		{Name: "model", Description: "选择模型", Source: "Claude Code", Cwd: "/claude-work"},
	}
	select {
	case job := <-jobs:
		job()
	case <-time.After(time.Second):
		t.Fatal("no command result")
	}
	tt.Frame()
	if _, ok := tt.Find("候选 /frontend-design"); !ok {
		t.Fatal("Claude skill missing from slash menu")
	}
	for _, command := range []string{"compact", "model", "new", "resume"} {
		if _, ok := tt.Find("候选 /" + command); ok {
			t.Fatalf("non-skill command /%s shown", command)
		}
	}
	tt.Key(0, native.KeyTab)
	if a.State.Draft != "/frontend-design " || sent != 0 {
		t.Fatalf("runtime command not inserted as text: %q sent=%d", a.State.Draft, sent)
	}
}

func TestClaudeSlashCompletionRetryClearsError(t *testing.T) {
	backend := &commandTestBackend{
		request: make(chan string, 1), result: make(chan []agent.Command, 1), failure: make(chan error, 1),
	}
	application := New(stubBackend{}, "test", "/claude-work", nil)
	if err := application.RegisterBackend(agent.Claude, "/claude", backend); err != nil {
		t.Fatal(err)
	}
	application.newSession("/claude-work")
	application.session.kind, application.session.runtimeKey = agent.Claude, "/claude"
	application.State.Agent = agent.Claude
	jobs := make(chan func(), 4)
	application.dispatch = func(job func()) { jobs <- job }
	awaitResult := func() {
		t.Helper()
		select {
		case job := <-jobs:
			job()
		case <-time.After(time.Second):
			t.Fatal("no command result")
		}
	}
	awaitRequest := func() {
		t.Helper()
		select {
		case cwd := <-backend.request:
			if cwd != "/claude-work" {
				t.Fatal(cwd)
			}
		case <-time.After(time.Second):
			t.Fatal("no command request")
		}
	}
	tester := native.NewTester(application.view, 640, 480)
	tester.Click("任务输入")
	tester.Type("/")
	awaitRequest()
	backend.failure <- errors.New("command catalog temporarily unavailable")
	awaitResult()
	tester.Frame()
	if application.completion.error == "" {
		t.Fatal("catalog failure was not shown")
	}
	if err := tester.Click("重新读取"); err != nil {
		t.Fatal(err)
	}
	awaitRequest()
	if !application.completion.loading || application.completion.error != "" {
		t.Fatalf("retry did not clear previous failure: %+v", application.completion)
	}
	backend.result <- []agent.Command{{Name: "frontend-design", Description: "设计界面", Cwd: "/claude-work", IsSkill: true}}
	awaitResult()
	tester.Frame()
	if _, ok := tester.Find("候选 /frontend-design"); !ok {
		t.Fatal("successful retry did not show command")
	}
	if _, ok := tester.Find("重新读取"); ok || application.completion.error != "" || application.completion.loading {
		t.Fatalf("successful retry still shows failure: %+v", application.completion)
	}
}

type commandTestBackend struct {
	stubBackend
	request chan string
	result  chan []agent.Command
	failure chan error
}

func (b *commandTestBackend) ListCommands(ctx context.Context, cwd string) ([]agent.Command, error) {
	b.request <- cwd
	select {
	case commands := <-b.result:
		return commands, nil
	case err := <-b.failure:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestCompletionKeyboardSelectionAndIMEGate(t *testing.T) {
	a := completionTestApp()
	tt := native.NewTester(a.view, 640, 480)
	tt.Click("任务输入")
	tt.Type("/")
	tt.Key(0, native.KeyDown)
	if a.completion.selected != 1 {
		t.Fatal("Down did not select next command")
	}
	tt.Key(0, native.KeyUp)
	if a.completion.selected != 0 {
		t.Fatal("Up did not select previous command")
	}
	tt.Key(native.Cmd, native.KeyA)
	tt.Type("$")
	tt.Compose("前端", 2)
	if _, ok := tt.Find("输入候选"); ok {
		t.Fatal("completion panel shown during IME composition")
	}
	tt.Key(0, native.KeyEnter)
	if len(a.completion.chosen) > 0 {
		t.Fatal("IME Enter accepted a skill")
	}
	tt.Compose("", 0)
}

func TestSkillReferencesSurviveDraftRestartAndFollowEdits(t *testing.T) {
	a := completionTestApp()
	a.State.Draft = "$front"
	q, _ := queryCompletion(a.State.Draft, 6, 6)
	a.acceptCompletion(q, completionItem{skill: &a.completion.skills[0]})
	a.State.Draft += "task"
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Save(a.snapshot()); err != nil {
		t.Fatal(err)
	}
	b := New(stubBackend{}, "test", "", nil)
	if err := b.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	refs := b.draftSkills(b.State.Draft)
	if len(refs) != 1 || refs[0].Path != "/tmp/skills/frontend-design/SKILL.md" {
		t.Fatalf("lost draft skill: %+v", refs)
	}
	b.State.Draft = "ordinary task"
	if len(b.snapshot().Sessions[0].DraftSkills) != 0 {
		t.Fatal("removed reference remained in saved draft")
	}
	b.State.Cwd = "/other"
	if len(b.draftSkills("$frontend-design task")) != 0 {
		t.Fatal("skill reference crossed working directories")
	}
}

func TestSkillMenuAllowsScrollingAndRevealsKeyboardSelection(t *testing.T) {
	a := completionTestApp()
	a.completion.skills = nil
	for i := range 20 {
		name := fmt.Sprintf("skill-%02d", i)
		a.completion.skills = append(a.completion.skills, codex.Skill{Name: name, Path: "/skills/" + name, Enabled: true})
	}
	tt := native.NewTester(a.view, 640, 480)
	tt.SetDark(true)
	tt.Click("任务输入")
	tt.Type("$")
	first, _ := tt.Find("候选 skill-00")
	tt.Scroll(first.X+40, first.Y+20, 0, 100)
	tt.Frame()
	scrolled, _ := tt.Find("候选 skill-00")
	if scrolled.H > 0 && scrolled.Y >= first.Y {
		t.Fatalf("manual scrolling snapped back to selected item: first=%+v after=%+v", first, scrolled)
	}
	tt.Key(0, native.KeyUp)
	tt.Frame()
	last, _ := tt.Find("候选 skill-19")
	panel, _ := tt.Find("输入候选")
	if a.completion.selected != 19 || last.Y < panel.Y || last.Y+last.H > panel.Y+panel.H {
		t.Fatalf("keyboard selection not revealed: selected=%d last=%+v panel=%+v", a.completion.selected, last, panel)
	}
}
