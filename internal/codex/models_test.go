package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/attachment"
)

func TestEmptyModelCatalogRetriesAndRecovers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := fixture(t, "models-empty-recover")
	models, err := client.readModelCatalog(ctx)
	if err != nil || len(models) != 1 || models[0].Model != "recovered-model" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	if client.nextID.Load() != 2 {
		t.Fatal("expected one retry")
	}
}

func TestEmptyModelCatalogStopsAfterOneRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := fixture(t, "models-empty")
	if _, err := client.readModelCatalog(ctx); err == nil || !strings.Contains(err.Error(), "Codex 未返回可用模型") {
		t.Fatal(err)
	}
	if client.nextID.Load() != 2 {
		t.Fatal("unbounded model retries")
	}
}

func TestEmptyModelCatalogRetryHonorsCancellation(t *testing.T) {
	client := fixture(t, "models-empty")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := client.readModelCatalog(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if client.nextID.Load() != 1 {
		t.Fatal("retried after cancellation")
	}
}

func TestEmptyModelCatalogReportsNativeRefreshTimeout(t *testing.T) {
	client := fixture(t, "models-empty-timeout")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.readModelCatalog(ctx)
	if err == nil || !strings.Contains(err.Error(), "请求超时") || strings.Contains(err.Error(), "secret-token") {
		t.Fatal(err)
	}
}

func TestModelCatalogDefaultAndMixedInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := fixture(t, "models")
	models, err := c.readModelCatalog(ctx)
	if err != nil || len(models) != 2 || models[0].Model != "wire-model" || models[0].ID == models[0].Model {
		t.Fatal(models, err)
	}
	model, err := c.DefaultModel(ctx, "/workspace")
	if err != nil || model != "provider/config-model" {
		t.Fatal(model, err)
	}
	turn, err := c.SendWithModel(ctx, "thread", "task", []Skill{{Name: "skill", Path: "/skill"}}, []attachment.Image{{Path: "/image.png"}}, models[0].Model)
	if err != nil || turn.ID != "model-turn" {
		t.Fatal(turn, err)
	}
	fallback := fixture(t, "models-default")
	model, err = fallback.DefaultModel(ctx, "/workspace")
	if err != nil || model != "wire-model" {
		t.Fatal(model, err)
	}
	cycle := fixture(t, "models-cycle")
	if _, err = cycle.readModelCatalog(ctx); err == nil {
		t.Fatal("repeated cursor was accepted")
	}
}

func TestListModelsRefreshesWithoutReplacingSessionServer(t *testing.T) {
	cwd := t.TempDir()
	executable := filepath.Join(cwd, "codex")
	t.Setenv("SASA_CATALOG_TEST_EXECUTABLE", os.Args[0])
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec \"$SASA_CATALOG_TEST_EXECUTABLE\" -test.run='^TestProtocolHelper$' -- models-snapshot\n"), 0700); err != nil {
		t.Fatal(err)
	}
	writeModel := func(model string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cwd, "catalog"), []byte(model), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeModel("previous")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Start(ctx, executable, cwd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	writeModel("current")
	models, err := client.ListModels(ctx)
	if err != nil || len(models) != 1 || models[0].Model != "current" {
		t.Fatalf("refreshed models=%v err=%v", models, err)
	}
	models, err = client.readModelCatalog(ctx)
	if err != nil || len(models) != 1 || models[0].Model != "previous" {
		t.Fatalf("original server no longer responds with its own snapshot: models=%v err=%v", models, err)
	}
}

func TestSendOptionsPassEffortAndServiceTier(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := fixture(t, "options")
	turn, err := c.SendWithOptions(ctx, "thread", "task", nil, nil,
		agent.TurnOptions{Model: "wire-model", Effort: "high", ServiceTier: "priority"})
	if err != nil || turn.ID != "option-turn" {
		t.Fatal(turn, err)
	}
}

// Reads native discovery/config only; no thread, turn or model invocation.
func TestLiveModelsReadOnly(t *testing.T) {
	if os.Getenv("SASA_LIVE_MODELS") != "1" {
		t.Skip("set SASA_LIVE_MODELS=1")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c, err := Start(ctx, "codex", cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	models, err := c.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	model, err := c.DefaultModel(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native catalog count=%d effective default model=%s", len(models), model)
	for _, m := range models {
		t.Logf("model=%s display=%s default=%v", m.Model, m.DisplayName, m.IsDefault)
	}
}
