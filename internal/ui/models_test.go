package ui

import (
	"context"
	"errors"
	"image/png"
	"os"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/attachment"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/storage"
)

type modelCaptureBackend struct {
	imageCaptureBackend
	catalogErr error
	defaultErr error
	effortPin  string
	request    chan modelRequest
}
type modelRequest struct {
	model   string
	options agent.TurnOptions
	skills  []codex.Skill
	images  []attachment.Image
}

func (b *modelCaptureBackend) ListModels(context.Context) ([]codex.Model, error) {
	return []codex.Model{{ID: "catalog-one", Model: "provider/one", DisplayName: "Model One"}, {Model: "provider/two", DisplayName: "Model Two"}, {Model: "hidden", Hidden: true}}, b.catalogErr
}
func (b *modelCaptureBackend) DefaultModel(context.Context, string) (string, error) {
	return "provider/default", b.defaultErr
}
func (b *modelCaptureBackend) SendWithModel(ctx context.Context, threadID, text string, skills []codex.Skill, images []attachment.Image, model string) (codex.Turn, error) {
	return b.SendWithOptions(ctx, threadID, text, skills, images, agent.TurnOptions{Model: model})
}
func (b *modelCaptureBackend) SendWithOptions(_ context.Context, _, _ string, skills []codex.Skill, images []attachment.Image, options agent.TurnOptions) (codex.Turn, error) {
	b.request <- modelRequest{options.Model, options, skills, images}
	return codex.Turn{ID: "model-turn"}, nil
}
func (b *modelCaptureBackend) PinnedEffort(string) string { return b.effortPin }

func TestModelMenuDisplayNameKeepsNativeAlias(t *testing.T) {
	backend := &modelCaptureBackend{}
	application := New(backend, "test", "/workspace", nil)
	store := imageStore(t, application)
	application.models = []codex.Model{
		{Model: "default", DisplayName: "Default (recommended)"},
		{Model: "opus", DisplayName: "winning/deepseek-v4-1-flash-261017"},
		{Model: "fable", DisplayName: "opencode/deepseek-v4.1-flash-free"},
		{Model: "sonnet", DisplayName: "zhipu/glm-5.3"},
		{Model: "haiku", DisplayName: "zhipu/glm-5.3-flash"},
		{Model: "opus[1m]", DisplayName: "Opus (1M context)"},
	}
	application.modelsLoaded = true
	application.selectedModel = "default"
	tester := native.NewTester(application.view, 1000, 760)
	if err := tester.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("选择模型 winning/deepseek-v4-1-flash-261017"); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		tester.Frame()
	}
	if application.selectedModel != "opus" {
		t.Fatal("wrong selected model", application.selectedModel)
	}
	restored := New(backend, "test", "/workspace", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.selectedModel != application.selectedModel {
		t.Fatal("persisted model differs", restored.selectedModel)
	}
}

