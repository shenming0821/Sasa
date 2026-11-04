package ui

import (
	"context"
	"errors"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
)

func TestStartupKeepsDraftEditableBeforeConnectionAndDiscardsLateResult(t *testing.T) {
	a := New(nil, "Codex", "/project", nil)
	jobs := make(chan func(), 8)
	a.dispatch = func(fn func()) { jobs <- fn }
	t.Cleanup(func() { close(a.shutdown) })
	started, release := make(chan struct{}), make(chan struct{})
	a.SetStartup(func(ctx context.Context, publish func(RuntimeConnection)) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		publish(RuntimeConnection{Kind: agent.Codex, Backend: stubBackend{}})
		<-ctx.Done()
	})
	stop := a.startRuntimeLoading()
	<-started
	a.State.Draft = "keep my draft"
	tester := native.NewTester(a.view, 1000, 760)
	tester.Frame()
	if !a.State.Disconnected || a.canSend() || a.State.Draft != "keep my draft" {
		t.Fatal("pending startup lost draft or allowed submission")
	}
	close(release)
	// Queue the connection, then quit before the UI callback applies it.
	job := <-jobs
	stop()
	job()
	if a.backend != nil || !a.State.Disconnected || a.State.Draft != "keep my draft" {
		t.Fatal("late startup result changed the closed application")
	}
}

func TestStartupInstallsAgentsIndependentlyAndKeepsRecoveryGuard(t *testing.T) {
	a := New(nil, "Codex", "/project", nil)
	a.SetStartup(nil)
	t.Cleanup(func() { close(a.shutdown) })
	codex := a.session
	codex.State.Draft = "codex draft"
	codex.recoveryBlocked = true
	a.newSessionForAgent("/project", agent.Claude, "/claude")
	claude := a.session
	claude.State.Draft = "claude draft"
	a.installRuntime(RuntimeConnection{Kind: agent.Claude, Runtime: "/claude", Backend: &stubBackend{}})
	if claude.State.Disconnected || !codex.State.Disconnected || claude.State.Draft != "claude draft" || !codex.recoveryBlocked {
		t.Fatal("connection changed another Agent or bypassed recovery")
	}
	a.installRuntime(RuntimeConnection{Kind: agent.Codex, Err: errors.New("unavailable")})
	if claude.State.Disconnected || codex.State.Error == "" || codex.State.Draft != "codex draft" {
		t.Fatal("failed Codex startup affected Claude or lost draft")
	}
	a.installRuntime(RuntimeConnection{Version: "test-version"})
	if a.version != "test-version" || a.backend != nil {
		t.Fatal("version-only update installed a connection")
	}
}

func TestStartupAddsHistorySourceDuringInflightList(t *testing.T) {
	first := &threadPagesBackend{threadPages: []agent.ThreadsPage{{Data: []agent.ThreadMeta{{ID: "codex", Cwd: "/project"}}}}}
	second := &threadPagesBackend{threadPages: []agent.ThreadsPage{{Data: []agent.ThreadMeta{{ID: "claude", Cwd: "/project"}}}}}
	a := New(first, "test", "/project", nil)
	t.Cleanup(func() { close(a.shutdown) })
	jobs := make(chan func(), 8)
	a.dispatch = func(fn func()) { jobs <- fn }
	a.historyOpen = true
	a.loadThreadList()
	a.installRuntime(RuntimeConnection{Kind: agent.Claude, Runtime: "/claude", Backend: second})
	pumpUpdate(t, jobs)
	pumpUpdate(t, jobs)
	if len(a.threadList.threads) != 2 || len(first.cursors) != 1 || len(second.cursors) != 1 {
		t.Fatal("late Agent history missing or existing history reread")
	}
}
