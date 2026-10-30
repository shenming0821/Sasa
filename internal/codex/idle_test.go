package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIdleReleaseProtectsNativeActivityAndBackgroundTerminals(t *testing.T) {
	for _, scenario := range []struct {
		name, marker string
		childMarker  string
		detached     bool
		wantError    bool
	}{
		{name: "idle", detached: true},
		{name: "active", marker: ".active"},
		{name: "background terminal after turn", marker: ".terminal"},
		{name: "active delegated child", childMarker: ".active"},
		{name: "child background terminal", childMarker: ".terminal"},
		{name: "uncertain unsubscribe", marker: ".unsubscribe-error", detached: true, wantError: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			threads := map[string]any{"native": map[string]any{"cwd": dir, "turns": []any{}}}
			if scenario.childMarker != "" {
				threads["child"] = map[string]any{"cwd": dir, "turns": []any{}, "parentThreadId": "native"}
				if err := os.WriteFile(filepath.Join(dir, "child"+scenario.childMarker), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			raw, _ := json.Marshal(threads)
			if err := os.WriteFile(filepath.Join(dir, "native-history.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if scenario.marker != "" {
				if err := os.WriteFile(filepath.Join(dir, "native"+scenario.marker), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			script, _ := filepath.Abs("testdata/p3_server.py")
			client, err := startProcess("/usr/bin/python3", []string{script, "--fixture-dir", dir}, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			detached, err := client.ReleaseIdleThread(ctx, "native")
			if detached != scenario.detached || (err != nil) != scenario.wantError {
				t.Fatalf("detached=%v err=%v", detached, err)
			}
			methods, err := os.ReadFile(filepath.Join(dir, "methods.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(methods), "thread/unsubscribe") != scenario.detached {
				t.Fatalf("unsafe or missing unsubscribe: %s", methods)
			}
			if !strings.Contains(string(methods), "thread/read") {
				t.Fatal("release skipped native activity verification")
			}
			// Unsubscription changes resource ownership, never durable history.
			if _, err := client.ResumeThread(ctx, "native"); err != nil {
				t.Fatal("history could not be resumed after release", err)
			}
		})
	}
}
