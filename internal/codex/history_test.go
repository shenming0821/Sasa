package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeHistoryPaginationAndLostTransportRequests(t *testing.T) {
	dir := t.TempDir()
	type savedThread struct {
		Cwd   string        `json:"cwd"`
		Title string        `json:"title"`
		Turns []HistoryTurn `json:"turns"`
	}
	turns := make([]HistoryTurn, 25)
	for i := range turns {
		turns[i] = HistoryTurn{ID: string(rune('A' + i)), Status: "completed", Items: []json.RawMessage{}}
	}
	raw, _ := json.Marshal(map[string]savedThread{"fixture-1": {Cwd: dir, Title: "native history", Turns: turns}})
	if err := os.WriteFile(filepath.Join(dir, "native-history.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("testdata/p3_server.py")
	if err != nil {
		t.Fatal(err)
	}
	open := func() *Client {
		t.Helper()
		c, err := startProcess("/usr/bin/python3", []string{script, "--fixture-dir", dir}, dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := open()
	resumed, err := c.ResumeThread(ctx, "fixture-1")
	if err != nil || resumed.Cwd != dir || len(resumed.ThreadMeta.Turns) != 0 {
		t.Fatalf("resume=%+v err=%v", resumed, err)
	}
	first, err := c.ListTurns(ctx, "fixture-1", nil)
	if err != nil || len(first.Data) != 20 || first.NextCursor == nil || first.Data[0].ID != "Y" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := c.ListTurns(ctx, "fixture-1", first.NextCursor)
	if err != nil || len(second.Data) != 5 || second.NextCursor != nil || second.Data[4].ID != "A" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	list, err := c.ListThreads(ctx, nil)
	if err != nil || len(list.Data) != 1 || list.Data[0].ID != "fixture-1" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	turn, err := c.Send(ctx, "fixture-1", "pending approval before connection loss")
	if err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case e := <-c.Events():
			if len(e.ID) > 0 {
				goto pending
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
pending:
	c.Close()
	reopened := open()
	if _, err := reopened.ResumeThread(ctx, "fixture-1"); err != nil {
		t.Fatal(err)
	}
	page, err := reopened.ListTurns(ctx, "fixture-1", nil)
	if err != nil || page.Data[0].ID != turn.ID || page.Data[0].Status != "inProgress" {
		t.Fatal("lost transport invented a terminal result", err)
	}
	reopened.mu.Lock()
	pendingRequests := len(reopened.server)
	reopened.mu.Unlock()
	if pendingRequests != 0 {
		t.Fatal("old approval revived on a new transport")
	}
}

func TestOccupiedReadForkAndRetryProtocol(t *testing.T) {
	dir := t.TempDir()
	raw := `{"source":{"cwd":"/tmp","title":"original","turns":[{"id":"old","status":"completed","itemsView":"full","items":[{"type":"agentMessage","id":"reply","text":"history"}]}]}}`
	if err := os.WriteFile(filepath.Join(dir, "native-history.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, "source.occupied")
	if err := os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("testdata/p3_server.py")
	if err != nil {
		t.Fatal(err)
	}
	c, err := startProcess("/usr/bin/python3", []string{script, "--fixture-dir", dir}, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.ResumeThread(ctx, "source"); !IsActiveWriter(err, "source") {
		t.Fatal("writer conflict not recognized", err)
	}
	meta, err := c.ReadThreadMeta(ctx, "source")
	if err != nil || meta.ID != "source" || len(meta.Turns) != 0 {
		t.Fatal("metadata-only read failed", err)
	}
	page, err := c.ListTurns(ctx, "source", nil)
	if err != nil || len(page.Data) != 1 || page.Data[0].ID != "old" {
		t.Fatal("occupied history unavailable", err)
	}
	branch, err := c.ForkThread(ctx, "source")
	if err != nil || branch.ThreadMeta.ID == "" || branch.ThreadMeta.ID == "source" {
		t.Fatal("fork identity not independent", err)
	}
	forkPage, err := c.ListTurns(ctx, branch.ThreadMeta.ID, nil)
	if err != nil || len(forkPage.Data) != 1 || forkPage.Data[0].ID != "old" {
		t.Fatal("fork did not preserve history", err)
	}
	if _, err := c.ResumeThread(ctx, "source"); !IsActiveWriter(err, "source") {
		t.Fatal("fork changed original writer ownership", err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ResumeThread(ctx, "source"); err != nil {
		t.Fatal("retry after unlock failed", err)
	}
}
