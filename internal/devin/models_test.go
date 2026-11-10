package devin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func catalogFixture(t *testing.T) map[string]*configOption {
	t.Helper()
	raw := json.RawMessage(`[
		{"id": "model", "type": "select", "currentValue": "swe-2-high", "options": [
			{"value": "adaptive", "name": "Adaptive"},
			{"value": "swe-2-high", "name": "SWE-2"},
			{"value": "swe-2-flash-medium", "name": "SWE-2 Flash"},
			{"value": "glm-5-3-max", "name": "GLM-5.3"},
			{"value": "glm-5-2", "name": "GLM-5.2 High"}
		]},
		{"id": "thought_level", "type": "select", "currentValue": "high", "options": [
			{"value": "medium", "name": "Medium"},
			{"value": "high", "name": "High"},
			{"value": "max", "name": "Max"}
		]},
		{"id": "speed", "type": "select", "currentValue": "standard", "options": [
			{"value": "standard", "name": "Standard"},
			{"value": "fast", "name": "Fast"}
		]}
	]`)
	return parseConfigOptions(raw)
}

func TestCatalogModelsKeepsServerOrderAndDerivesEffortDefault(t *testing.T) {
	models := catalogModels(catalogFixture(t))
	want := []struct {
		model, name, effort string
	}{
		{"adaptive", "Adaptive", ""},
		{"swe-2-high", "SWE-2", "high"},
		{"swe-2-flash-medium", "SWE-2 Flash", "medium"},
		{"glm-5-3-max", "GLM-5.3", "max"},
		{"glm-5-2", "GLM-5.2 High", ""},
	}
	if len(models) != len(want) {
		t.Fatalf("model count %d", len(models))
	}
	for i, w := range want {
		m := models[i]
		if m.Model != w.model || m.DisplayName != w.name || m.DefaultReasoningEffort != w.effort {
			t.Fatalf("entry %d: %+v", i, m)
		}
		if len(m.SupportedReasoningEfforts) != 3 || m.SupportedReasoningEfforts[0].ReasoningEffort != "medium" {
			t.Fatalf("effort list not in server order: %+v", m.SupportedReasoningEfforts)
		}
		if len(m.ServiceTiers) != 1 || m.ServiceTiers[0].ID != "fast" {
			t.Fatalf("standard tier should fold into the built-in baseline chip: %+v", m.ServiceTiers)
		}
	}
	if !models[1].IsDefault {
		t.Fatal("currentValue should mark the default model")
	}
}

func TestApplyOptionsMapsBaselineTierToStandard(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, err := c.StartThread(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, "fuyu/runtime")
	_, err = c.SendWithOptions(ctx, thread.Thread.ID, "hi", nil, nil, agent.TurnOptions{ServiceTier: "default"})
	if err != nil {
		t.Fatalf("baseline tier should map to standard, got %v", err)
	}
	collectUntil(t, c, "turn/completed")
	c.mu.Lock()
	current := c.sessions["acp-session-1"].options["speed"].Current
	c.mu.Unlock()
	if current != "standard" {
		t.Fatalf("baseline tier sent %q instead of standard", current)
	}
	if _, err := c.SendWithOptions(ctx, thread.Thread.ID, "hi", nil, nil, agent.TurnOptions{ServiceTier: "fast"}); err != nil {
		t.Fatal(err)
	}
}
