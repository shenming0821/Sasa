package ui

import (
	"context"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/storage"
)

type sharedModelsBackend struct {
	stubBackend
	requests chan string
	results  chan []agent.Model
}

func TestModelCatalogRestoresOptionsAndRefreshesSilently(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			backend := &sharedModelsBackend{requests: make(chan string, 8), results: make(chan []agent.Model, 8)}
			var codexBackend Backend = backend
			if kind == agent.Claude {
				codexBackend = nil
			}
			first := New(codexBackend, "test", "/project", nil)
			store := imageStore(t, first)
			jobs := make(chan func(), 8)
			first.dispatch = func(fn func()) { jobs <- fn }
			if kind == agent.Claude {
				if err := first.RegisterBackend(kind, "/claude", backend); err != nil {
					t.Fatal(err)
				}
				first.newSessionForAgent("/project", kind, "/claude")
			}
			first.loadModels()
			<-backend.requests
			backend.results <- []agent.Model{{
				Model: "cached", DisplayName: "Cached Model", IsDefault: true,
				SupportedReasoningEfforts: []agent.ReasoningEffortOption{{ReasoningEffort: "medium"}, {ReasoningEffort: "high"}},
				DefaultReasoningEffort:    "medium",
				ServiceTiers:              []agent.ModelServiceTier{{ID: "priority", Name: "Fast"}},
			}}
			pumpUpdate(t, jobs)
			first.catalogWrites.Wait()
			first.rememberModelEffort("cached", "high")
			if err := first.persist(); err != nil {
				t.Fatal(err)
			}
			restored := New(nil, "test", "/project", nil)
			restored.dispatch = first.dispatch
			t.Cleanup(func() {
				close(first.shutdown)
				close(restored.shutdown)
				restored.catalogWrites.Wait()
			})
			if err := restored.AttachStore(store); err != nil {
				t.Fatal(err)
			}
			restored.loadCatalogDiskCaches()
			pumpUpdate(t, jobs)
			tester := native.NewTester(restored.view, 1000, 760)
			if !restored.State.Disconnected || !tester.HasText("Cached Model high") {
				t.Fatal("persisted catalog was not visible before runtime connection")
			}
			if err := tester.Click("选择模型"); err != nil {
				t.Fatal(err)
			}
			if !tester.HasText("推理强度 high") || !tester.HasText("速度 default") {
				t.Fatal("cached options were hidden before runtime connection")
			}
			restored.installRuntime(RuntimeConnection{Kind: kind, Runtime: first.runtimeKey, Backend: backend})
			<-backend.requests
			options := restored.selectedTurnOptions()
			if restored.modelsLoading || options.Model != "cached" || options.Effort != "high" {
				t.Fatal("background refresh hid the cached model")
			}
			backend.results <- nil
			pumpUpdate(t, jobs)
			if restored.modelsError != "" || restored.models[0].Model != "cached" || store.LoadModelCatalogs()[0].Models[0].Model != "cached" {
				t.Fatal("failed background refresh interrupted or replaced the cached result")
			}
			restored.loadModelCatalog(true)
			<-backend.requests
			if !restored.modelsLoading || restored.models[0].Model != "cached" {
				t.Fatal("manual refresh did not preserve available models")
			}
			backend.results <- []agent.Model{{Model: "new", DisplayName: "New Model"}}
			pumpUpdate(t, jobs)
			restored.catalogWrites.Wait()
			if restored.models[0].Model != "new" || store.LoadModelCatalogs()[0].Models[0].Model != "new" {
				t.Fatal("successful refresh did not replace the cached result")
			}
			restored.applyModelDiskCache([]storage.Catalog{{
				Key: restored.completionCacheKey(), Models: []agent.Model{{Model: "old"}}, Checked: time.Now().Add(-time.Hour),
			}})
			if restored.models[0].Model != "new" {
				t.Fatal("late disk read replaced a newer runtime result")
			}
		})
	}
}

