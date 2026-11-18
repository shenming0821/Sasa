package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/editor"
	"github.com/shenming0821/Sasa/internal/storage"
)

func TestWorkspaceBatchChecksOnlyAffectedFilesAndKeepsFollowup(t *testing.T) {
	application := New(stubBackend{}, "test", t.TempDir(), nil)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	application.store = store
	jobs := make(chan func(), 8)
	application.dispatch = func(job func()) { jobs <- job }
	t.Cleanup(func() { close(application.shutdown) })
	root := application.State.Cwd
	application.files = make(map[string]*filePage)
	for _, name := range []string{"first.txt", "second.txt"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		file, err := editor.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		application.files[name] = &filePage{id: name, root: root, path: path, kind: "file", loaded: true, buffer: native.NewTextBuffer(file.Text), file: file, savedText: file.Text}
		application.files[name].highlighting = true
		application.files[name].observed = application.files[name].buffer.Snapshot()
	}
	first, second := application.files["first.txt"], application.files["second.txt"]
	if err := os.WriteFile(first.path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	workspace := application.workspace(root)
	workspace.expanded = nil
	application.applyWorkspaceChanges(workspace, map[string]bool{first.path: true})
	if !first.checking || second.checking || !second.checkedAt.IsZero() {
		t.Fatal("file event checked unrelated open documents")
	}
	application.checkFile(first)
	if !first.checkPending {
		t.Fatal("event during in-flight check was discarded")
	}
	pumpUpdate(t, jobs)
	pumpUpdate(t, jobs)
	if first.buffer.String() != "changed" || first.checking || first.checkPending {
		t.Fatalf("coalesced check: text=%q checking=%v pending=%v conflict=%v modified=%v err=%q jobs=%d", first.buffer.String(), first.checking, first.checkPending, first.conflict, first.modified, first.err, len(jobs))
	}
	application.savePending = false
	application.checkFile(first)
	pumpUpdate(t, jobs)
	if application.savePending {
		t.Fatal("unchanged disk check scheduled a save or repaint")
	}
	if !workspacePathChanged(map[string]bool{root: true}, first.path) || workspacePathChanged(map[string]bool{first.path: true}, second.path) {
		t.Fatal("directory change matching is incorrect")
	}
}

func TestWorkspaceBatchQueuesOneGitRefreshAndKeepsCatalogInvalidation(t *testing.T) {
	application, backend, jobs := catalogApp(t)
	application.prewarmCompletionSkills()
	cwd := awaitCatalog(t, backend, false)
	workspace := application.workspace(cwd)
	application.historyOpen, application.rightTab = true, "git"
	workspace.gitLoading = true
	for range 50 {
		application.applyWorkspaceChanges(workspace, map[string]bool{cwd + "/SKILL.md": true})
	}
	if !workspace.gitRefreshPending {
		t.Fatal("events during an in-flight Git read were discarded")
	}
	backend.results <- catalogSkills(cwd, "before-change")
	pumpUpdate(t, jobs)
	entry := application.completionCatalogs[application.completionCacheKey()]
	if !entry.checked.IsZero() || entry.generation != 50 {
		t.Fatal("in-flight catalog completion erased a later invalidation")
	}
	application.cancelGitRefresh(workspace)
	if workspace.gitRefreshPending {
		t.Fatal("cancel retained a queued refresh")
	}
	entry.checked = time.Now()
}

func TestWorkspaceGitRefreshReplaysEventsArrivingAfterSnapshotRead(t *testing.T) {
	application, jobs, root, _ := gitWorkflowApp(t)
	application.branchCatalog(root).loaded = true
	application.branchCatalog(root).checked = time.Now()
	workspace := application.workspace(root)
	workspace.expanded = nil
	application.refreshGit(workspace)
	var installSnapshot func()
	select {
	case installSnapshot = <-jobs:
	case <-time.After(3 * time.Second):
		t.Fatal("initial Git read did not finish")
	}
	path := filepath.Join(root, "late.txt")
	if err := os.WriteFile(path, []byte("late change"), 0600); err != nil {
		t.Fatal(err)
	}
	for range 50 {
		application.applyWorkspaceChanges(workspace, map[string]bool{path: true})
	}
	installSnapshot()
	if !workspace.gitLoading || workspace.gitRefreshPending {
		t.Fatal("queued batch did not start exactly one follow-up read")
	}
	pumpUpdate(t, jobs)
	if workspace.gitLoading || len(workspace.git.Entries) != 1 || workspace.git.Entries[0].Path != "late.txt" || len(jobs) != 0 {
		t.Fatalf("late Git change lost or redundant reads queued: %+v jobs=%d", workspace.git, len(jobs))
	}
}
