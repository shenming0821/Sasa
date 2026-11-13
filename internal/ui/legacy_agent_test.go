package ui

import (
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func TestLegacyAgentHistorySurvivesWithoutDesktopRuntime(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	legacy := storage.Session{
		ID: 1, Agent: agent.Devin, Runtime: "/legacy/devin",
		Cwd: "/project", ThreadID: "legacy-native", Draft: "保留旧草稿",
		Title: "旧会话", SelectedModel: "legacy-model", ModelAgent: agent.Devin,
	}
	if err := store.Save(storage.Snapshot{
		DefaultAgent: agent.Devin, Selected: 1, OpenTabs: []int{1},
		Sessions: []storage.Session{legacy},
	}); err != nil {
		t.Fatal(err)
	}
	a := New(stubBackend{}, "test", "/project", nil)
	if err := a.AttachStore(store); err != nil {
		t.Fatal("legacy history blocked application startup", err)
	}
	if a.kind != agent.Devin || a.State.ThreadID != legacy.ThreadID ||
		a.State.Draft != legacy.Draft || a.sessionBackend() != nil || !a.State.Disconnected {
		t.Fatal("legacy session changed identity, lost its draft, or used another backend")
	}
	tt := native.NewTester(func(c *native.Context) { a.agentSessionButtons(c) }, 400, 80)
	if tt.HasText("使用 Devin 新建会话") ||
		!tt.HasText("使用 Codex 新建会话") || !tt.HasText("使用 Claude Code 新建会话") {
		t.Fatal("desktop runtime choices are incorrect")
	}
	a.newSession("/project")
	if a.kind != agent.Codex || a.defaultAgent != agent.Codex {
		t.Fatal("legacy default leaked into a new desktop session")
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, saved := range snapshot.Sessions {
		if saved.ID == legacy.ID {
			if saved.Agent != agent.Devin || saved.ThreadID != legacy.ThreadID ||
				saved.Draft != legacy.Draft || saved.SelectedModel != legacy.SelectedModel {
				t.Fatal("saving a new session rewrote legacy history")
			}
			return
		}
	}
	t.Fatal("saving a new session dropped legacy history")
}
