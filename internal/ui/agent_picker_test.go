package ui

import (
	"testing"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func TestAgentChoicePreservesDraftAndStartedSession(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	a.State.Draft = "preserve draft"
	a.selectedModel = "codex-model"
	if err := a.chooseAgent(agent.Claude); err != nil {
		t.Fatal(err)
	}
	if a.State.Draft != "preserve draft" || a.selectedModel != "" || a.kind != agent.Claude || a.runtimeKey != "/claude" || a.State.Agent != agent.Claude {
		t.Fatal("agent selection lost draft or retained another agent model")
	}
	original := a.session
	original.State.ThreadID = "claude-session"
	original.State.Busy = true
	if err := a.chooseAgent(agent.Codex); err != nil {
		t.Fatal(err)
	}
	if a.session == original || a.kind.Normalized() != agent.Codex || original.kind != agent.Claude || original.State.ThreadID != "claude-session" || !original.State.Busy {
		t.Fatal("changing agent mutated a running session")
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
	if err := a.chooseAgent(agent.Claude); err != nil {
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
	if err := a.chooseAgent(agent.Claude); err != nil {
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
	if err := a.chooseAgent(agent.Claude); err != nil {
		t.Fatal(err)
	}
	if a.kind != agent.Claude || a.State.Disconnected || a.State.Error != "" {
		t.Fatal("Codex failure disabled Claude", a.State.Error)
	}
}

func TestCompletedClaudeAppearsInMatchingHistory(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	codex := a.session
	codex.State.ThreadID = "original-codex"
	if err := a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	if err := a.chooseAgent(agent.Claude); err != nil {
		t.Fatal(err)
	}
	a.State.ThreadID = "new-claude"
	a.recordClaudeHistory()
	a.recordClaudeHistory()
	if len(a.nativeThreads) != 1 || a.nativeThreads[0].ID != "new-claude" {
		t.Fatal("completed session missing or duplicated")
	}
	if codex != a.session && len(codex.nativeThreads) != 0 {
		t.Fatal("Claude history crossed into Codex")
	}
}
