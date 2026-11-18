package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/codex"
)

// Opt in: a real Codex task writes only two files in an isolated temporary workspace.
func TestLiveP4AgentWritesWhileEditing(t *testing.T) {
	if os.Getenv("SASA_LIVE") != "1" {
		t.Skip("set SASA_LIVE=1 to use the real Codex runtime")
	}
	liveP4FileWrites(t, 2, 2)
}

func TestLiveP4BatchWritesWhileEditing(t *testing.T) {
	if os.Getenv("SASA_LIVE_BATCH") != "1" {
		t.Skip("set SASA_LIVE_BATCH=1 to use the real Codex batch workload")
	}
	liveP4FileWrites(t, 500, 10)
}

func liveP4FileWrites(t *testing.T, count, watched int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	root := t.TempDir()
	names := []string{"clean.txt", "dirty.txt"}
	for i := 2; i < watched; i++ {
		names = append(names, fmt.Sprintf("watched-%02d.txt", i))
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, name), []byte("baseline\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	client, err := codex.Start(ctx, "codex", root)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	a := New(client, "live P4", root, nil)
	q := make(chan func(), 500)
	a.dispatch = func(fn func()) {
		select {
		case q <- fn:
		case <-a.shutdown:
		}
	}
	defer func() { close(a.shutdown); a.closeWorkspaceResources() }()
	tt := native.NewTester(a.view, 1100, 760)
	var documents []*filePage
	for i, name := range names {
		if i > 0 && watched > 2 {
			a.newSession(root)
		}
		a.openFile(root, filepath.Join(root, name))
		p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
		documents = append(documents, a.currentFile())
	}
	dirty := documents[1]
	dirty.buffer.Set("local draft must survive\n")
	dirty.observe()
	owner := a.session
	nonce := "sasa-p4-" + time.Now().Format("150405.000000")
	a.activeTab = sessionTabID(owner.id)
	owner.State.Draft = fmt.Sprintf("In the working directory, overwrite only these files: %s. Each file must contain exactly %s followed by one newline. Do not read or change any other files. Then reply done.", strings.Join(names, ", "), nonce)
	if count > watched {
		owner.State.Draft = fmt.Sprintf("In this isolated working directory, use one shell command to overwrite these files: %s, and create batch/000.txt through batch/%03d.txt inclusive. All %d files must contain exactly %s followed by one newline. You may create the batch directory. Do not read or modify any other path or use the network. Then reply done.", strings.Join(names, ", "), count-watched-1, count, nonce)
	}
	started := time.Now()
	a.send()
	if !owner.State.Busy {
		t.Fatal("real task was not submitted")
	}
	a.selectFile(dirty)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	events := client.Events()
	var frames []time.Duration
	allCleanUpdated := func() bool {
		for _, d := range documents {
			if d != dirty && d.buffer.String() != nonce+"\n" {
				return false
			}
		}
		return true
	}
	for {
		select {
		case fn := <-q:
			fn()
		case event, ok := <-events:
			if !ok {
				t.Fatal("runtime disconnected before P4 linkage completed")
			}
			a.applyBatch([]codex.Event{event})
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err(), owner.State.Phase, owner.State.Error)
		}
		frame := time.Now()
		tt.Frame()
		frames = append(frames, time.Since(frame))
		if len(owner.State.PendingInteractions()) > 0 {
			t.Fatal("real task requires user interaction; linkage not verified")
		}
		if owner.State.Error != "" {
			t.Fatal(owner.State.Error)
		}
		if !owner.State.Busy && allCleanUpdated() && dirty.conflict {
			break
		}
	}
	for i := 0; i < count-watched; i++ {
		names = append(names, fmt.Sprintf("batch/%03d.txt", i))
	}
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(raw) != nonce+"\n" {
			t.Fatal("real task did not produce expected disk content", name, string(raw), err)
		}
	}
	if a.currentFile() != dirty || dirty.buffer.String() != "local draft must survive\n" || !dirty.dirty() {
		t.Fatal("background task stole the tab or replaced local edits")
	}
	if !tt.HasText("文件已在外部更改") {
		t.Fatal("external write did not render a conflict notice")
	}
	if watched > 2 && len(a.tabs) != 20 {
		t.Fatal("batch workload did not preserve 20 mixed tabs", len(a.tabs))
	}
	slices.Sort(frames)
	t.Logf("cwd=%s thread=%s turn=%s model=%s: %d real disk writes, %d open files, %d tabs, clean reload, dirty conflict and background completion verified in memory renderer; end-to-end=%s; frame p95=%s, max=%s, samples=%d", root, owner.State.ThreadID, owner.State.TurnID, owner.State.Model, count, watched, len(a.tabs), time.Since(started), frames[(len(frames)-1)*95/100], frames[len(frames)-1], len(frames))
}