func TestSendUsesCachedModelWhenDefaultLookupIsUnavailable(t *testing.T) {
	backend := &modelCaptureBackend{request: make(chan modelRequest, 1), defaultErr: context.DeadlineExceeded}
	a := New(backend, "test", "/project", nil)
	a.applyModelDiskCache([]storage.Catalog{
		{Key: agent.CacheKey{Agent: agent.Codex, Cwd: "/elsewhere"}, Models: []agent.Model{{Model: "foreign", IsDefault: true}}},
		{Key: a.completionCacheKey(), Models: []agent.Model{{
			Model: "cached", IsDefault: true,
			SupportedReasoningEfforts: []agent.ReasoningEffortOption{{ReasoningEffort: "high"}},
			ServiceTiers:              []agent.ModelServiceTier{{ID: "priority"}},
		}}},
	})
	owner := a.session
	a.newSession("/uncached")
	if a.turnModel() != "" || a.modelsLoaded {
		t.Fatal("cached catalog leaked into a different directory")
	}
	a.selectSession(owner)
	a.selectedEffort, a.selectedTier = "high", "priority"
	a.State.ThreadID, a.State.Draft = "native", "send while refreshing"
	jobs := make(chan func(), 8)
	a.dispatch = func(fn func()) { jobs <- fn }
	a.send()
	pumpUpdate(t, jobs)
	select {
	case request := <-backend.request:
		if request.model != "cached" || request.options.Effort != "high" || request.options.ServiceTier != "priority" {
			t.Fatal("send lost cached model options", request)
		}
	default:
		t.Fatalf("cached model did not reach the runtime: %s", a.State.Error)
	}
	if a.rememberedModelEffort(agent.Codex, "", "cached") != "high" {
		t.Fatal("accepted request did not remember its concrete effort")
	}
}

