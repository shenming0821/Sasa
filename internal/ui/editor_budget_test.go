package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
)

func TestP4DirtyBudgetRejectsWholeEditWithoutChangingOtherDrafts(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	a.files = map[string]*filePage{}
	text := strings.Repeat(strings.Repeat("x", 99)+"\n", 40000)
	for i := range 16 {
		buffer := native.NewTextBuffer(text)
		d := &filePage{id: fmt.Sprint(i), kind: "file", loaded: true, modified: true, buffer: buffer, observed: buffer.Snapshot()}
		a.files[d.id] = d
	}
	target := &filePage{id: "target", kind: "file", loaded: true, buffer: native.NewTextBuffer("")}
	target.observed = target.buffer.Snapshot()
	a.files[target.id] = target
	target.buffer.Set(text)
	a.observeFile(target)
	if target.buffer.String() != "" || !strings.Contains(target.err, "64 MiB") {
		t.Fatal("oversized aggregate edit was not rejected as a whole")
	}
	for id, d := range a.files {
		if id != target.id && (!d.dirty() || d.buffer.String() != text) {
			t.Fatal("budget enforcement changed another draft", id)
		}
	}
}

func TestP4CleanCacheEvictsOldestHiddenDocument(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	a.files = map[string]*filePage{}
	text := strings.Repeat(strings.Repeat("x", 99)+"\n", 40000)
	for i := range 17 {
		buffer := native.NewTextBuffer(text)
		d := &filePage{id: fmt.Sprint(i), kind: "file", loaded: true, buffer: buffer, observed: buffer.Snapshot(), savedText: text, lastVisited: time.Unix(int64(i), 0)}
		a.files[d.id] = d
	}
	a.activeTab = "16"
	active := a.files[a.activeTab].buffer
	a.trimEditorCaches()
	if a.files["0"].buffer != nil || a.files["0"].loaded || a.files["1"].buffer == nil || a.files["16"].buffer != active {
		t.Fatal("cache eviction did not preserve recent/active documents")
	}
}

func TestP4UndoBudgetReleasesHiddenHistoryWithoutLosingDirtyText(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	a.files = map[string]*filePage{}
	text := strings.Repeat(strings.Repeat("x", 99)+"\n", 40000)
	for i := range 5 {
		buffer := native.NewTextBuffer(text)
		snapshot := buffer.Snapshot()
		d := &filePage{id: fmt.Sprint(i), kind: "file", loaded: true, modified: true, buffer: buffer, observed: snapshot, undo: []native.TextSnapshot{snapshot, snapshot}, lastVisited: time.Unix(int64(i), 0)}
		a.files[d.id] = d
	}
	a.activeTab = "4"
	a.trimEditorCaches()
	if len(a.files["0"].undo) != 0 || len(a.files["1"].undo) != 2 || len(a.files["4"].undo) != 2 {
		t.Fatal("undo eviction did not release the oldest hidden history first")
	}
	for id, d := range a.files {
		if !d.dirty() || d.buffer.String() != text {
			t.Fatal("undo trimming lost unsaved text", id)
		}
	}
}
