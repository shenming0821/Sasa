package claude

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func TestPlanModeSwitchesReusedProcessBeforeUserFrame(t *testing.T) {
	client, cwd := persistentFixture(t)
	if err := os.WriteFile(filepath.Join(client.home, "settings.json"), []byte(`{"permissions":{"defaultMode":"acceptEdits"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, _ := client.StartThread(ctx, cwd)
	for _, mode := range []agent.InteractionMode{"", agent.ModePlan, agent.ModePlan, agent.ModeDefault} {
		if _, err := client.SendWithOptions(ctx, thread.Thread.ID, "ordinary", nil, nil, agent.TurnOptions{Mode: mode}); err != nil {
			t.Fatal(err)
		}
		waitTerminal(t, client, thread.Thread.ID)
	}
	raw, err := os.ReadFile(filepath.Join(cwd, "mode-turns"))
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	for index, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var frame struct {
			Mode string
			PID  int
		}
		if json.Unmarshal([]byte(line), &frame) != nil {
			t.Fatal("invalid recorded user frame")
		}
		expected := []string{"acceptEdits", "plan", "plan", "acceptEdits"}[index]
		if frame.Mode != expected || index > 0 && frame.PID != pid {
			t.Fatal("wrong mode or process", frame, expected)
		}
		pid = frame.PID
	}
	raw, _ = os.ReadFile(filepath.Join(cwd, "mode-controls"))
	if string(raw) != "plan\nacceptEdits\n" {
		t.Fatal("native mode control lost or duplicated", string(raw))
	}
}

func TestPlanModeRejectionNeverSendsUserFrame(t *testing.T) {
	client, cwd := persistentFixture(t)
	if err := os.WriteFile(filepath.Join(cwd, "reject-plan-mode"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	thread, _ := client.StartThread(ctx, cwd)
	_, err := client.SendWithOptions(ctx, thread.Thread.ID, "must not execute", nil, nil, agent.TurnOptions{Mode: agent.ModePlan})
	var notSubmitted *agent.NotSubmittedError
	if !errors.As(err, &notSubmitted) {
		t.Fatal("mode rejection did not preserve unsent status", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "mode-turns")); !os.IsNotExist(err) {
		t.Fatal("user frame was sent after mode rejection")
	}
}

func TestPlanExitApprovalRestoresOriginalPermissions(t *testing.T) {
	for _, decision := range []string{"accept", "decline"} {
		t.Run(decision, func(t *testing.T) {
			client, cwd := persistentFixture(t)
			if err := os.WriteFile(filepath.Join(client.home, "settings.json"), []byte(`{"permissions":{"defaultMode":"acceptEdits"}}`), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			thread, _ := client.StartThread(ctx, cwd)
			if _, err := client.SendWithOptions(ctx, thread.Thread.ID, "plan-exit", nil, nil, agent.TurnOptions{Mode: agent.ModePlan}); err != nil {
				t.Fatal(err)
			}
			var request agent.Event
			for len(request.ID) == 0 {
				select {
				case event := <-client.Events():
					if len(event.ID) > 0 {
						request = event
					}
				case <-ctx.Done():
					t.Fatal("plan approval missing")
				}
			}
			interaction, err := agent.DecodeInteraction(request)
			if err != nil || interaction.Kind != "ExitPlanMode" || interaction.Plan == "" || interaction.Command != "" {
				t.Fatal("plan presented as a generic command", interaction, err)
			}
			if err := client.Respond(request.ID, map[string]any{"decision": decision}); err != nil {
				t.Fatal(err)
			}
			waitTerminal(t, client, thread.Thread.ID)
			raw, err := os.ReadFile(filepath.Join(cwd, "plan-decision"))
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Mode     string
				Decision struct{ Behavior string }
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			expectedMode, expectedBehavior := "plan", "deny"
			if decision == "accept" {
				expectedMode, expectedBehavior = "acceptEdits", "allow"
			}
			if result.Mode != expectedMode || result.Decision.Behavior != expectedBehavior {
				t.Fatal("approval restored the wrong permissions", result)
			}
			if err := client.Respond(request.ID, map[string]any{"decision": decision}); err == nil {
				t.Fatal("plan approved twice")
			}
		})
	}
}
