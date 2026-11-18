package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
)

func TestLiveCodexPlanThenImplement(t *testing.T) {
	if os.Getenv("SASA_LIVE_PLAN") != "1" {
		t.Skip("set SASA_LIVE_PLAN=1 for isolated real plan/execute verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dir := t.TempDir()
	c, err := Start(ctx, "codex", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var thread Thread
	if err := c.Call(ctx, "thread/start", map[string]any{
		"cwd": dir, "ephemeral": true, "sandbox": "workspace-write", "approvalPolicy": "never",
	}, &thread); err != nil {
		t.Fatal(err)
	}
	t.Logf("model=%s approvalPolicy=%s sandbox=%s", thread.Model, thread.ApprovalPolicy, thread.Sandbox)
	wait := func(turn Turn) bool {
		t.Helper()
		planSeen := false
		for {
			select {
			case event, ok := <-c.Events():
				if !ok || event.Method == "sasa/disconnected" {
					t.Fatal("runtime disconnected", event.Err)
				}
				if len(event.ID) > 0 {
					t.Fatal("unexpected interaction in a fully specified isolated task", event.Method)
				}
				var p struct {
					Turn Turn                        `json:"turn"`
					Item struct{ Type, Text string } `json:"item"`
				}
				_ = json.Unmarshal(event.Params, &p)
				if event.Method == "item/completed" && p.Item.Type == "plan" && p.Item.Text != "" {
					planSeen = true
				}
				if event.Method == "turn/completed" && p.Turn.ID == turn.ID {
					if p.Turn.Status != "completed" {
						t.Fatal("turn failed", p.Turn.Status, p.Turn.Error)
					}
					return planSeen
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	first, err := c.SendWithOptions(ctx, thread.Thread.ID,
		"Create a single file named plan-check.txt in the working directory with exactly the text plan-ok followed by a newline. No other changes. All requirements are specified; choose implementation details yourself.",
		nil, nil, agent.TurnOptions{Model: thread.Model, Mode: agent.ModePlan})
	if err != nil {
		t.Fatal(err)
	}
	if !wait(first) {
		t.Fatal("native final plan item missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "plan-check.txt")); !os.IsNotExist(err) {
		t.Fatal("planning modified the workspace", err)
	}
	t.Log("plan completed with native plan item; target file absent")
	second, err := c.SendWithOptions(ctx, thread.Thread.ID,
		"Implement the plan now. Create only plan-check.txt containing exactly plan-ok followed by a newline.",
		nil, nil, agent.TurnOptions{Model: thread.Model, Mode: agent.ModeDefault})
	if err != nil {
		t.Fatal(err)
	}
	wait(second)
	raw, err := os.ReadFile(filepath.Join(dir, "plan-check.txt"))
	if err != nil || string(raw) != "plan-ok\n" {
		t.Fatal("explicit execution did not implement the plan", string(raw), err)
	}
	t.Log("explicit default-mode continuation created the expected file")
}