func TestClaudeModelChoiceBecomesDefaultAcrossSessionsAndRestart(t *testing.T) {
	backend := &modelCaptureBackend{}
	application := New(backend, "test", "/workspace", nil)
	if err := application.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	store := imageStore(t, application)
	if err := application.newAgentSession(agent.Claude); err != nil {
		t.Fatal(err)
	}
	application.models = []agent.Model{
		{Model: "opus", DisplayName: "winning/deepseek-v4-1-flash-261017"},
		{Model: "sonnet", DisplayName: "zhipu/glm-5.3"},
	}
	application.modelsCwd, application.modelsRuntime = application.State.Cwd, application.runtimeKey
	application.modelsLoaded = true
	tester := native.NewTester(application.view, 1000, 760)
	if err := tester.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("选择模型 zhipu/glm-5.3"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("glm-5.3") || tester.HasText("sonnet") {
		t.Fatal("selected alias was displayed instead of concrete model")
	}
	application.newSession("/another")
	if application.selectedModel != "sonnet" {
		t.Fatal("new Claude session lost last selected model", application.selectedModel)
	}
	if err := application.newAgentSession(agent.Codex); err != nil {
		t.Fatal(err)
	}
	if application.selectedModel != "" {
		t.Fatal("Claude model leaked to Codex")
	}
	if err := application.newAgentSession(agent.Claude); err != nil {
		t.Fatal(err)
	}
	if application.selectedModel != "sonnet" {
		t.Fatal("Agent switch lost last Claude model")
	}
	restored := New(backend, "test", "/workspace", nil)
	if err := restored.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	restored.newSession("/after-restart")
	if restored.selectedModel != "sonnet" {
		t.Fatal("restart lost last selected model", restored.selectedModel)
	}
	restored.lastModels = map[string]string{modelPreferenceKey(agent.Claude, "/other-runtime"): "opus"}
	restored.newSession("/isolated")
	if restored.selectedModel != "" {
		t.Fatal("model preference crossed Claude runtime boundaries")
	}
}

func TestClaudeCatalogReplacesLegacyDefaultWithConcreteModel(t *testing.T) {
	application := New(stubBackend{}, "test", "/workspace", nil)
	if err := application.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	if err := application.newAgentSession(agent.Claude); err != nil {
		t.Fatal(err)
	}
	application.selectedModel = "default"
	queue := make(chan func(), 4)
	application.dispatch = func(fn func()) { queue <- fn }
	application.loadModels()
	pumpUpdate(t, queue)
	if application.selectedModel != "provider/one" {
		t.Fatal("legacy default alias remained selected", application.selectedModel)
	}
	tester := native.NewTester(application.view, 1000, 760)
	if err := tester.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if tester.HasText("default（当前选择）") || !tester.HasText("Model One") {
		t.Fatal("legacy alias leaked into model menu")
	}
}

func TestClaudeModelPreferenceMigratesExistingSelection(t *testing.T) {
	application := New(stubBackend{}, "test", "/workspace", nil)
	store := imageStore(t, application)
	if err := store.Save(storage.Snapshot{
		DefaultAgent: agent.Claude, Selected: 1, OpenTabs: []int{1, 2},
		Sessions: []storage.Session{
			{ID: 1, Agent: agent.Claude, Runtime: "/claude", Cwd: "/workspace", SelectedModel: "sonnet"},
			{ID: 2, Agent: agent.Claude, Runtime: "/claude", Cwd: "/other", SelectedModel: "opus"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "/workspace", nil)
	if err := restored.RegisterBackend(agent.Claude, "/claude", &modelCaptureBackend{}); err != nil {
		t.Fatal(err)
	}
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	restored.newSession("/new")
	if restored.selectedModel != "sonnet" {
		t.Fatal("existing active session model was not adopted", restored.selectedModel)
	}
}

func TestModelMenuSelectionIsolationPersistenceAndDetails(t *testing.T) {
	b := &modelCaptureBackend{}
	a := New(b, "test-version", "/workspace", nil)
	store := imageStore(t, a)
	a.models = []codex.Model{{Model: "provider/one", DisplayName: "Model One"}, {Model: "provider/two", DisplayName: "Model Two"}}
	a.modelsLoaded = true
	owner := a.session
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("跟随默认配置") || !tt.HasText("Model One") {
		t.Fatal("model choices missing")
	}
	if err := tt.Click("选择模型 Model One"); err != nil {
		t.Fatal(err)
	}
	if a.selectedModel != "provider/one" || a.modelMenuOpen || !tt.HasText("Model One") {
		t.Fatal("model selection did not close/update")
	}
	a.newSession("/second")
	tt.Frame()
	if a.selectedModel != "provider/one" {
		t.Fatal("new session did not reuse last selected model", a.selectedModel)
	}
	a.selectSession(owner)
	tt.Frame()
	if a.selectedModel != "provider/one" {
		t.Fatal("session lost selection")
	}
	a.State.Busy = true
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	tt.Click("选择模型 Model Two")
	if a.selectedModel != "provider/one" {
		t.Fatal("model changed during active turn")
	}
	a.State.Busy = false
	tt.Frame()
	if err := tt.Click("选择模型 Model Two"); err != nil {
		t.Fatal(err)
	}
	restored := New(b, "test", "/workspace", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.selectedModel != "provider/two" {
		t.Fatal("selection not restored", restored.selectedModel)
	}
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if output := os.Getenv("FUYU_MODELS_SCREENSHOT"); output != "" {
		f, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	tt.Key(0, native.KeyEscape)
	a.State.Policy, a.State.Sandbox = `"never"`, `{"type":"dangerFullAccess"}`
	tt.Click("选择模型")
	tt.Click("运行详情…")
	if !tt.HasText("无需审批") || !tt.HasText("完全访问") || tt.HasText(a.State.Sandbox) {
		t.Fatal("runtime details not readable")
	}
}

func TestModelSelectionUsesExplicitDefaultAndPreservesMixedInput(t *testing.T) {
	ref, _ := imageFixture(t)
	for _, selection := range []string{"provider/one", ""} {
		b := &modelCaptureBackend{request: make(chan modelRequest, 1)}
		a := New(b, "test", "/workspace", nil)
		a.selectedModel = selection
		a.State.ThreadID, a.State.Model = "native", "provider/previous"
		a.State.Draft = "$design task"
		a.State.DraftImages = []attachment.Image{ref}
		a.completion.cwd = a.State.Cwd
		a.completion.chosen = map[string]codex.Skill{"design": {Name: "design", Path: "/design/SKILL.md", Enabled: true}}
		queue := make(chan func(), 4)
		a.dispatch = func(fn func()) { queue <- fn }
		owner := a.session
		a.send()
		a.newSession("/other")
		pumpUpdate(t, queue)
		request := <-b.request
		want := selection
		if want == "" {
			want = "provider/default"
		}
		if request.model != want || len(request.images) != 1 || len(request.skills) != 1 || owner.State.Model != want || a.State.Model != "" {
			t.Fatal("wrong model/input/owner", request, owner.State.Model)
		}
	}
}

func TestModelLoadAndDefaultFailure(t *testing.T) {
	b := &modelCaptureBackend{catalogErr: errors.New("offline"), defaultErr: errors.New("config missing")}
	a := New(b, "test", "/workspace", nil)
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	a.loadModels()
	a.loadModels()
	pumpUpdate(t, queue)
	if a.modelsLoading || a.modelsError == "" {
		t.Fatal("catalog failure hidden")
	}
	b.catalogErr = nil
	a.loadModels()
	pumpUpdate(t, queue)
	if !a.modelsLoaded || a.modelsError != "" {
		t.Fatal("catalog retry failed")
	}
	a.State.ThreadID = "native"
	a.State.Draft = "keep this"
	a.send()
	pumpUpdate(t, queue)
	if a.State.Busy || a.State.Disconnected || a.State.Draft != "keep this" || a.State.Phase != "未发送" {
		t.Fatal("config read failure lost draft or blocked recovery")
	}
}

func TestModelMenuNarrowLayoutAndEscape(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := New(&modelCaptureBackend{}, "test", "/workspace", nil)
		a.modelsLoaded = true
		for _, name := range []string{"GPT-6.1-Sol · Codex", "GPT-6-Astra · Codex", "GLM-5.3-Flash · GLM Coding", "GLM-5.3 · GLM Coding", "deepseek-v4-1-flash-261017 · winning", "DeepSeek V4.1 Flash Free · OpenCode"} {
			a.models = append(a.models, codex.Model{Model: name, DisplayName: name})
		}
		tt := native.NewTester(a.view, 640, 480)
		tt.SetDark(dark)
		if err := tt.Click("选择模型"); err != nil {
			t.Fatal(err)
		}
		for _, label := range []string{"选择模型 GPT-6.1-Sol · Codex", "运行详情…"} {
			r, ok := tt.Find(label)
			if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 640 || r.Y+r.H > 480 {
				t.Fatal("model menu clipped", label, r)
			}
		}
		tt.Key(0, native.KeyEscape)
		if a.modelMenuOpen || a.selectedModel != "" {
			t.Fatal("Escape changed selection or left menu open")
		}
	}
}

func TestModelMenuEffortAndSpeedOptionsSendAndPersist(t *testing.T) {
	b := &modelCaptureBackend{request: make(chan modelRequest, 1)}
	a := New(b, "test", "/workspace", nil)
	store := imageStore(t, a)
	a.models = []codex.Model{
		{Model: "provider/one", DisplayName: "Model One",
			SupportedReasoningEfforts: []agent.ReasoningEffortOption{
				{ReasoningEffort: "low"}, {ReasoningEffort: "medium"}, {ReasoningEffort: "high"}},
			DefaultReasoningEffort: "medium",
			ServiceTiers:           []agent.ModelServiceTier{{ID: "priority", Name: "Fast"}}},
		{Model: "provider/two", DisplayName: "Model Two"},
	}
	a.modelsLoaded = true
	a.selectedModel = "provider/one"
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"低", "中", "高", "标准", "Fast"} {
		if !tt.HasText(label) {
			t.Fatal("missing turn option", label)
		}
	}
	if !tt.HasText("Model One 中") {
		t.Fatal("default effort not shown on the model button")
	}
	if err := tt.Click("推理强度 高"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("速度 Fast"); err != nil {
		t.Fatal(err)
	}
	if a.selectedEffort != "high" || a.selectedTier != "priority" || !a.modelMenuOpen {
		t.Fatal("option choice lost or menu closed", a.selectedEffort, a.selectedTier, a.modelMenuOpen)
	}
	if !tt.HasText("Model One 高") {
		t.Fatal("chosen effort not shown on the model button")
	}
	restored := New(b, "test", "/workspace", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.selectedEffort != "high" || restored.selectedTier != "priority" {
		t.Fatal("options not persisted", restored.selectedEffort, restored.selectedTier)
	}
	if err := tt.Click("选择模型 Model Two"); err != nil {
		t.Fatal(err)
	}
	if a.selectedEffort != "" || a.selectedTier != "" {
		t.Fatal("options survived a model change", a.selectedEffort, a.selectedTier)
	}
	for _, label := range []string{"低", "中", "高"} {
		if tt.HasText("Model Two " + label) {
			t.Fatal("effort shown for a model without catalog entries")
		}
	}
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"低", "标准", "Fast"} {
		if tt.HasText(label) {
			t.Fatal("options rendered for a model without catalog entries", label)
		}
	}
	if err := tt.Click("选择模型 Model One"); err != nil {
		t.Fatal(err)
	}
	if a.selectedEffort != "high" || a.selectedTier != "" || !tt.HasText("Model One 高") {
		t.Fatal("remembered model effort was not restored or tier leaked", a.selectedEffort, a.selectedTier)
	}
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("推理强度 中"); err != nil {
		t.Fatal(err)
	}
	if a.selectedEffort != "medium" || !tt.HasText("Model One 中") {
		t.Fatal("updated model effort not applied", a.selectedEffort)
	}
	if err := tt.Click("选择模型 Model Two"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("选择模型 Model One"); err != nil {
		t.Fatal(err)
	}
	if a.selectedEffort != "medium" || !tt.HasText("Model One 中") {
		t.Fatal("updated model effort not remembered on reselect", a.selectedEffort)
	}
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("推理强度 高"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("速度 Fast"); err != nil {
		t.Fatal(err)
	}
	if output := os.Getenv("FUYU_OPTIONS_SCREENSHOT"); output != "" {
		tt.Frame()
		f, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	tt.Key(0, native.KeyEscape)
	a.State.ThreadID = "native"
	a.State.Draft = "task"
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	a.send()
	pumpUpdate(t, queue)
	req := <-b.request
	if req.options.Model != "provider/one" || req.options.Effort != "high" || req.options.ServiceTier != "priority" {
		t.Fatal("turn options not sent", req.options)
	}
}

func TestStaleTurnOptionsFallBackToModelCapabilities(t *testing.T) {
	a := New(&modelCaptureBackend{}, "test", "/workspace", nil)
	a.selectedModel = "provider/one"
	a.selectedEffort, a.selectedTier = "ultra", "flex"
	if effort, tier := a.sendTurnOptions(); effort != "ultra" || tier != "flex" {
		t.Fatal("unloaded catalog should not rewrite choices", effort, tier)
	}
	a.modelsLoaded = true
	a.models = []codex.Model{{
		Model: "provider/one",
		SupportedReasoningEfforts: []agent.ReasoningEffortOption{
			{ReasoningEffort: "low"}, {ReasoningEffort: "high"}},
		ServiceTiers: []agent.ModelServiceTier{{ID: "priority"}},
	}}
	if effort, tier := a.sendTurnOptions(); effort != "" || tier != "" {
		t.Fatal("unsupported choices were sent", effort, tier)
	}
	a.selectedEffort, a.selectedTier = "high", "default"
	if effort, tier := a.sendTurnOptions(); effort != "high" || tier != "default" {
		t.Fatal("supported choices were dropped", effort, tier)
	}
	a.selectedEffort, a.selectedTier = "ultra", "flex"
	a.selectedModel = "provider/removed"
	if effort, tier := a.sendTurnOptions(); effort != "ultra" || tier != "flex" {
		t.Fatal("missing catalog entry rewrote choices", effort, tier)
	}
}

func TestClaudeModelMenuShowsEffortOptionsAndSendsThem(t *testing.T) {
	claude := &modelCaptureBackend{effortPin: "high", request: make(chan modelRequest, 1)}
	a := New(&modelCaptureBackend{}, "test", "/workspace", nil)
	if err := a.RegisterBackend(agent.Claude, "/claude", claude); err != nil {
		t.Fatal(err)
	}
	store := imageStore(t, a)
	if err := a.newAgentSession(agent.Claude); err != nil {
		t.Fatal(err)
	}
	a.models = []agent.Model{
		{Model: "opus", DisplayName: "fixture/model",
			SupportedReasoningEfforts: []agent.ReasoningEffortOption{
				{ReasoningEffort: "low"}, {ReasoningEffort: "medium"}, {ReasoningEffort: "high"},
				{ReasoningEffort: "xhigh"}, {ReasoningEffort: "max"}}},
		{Model: "sonnet", DisplayName: "zhipu/glm-5.3"},
	}
	a.modelsCwd, a.modelsRuntime = a.State.Cwd, a.runtimeKey
	a.modelsLoaded = true
	a.selectedModel = "opus"
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"低", "中", "高", "极高", "最大"} {
		if !tt.HasText(label) {
			t.Fatal("missing effort option", label)
		}
	}
	if tt.HasText("速度") {
		t.Fatal("service tier row rendered without catalog tiers")
	}
	for _, label := range []string{"低", "中", "高", "极高", "最大"} {
		if tt.HasText("model " + label) {
			t.Fatal("effort shown before any pick or catalog default", label)
		}
	}
	if !tt.HasText("选择不会生效") {
		t.Fatal("effort pin warning not shown")
	}
	if err := tt.Click("推理强度 高"); err != nil {
		t.Fatal(err)
	}
	if a.selectedEffort != "high" || !a.modelMenuOpen {
		t.Fatal("effort choice lost or menu closed", a.selectedEffort)
	}
	if !tt.HasText("model 高") {
		t.Fatal("chosen effort not shown on the model button")
	}
	restored := New(&modelCaptureBackend{}, "test", "/workspace", nil)
	if err := restored.RegisterBackend(agent.Claude, "/claude", claude); err != nil {
		t.Fatal(err)
	}
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.selectedEffort != "high" {
		t.Fatal("effort was not persisted", restored.selectedEffort)
	}
	tt.Key(0, native.KeyEscape)
	a.State.ThreadID = "native"
	a.State.Draft = "task"
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	a.send()
	pumpUpdate(t, queue)
	req := <-claude.request
	if req.options.Model != "opus" || req.options.Effort != "high" || req.options.ServiceTier != "" {
		t.Fatal("turn options not sent to Claude", req.options)
	}
}

func TestInitialModelResolvesToConcreteSessionSelection(t *testing.T) {
	b := &modelCaptureBackend{}
	a := New(b, "test", "/workspace", nil)
	a.rememberModelEffort("provider/default", "medium")
	queue := make(chan func(), 4)
	a.dispatch = func(fn func()) { queue <- fn }
	owner := a.session
	a.resolveInitialModel()
	a.newSession("/other")
	pumpUpdate(t, queue)
	if owner.selectedModel != "provider/default" || owner.selectedEffort != "medium" || a.selectedModel != "" {
		t.Fatal("default resolution changed wrong session or missed remembered effort", owner.selectedModel, owner.selectedEffort, a.selectedModel)
	}
	a.resolveInitialModel()
	a.selectedModel = "provider/one"
	pumpUpdate(t, queue)
	if a.selectedModel != "provider/one" {
		t.Fatal("late default replaced explicit selection")
	}
}

func TestModelEffortBindingAcrossSessionsAgentsAndMigration(t *testing.T) {
	codexBackend := &modelCaptureBackend{}
	claudeBackend := &modelCaptureBackend{}
	a := New(codexBackend, "test", "/workspace", nil)
	if err := a.RegisterBackend(agent.Claude, "/claude", claudeBackend); err != nil {
		t.Fatal(err)
	}
	store := imageStore(t, a)
	if err := store.Save(storage.Snapshot{
		DefaultAgent: agent.Claude, Selected: 2, OpenTabs: []int{1, 2},
		Sessions: []storage.Session{
			{ID: 1, Agent: agent.Codex, Cwd: "/workspace", SelectedModel: "provider/one", SelectedEffort: "medium"},
			{ID: 2, Agent: agent.Claude, Runtime: "/claude", Cwd: "/workspace", SelectedModel: "sonnet", SelectedEffort: "high"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	restored := New(codexBackend, "test", "/workspace", nil)
	if err := restored.RegisterBackend(agent.Claude, "/claude", claudeBackend); err != nil {
		t.Fatal(err)
	}
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	restored.newSession("/claude-next")
	if restored.selectedModel != "sonnet" || restored.selectedEffort != "high" {
		t.Fatal("new Claude session did not inherit bound model effort", restored.selectedModel, restored.selectedEffort)
	}
	if err := restored.newAgentSession(agent.Codex); err != nil {
		t.Fatal(err)
	}
	restored.models = []codex.Model{
		{Model: "provider/one", DisplayName: "Model One",
			SupportedReasoningEfforts: []agent.ReasoningEffortOption{{ReasoningEffort: "low"}, {ReasoningEffort: "medium"}, {ReasoningEffort: "high"}},
			DefaultReasoningEffort:    "low"},
		{Model: "sonnet", DisplayName: "Codex Sonnet",
			SupportedReasoningEfforts: []agent.ReasoningEffortOption{{ReasoningEffort: "low"}, {ReasoningEffort: "high"}}},
	}
	restored.modelsLoaded = true
	tt := native.NewTester(restored.view, 1000, 760)
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("选择模型 Model One"); err != nil {
		t.Fatal(err)
	}
	if restored.selectedEffort != "medium" || !tt.HasText("Model One 中") {
		t.Fatal("migrated Codex model effort was not restored", restored.selectedEffort)
	}
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("选择模型 Codex Sonnet"); err != nil {
		t.Fatal(err)
	}
	if restored.selectedEffort != "" {
		t.Fatal("Claude effort binding leaked across Agent boundary", restored.selectedEffort)
	}
}

func TestModelPickerShowsDisplayNameForResolvedModelIDs(t *testing.T) {
	a := New(&modelCaptureBackend{}, "test", "/workspace", nil)
	a.models = []codex.Model{
		{Model: "swe-2-high", DisplayName: "SWE-2",
			SupportedReasoningEfforts: []agent.ReasoningEffortOption{
				{ReasoningEffort: "medium"}, {ReasoningEffort: "high"}, {ReasoningEffort: "max"}},
			DefaultReasoningEffort: "high"},
		{Model: "swe-2-flash-medium", DisplayName: "SWE-2 Flash",
			SupportedReasoningEfforts: []agent.ReasoningEffortOption{
				{ReasoningEffort: "medium"}, {ReasoningEffort: "high"}, {ReasoningEffort: "max"}},
			DefaultReasoningEffort: "medium"},
	}
	a.modelsLoaded = true
	a.selectedModel = "swe-2-high"
	tt := native.NewTester(a.view, 1000, 760)
	if !tt.HasText("SWE-2 高") || tt.HasText("swe-2-high") {
		t.Fatal("resolved model id leaked into the model button")
	}
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("SWE-2 Flash") || tt.HasText("swe-2-flash-medium") {
		t.Fatal("resolved model id leaked into the model menu")
	}
	for _, label := range []string{"中", "高", "最大"} {
		if !tt.HasText(label) {
			t.Fatal("missing effort chip", label)
		}
	}
	if err := tt.Click("选择模型 SWE-2 Flash"); err != nil {
		t.Fatal(err)
	}
	if a.selectedModel != "swe-2-flash-medium" || !tt.HasText("SWE-2 Flash 中") {
		t.Fatal("selection did not apply the catalog display name", a.selectedModel)
	}
}

func TestModelButtonDropsProviderSuffix(t *testing.T) {
	a := New(&modelCaptureBackend{}, "test", "/workspace", nil)
	a.models = []codex.Model{
		{Model: "gpt-6-1-sol", DisplayName: "GPT-6.1-Sol · Codex",
			SupportedReasoningEfforts: []agent.ReasoningEffortOption{{ReasoningEffort: "medium"}},
			DefaultReasoningEffort:    "medium"},
	}
	a.modelsLoaded = true
	a.selectedModel = "gpt-6-1-sol"
	tt := native.NewTester(a.view, 1000, 760)
	if !tt.HasText("GPT-6.1-Sol 中") || tt.HasText("GPT-6.1-Sol · Codex") {
		t.Fatal("provider suffix leaked into the model button")
	}
	if err := tt.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("GPT-6.1-Sol · Codex") {
		t.Fatal("menu row should keep the provider suffix")
	}
}

func TestModelPickerKeepsDisplayNameAcrossRestart(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			application := New(&modelCaptureBackend{}, "test", "/workspace", nil)
			application.kind = kind
			store := imageStore(t, application)
			application.selectedModel = "devin/swe-2"
			application.selectedEffort = "high"
			catalog := &modelCatalogEntry{
				loaded: true,
				models: []agent.Model{{Model: "devin/swe-2", DisplayName: "SWE-2 · Devin",
					SupportedReasoningEfforts: []agent.ReasoningEffortOption{{ReasoningEffort: "high"}}}},
			}
			application.applyModelCatalog(application.completionCacheKey(), catalog)
			if err := application.persist(); err != nil {
				t.Fatal(err)
			}
			restored := New(&modelCaptureBackend{}, "test", "/workspace", nil)
			if err := restored.AttachStore(store); err != nil {
				t.Fatal(err)
			}
			if restored.modelsLoaded || restored.selectedModel != "devin/swe-2" {
				t.Fatal("restoring a display name changed model availability or identity")
			}
			tester := native.NewTester(restored.view, 1000, 760)
			if !tester.HasText("SWE-2 高") || tester.HasText("swe-2 高") {
				t.Fatal("startup did not preserve the catalog display name")
			}
			restored.applyModelCatalog(restored.completionCacheKey(), catalog)
			tester.Frame()
			if !tester.HasText("SWE-2 高") {
				t.Fatal("loading the catalog changed the displayed model name")
			}
			if err := tester.Click("选择模型"); err != nil {
				t.Fatal(err)
			}
			if !tester.HasText("SWE-2 · Devin") {
				t.Fatal("menu lost the catalog display name")
			}
		})
	}
}

func TestModelPickerWaitsForUncachedName(t *testing.T) {
	application := New(&modelCaptureBackend{}, "test", "/workspace", nil)
	application.selectedModel = "devin/swe-2"
	application.selectedEffort = "high"
	application.modelsLoading = true
	tester := native.NewTester(application.view, 1000, 760)
	if !tester.HasText("读取模型… 高") || tester.HasText("swe-2 高") {
		t.Fatal("uncached model id flashed before its display name arrived")
	}
	application.applyModelCatalog(application.completionCacheKey(), &modelCatalogEntry{
		loaded: true,
		models: []agent.Model{{Model: "devin/swe-2", DisplayName: "SWE-2 · Devin",
			SupportedReasoningEfforts: []agent.ReasoningEffortOption{{ReasoningEffort: "high"}}}},
	})
	tester.Frame()
	if !tester.HasText("SWE-2 高") || application.selectedModel != "devin/swe-2" {
		t.Fatal("catalog display name was not applied while preserving model identity")
	}
}

func TestModelDisplayNameCacheIsScoped(t *testing.T) {
	application := New(&modelCaptureBackend{}, "test", "/workspace", nil)
	application.selectedModel = "devin/swe-2"
	application.models = []agent.Model{{Model: "devin/swe-2", DisplayName: "SWE-2 · Devin"}}
	application.rememberModel(application.selectedModel)
	application.models = nil
	if application.selectedModelDisplayName() != "SWE-2 · Devin" {
		t.Fatal("selected model display name was not cached")
	}
	for _, key := range []agent.CacheKey{
		{Agent: agent.Claude, Cwd: "/workspace"},
		{Agent: agent.Codex, Runtime: "/other", Cwd: "/workspace"},
		{Agent: agent.Codex, Cwd: "/other"},
	} {
		bound := application.forSession(&session{kind: key.Agent, runtimeKey: key.Runtime, selectedModel: "devin/swe-2"})
		bound.State.Cwd = key.Cwd
		if bound.selectedModelDisplayName() != "" {
			t.Fatal("model display name leaked across catalog scope", key)
		}
	}
}
