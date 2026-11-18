package ui

import (
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
)

func TestNewTabTitlesMatchAcrossContentTypes(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	if err := a.newAgentSession(agent.Claude); err != nil {
		t.Fatal(err)
	}
	a.terminalFactory = func(string) (terminalConnection, error) { return &testTerminal{}, nil }
	t.Cleanup(a.closeWorkspaceResources)
	a.newTerminal()
	terminalID := a.activeTab
	pane := focusedPane(a.layoutFor("/project"))
	a.newEmptyTab("/project", pane)
	tester := native.NewTester(func(c *native.Context) {
		a.withPane("/project", pane, func() {
			native.Column(c).Fill().Children(func() { a.sessionTabs(c, true, true) })
		})
	}, 1200, 80)
	count := 0
	for _, text := range tester.Texts() {
		if text == "新标签页" {
			count++
		}
	}
	if count != 4 {
		t.Fatalf("expected four matching initial tab titles, got %d: %v", count, tester.Texts())
	}
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		if _, found := tester.Find("会话 Agent " + kind.String()); !found {
			t.Fatal("new session tab lost its Agent icon", kind)
		}
	}
	bounds, found := tester.Find("终端标签 " + terminalID)
	if !found {
		t.Fatal("terminal tab lost its identity")
	}
	tester.Move(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
	tester.Advance(time.Second)
	if !tester.HasText("终端 · /project") {
		t.Fatal("terminal tooltip did not show its full directory")
	}
}

func TestSessionTabTitlePreservesContentAndCustomNames(t *testing.T) {
	for _, example := range []struct {
		name, saved, custom, message, want string
	}{
		{name: "new", want: "新标签页"},
		{name: "restored placeholder", saved: "新会话", want: "新标签页"},
		{name: "first question", message: "检查编辑器布局", want: "检查编辑器布局"},
		{name: "question matching placeholder", message: "新会话", want: "新会话"},
		{name: "runtime title", saved: "布局调整", want: "布局调整"},
		{name: "custom title", custom: "我的任务", want: "我的任务"},
		{name: "custom placeholder", custom: "新会话", want: "新会话"},
	} {
		t.Run(example.name, func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			a.saved.Title, a.saved.CustomTitle = example.saved, example.custom
			if example.message != "" {
				a.State.Messages = []*app.Message{{Role: "user", Text: example.message}}
			}
			if got := a.tabTitle(a.tabs[0]); got != example.want {
				t.Fatalf("title=%q, want %q", got, example.want)
			}
		})
	}
}
