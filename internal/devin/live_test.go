package devin

import (
	"context"
	"os"
	"testing"
	"time"
)

// Uses the installed devin acp and real credentials to resume index sessions.
// It never submits a prompt; locked sessions may report their server error.
func TestLiveResumeThread(t *testing.T) {
	if os.Getenv("FUYU_LIVE_DEVIN") != "1" {
		t.Skip("set FUYU_LIVE_DEVIN=1 to resume real Devin sessions")
	}
	executable, err := ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	apiKey, err := ResolveAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := New(ctx, executable, apiKey, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go func() {
		for range c.Events() {
		}
	}()
	entries, _, err := c.listSessions(ctx, nil)
	if err != nil || len(entries) == 0 {
		t.Fatalf("session/list: %v", err)
	}
	resumed := 0
	var failed []string
	for _, entry := range entries {
		thread, err := c.ResumeThread(ctx, entry.SessionID)
		if err != nil {
			failed = append(failed, entry.SessionID+": "+err.Error())
			continue
		}
		if thread.ThreadMeta.ID != entry.SessionID || thread.Cwd == "" {
			t.Fatalf("resumed %+v", thread)
		}
		if _, err := c.ListTurns(ctx, entry.SessionID, nil); err != nil {
			t.Fatalf("turns %s: %v", entry.SessionID, err)
		}
		_, _ = c.ReleaseIdleThread(ctx, entry.SessionID)
		resumed++
	}
	if resumed == 0 {
		t.Fatalf("no session resumed; failures: %v", failed)
	}
	t.Logf("resumed %d sessions; failed or locked: %v", resumed, failed)
}
