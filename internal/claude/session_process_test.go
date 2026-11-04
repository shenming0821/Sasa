package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func persistentFixture(t *testing.T) (*Client, string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	script, err := filepath.Abs("testdata/persistent_server.py")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec '"+python+"' -I '"+script+"' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := New(path, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, t.TempDir()
}

func TestIdleExpiryAndLimitReapOnlyIdleProcesses(t *testing.T) {
	client, cwd := persistentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var oldest *execution
	for index := range sessionIdleLimit + 1 {
		thread, _ := client.StartThread(ctx, cwd)
		if _, err := client.Send(ctx, thread.Thread.ID, "ordinary"); err != nil {
			t.Fatal(err)
		}
		waitTerminal(t, client, thread.Thread.ID)
		run := idleExecution(t, client, thread.Thread.ID)
		if index == 0 {
			oldest = run
		}
		if index == sessionIdleLimit {
			run.expireIdle()
			select {
			case <-run.done:
			default:
				t.Fatal("idle expiry did not wait for its process")
			}
		}
	}
	select {
	case <-oldest.done:
	case <-ctx.Done():
		t.Fatal("oldest idle process was not evicted")
	}
}

func TestIdleBookkeepingDoesNotPermitConversationChanges(t *testing.T) {
	client, cwd := persistentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, _ := client.StartThread(ctx, cwd)
	if _, err := client.Send(ctx, thread.Thread.ID, "first"); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, client, thread.Thread.ID)
	previous := idleExecution(t, client, thread.Thread.ID)
	path := filepath.Join(client.home, "projects", "fixture", thread.Thread.ID+".jsonl")
	appendRecord := func(raw string) {
		t.Helper()
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = file.WriteString(raw + "\n")
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	appendRecord(`{"type":"last-prompt","value":"metadata only"}`)
	if _, err := client.Send(ctx, thread.Thread.ID, "second"); err != nil {
		t.Fatal("bookkeeping prevented continuation", err)
	}
	waitTerminal(t, client, thread.Thread.ID)
	if idleExecution(t, client, thread.Thread.ID).process != previous.process {
		t.Fatal("bookkeeping unnecessarily restarted the process")
	}
	raw, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var tail map[string]any
	_ = json.Unmarshal([]byte(lines[len(lines)-1]), &tail)
	change, _ := json.Marshal(map[string]any{"type": "user", "uuid": "external", "parentUuid": tail["uuid"], "message": map[string]any{"content": "external input"}})
	appendRecord(string(change))
	if _, err := client.Send(ctx, thread.Thread.ID, "must not send"); err == nil {
		t.Fatal("external conversation change was silently accepted")
	}
}

func TestReusedProcessFailureKeepsUnknownGuard(t *testing.T) {
	client, cwd := persistentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, _ := client.StartThread(ctx, cwd)
	if _, err := client.Send(ctx, thread.Thread.ID, "first"); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, client, thread.Thread.ID)
	if _, err := client.Send(ctx, thread.Thread.ID, "exit"); err != nil {
		t.Fatal(err)
	}
	if turn := waitTerminal(t, client, thread.Thread.ID); turn.Status != "failed" {
		t.Fatal("unconfirmed exit became successful", turn)
	}
	if _, err := client.Send(ctx, thread.Thread.ID, "must not replay"); err == nil {
		t.Fatal("unknown task was replayed")
	}
	client.mu.Lock()
	status := client.sessions[thread.Thread.ID].Turns[1].Status
	client.mu.Unlock()
	if status != "unknown" {
		t.Fatal("unknown checkpoint lost", status)
	}
}

func idleExecution(t *testing.T, client *Client, id string) *execution {
	t.Helper()
	client.mu.Lock()
	defer client.mu.Unlock()
	run := client.sessions[id].idle
	if run == nil {
		t.Fatal("successful turn did not retain its process")
	}
	return run
}

