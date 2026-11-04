package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func catalogMarker(t *testing.T, cwd, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(cwd, name), nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func catalogStarts(t *testing.T, cwd string, want int) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(cwd, "catalog-starts"))
	if err != nil || strings.Count(string(raw), "initialize\n") != want {
		t.Fatalf("initializations=%q error=%v want=%d", raw, err, want)
	}
}

func TestCatalogSharesProbeAndPublishesModelsBeforeSkills(t *testing.T) {
	client := fixtureClient(t)
	cwd := t.TempDir()
	catalogMarker(t, cwd, "record-catalog")
	catalogMarker(t, cwd, "skills-wait")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, err := client.ListModelsForDirectory(ctx, cwd)
	if err != nil || len(models) != 1 {
		t.Fatalf("models waited for blocked skills: %v %v", models, err)
	}
	if levels := models[0].SupportedReasoningEfforts; len(levels) != 5 || levels[0].ReasoningEffort != "low" || levels[4].ReasoningEffort != "max" {
		t.Fatal("effort levels were not parsed", levels)
	}
	models[0].Model = "modified"
	model, err := client.DefaultModel(ctx, cwd)
	if err != nil || model != "opus" {
		t.Fatalf("shared model was changed: %s %v", model, err)
	}
	// Canceling a candidate-list reader must leave another session's probe alive.
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := client.ListCommands(canceled, cwd); err != context.Canceled {
		t.Fatal("canceled reader did not return", err)
	}
	if err := os.Remove(filepath.Join(cwd, "skills-wait")); err != nil {
		t.Fatal(err)
	}
	commands, err := client.ListCommands(ctx, cwd)
	if err != nil || len(commands) == 0 {
		t.Fatal("shared probe did not finish", err)
	}
	commands[0].Arguments[0] = "modified"
	again, err := client.ListCommands(ctx, cwd)
	if err != nil || again[0].Arguments[0] == "modified" {
		t.Fatal("command cache shares mutable arguments", err)
	}
	catalogStarts(t, cwd, 1)
	if _, err := client.ReloadCommands(ctx, cwd); err != nil {
		t.Fatal(err)
	}
	catalogStarts(t, cwd, 2)
	other := t.TempDir()
	catalogMarker(t, other, "record-catalog")
	if _, err := client.ListCommands(ctx, other); err != nil {
		t.Fatal(err)
	}
	catalogStarts(t, other, 1)
}

func TestCatalogSkillFailureKeepsModelsAndExplicitRetry(t *testing.T) {
	client := fixtureClient(t)
	cwd := t.TempDir()
	catalogMarker(t, cwd, "record-catalog")
	catalogMarker(t, cwd, "skills-unavailable")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.ListCommands(ctx, cwd); err == nil {
		t.Fatal("skill error was hidden")
	}
	if models, err := client.ListModelsForDirectory(ctx, cwd); err != nil || len(models) == 0 {
		t.Fatal("skill error blocked valid models", err)
	}
	catalogStarts(t, cwd, 1)
	if err := os.Remove(filepath.Join(cwd, "skills-unavailable")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReloadCommands(ctx, cwd); err != nil {
		t.Fatal("explicit retry reused failed result", err)
	}
	catalogStarts(t, cwd, 2)
}

func TestCatalogCloseCancelsAndReapsBlockedProbe(t *testing.T) {
	client := fixtureClient(t)
	cwd := t.TempDir()
	catalogMarker(t, cwd, "skills-wait")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.ListModelsForDirectory(ctx, cwd); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() { client.Close(); close(closed) }()
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("closing did not reap catalog probe")
	}
	if _, err := client.ListCommands(ctx, cwd); err == nil {
		t.Fatal("closed client started another probe")
	}
}
