package ui

import (
	"encoding/json"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/storage"
)

func TestOpenHistoryReusesTheSharedListAcrossAgents(t *testing.T) {
	backend := &threadPagesBackend{
		recoveryBackend: *recoveredBackend("completed"),
		threadPages:     []agent.ThreadsPage{{Data: []agent.ThreadMeta{{ID: "native", Cwd: "/first", Preview: "history"}}}},
	}
	a := New(backend, "test", "/source", nil)
	source := a.session
	a.threadList.threads = []agent.ThreadMeta{{Agent: agent.Claude, Runtime: "/claude", ID: "native", Cwd: "/first", Preview: "history"}}
	a.threadList.sources = map[backendKey]threadSource{{kind: agent.Claude, runtime: "/claude"}: {loaded: true}}
	a.historyScope, a.historyOpen = 1, true
	tt := native.NewTester(a.view, 1400, 800)
	if err := tt.Click("打开历史 native"); err != nil {
		t.Fatal(err)
	}
	if a.session == source || a.kind != agent.Claude || a.runtimeKey != "/claude" || a.State.Cwd != "/first" {
		t.Fatal("opening history lost the row's agent, runtime or directory")
	}
	if len(backend.cursors) != 0 {
		t.Fatal("opening history issued another catalog request")
	}
	if len(a.threadList.threads) != 1 || len(a.threadList.sources) != 1 {
		t.Fatal("opening history discarded the shared catalog")
	}
	a.selectSession(source)
	if len(a.threadList.threads) != 1 || len(backend.cursors) != 0 {
		t.Fatal("switching back reloaded or discarded the shared catalog")
	}
}

func TestSwitchingProjectsNeverReloadsTheSharedHistory(t *testing.T) {
	backend := &threadPagesBackend{
		recoveryBackend: *recoveredBackend("completed"),
		threadPages: []agent.ThreadsPage{{Data: []agent.ThreadMeta{
			{ID: "one", Cwd: "/first", Preview: "first task"},
			{ID: "two", Cwd: "/second", Preview: "second task"},
		}}},
	}
	a := New(backend, "test", "/first", nil)
	first := a.session
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	a.loadThreadList()
	pumpUpdate(t, queue)
	a.newSession("/second")
	second := a.session
	a.historyOpen = true
	tt := native.NewTester(a.view, 1400, 800)
	if !tt.HasText("second task") || tt.HasText("first task") {
		t.Fatal("current-project filter did not limit shared history")
	}
	a.selectSession(first)
	tt.Frame()
	if !tt.HasText("first task") || tt.HasText("second task") || tt.HasText("已加载的历史中没有匹配会话") {
		t.Fatal("switching projects emptied or mislabeled shared history")
	}
	a.selectSession(second)
	tt.Frame()
	if !tt.HasText("second task") || tt.HasText("已加载的历史中没有匹配会话") || len(backend.cursors) != 1 {
		t.Fatal("switching projects reloaded or emptied shared history")
	}
}

func TestInFlightThreadListSurvivesSessionSwitch(t *testing.T) {
	backend := &threadPagesBackend{
		recoveryBackend: *recoveredBackend("completed"),
		threadPages:     []agent.ThreadsPage{{Data: []agent.ThreadMeta{{ID: "one", Cwd: "/first", Preview: "history"}}}},
	}
	a := New(backend, "test", "/first", nil)
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	a.loadThreadList()
	a.newSession("/second")
	pumpUpdate(t, queue)
	if len(a.threadList.threads) != 1 || a.threadList.threads[0].ID != "one" || len(backend.cursors) != 1 {
		t.Fatal("session switch dropped or restarted the in-flight catalog load")
	}
}

func TestCompletedThreadJoinsTheSharedHistory(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	a.State.ThreadID = "new-codex"
	a.recordThreadHistory()
	if len(a.threadList.threads) != 0 {
		t.Fatal("unread source recorded a session")
	}
	a.threadList.sources = map[backendKey]threadSource{{kind: agent.Codex}: {loaded: true}}
	a.recordThreadHistory()
	a.recordThreadHistory()
	if len(a.threadList.threads) != 1 || a.threadList.threads[0].ID != "new-codex" || a.threadList.threads[0].Agent != agent.Codex {
		t.Fatal("completed session missing or duplicated in shared history")
	}
}

func TestSessionBackendRoutingAndMissingClaude(t *testing.T) {
	codexBackend := &modelCaptureBackend{}
	claudeBackend := &modelCaptureBackend{}
	a := New(codexBackend, "test", "/one", nil)
	codexSession := a.session
	if err := a.RegisterBackend(agent.Claude, "/claude", claudeBackend); err != nil {
		t.Fatal(err)
	}
	a.newSession("/two")
	claudeSession := a.session
	claudeSession.kind, claudeSession.runtimeKey = agent.Claude, "/claude"
	if a.sessionBackend() != claudeBackend || a.forSession(codexSession).sessionBackend() != codexBackend {
		t.Fatal("会话未选择所属后端")
	}
	claudeSession.runtimeKey = "/missing"
	if a.sessionBackend() != nil {
		t.Fatal("缺失 Claude 连接不能回落到 Codex")
	}
	if a.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}) == nil ||
		a.RegisterBackend(agent.Claude, "/other", claudeBackend) == nil ||
		a.RegisterBackend("unknown", "/unknown", &modelCaptureBackend{}) == nil {
		t.Fatal("重复连接或未知 Agent 被注册")
	}
}

