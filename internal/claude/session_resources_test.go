package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReleaseIdleThreadReapsProcessAndRestoresHistory(t *testing.T) {
	client, cwd := persistentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	thread, err := client.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Send(ctx, thread.Thread.ID, "first"); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, client, thread.Thread.ID)
	idle := idleExecution(t, client, thread.Thread.ID)
	if released, err := client.ReleaseIdleThread(ctx, thread.Thread.ID); err != nil || !released {
		t.Fatal("idle session was not released", released, err)
	}
	select {
	case <-idle.done:
	default:
		t.Fatal("release returned before process cleanup")
	}
	page, err := client.ListTurns(ctx, thread.Thread.ID, nil)
	if err != nil || len(page.Data) != 1 {
		t.Fatal("release lost native history", err)
	}
	if released, err := client.ReleaseIdleThread(ctx, thread.Thread.ID); err != nil || !released {
		t.Fatal("already expired session cannot release its display cache", err)
	}
	if _, err := client.Send(ctx, thread.Thread.ID, "second"); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, client, thread.Thread.ID)
	if idleExecution(t, client, thread.Thread.ID).process == idle.process {
		t.Fatal("released process was reused")
	}
	page, err = client.ListTurns(ctx, thread.Thread.ID, nil)
	if err != nil || len(page.Data) != 2 {
		t.Fatal("continuation lost earlier turns", err)
	}
}

func TestReleaseIdleThreadProtectsActiveAndUnknownTurns(t *testing.T) {
	client, cwd := persistentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	thread, _ := client.StartThread(ctx, cwd)
	turn, err := client.Send(ctx, thread.Thread.ID, "wait")
	if err != nil {
		t.Fatal(err)
	}
	if released, err := client.ReleaseIdleThread(ctx, thread.Thread.ID); err != nil || released {
		t.Fatal("active task was released", err)
	}
	if err := client.Interrupt(ctx, thread.Thread.ID, turn.ID); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, client, thread.Thread.ID)
	client.mu.Lock()
	client.sessions[thread.Thread.ID].Turns[0].Status = "unknown"
	client.mu.Unlock()
	if released, err := client.ReleaseIdleThread(ctx, thread.Thread.ID); err != nil || released {
		t.Fatal("unknown task was released", err)
	}
}

func TestReleaseIdleThreadRequiresRecoverableHistory(t *testing.T) {
	client, cwd := persistentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	thread, _ := client.StartThread(ctx, cwd)
	if _, err := client.Send(ctx, thread.Thread.ID, "first"); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, client, thread.Thread.ID)
	idleExecution(t, client, thread.Thread.ID).expireIdle()
	projects, err := os.ReadDir(filepath.Join(client.home, "projects"))
	if err != nil || len(projects) != 1 {
		t.Fatal("missing fixture transcript", err)
	}
	path := filepath.Join(client.home, "projects", projects[0].Name(), thread.Thread.ID+".jsonl")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if released, err := client.ReleaseIdleThread(ctx, thread.Thread.ID); err == nil || released {
		t.Fatal("missing history allowed display cache eviction", released, err)
	}
}
