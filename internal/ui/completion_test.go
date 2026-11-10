package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	for _, text := range []string{"/tmp/file", "hello /status", "cost$front", "$$front", "@/Users/me/Docu", "看 @/"} {
		if _, ok := queryCompletion(text, len([]rune(text)), len([]rune(text)), agent.Codex); ok {
			t.Fatalf("unexpected completion %q", text)
		}
	}
	text := "中文 $front suffix"
	caret := len([]rune("中文 $front"))
	q, ok := queryCompletion(text, caret, caret, agent.Codex)
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

func TestDevinDollarCompletionInsertsNativeSkillCommand(t *testing.T) {
	b := &commandTestBackend{request: make(chan string, 1), result: make(chan []agent.Command, 1)}
	a := New(stubBackend{}, "test", "/devin-work", nil)
	if err := a.RegisterBackend(agent.Devin, "/devin", b); err != nil {
		t.Fatal(err)
	}
	a.newSession("/devin-work")
	a.session.kind, a.session.runtimeKey = agent.Devin, "/devin"
	a.session.State.Agent = agent.Devin
	jobs := make(chan func(), 4)
	a.dispatch = func(fn func()) { jobs <- fn }
	tt := native.NewTester(a.view, 640, 480)
	tt.Click("任务输入")
	tt.Type("$")
	select {
	case cwd := <-b.request:
		if cwd != "/devin-work" {
			t.Fatal(cwd)
		}
	case <-time.After(time.Second):
		t.Fatal("typing $ did not read Devin commands")
	}
	b.result <- []agent.Command{
		{Name: "review", Description: "审查改动", Source: "Devin", Cwd: "/devin-work", IsSkill: true},
		{Name: "status", Description: "查看状态", Source: "Devin", Cwd: "/devin-work"},
	}
	select {
	case job := <-jobs:
		job()
	case <-time.After(time.Second):
		t.Fatal("no command result")
	}
	tt.Frame()
	if _, ok := tt.Find("候选 /review"); !ok {
		t.Fatal("Devin skill missing from $ menu")
	}
	if _, ok := tt.Find("候选 /status"); ok {
		t.Fatal("non-skill command shown in $ menu")
	}
	tt.Key(0, native.KeyTab)
	if a.State.Draft != "/review " {
		t.Fatalf("skill not inserted as native command: %q", a.State.Draft)
	}
	tt.Type("x $r")
	if a.State.Draft != "/review x $r" {
		t.Fatalf("typing lost focus/caret: %q", a.State.Draft)
	}
	if _, ok := tt.Find("输入候选"); ok {
		t.Fatal("mid-text $ opened completion for Devin")
	}
}