func TestModelPickerLoadsCatalogAfterRuntimeStartup(t *testing.T) {
	backend := &sharedModelsBackend{requests: make(chan string, 8), results: make(chan []agent.Model, 8)}
	application := New(nil, "test", "/project", nil)
	t.Cleanup(func() { close(application.shutdown) })
	jobs := make(chan func(), 8)
	application.dispatch = func(fn func()) { jobs <- fn }
	application.selectedModel = "provider/previous"
	tester := native.NewTester(application.view, 1000, 760)
	application.installRuntime(RuntimeConnection{Kind: agent.Codex, Backend: backend})
	if err := tester.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	select {
	case cwd := <-backend.requests:
		if cwd != "/project" {
			t.Fatal("wrong model directory", cwd)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opening the picker did not request models")
	}
	backend.results <- []agent.Model{{Model: "provider/one", DisplayName: "Model One"}}
	pumpUpdate(t, jobs)
	tester.Frame()
	if !tester.HasText("Model One") {
		t.Fatal("runtime model catalog did not reach the picker")
	}
}

func TestModelPickerRecoversEmptyCatalogWithoutRestart(t *testing.T) {
	for _, empty := range [][]agent.Model{nil, {{Model: "hidden", Hidden: true}}, {{ID: "missing-model"}}} {
		backend := &sharedModelsBackend{requests: make(chan string, 8), results: make(chan []agent.Model, 8)}
		application := New(backend, "test", "/project", nil)
		t.Cleanup(func() { close(application.shutdown) })
		jobs := make(chan func(), 8)
		application.dispatch = func(fn func()) { jobs <- fn }
		application.selectedModel = "provider/previous"
		application.loadModels()
		<-backend.requests
		backend.results <- empty
		pumpUpdate(t, jobs)
		if application.modelsLoaded || application.modelsError == "" {
			t.Fatal("empty catalog was cached as a successful model list")
		}
		tester := native.NewTester(application.view, 1000, 760)
		if err := tester.Click("选择模型"); err != nil {
			t.Fatal(err)
		}
		<-backend.requests
		backend.results <- []agent.Model{{Model: "provider/one", DisplayName: "Model One"}}
		pumpUpdate(t, jobs)
		tester.Frame()
		if !tester.HasText("Model One") || application.modelsError != "" || application.selectedModel != "provider/previous" {
			t.Fatal("reopening the picker did not recover the catalog while preserving selection")
		}
	}
}

func TestModelPickerRefreshPreservesAvailableModels(t *testing.T) {
	backend := &sharedModelsBackend{requests: make(chan string, 8), results: make(chan []agent.Model, 8)}
	application := New(backend, "test", "/project", nil)
	t.Cleanup(func() { close(application.shutdown) })
	jobs := make(chan func(), 8)
	application.dispatch = func(fn func()) { jobs <- fn }
	application.loadModels()
	<-backend.requests
	backend.results <- []agent.Model{{Model: "provider/one", DisplayName: "Model One"}}
	pumpUpdate(t, jobs)
	application.State.Busy = true
	tester := native.NewTester(application.view, 1000, 760)
	if err := tester.Click("选择模型"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("刷新模型列表"); err != nil {
		t.Fatal(err)
	}
	<-backend.requests
	backend.results <- nil
	pumpUpdate(t, jobs)
	tester.Frame()
	if !tester.HasText("Model One") || !application.modelsLoaded || application.modelsError == "" {
		t.Fatal("unsuccessful refresh replaced the available catalog")
	}
	if err := tester.Click("重试读取模型"); err != nil {
		t.Fatal(err)
	}
	<-backend.requests
	backend.results <- []agent.Model{{Model: "provider/two", DisplayName: "Model Two"}}
	pumpUpdate(t, jobs)
	tester.Frame()
	if !tester.HasText("Model Two") || tester.HasText("Model One") || application.modelsError != "" {
		t.Fatal("manual retry did not apply the recovered model catalog")
	}
}

func (b *sharedModelsBackend) ListModels(ctx context.Context) ([]agent.Model, error) {
	return b.ListModelsForDirectory(ctx, "")
}

func (b *sharedModelsBackend) ListModelsForDirectory(ctx context.Context, cwd string) ([]agent.Model, error) {
	select {
	case b.requests <- cwd:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case result := <-b.results:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestModelPrewarmSharesPendingAndCompletedCatalogs(t *testing.T) {
	b := &sharedModelsBackend{requests: make(chan string, 8), results: make(chan []agent.Model, 8)}
	a := New(b, "test", "/first", nil)
	t.Cleanup(func() { close(a.shutdown) })
	jobs := make(chan func(), 8)
	a.dispatch = func(fn func()) { jobs <- fn }
	a.prewarmModels()
	if cwd := <-b.requests; cwd != "/first" {
		t.Fatal("wrong prewarm directory", cwd)
	}
	first := a.session
	a.newSession("/first")
	second := a.session
	a.loadModels()
	if !first.modelsLoading || !second.modelsLoading || len(b.requests) != 0 {
		t.Fatal("same-directory sessions duplicated in-flight model request")
	}
	b.results <- []agent.Model{{Model: "one"}}
	pumpUpdate(t, jobs)
	if !first.modelsLoaded || !second.modelsLoaded || first.models[0].Model != "one" {
		t.Fatal("model result did not reach both owners")
	}
	a.newSession("/first")
	a.loadModels()
	if !a.modelsLoaded || len(b.requests) != 0 {
		t.Fatal("new session did not reuse completed model catalog")
	}
	a.newSession("/second")
	a.loadModels()
	if cwd := <-b.requests; cwd != "/second" {
		t.Fatal("model cache crossed directories")
	}
	b.results <- []agent.Model{{Model: "two"}}
	pumpUpdate(t, jobs)
	if a.models[0].Model != "two" || first.models[0].Model != "one" {
		t.Fatal("directory-specific model results were mixed")
	}
}