func TestDisconnectScopeAndLateProcessGeneration(t *testing.T) {
	a := New(&modelCaptureBackend{}, "test", "/one", nil)
	codexSession := a.session
	a.State.ThreadID = "same"
	a.newSession("/two")
	first := a.session
	first.kind, first.runtimeKey, first.generation = agent.Claude, "/claude", 2
	first.State.ThreadID = "same"
	a.newSession("/three")
	second := a.session
	second.kind, second.runtimeKey, second.generation = agent.Claude, "/claude", 1
	second.State.ThreadID = "other"
	source := agent.SessionKey{Agent: agent.Claude, Runtime: "/claude", SessionID: "same"}
	a.applyBatch([]agent.Event{{Session: source, Generation: 1, Method: "sasa/disconnected"}})
	if first.State.Disconnected || second.State.Disconnected || codexSession.State.Disconnected {
		t.Fatal("旧进程事件污染新连接")
	}
	a.applyBatch([]agent.Event{{Session: source, Generation: 2, Method: "sasa/disconnected"}})
	if !first.State.Disconnected || second.State.Disconnected || codexSession.State.Disconnected {
		t.Fatal("单会话断线传播到其他 Agent 或会话")
	}
	a.applyBatch([]agent.Event{{Method: "sasa/disconnected"}})
	if !codexSession.State.Disconnected || second.State.Disconnected {
		t.Fatal("旧 Codex 广播失联语义不兼容或污染 Claude")
	}
}

func TestModelCatalogCallbackKeepsAgentAndSessionOwner(t *testing.T) {
	b := &modelCaptureBackend{}
	a := New(b, "test", "/codex", nil)
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	codexSession := a.session
	a.loadModels()
	a.newSession("/claude")
	claudeSession := a.session
	claudeSession.kind = agent.Claude
	pumpUpdate(t, queue)
	if !codexSession.modelsLoaded || len(codexSession.models) == 0 ||
		claudeSession.modelsLoaded || len(claudeSession.models) != 0 {
		t.Fatal("模型目录跨会话或 Agent 写回")
	}
	a.selectSession(codexSession)
	a.loadModelCatalog(true)
	a.State.Cwd = "/moved"
	pumpUpdate(t, queue)
	if codexSession.modelsLoaded || len(codexSession.models) != 0 {
		t.Fatal("旧 cwd 的模型响应进入新目录缓存")
	}
}

func TestScopedEventDoesNotUseEqualNativeIDFromOtherAgent(t *testing.T) {
	a := New(&modelCaptureBackend{}, "test", "/one", nil)
	codexSession := a.session
	codexSession.State.ThreadID = "same"
	a.newSession("/two")
	a.kind = agent.Claude
	a.State.ThreadID = "same"
	a.applyBatch([]agent.Event{{
		Session: agent.SessionKey{Agent: agent.Claude},
		Method:  "item/agentMessage/delta",
		Params:  json.RawMessage(`{"threadId":"same","turnId":"turn","itemId":"reply","delta":"Claude"}`),
	}})
	if len(codexSession.State.Messages) != 0 {
		t.Fatal("相同原生 ID 的 Claude 事件进入 Codex 会话")
	}
}

func TestRestoredClaudeSessionKeepsAgentModelAndDraft(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Save(storage.Snapshot{
		Selected: 2, OpenTabs: []int{1, 2},
		Sessions: []storage.Session{
			{ID: 1, ThreadID: "same", Cwd: "/codex"},
			{
				ID: 2, Agent: agent.Claude, Runtime: "/claude", ModelAgent: agent.Claude,
				ThreadID: "same", Cwd: "/project", SelectedModel: "claude-model", Draft: "保留草稿",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	a := New(&modelCaptureBackend{}, "test", "", nil)
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if a.kind != agent.Claude || a.State.Agent != agent.Claude || a.selectedModel != "claude-model" ||
		a.State.Draft != "保留草稿" || a.sessionBackend() != nil || !a.State.Disconnected || a.canSend() {
		t.Fatal("重开改写 Claude 归属或允许使用 Codex 后端")
	}
	saved := a.snapshot().Sessions[1]
	if saved.Agent != agent.Claude || saved.ModelAgent != agent.Claude || saved.Runtime != "/claude" ||
		saved.SelectedModel != "claude-model" || saved.Draft != "保留草稿" {
		t.Fatal("未接入 Agent 的索引未保留", saved)
	}
}
