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

func localCommandTestSession(t *testing.T) (*Client, *session, string) {
	t.Helper()
	client, err := New("unused", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	thread, err := client.StartThread(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	saved := client.sessions[thread.Thread.ID]
	saved.Generation = 1
	saved.Turns = []checkpoint{{ID: "local-turn", UserUUID: "local-input", Status: "unknown", LastUUID: "local-output", Command: "/model"}}
	directory := filepath.Join(client.Home(), "projects", "fixture")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, saved.ID+".jsonl")
	raw := []byte(`{"type":"user","uuid":"local-input","parentUuid":null,"message":{"content":"<command-name>/model</command-name>"}}` + "\n" +
		`{"type":"system","subtype":"local_command","uuid":"local-output","parentUuid":"local-input","level":"info","commandRun":{"command":"model","args":""},"content":"<local-command-stdout>Current model: fixture/model</local-command-stdout>"}` + "\n")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	saved.Digest = digest(raw)
	if err := client.save(saved); err != nil {
		t.Fatal(err)
	}
	return client, saved, path
}

func TestLocalCommandMissingTerminalReasonCompletesOnlyWithEvidence(t *testing.T) {
	for _, scenario := range []string{"success", "exit-error", "no-result", "error-result", "unknown-reason", "missing-output"} {
		t.Run(scenario, func(t *testing.T) {
			client, saved, path := localCommandTestSession(t)
			saved.Turns[0].Status = "inProgress"
			run := &execution{client: client, session: saved, identity: agent.TurnIdentity{ID: "local-turn"}, userAttempted: true,
				result: &frame{Subtype: "success"}, lastUUID: "local-output"}
			var exitErr error
			switch scenario {
			case "exit-error":
				exitErr = errors.New("exit status 1")
			case "no-result":
				run.result = nil
			case "error-result":
				run.result.IsError = true
			case "unknown-reason":
				run.result.TerminalReason = "future_reason"
			case "missing-output":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			run.finish(exitErr)
			expected := "unknown"
			if scenario == "success" {
				expected = "completed"
			} else if scenario == "error-result" {
				expected = "failed"
			}
			if saved.Turns[0].Status != expected {
				t.Fatalf("status=%s expected=%s", saved.Turns[0].Status, expected)
			}
		})
	}
}

func TestLocalCommandRecoveryChecksOwnershipAndStableHistory(t *testing.T) {
	for _, scenario := range []string{"completed-local", "changed-history", "active", "wrong-parent", "wrong-command", "error-output", "wrong-tail", "no-command", "in-progress", "assistant-only"} {
		t.Run(scenario, func(t *testing.T) {
			client, saved, path := localCommandTestSession(t)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "changed-history":
				saved.Digest = "previous-digest"
			case "active":
				saved.run = &execution{}
				t.Cleanup(func() { saved.run = nil })
			case "wrong-parent":
				raw = []byte(strings.ReplaceAll(string(raw), `"parentUuid":"local-input"`, `"parentUuid":null`))
			case "wrong-command":
				saved.Turns[0].Command = "/clear"
			case "error-output":
				raw = []byte(strings.ReplaceAll(string(raw), `"level":"info"`, `"level":"error"`))
			case "wrong-tail":
				saved.Turns[0].LastUUID = "other-output"
			case "no-command":
				saved.Turns[0].Command = ""
			case "in-progress":
				saved.Turns[0].Status = "inProgress"
			case "assistant-only":
				raw = []byte(`{"type":"user","uuid":"local-input","parentUuid":null,"message":{"content":"/model"}}` + "\n" +
					`{"type":"assistant","uuid":"local-output","parentUuid":"local-input","message":{"id":"reply","content":[{"type":"text","text":"done"}]}}` + "\n")
			}
			if scenario != "changed-history" {
				saved.Digest = digest(raw)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			resumed, err := client.ResumeThread(context.Background(), saved.ID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "completed-local" {
				if resumed.ThreadMeta.Status.Type == "idle" || saved.Turns[0].Status == "completed" {
					t.Fatal("unconfirmed task was unlocked")
				}
				return
			}
			if resumed.ThreadMeta.Status.Type != "idle" || saved.Turns[0].Status != "completed" {
				t.Fatal("confirmed local command remains blocked")
			}
			page, err := client.ListTurns(context.Background(), saved.ID, nil)
			encoded, _ := json.Marshal(page)
			if err != nil || len(page.Data) != 1 || page.Data[0].Status != "completed" || !strings.Contains(string(encoded), "Current model: fixture/model") {
				t.Fatalf("local output or terminal missing: %s err=%v", encoded, err)
			}
			reopened, err := New("unused", client.Home(), client.stateDir)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			resumed, err = reopened.ResumeThread(context.Background(), saved.ID)
			if err != nil || resumed.ThreadMeta.Status.Type != "idle" {
				t.Fatalf("recovered terminal not persisted: %+v err=%v", resumed, err)
			}
		})
	}
}

func TestLiveClaudeLocalCommandResume(t *testing.T) {
	if os.Getenv("FUYU_LIVE_CLAUDE") != "1" {
		t.Skip("set FUYU_LIVE_CLAUDE=1 for real local command recovery verification")
	}
	executable, err := ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(executable, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	thread, err := client.StartThread(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Send(ctx, thread.Thread.ID, "/model"); err != nil {
		t.Fatal(err)
	}
	if terminal := waitTerminal(t, client, thread.Thread.ID); terminal.Status != "completed" {
		t.Fatal(terminal)
	}
	resumed, err := client.ResumeThread(ctx, thread.Thread.ID)
	if err != nil || resumed.ThreadMeta.Status.Type != "idle" {
		t.Fatalf("local command blocked resume: %+v err=%v", resumed, err)
	}
	client.Close()
	saved := client.sessions[thread.Thread.ID]
	saved.Turns[len(saved.Turns)-1].Status = "unknown"
	if err := client.save(saved); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(executable, client.Home(), client.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	resumed, err = reopened.ResumeThread(ctx, saved.ID)
	if err != nil || resumed.ThreadMeta.Status.Type != "idle" {
		t.Fatalf("legacy local command remained blocked after restart: %+v err=%v", resumed, err)
	}
	page, err := reopened.ListTurns(ctx, saved.ID, nil)
	if err != nil || len(page.Data) != 1 || page.Data[0].Status != "completed" || len(page.Data[0].Items) < 2 {
		t.Fatalf("recovered local command output missing: %+v err=%v", page, err)
	}
}
