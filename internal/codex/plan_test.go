package codex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
)

func TestPlanModeUsesNativeInstructionsAndExplicitExit(t *testing.T) {
	c := fixture(t, "plan-mode")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, mode := range []agent.InteractionMode{agent.ModePlan, agent.ModeDefault} {
		turn, err := c.SendWithOptions(ctx, "thread", "task", nil, nil,
			agent.TurnOptions{Model: "wire-model", Effort: "high", Mode: mode})
		if err != nil || turn.ID != string(mode) {
			t.Fatal("native collaboration mode missing", turn, err)
		}
	}
	_, err := c.SendWithOptions(ctx, "thread", "task", nil, nil, agent.TurnOptions{Mode: "invalid"})
	var rejected *agent.NotSubmittedError
	if !errors.As(err, &rejected) {
		t.Fatal("invalid mode reached the runtime", err)
	}
}
