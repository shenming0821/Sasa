package ui

import (
	"context"
	"testing"

	"github.com/shenming0821/Fuyu/internal/agent"
)

type sharedModelsBackend struct {
	stubBackend
	requests chan string
	results  chan []agent.Model
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
