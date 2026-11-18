package ui

import (
	"context"
	"errors"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/storage"
)

type failingHistoryBackend struct {
	threadPagesBackend
	failure error
}

func (backend *failingHistoryBackend) ListThreads(ctx context.Context, cursor *string) (agent.ThreadsPage, error) {
	if backend.failure != nil {
		return agent.ThreadsPage{}, backend.failure
	}
	return backend.threadPagesBackend.ListThreads(ctx, cursor)
}

func TestHistoryCombinesAgentsWithIndependentPagination(t *testing.T) {
	cursor := "codex-next"
	codex := &threadPagesBackend{threadPages: []agent.ThreadsPage{
		{Data: []agent.ThreadMeta{{ID: "same", Cwd: "/project", Preview: "Codex task"}}, NextCursor: &cursor},
		{Data: []agent.ThreadMeta{{ID: "same", Cwd: "/project", Preview: "Updated Codex"}, {ID: "other", Cwd: "/other", Preview: "Other project"}}},
	}}
	claude := &threadPagesBackend{threadPages: []agent.ThreadsPage{
		{Data: []agent.ThreadMeta{{ID: "same", Cwd: "/project", Preview: "Claude task"}}},
	}}
	application := New(codex, "test", "/project", nil)
	if err := application.RegisterBackend(agent.Claude, "/claude", claude); err != nil {
		t.Fatal(err)
	}
	queue := make(chan func(), 4)
	application.dispatch = func(update func()) { queue <- update }
	application.loadThreadList()
	application.loadThreadList()
	pumpUpdate(t, queue)
	if len(application.threadList.threads) != 2 || len(codex.cursors) != 1 || len(claude.cursors) != 1 {
		t.Fatal("both Agents were not loaded once or shared IDs were merged")
	}
	application.loadThreadList()
	pumpUpdate(t, queue)
	application.loadThreadList()
	if len(application.threadList.threads) != 3 || len(claude.cursors) != 1 || len(codex.cursors) != 2 || codex.cursors[1] != cursor || application.threadList.cursor != nil {
		t.Fatal("source cursors or exhausted pagination were lost")
	}
	application.historyOpen = true
	tester := native.NewTester(application.view, 1400, 800)
	for _, title := range []string{"Claude task", "Updated Codex"} {
		if !tester.HasText(title) {
			t.Fatal("mixed history row missing", title)
		}
	}
	if tester.HasText("Other project") {
		t.Fatal("current project filter leaked another project")
	}
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		icon, exists := tester.Find("历史 Agent " + kind.String())
		if !exists || icon.W != 12 || icon.H != 12 {
			t.Fatal("history lost its Agent icon", kind, icon)
		}
	}
	if err := tester.Click("全部项目"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("Other project") {
		t.Fatal("all projects omitted history")
	}
	application.historyQuery = "Claude"
	tester.Frame()
	if !tester.HasText("Claude task") || tester.HasText("Updated Codex") {
		t.Fatal("mixed history search failed")
	}
	application.historyQuery = ""
	application.deletedThreads = []storage.DeletedThread{{Agent: agent.Claude, Runtime: "/claude", ThreadID: "same"}}
	tester.Frame()
	if tester.HasText("Claude task") || !tester.HasText("Updated Codex") {
		t.Fatal("hidden history crossed Agent identity")
	}
}

func TestHistoryKeepsSuccessfulSourceAndRetriesFailure(t *testing.T) {
	codex := &threadPagesBackend{threadPages: []agent.ThreadsPage{{Data: []agent.ThreadMeta{{ID: "codex", Cwd: "/project"}}}}}
	claude := &failingHistoryBackend{
		failure:            errors.New("temporarily unavailable"),
		threadPagesBackend: threadPagesBackend{threadPages: []agent.ThreadsPage{{Data: []agent.ThreadMeta{{ID: "claude", Cwd: "/project"}}}}},
	}
	application := New(codex, "test", "/project", nil)
	if err := application.RegisterBackend(agent.Claude, "/claude", claude); err != nil {
		t.Fatal(err)
	}
	queue := make(chan func(), 4)
	application.dispatch = func(update func()) { queue <- update }
	application.loadThreadList()
	pumpUpdate(t, queue)
	if len(application.threadList.threads) != 1 || application.threadList.threads[0].Agent != agent.Codex || application.threadList.err == "" {
		t.Fatal("failed source hid available history or lost its error")
	}
	claude.failure = nil
	application.loadThreadList()
	pumpUpdate(t, queue)
	if len(application.threadList.threads) != 2 || len(codex.cursors) != 1 || application.threadList.err != "" {
		t.Fatal("retry reloaded the exhausted source or lost history")
	}
}