func TestSessionReusesProcessWithDistinctTurnOwnershipAndRestartsAfterClose(t *testing.T) {
	client, cwd := persistentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, err := client.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.Send(ctx, thread.Thread.ID, "first")
	if err != nil || waitTerminal(t, client, thread.Thread.ID).Status != "completed" {
		t.Fatal("first turn failed", err)
	}
	previous := idleExecution(t, client, thread.Thread.ID)
	second, err := client.Send(ctx, thread.Thread.ID, "second")
	if err != nil || waitTerminal(t, client, thread.Thread.ID).Status != "completed" {
		t.Fatal("second turn failed", err)
	}
	current := idleExecution(t, client, thread.Thread.ID)
	if first.ID == second.ID || previous == current || previous.process != current.process || previous.identity.Generation == current.identity.Generation {
		t.Fatal("reuse lost process or turn isolation")
	}
	resumed, err := client.ResumeThread(ctx, thread.Thread.ID)
	if err != nil || resumed.ThreadMeta.Status.Type != "idle" {
		t.Fatal("idle process was mistaken for an active task", err)
	}
	raw, err := os.ReadFile(filepath.Join(cwd, "process-starts"))
	if err != nil || len(strings.Fields(string(raw))) != 1 {
		t.Fatal("second turn spawned another process", string(raw), err)
	}
	client.Close()
	select {
	case <-current.done:
	default:
		t.Fatal("client close did not wait for reused process")
	}
	reopened, err := New(client.executable, client.home, client.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Send(ctx, thread.Thread.ID, "third after restart"); err != nil ||
		waitTerminal(t, reopened, thread.Thread.ID).Status != "completed" {
		t.Fatal("resume after restart failed", err)
	}
}

func TestSessionChangesModelAndConfigurationRestartProcess(t *testing.T) {
	for _, scenario := range []string{"model", "configuration", "idle-exit"} {
		t.Run(scenario, func(t *testing.T) {
			client, cwd := persistentFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			thread, _ := client.StartThread(ctx, cwd)
			if _, err := client.Send(ctx, thread.Thread.ID, "first"); err != nil {
				t.Fatal(err)
			}
			waitTerminal(t, client, thread.Thread.ID)
			previous := idleExecution(t, client, thread.Thread.ID)
			model := ""
			switch scenario {
			case "model":
				model = "different"
			case "configuration":
				if err := os.WriteFile(filepath.Join(client.home, "settings.json"), []byte(`{"permissions":{"defaultMode":"plan"}}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "idle-exit":
				previous.stopProcess()
			}
			if _, err := client.SendWithModel(ctx, thread.Thread.ID, "second", nil, nil, model); err != nil {
				t.Fatal(err)
			}
			if turn := waitTerminal(t, client, thread.Thread.ID); turn.Status != "completed" {
				t.Fatal(turn)
			}
			if idleExecution(t, client, thread.Thread.ID).process == previous.process {
				t.Fatal("obsolete process was reused")
			}
		})
	}
}

func TestPinnedEffortReadsConfigAndEnvironment(t *testing.T) {
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "")
	client, cwd := persistentFixture(t)
	if pin := client.PinnedEffort(cwd); pin != "" {
		t.Fatal("unexpected effort pin", pin)
	}
	if err := os.WriteFile(filepath.Join(client.home, "settings.json"), []byte(`{"env":{"CLAUDE_CODE_EFFORT_LEVEL":"high"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if pin := client.PinnedEffort(cwd); pin != "high" {
		t.Fatal("config root pin not detected", pin)
	}
	project := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "settings.local.json"), []byte(`{"env":{"CLAUDE_CODE_EFFORT_LEVEL":"low"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if pin := client.PinnedEffort(cwd); pin != "low" {
		t.Fatal("project pin did not take precedence", pin)
	}
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "xhigh")
	if pin := client.PinnedEffort(cwd); pin != "xhigh" {
		t.Fatal("environment pin not detected", pin)
	}
}

func TestSendWithOptionsPassesEffortAndKeysProcessReuse(t *testing.T) {
	client, cwd := persistentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, _ := client.StartThread(ctx, cwd)
	if _, err := client.Send(ctx, thread.Thread.ID, "first"); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, client, thread.Thread.ID)
	plain := idleExecution(t, client, thread.Thread.ID)
	if _, err := client.SendWithOptions(ctx, thread.Thread.ID, "second", nil, nil, agent.TurnOptions{Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, client, thread.Thread.ID)
	high := idleExecution(t, client, thread.Thread.ID)
	if high.process == plain.process {
		t.Fatal("effort change reused the process started without it")
	}
	if _, err := client.SendWithOptions(ctx, thread.Thread.ID, "third", nil, nil, agent.TurnOptions{Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, client, thread.Thread.ID)
	if idleExecution(t, client, thread.Thread.ID).process != high.process {
		t.Fatal("same effort did not reuse the idle process")
	}
	raw, err := os.ReadFile(filepath.Join(cwd, "spawn-args"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || strings.Contains(lines[0], "--effort") || !strings.Contains(lines[1], "--effort high") {
		t.Fatal("effort was not passed to the spawned CLI", lines)
	}
}

func TestReusedTurnScopesApprovalsAndInterrupts(t *testing.T) {
	client, cwd := persistentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, _ := client.StartThread(ctx, cwd)
	var old json.RawMessage
	for index, prompt := range []string{"permission", "question"} {
		if _, err := client.Send(ctx, thread.Thread.ID, prompt); err != nil {
			t.Fatal(err)
		}
		var id json.RawMessage
		for len(id) == 0 {
			select {
			case event := <-client.Events():
				id = event.ID
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		if index == 1 && client.Respond(old, map[string]string{"decision": "accept"}) == nil {
			t.Fatal("old approval affected the new turn")
		}
		var response any = map[string]string{"decision": "accept"}
		if index == 1 {
			response = map[string]any{"answers": map[string]any{"0": map[string]any{"answers": []string{"One"}}}}
		}
		if err := client.Respond(id, response); err != nil {
			t.Fatal(err)
		}
		if turn := waitTerminal(t, client, thread.Thread.ID); turn.Status != "completed" {
			t.Fatal(turn)
		}
		old = id
	}
	idle := idleExecution(t, client, thread.Thread.ID)
	turn, err := client.Send(ctx, thread.Thread.ID, "wait")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Interrupt(ctx, thread.Thread.ID, turn.ID); err != nil {
		t.Fatal(err)
	}
	if terminal := waitTerminal(t, client, thread.Thread.ID); terminal.Status != "interrupted" {
		t.Fatal("interrupt did not confirm native terminal", terminal)
	}
	select {
	case <-idle.done:
	case <-ctx.Done():
		t.Fatal("interrupted process was not reaped")
	}
	if _, err := client.Send(ctx, thread.Thread.ID, "after interrupt"); err != nil ||
		waitTerminal(t, client, thread.Thread.ID).Status != "completed" {
		t.Fatal("cannot continue after interrupt", err)
	}
}
