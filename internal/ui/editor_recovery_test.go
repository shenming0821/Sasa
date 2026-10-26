package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/editor"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func TestP4RecoveryCleanupFailureKeepsDirtyFileOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not constrain root")
	}
	a, q, root := p4App(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.store = store
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("draft retained after cleanup failure\n")
	d.observe()
	d.changedAt = time.Now().Add(-2 * time.Second)
	a.checkpointFiles()
	p4Wait(t, tt, q, func() bool { return !d.checkpointBusy && d.checkpointVersion == d.buffer.Version() })
	dir := a.recoveryDir()
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	a.quitPrompt = func(mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 1}, nil
	}
	a.closeFile(d)
	p4Wait(t, tt, q, func() bool { return !a.quitDialog })
	if a.files[d.id] != d || !d.dirty() || d.err == "" {
		t.Fatal("recovery cleanup failure discarded or closed the local draft")
	}
	copies, err := editor.Recoveries(dir)
	if err != nil || len(copies) != 1 || copies[0].Text != d.buffer.String() {
		t.Fatal("failed cleanup lost the durable recovery copy", copies, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != d.savedText {
		t.Fatal("failed close modified the source file", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a.closeFile(d)
	p4Wait(t, tt, q, func() bool { return !a.quitDialog })
	if a.files[d.id] != nil {
		t.Fatal("retry after fixing recovery permissions could not close the file")
	}
}

func TestP4DraftCheckpointResumesAfterRecoveryCleanup(t *testing.T) {
	a, q, root := p4App(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.store = store
	tt := native.NewTester(a.view, 1100, 760)
	a.openFile(root, filepath.Join(root, "README.md"))
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("需要继续保护的草稿\n")
	d.observe()
	d.changedAt = time.Now().Add(-2 * time.Second)
	a.checkpointFiles()
	p4Wait(t, tt, q, func() bool { return !d.checkpointBusy && d.checkpointVersion == d.buffer.Version() })
	if !a.clearRecovery(d) {
		t.Fatal(d.err)
	}
	// A later close/exit step returned to the app; no new input occurred.
	a.checkpointFiles()
	p4Wait(t, tt, q, func() bool { return !d.checkpointBusy && d.checkpointVersion == d.buffer.Version() })
	got, err := editor.Recoveries(a.recoveryDir())
	if err != nil || len(got) != 1 || got[0].Text != d.buffer.String() {
		t.Fatal("unchanged dirty draft lost recovery protection", got, err)
	}
}

func TestP4RecoveryKeepsBaselineAcrossSecondCheckpoint(t *testing.T) {
	a, _, root := p4App(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.store = store
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "README.md")
	base, err := editor.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	id := editor.RecoveryID(root, path)
	if err := editor.WriteRecovery(a.recoveryDir(), editor.Recovery{ID: id, Workspace: root, Path: path, Text: "first draft", Base: base}); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(a.recoveryDir(), "000-broken.json")
	if err := os.WriteFile(corrupt, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	b := New(stubBackend{}, "test", "", nil)
	if err := b.AttachStore(store); err != nil {
		t.Fatal("one malformed copy blocked valid recovery", err)
	}
	if b.recoveryWarning == "" {
		t.Fatal("malformed copy was not reported")
	}
	d := b.files["file-"+id]
	if d == nil || d.buffer.String() != "first draft" || d.file.Text != base.Text {
		t.Fatal("first recovery lost local or baseline text")
	}
	d.buffer.Set("second draft")
	d.observe()
	if err := editor.WriteRecovery(b.recoveryDir(), editor.Recovery{ID: id, Workspace: root, Path: path, Text: d.buffer.String(), Base: d.file}); err != nil {
		t.Fatal(err)
	}
	recoveries, err := editor.Recoveries(b.recoveryDir())
	if err == nil || len(recoveries) != 1 || recoveries[0].Text != "second draft" || recoveries[0].DiskText != base.Text {
		t.Fatal("second checkpoint became corrupt or lost baseline", recoveries, err)
	}
	if _, err := os.Stat(corrupt); err != nil {
		t.Fatal("corrupt evidence was removed", err)
	}
}

func TestP4ExitWaitsForSaveAndCanCancelWaiting(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("saved before exit\n")
	d.observe()
	exited := false
	a.quitApp = func() { exited = true }
	a.saveFile(d, false, false, nil)
	a.requestFileExit()
	tt.Frame()
	if exited || !a.fileExitRequested || !tt.HasText("正在等待文件保存完成…") {
		t.Fatal("exit did not wait visibly for save")
	}
	if err := tt.Click("取消退出"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !d.saving })
	a.advanceFileExit()
	if exited || a.fileExitRequested {
		t.Fatal("canceled exit continued after save")
	}
	d.buffer.Set("latest saved before exit\n")
	d.observe()
	a.saveFile(d, false, false, nil)
	a.requestFileExit()
	p4Wait(t, tt, q, func() bool {
		a.advanceFileExit()
		return exited
	})
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "latest saved before exit\n" || !a.quitting {
		t.Fatal("exit preceded durable save", string(raw), err)
	}
}

func TestP4HistoryFilterIsPerWorkspace(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	first := filepath.Join(root, "README.md")
	a.openFile(root, first)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	a.historyOpen, a.rightTab = true, "history"
	tt.Frame()
	if err := tt.Click("搜索历史"); err != nil {
		t.Fatal(err)
	}
	tt.Type("first-filter")
	a.historyScope = 1
	secondRoot := t.TempDir()
	second := filepath.Join(secondRoot, "README.md")
	os.WriteFile(second, []byte("second"), 0600)
	a.openFile(secondRoot, second)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	if a.historyQuery != "" || a.historyScope != 0 {
		t.Fatal("first workspace filter leaked into second")
	}
	if err := tt.Click("搜索历史"); err != nil {
		t.Fatal(err)
	}
	tt.Type("second-filter")
	if err := tt.Click("文件标签 " + first); err != nil {
		t.Fatal(err)
	}
	if a.historyQuery != "first-filter" || a.historyScope != 1 {
		t.Fatal("first workspace filter not restored", a.historyQuery)
	}
	if err := tt.Click("文件标签 " + second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.historyQuery, "second-filter") || a.historyScope != 0 {
		t.Fatal("second workspace filter not restored", a.historyQuery)
	}
}

func TestP4CanceledExitAfterTaskStopKeepsDraftAndExternalDiskVersion(t *testing.T) {
	a, q, root := p4App(t)
	b := &quitBackend{stops: make(chan string, 1)}
	a.backend = b
	owner := a.session
	owner.State.ThreadID = "quit-A"
	owner.State.Begin("running task")
	owner.State.TurnID = "turn-A"
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.store = store
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("local draft before stopping task\n")
	d.observe()
	d.changedAt = time.Now().Add(-2 * time.Second)
	a.checkpointFiles()
	p4Wait(t, tt, q, func() bool { return !d.checkpointBusy && d.checkpointVersion == d.buffer.Version() })
	exited := false
	a.quitApp = func() { exited = true }
	a.quitPrompt = func(mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.MessageResult{Button: 0}, nil
	}
	a.beginQuit()
	a.advanceQuit()
	select {
	case target := <-b.stops:
		if target != "quit-A/turn-A" {
			t.Fatal("incorrect stop target", target)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop request did not run")
	}
	if exited || !a.quitStopping || !owner.State.Busy {
		t.Fatal("stop RPC success incorrectly finished quit")
	}
	if err := os.WriteFile(path, []byte("external change while waiting for stop\n"), 0640); err != nil {
		t.Fatal(err)
	}
	a.applyBatch([]codex.Event{{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"quit-A","turn":{"id":"turn-A","status":"interrupted"}}`)}})
	a.advanceQuit()
	p4Wait(t, tt, q, func() bool { return !a.quitDialog && !a.fileQuitPending })
	p4Wait(t, tt, q, func() bool {
		if !d.checking && !d.conflict {
			a.checkFile(d)
		}
		return d.conflict
	})
	if exited || a.quitting || a.quitStopping || !d.dirty() || d.buffer.String() != "local draft before stopping task\n" {
		t.Fatal("canceled exit lost the unsaved version")
	}
	records, err := editor.Recoveries(a.recoveryDir())
	if err != nil || len(records) != 1 || records[0].Text != d.buffer.String() || records[0].DiskText != d.savedText {
		t.Fatal("canceled exit removed recovery protection", records, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "external change while waiting for stop\n" {
		t.Fatal("canceled exit overwrote the external version", string(raw), err)
	}
}
