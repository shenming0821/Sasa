package ui

import (
	"context"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
)

type sharedModelsBackend struct {
	stubBackend
	requests chan string
	results  chan []agent.Model
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
