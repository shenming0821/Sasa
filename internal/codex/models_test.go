package codex

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/attachment"
)

func TestModelCatalogDefaultAndMixedInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := fixture(t, "models")
	models, err := c.ListModels(ctx)
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
	if _, err = cycle.ListModels(ctx); err == nil {
		t.Fatal("repeated cursor was accepted")
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
	if os.Getenv("FUYU_LIVE_MODELS") != "1" {
		t.Skip("set FUYU_LIVE_MODELS=1")
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
