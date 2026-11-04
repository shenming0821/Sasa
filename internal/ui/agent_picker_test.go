package ui

import (
	"errors"
	"fmt"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func TestAgentSessionButtonsPreserveOriginalSession(t *testing.T) {
	for _, width := range []float32{480, 1000} {
		for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
			for _, busy := range []bool{false, true} {
				t.Run(fmt.Sprintf("%v/%s/busy=%t", width, kind, busy), func(t *testing.T) {
					a := New(stubBackend{}, "test", "/project", nil)
					if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
						t.Fatal(err)
					}
					original := a.session
					original.State.Draft = "preserve draft"
					original.State.Busy = busy
					if busy {
						original.State.ThreadID = "original-session"
					}
					original.selectedModel = "codex-model"
					tester := native.NewTester(a.view, int(width), 760)
					codex, codexOK := tester.Find("使用 Codex 新建会话")
					claude, claudeOK := tester.Find("使用 Claude Code 新建会话")
					tab, tabOK := tester.Find(fmt.Sprintf("会话标签 %d", original.id))
					if !codexOK || !claudeOK || !tabOK || tab.Y > 1 || codex.Y > tab.Y+tab.H || claude.Y != codex.Y || codex.X+codex.W > claude.X || claude.X+claude.W > width || codex.W != 30 || claude.W != 30 {
						t.Fatal("Agent create buttons missing or outside the upper-right toolbar", codex, claude)
					}
					if _, exists := tester.Find("工作目录"); exists {
						t.Fatal("removed directory toolbar is still visible")
					}
					if _, exists := tester.Find("选择 Agent"); exists {
						t.Fatal("composer still contains Agent picker")
					}
					if _, exists := tester.Find("选择模型"); !exists {
						t.Fatal("composer lost model picker")
					}
					if err := tester.Click("使用 " + kind.String() + " 新建会话"); err != nil {
						t.Fatal(err)
					}
					if a.session == original || a.kind.Normalized() != kind || a.State.Cwd != "/project" || a.State.Draft != "" || a.State.ThreadID != "" || a.State.Busy || a.selectedModel != "" {
						t.Fatal("Agent button failed to create an independent empty session")
					}
					if original.State.Draft != "preserve draft" || original.selectedModel != "codex-model" || original.State.Busy != busy || !original.open || original.kind.Normalized() != agent.Codex {
						t.Fatal("new session changed the original draft, model, task, or tab")
					}
					if busy && original.State.ThreadID != "original-session" {
						t.Fatal("new session lost original task identity")
					}
					if kind == agent.Claude && (a.runtimeKey != "/claude" || a.State.Agent != agent.Claude) {
						t.Fatal("new Claude session lost runtime ownership")
					}
				})
			}
		}
	}
}

func TestAgentSessionUnavailablePreservesCurrentSession(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(kind.String(), func(t *testing.T) {
			a := New(nil, "test", "/project", nil)
			a.SetAgentError(kind, errors.New("CLI missing"))
			a.State.Draft = "preserve draft"
			a.closeSession(a.session)
			original := a.session
			count := len(a.sessions)
			tester := native.NewTester(a.view, 1000, 760)
			label := "新建 Codex 会话"
			if kind == agent.Claude {
				label = "新建 Claude 会话"
			}
			if err := tester.Click(label); err != nil {
				t.Fatal(err)
			}
			if a.session != original || len(a.sessions) != count || a.State.Draft != "preserve draft" || a.paneErrors[a.layouts[a.activeWorkspace].Focused] != "CLI missing" || a.defaultAgent.Normalized() != agent.Codex {
				t.Fatal("unavailable Agent changed the session or hid the failure")
			}
			if !tester.HasText("CLI missing") {
				t.Fatal("empty workspace hid the unavailable Agent error")
			}
		})
	}
}

func TestAgentSessionButtonsUseEmptyWorkspaceDirectory(t *testing.T) {
	a := New(stubBackend{}, "test", t.TempDir(), nil)
	if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	original := a.session
	original.State.Draft = "closed draft"
	a.closeSession(original)
	a.activeWorkspace = t.TempDir()
	cwd := a.activeWorkspace
	tester := native.NewTester(a.view, 1000, 760)
	if err := tester.Click("新建 Claude 会话"); err != nil {
		t.Fatal(err)
	}
	if a.session == original || a.State.Cwd != cwd || a.kind != agent.Claude || original.open || original.State.Draft != "closed draft" {
		t.Fatal("new session ignored the empty workspace or changed the closed draft")
	}
}