func TestHistoryOpensTheRowAgentInsteadOfDefault(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			application := New(stubBackend{}, "test", "/project", nil)
			application.defaultAgent = agent.Claude
			application.lastModels = map[string]string{modelPreferenceKey(agent.Claude, "/claude"): "claude-model"}
			application.agentErrors = map[agent.AgentKind]string{agent.Claude: "unavailable"}
			runtime := ""
			if kind == agent.Claude {
				runtime = "/claude"
			}
			application.threadList.threads = []agent.ThreadMeta{{Agent: kind, Runtime: runtime, ID: "target", Cwd: "/project", Preview: "History target"}}
			application.historyOpen = true
			tester := native.NewTester(application.view, 1400, 800)
			if err := tester.RightClick("打开历史 target"); err != nil {
				t.Fatal(err)
			}
			if err := tester.ChooseMenuItem("重命名"); err != nil {
				t.Fatal(err)
			}
			if application.sessionRenameTarget.kind != kind || application.sessionRenameTarget.runtimeKey != runtime {
				t.Fatal("rename targeted the current Agent instead of the row")
			}
			application.sessionRenameOpen = false
			tester.Frame()
			if err := tester.Click("打开历史 target"); err != nil {
				t.Fatal(err)
			}
			if application.kind != kind || application.runtimeKey != runtime || application.State.Agent != kind || application.State.ThreadID != "target" {
				t.Fatal("opened history under the wrong Agent or runtime")
			}
			if kind == agent.Codex && (application.selectedModel != "" || application.State.Disconnected) {
				t.Fatal("Claude default model or failure leaked into Codex history")
			}
		})
	}
}

func TestHistoryPanelRemovesProjectCaption(t *testing.T) {
	application := New(stubBackend{}, "test", "/project/Sasa", nil)
	tester := native.NewTester(application.historyPanel, 360, 600)
	if tester.HasText("Sasa") || !tester.HasText("当前项目") || !tester.HasText("全部项目") {
		t.Fatal("history scope retained the redundant project caption or lost the controls")
	}
}

func TestHistoryEmptySourcesAreNotReloaded(t *testing.T) {
	codex := &threadPagesBackend{threadPages: []agent.ThreadsPage{{}}}
	claude := &threadPagesBackend{threadPages: []agent.ThreadsPage{{}}}
	application := New(codex, "test", "/project", nil)
	if err := application.RegisterBackend(agent.Claude, "/claude", claude); err != nil {
		t.Fatal(err)
	}
	queue := make(chan func(), 4)
	application.dispatch = func(update func()) { queue <- update }
	application.loadThreadList()
	pumpUpdate(t, queue)
	application.loadThreadList()
	if application.threadList.loading || len(codex.cursors) != 1 || len(claude.cursors) != 1 {
		t.Fatal("empty sources restarted at the first page")
	}
}

func TestHistoryHeadRefreshPrependsAndKeepsPagination(t *testing.T) {
	next := "next"
	codex := &threadPagesBackend{threadPages: []agent.ThreadsPage{
		{Data: []agent.ThreadMeta{{ID: "one", Cwd: "/project", Preview: "one old"}}, NextCursor: &next},
		{Data: []agent.ThreadMeta{{ID: "two", Cwd: "/project", Preview: "two"}}},
	}}
	application := New(codex, "test", "/project", nil)
	queue := make(chan func(), 4)
	application.dispatch = func(update func()) { queue <- update }
	application.loadThreadList()
	pumpUpdate(t, queue)
	application.loadThreadList()
	pumpUpdate(t, queue)
	if len(application.threadList.threads) != 2 || application.threadList.cursor != nil {
		t.Fatal("fixture did not load both pages")
	}
	// The head page gains a new thread and a renamed one; the older page stays.
	codex.threadPages[0].Data = []agent.ThreadMeta{
		{ID: "new", Cwd: "/project", Preview: "brand new"},
		{ID: "one", Cwd: "/project", Preview: "one new"},
	}
	codex.cursors = nil
	application.openThreadList()
	if !application.threadList.loading || len(application.threadList.threads) != 2 {
		t.Fatal("head refresh hid or moved the loaded list")
	}
	pumpUpdate(t, queue)
	threads := application.threadList.threads
	if len(threads) != 3 || threads[0].ID != "new" || threads[1].ID != "one" || threads[1].Preview != "one new" || threads[2].ID != "two" {
		t.Fatal("head refresh did not prepend new rows or update in place", threads)
	}
	if len(codex.cursors) != 1 || codex.cursors[0] != "" || application.threadList.cursor != nil {
		t.Fatal("head refresh paginated or restarted the catalog")
	}
	if state := application.threadList.sources[backendKey{kind: agent.Codex}]; !state.loaded || state.cursor != nil {
		t.Fatal("head refresh rewrote source pagination state")
	}
}