func TestDevinCommandsEventRefreshesEmptyCatalog(t *testing.T) {
	b := &commandTestBackend{request: make(chan string, 1), result: make(chan []agent.Command, 1)}
	a := New(stubBackend{}, "test", "/devin-work", nil)
	if err := a.RegisterBackend(agent.Devin, "/devin", b); err != nil {
		t.Fatal(err)
	}
	a.newSession("/devin-work")
	a.session.kind, a.session.runtimeKey = agent.Devin, "/devin"
	a.session.State.Agent = agent.Devin
	jobs := make(chan func(), 4)
	a.dispatch = func(fn func()) { jobs <- fn }
	tt := native.NewTester(a.view, 640, 480)
	tt.Click("任务输入")
	tt.Type("$")
	awaitRequest := func() {
		t.Helper()
		select {
		case <-b.request:
		case <-time.After(time.Second):
			t.Fatal("no command request")
		}
	}
	awaitJob := func() {
		t.Helper()
		select {
		case job := <-jobs:
			job()
		case <-time.After(time.Second):
			t.Fatal("no command result")
		}
	}
	awaitRequest()
	b.result <- nil
	awaitJob()
	tt.Frame()
	if _, ok := tt.Find("候选 /review"); ok {
		t.Fatal("empty catalog showed a candidate")
	}
	a.applyBatch([]agent.Event{{Session: agent.SessionKey{Agent: agent.Devin, Runtime: "/devin"}, Method: "fuyu/commands"}})
	tt.Frame()
	awaitRequest()
	b.result <- []agent.Command{{Name: "review", Description: "审查改动", Source: "Devin", Cwd: "/devin-work", IsSkill: true}}
	awaitJob()
	tt.Frame()
	if _, ok := tt.Find("候选 /review"); !ok {
		t.Fatal("advertised commands did not refresh the $ menu")
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
	q, _ := queryCompletion(a.State.Draft, 6, 6, agent.Codex)
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

func TestCompletionQueryFileMention(t *testing.T) {
	for _, c := range []struct {
		text  string
		kind  byte
		query string
	}{
		{"@", '@', ""},
		{"task @int", '@', "int"},
		{"@internal/ui", '@', "internal/ui"},
		{"@a $design", '$', "design"},
	} {
		caret := len([]rune(c.text))
		q, ok := queryCompletion(c.text, caret, caret, agent.Codex)
		if !ok || q.kind != c.kind || q.query != c.query {
			t.Fatalf("%q → %+v ok=%v", c.text, q, ok)
		}
	}
	text := "fix @internal/ui/completion.go now"
	caret := len([]rune("fix @internal/ui/completion.go"))
	q, ok := queryCompletion(text, caret, caret, agent.Codex)
	if !ok || q.kind != '@' || q.query != "internal/ui/completion.go" {
		t.Fatalf("mid-sentence query failed: %+v ok=%v", q, ok)
	}
	text = "fix @internal/ui/old.go now"
	caret = len([]rune("fix @internal/u"))
	q, ok = queryCompletion(text, caret, caret, agent.Codex)
	if !ok || q.query != "internal/u" {
		t.Fatalf("mid-token query failed: %+v ok=%v", q, ok)
	}
	a := completionTestApp()
	a.State.Draft = text
	a.acceptCompletion(q, completionItem{path: "internal/ui/view.go"})
	if a.State.Draft != "fix @internal/ui/view.go now" || a.completion.caret != len([]rune("fix @internal/ui/view.go ")) {
		t.Fatalf("picking inside a path did not replace it: %q caret=%d", a.State.Draft, a.completion.caret)
	}
	if _, ok := queryCompletion("@/Users/me/a.go", 1, 1, agent.Codex); ok {
		t.Fatal("caret after @ of an absolute path opened completion")
	}
	for _, text := range []string{"user@host", "email@x.com", "@a@b", "@dir/file.go ", "@@"} {
		caret := len([]rune(text))
		if _, ok := queryCompletion(text, caret, caret, agent.Codex); ok {
			t.Fatalf("unexpected completion %q", text)
		}
	}
}

func awaitFileIndex(t *testing.T, a *App, jobs chan func()) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for a.session.completion.filesVersion == 0 {
		select {
		case job := <-jobs:
			job()
		case <-time.After(100 * time.Millisecond):
			if time.Now().After(deadline) {
				t.Fatal("file index not loaded")
			}
		}
	}
}

func TestFileMentionCompletionInsertsPathsAndDescends(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sub"), 0700)
	os.WriteFile(filepath.Join(root, "sub", "note.md"), nil, 0600)
	os.WriteFile(filepath.Join(root, "top.go"), nil, 0600)
	a := New(stubBackend{}, "test", root, nil)
	jobs := make(chan func(), 8)
	a.dispatch = func(fn func()) { jobs <- fn }
	sent := 0
	a.submit = func() { sent++ }
	tt := native.NewTester(a.view, 640, 480)
	tt.Click("任务输入")
	tt.Type("@")
	awaitFileIndex(t, a, jobs)
	tt.Frame()
	if _, ok := tt.Find("候选 sub"); !ok {
		t.Fatal("directory not listed")
	}
	if _, ok := tt.Find("候选 top.go"); !ok {
		t.Fatal("file not listed")
	}
	tt.Type("sub")
	if _, ok := tt.Find("候选 sub"); !ok {
		t.Fatal("query did not keep directory candidate")
	}
	if _, ok := tt.Find("候选 top.go"); ok {
		t.Fatal("unrelated file stayed visible")
	}
	tt.Key(0, native.KeyEnter)
	if a.State.Draft != "@sub/" {
		t.Fatalf("directory did not stay open: %q", a.State.Draft)
	}
	tt.Frame()
	if _, ok := tt.Find("候选 note.md"); !ok {
		t.Fatal("directory children not listed")
	}
	tt.Key(0, native.KeyEnter)
	if a.State.Draft != "@sub/note.md " {
		t.Fatalf("file reference not inserted: %q", a.State.Draft)
	}
	tt.Type("task")
	if a.State.Draft != "@sub/note.md task" {
		t.Fatalf("typing lost focus/caret: %q", a.State.Draft)
	}
	if sent != 0 {
		t.Fatal("completion submitted a task")
	}
}

func TestFileMentionEscapeDismissesAndTextSendsVerbatim(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "only.go"), nil, 0600)
	a := New(stubBackend{}, "test", root, nil)
	jobs := make(chan func(), 8)
	a.dispatch = func(fn func()) { jobs <- fn }
	tt := native.NewTester(a.view, 640, 480)
	tt.Click("任务输入")
	tt.Type("@")
	awaitFileIndex(t, a, jobs)
	tt.Frame()
	tt.Key(0, native.KeyEscape)
	if _, ok := tt.Find("输入候选"); ok {
		t.Fatal("Escape did not dismiss file candidates")
	}
	a.State.Draft = "@only.go 说明这个文件"
	a.completion.caret = len([]rune(a.State.Draft))
	a.completion.focus = true
	tt.Frame()
	sent := ""
	a.submit = func() { sent = a.State.Draft }
	tt.Key(0, native.KeyEnter)
	if sent != "@only.go 说明这个文件" {
		t.Fatalf("mention text not sent verbatim: %q", sent)
	}
}

func TestFileMentionPagesMoreItemsAtScrollEnd(t *testing.T) {
	root := t.TempDir()
	for i := range 45 {
		os.WriteFile(filepath.Join(root, fmt.Sprintf("f%02d.go", i)), nil, 0600)
	}
	a := New(stubBackend{}, "test", root, nil)
	jobs := make(chan func(), 8)
	a.dispatch = func(fn func()) { jobs <- fn }
	tt := native.NewTester(a.view, 640, 480)
	tt.Click("任务输入")
	tt.Type("@f")
	awaitFileIndex(t, a, jobs)
	tt.Frame()
	if _, ok := tt.Find("候选 f29.go"); !ok {
		t.Fatal("first page missing")
	}
	if _, ok := tt.Find("候选 f30.go"); ok {
		t.Fatal("second page rendered before scrolling")
	}
	panel, _ := tt.Find("输入候选")
	tt.Scroll(panel.X+40, panel.Y+80, 0, 2000)
	tt.Frame()
	tt.Frame()
	if _, ok := tt.Find("候选 f30.go"); !ok {
		t.Fatal("scroll end did not load next page")
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
