package claude

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
)

func TestCancelInitializationDoesNotSubmitOrBlockRetry(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	script, err := filepath.Abs("testdata/startup_server.py")
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec '"+python+"' -I '"+script+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := New(executable, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cwd := t.TempDir()
	thread, err := client.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	sendCtx, stop := context.WithCancel(ctx)
	defer stop()
	result := make(chan error, 1)
	go func() {
		_, err := client.Send(sendCtx, thread.Thread.ID, "must not be sent")
		result <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(filepath.Join(cwd, "initialize-ready")); err == nil {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	stop()
	select {
	case err := <-result:
		var notSubmitted *agent.NotSubmittedError
		if !errors.Is(err, context.Canceled) || !errors.As(err, &notSubmitted) {
			t.Fatalf("cancellation did not retain unsent certainty: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := os.Stat(filepath.Join(cwd, "user-received")); !os.IsNotExist(err) {
		t.Fatal("cancelled initialization submitted user input", err)
	}
	saved, err := client.sessionCopy(thread.Thread.ID)
	if err != nil || len(saved.Turns) != 0 || saved.run != nil {
		t.Fatalf("cancelled initialization left a pending checkpoint: %+v err=%v", saved, err)
	}
	select {
	case event := <-client.Events():
		t.Fatal("unsent cancellation emitted an execution event", event.Method)
	default:
	}
	if _, err := client.Send(ctx, thread.Thread.ID, "explicit retry"); err != nil {
		t.Fatal(err)
	}
	if terminal := waitTerminal(t, client, thread.Thread.ID); terminal.Status != "completed" {
		t.Fatal(terminal)
	}
	if _, err := os.Stat(filepath.Join(cwd, "user-received")); err != nil {
		t.Fatal("explicit retry did not submit input", err)
	}
}