func TestAgentSessionIconsKeepBrandColor(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	tester := native.NewTester(a.view, 1000, 760)
	button, exists := tester.Find("使用 Claude Code 新建会话")
	if !exists {
		t.Fatal("Claude create button missing")
	}
	image := tester.Image()
	colored := 0
	for vertical := int(button.Y); vertical < int(button.Y+button.H); vertical++ {
		for horizontal := int(button.X); horizontal < int(button.X+button.W); horizontal++ {
			pixel := image.RGBAAt(horizontal, vertical)
			if pixel.R == 217 && pixel.G == 119 && pixel.B == 87 {
				colored++
			}
		}
	}
	if colored < 10 {
		t.Fatal("Claude SVG lost its original orange tint", colored)
	}
}

func TestSessionTabsShowMatchingAgentIcons(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	a.saved.Title = "Codex task"
	if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	if err := a.newAgentSession(agent.Claude); err != nil {
		t.Fatal(err)
	}
	a.saved.Title = "Claude task"
	tester := native.NewTester(a.view, 1400, 760)
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		icon, exists := tester.Find("会话 Agent " + kind.String())
		if !exists || icon.W != 12 || icon.H != 12 {
			t.Fatal("session tab lost its small Agent icon", kind, icon)
		}
	}
	if tester.HasText("Claude · Claude task") || !tester.HasText("Claude task") {
		t.Fatal("tab title retained the replaced Agent text prefix")
	}
}

func TestAgentChoiceDefaultSurvivesRestart(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := New(stubBackend{}, "test", "/project", nil)
	if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if err := a.newAgentSession(agent.Claude); err != nil {
		t.Fatal(err)
	}
	b := New(stubBackend{}, "test", "", nil)
	if err := b.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	if err := b.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	b.newSession("/another")
	if b.kind != agent.Claude || b.runtimeKey != "/claude" {
		t.Fatal("last selected agent was not restored")
	}
}

func TestClaudeGenerationIgnoresOtherRuntimeEvents(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	if err := a.newAgentSession(agent.Claude); err != nil {
		t.Fatal(err)
	}
	a.State.ThreadID = "shared"
	a.generation = 1
	for _, source := range []agent.SessionKey{
		{Agent: agent.Codex, Runtime: "/claude", SessionID: "shared"},
		{Agent: agent.Claude, Runtime: "/another", SessionID: "shared"},
	} {
		a.applyBatch([]agent.Event{{Session: source, Generation: 99, Method: "ignored"}})
		if a.generation != 1 {
			t.Fatal("foreign event advanced Claude generation")
		}
	}
	a.applyBatch([]agent.Event{{Session: agent.SessionKey{Agent: agent.Claude, Runtime: "/claude", SessionID: "shared"}, Generation: 2, Method: "ignored"}})
	if a.generation != 2 {
		t.Fatal("current runtime generation was not adopted")
	}
}

func TestNewClaudeSessionDoesNotInheritCodexFailure(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	a.State.ThreadID = "old-codex"
	a.State.Disconnected = true
	a.State.Error = "Codex unavailable"
	if err := a.newAgentSession(agent.Claude); err != nil {
		t.Fatal(err)
	}
	if a.kind != agent.Claude || a.State.Disconnected || a.State.Error != "" {
		t.Fatal("Codex failure disabled Claude", a.State.Error)
	}
}

func TestCompletedClaudeAppearsInMatchingHistory(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	a.session.State.ThreadID = "original-codex"
	if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	if err := a.newAgentSession(agent.Claude); err != nil {
		t.Fatal(err)
	}
	a.State.ThreadID = "new-claude"
	a.recordThreadHistory()
	if len(a.threadList.threads) != 0 {
		t.Fatal("unread source recorded a session")
	}
	a.threadList.sources = map[backendKey]threadSource{{kind: agent.Claude, runtime: "/claude"}: {loaded: true}}
	a.recordThreadHistory()
	a.recordThreadHistory()
	if len(a.threadList.threads) != 1 || a.threadList.threads[0].ID != "new-claude" {
		t.Fatal("completed session missing or duplicated")
	}
}
