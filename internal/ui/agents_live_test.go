package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/claude"
	"github.com/shenming0821/Fuyu/internal/codex"
)

func TestLiveAgentsRunIndependently(t *testing.T) {
	if os.Getenv("FUYU_LIVE_AGENTS") != "1" {
		t.Skip("set FUYU_LIVE_AGENTS=1 to verify both real runtimes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cwd := t.TempDir()
	codexClient, err := codex.Start(ctx, "codex", cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer codexClient.Close()
	executable, err := claude.ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	claudeClient, err := claude.New(executable, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer claudeClient.Close()
	a := New(codexClient, "live agents", cwd, nil)
	if err := a.RegisterBackend(agent.Claude, claudeClient.Home(), claudeClient); err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 500)
	a.dispatch = func(fn func()) {
		select {
		case updates <- fn:
		case <-a.shutdown:
		}
	}
	defer func() { close(a.shutdown); a.closeWorkspaceResources() }()
	tester := native.NewTester(a.view, 1200, 800)
	codexSession := a.session
	codexSession.State.Draft = "Use the shell tool to run sleep 2; printf 'FUYU_CODEX_PARALLEL\\n' > codex-proof.txt in this working directory. Do not modify any other file. Then reply CODEX_DONE."
	a.send()
	if err := a.chooseAgent(agent.Claude); err != nil {
		t.Fatal(err)
	}
	claudeSession := a.session
	if claudeSession == codexSession || !codexSession.State.Busy {
		t.Fatal("switch did not preserve the running Codex task")
	}
	claudeSession.State.Draft = "Use Bash to write claude-proof.txt containing exactly FUYU_CLAUDE_PARALLEL followed by one newline. Do not change other files, use background tasks, or save memory. Then reply CLAUDE_DONE."
	a.send()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var approval *agent.Event
	codexFinishedWhileClaudeWaiting := false
	for {
		select {
		case update := <-updates:
			update()
		case event := <-codexClient.Events():
			event.Session.Agent = agent.Codex
			a.applyBatch([]agent.Event{event})
		case event := <-claudeClient.Events():
			event.Session.Agent, event.Session.Runtime = agent.Claude, claudeClient.Home()
			if len(event.ID) > 0 {
				if codexFinishedWhileClaudeWaiting {
					if err := claudeClient.Respond(event.ID, map[string]string{"decision": "accept"}); err != nil {
						t.Fatal(err)
					}
				}
				copy := event
				approval = &copy
			}
			a.applyBatch([]agent.Event{event})
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err(), codexSession.State.Error, claudeSession.State.Error)
		}
		tester.Frame()
		if codexSession.State.Error != "" || claudeSession.State.Error != "" {
			t.Fatal(codexSession.State.Error, claudeSession.State.Error)
		}
		if approval != nil && !codexSession.State.Busy && !codexFinishedWhileClaudeWaiting {
			if !claudeSession.State.Busy || len(claudeSession.State.PendingInteractions()) == 0 {
				t.Fatal("Claude permission did not remain independently pending")
			}
			codexFinishedWhileClaudeWaiting = true
			if err := claudeClient.Respond(approval.ID, map[string]string{"decision": "accept"}); err != nil {
				t.Fatal(err)
			}
		}
		if codexFinishedWhileClaudeWaiting && !claudeSession.State.Busy {
			break
		}
	}
	for name, expected := range map[string]string{"codex-proof.txt": "FUYU_CODEX_PARALLEL\n", "claude-proof.txt": "FUYU_CLAUDE_PARALLEL\n"} {
		raw, err := os.ReadFile(filepath.Join(cwd, name))
		if err != nil || string(raw) != expected {
			t.Fatalf("%s=%q err=%v", name, raw, err)
		}
	}
	if a.session != claudeSession || codexSession.kind.Normalized() != agent.Codex || claudeSession.kind != agent.Claude {
		t.Fatal("background completion changed session ownership or focus")
	}
	t.Logf("codex=%s claude=%s pending-isolation=true files=passed", codexSession.State.ThreadID, claudeSession.State.ThreadID)
}
