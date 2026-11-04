package ui

import (
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/attachment"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func clearTestApp(t *testing.T) *App {
	t.Helper()
	application := New(stubBackend{}, "test", "/claude-work", nil)
	if err := application.RegisterBackend(agent.Claude, "/claude", &stubBackend{}); err != nil {
		t.Fatal(err)
	}
	application.kind, application.runtimeKey = agent.Claude, "/claude"
	application.State.Agent = agent.Claude
	application.completion.cwd, application.completion.loaded = "/claude-work", true
	application.completion.cacheKey, application.completion.checked = application.completionCacheKey(), time.Now()
	application.selectedModel = "test-model"
	return application
}

func TestClaudeClearCompletionAndExecutionPreserveHistory(t *testing.T) {
	application := clearTestApp(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	application.store = store
	previous := application.session
	previous.State.ThreadID = "previous-thread"
	previous.State.Messages = []*app.Message{{ID: "previous-message", Role: "user", Text: "remember this"}}
	tester := native.NewTester(application.view, 640, 480)
	tester.Click("任务输入")
	tester.Type("/cle")
	if _, ok := tester.Find("候选 /clear"); !ok {
		t.Fatal("clear command missing without runtime catalog")
	}
	tester.Key(0, native.KeyTab)
	if application.State.Draft != "/clear " || application.session != previous {
		t.Fatal("completion must insert the command before execution")
	}
	tester.Key(0, native.KeyEnter)
	if application.session == previous || !application.open || previous.open || application.State.ThreadID != "" || len(application.State.Messages) != 0 || application.State.Draft != "" {
		t.Fatal("clear did not open an empty context and close the previous tab")
	}
	if previous.State.ThreadID != "previous-thread" || len(previous.State.Messages) != 1 || previous.State.Messages[0].Text != "remember this" || previous.State.Draft != "" {
		t.Fatal("clear changed previous history")
	}
	if application.kind != agent.Claude || application.runtimeKey != "/claude" || application.State.Agent != agent.Claude || application.State.Cwd != "/claude-work" || application.selectedModel != "test-model" {
		t.Fatal("clear changed agent, runtime, directory or model")
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Sessions) != 2 || snapshot.Sessions[0].ThreadID != "previous-thread" || len(snapshot.OpenTabs) != 1 || snapshot.Selected != application.id || snapshot.OpenTabs[0] != application.id {
		t.Fatalf("old history or new tab state was not saved: %+v", snapshot)
	}
	tester.Frame()
	if tester.HasText("remember this") || !tester.HasText("任务输入") {
		t.Fatal("old conversation remained in the new view")
	}
}

func TestClaudeClearRejectsUnsafeSubmission(t *testing.T) {
	for _, scenario := range []string{"busy", "recovering", "blocked", "images", "arguments"} {
		t.Run(scenario, func(t *testing.T) {
			application := clearTestApp(t)
			previous := application.session
			application.State.Draft = "/clear"
			switch scenario {
			case "busy":
				application.State.Busy = true
			case "recovering":
				application.recovering = true
			case "blocked":
				application.recoveryBlocked = true
			case "images":
				application.State.DraftImages = []attachment.Image{{}}
			case "arguments":
				application.State.Draft = "/clear\tnamed-session"
			}
			draft := application.State.Draft
			application.send()
			if application.session != previous || application.State.Draft != draft || !previous.open {
				t.Fatal("clear changed a session that cannot be cleared")
			}
			if (scenario == "images" || scenario == "arguments") && application.State.Error == "" {
				t.Fatal("unsupported clear input did not explain how to proceed")
			}
		})
	}
}

func TestClaudeClearKeepsCurrentSessionWhenHistorySaveFails(t *testing.T) {
	application := clearTestApp(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	application.store = store
	previous := application.session
	application.State.Draft = "/clear"
	application.send()
	if application.session != previous || !previous.open || application.State.Draft != "/clear" || application.State.Error == "" {
		t.Fatal("failed history save did not preserve the current session and command")
	}
}
