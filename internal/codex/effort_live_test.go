package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
)

// Opt in explicitly: this test uses the existing Codex login and real model service.
// It verifies turn/start effort reaches the native rollout turn_context.
func TestLiveEffortReachesTurnContext(t *testing.T) {
	if os.Getenv("SASA_LIVE") != "1" {
		t.Skip("set SASA_LIVE=1 to run real Codex")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	dir := t.TempDir()
	c, err := Start(ctx, "codex", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	thread, err := c.StartThread(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, effort := range []string{"low", "max"} {
		turn, err := c.SendWithOptions(ctx, thread.Thread.ID,
			"Reply with exactly the single word: ok", nil, nil, agent.TurnOptions{Effort: effort})
		if err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case e, ok := <-c.Events():
				if !ok {
					t.Fatal("disconnected before completion")
				}
				var p struct {
					ThreadID string `json:"threadId"`
					Turn     Turn   `json:"turn"`
				}
				if e.Method == "turn/completed" && json.Unmarshal(e.Params, &p) == nil && p.Turn.ID == turn.ID {
					if p.Turn.Status != "completed" {
						t.Fatalf("effort=%s turn status=%s error=%+v", effort, p.Turn.Status, p.Turn.Error)
					}
					goto next
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	next:
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var rollout string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = filepath.Walk(filepath.Join(home, ".codex", "sessions"), func(path string, info os.FileInfo, err error) error {
			if err == nil && strings.HasSuffix(info.Name(), "-"+thread.Thread.ID+".jsonl") {
				rollout = path
			}
			return nil
		})
		if rollout != "" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if rollout == "" {
		t.Fatal("rollout file for the live thread not found")
	}
	var efforts []string
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		efforts = efforts[:0]
		raw, err := os.ReadFile(rollout)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			var record struct {
				Type    string `json:"type"`
				Payload struct {
					Effort string `json:"effort"`
				} `json:"payload"`
			}
			if json.Unmarshal([]byte(line), &record) == nil && record.Type == "turn_context" {
				efforts = append(efforts, record.Payload.Effort)
			}
		}
		if len(efforts) >= 2 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if len(efforts) < 2 || efforts[len(efforts)-2] != "low" || efforts[len(efforts)-1] != "max" {
		t.Fatalf("rollout turn_context efforts=%v want trailing [low max]", efforts)
	}
	t.Logf("rollout turn_context efforts=%v", efforts)
}
