package ui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
)

func TestFilePreviewTreeTabAndEdit(t *testing.T) {
	a, queue, root := p4App(t)
	a.historyOpen, a.rightTab = true, "files"
	first := filepath.Join(root, "README.md")
	second := filepath.Join(root, "second.txt")
	third := filepath.Join(root, "third.txt")
	for _, path := range []string{second, third} {
		if err := os.WriteFile(path, []byte("original\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tester := native.NewTester(a.view, 1280, 800)
	pane := focusedPane(a.layoutFor(root))
	tree := a.workspace(root)
	p4Wait(t, tester, queue, func() bool { return len(tree.children[root]) == 4 })
	clickTree := func(path string) *filePage {
		t.Helper()
		tester.Advance(time.Second)
		if err := tester.Click("文件树 " + path); err != nil {
			t.Fatal(err)
		}
		p4Wait(t, tester, queue, func() bool {
			d := a.currentFile()
			return d != nil && d.path == path && d.loaded
		})
		return a.currentFile()
	}
	old := clickTree(first)
	if !old.preview {
		t.Fatal("single click created a regular tab")
	}
	index := slices.Index(pane.Tabs, old.id)
	preview := clickTree(second)
	if !preview.preview || index < 0 || slices.Index(pane.Tabs, preview.id) != index || a.files[old.id] != nil || len(a.tabs) != 2 {
		t.Fatalf("preview was not replaced in place: %v", pane.Tabs)
	}
	tab, ok := tester.Find("文件标签 " + second)
	if !ok {
		t.Fatal("preview tab missing from the strip")
	}
	tester.Advance(time.Second)
	tester.ClickAt(tab.X+tab.W/2, tab.Y+tab.H/2)
	tester.ClickAt(tab.X+tab.W/2, tab.Y+tab.H/2)
	tester.Frame()
	if preview.preview {
		t.Fatal("double-clicking the label left it replaceable")
	}
	draft := clickTree(third)
	if err := tester.Click("文件内容 " + third); err != nil {
		t.Fatal(err)
	}
	tester.Type("local")
	tester.Frame()
	if draft.preview || !draft.dirty() {
		t.Fatal("typing in the preview did not keep the edited tab")
	}
	next := clickTree(first)
	if !next.preview || a.files[draft.id] != draft || a.files[preview.id] != preview {
		t.Fatal("next preview did not preserve both regular tabs")
	}
	if selected := clickTree(second); selected != preview || selected.preview || a.files[next.id] != next {
		t.Fatal("reopening an existing regular tab disturbed the preview")
	}
}

func TestFilePreviewReplacementKeepsContentUntilReadCompletes(t *testing.T) {
	a, queue, root := p4App(t)
	tester := native.NewTester(a.view, 1280, 800)
	first := filepath.Join(root, "README.md")
	last := filepath.Join(root, "internal", "main.go")
	a.openFileWithPreview(root, first, true)
	p4Wait(t, tester, queue, func() bool { return a.currentFile().loaded })
	a.openFileWithPreview(root, filepath.Join(root, "missing"), true)
	a.openFileWithPreview(root, last, true)
	tester.Frame()
	if _, ok := tester.Find("文件内容 " + first); !ok || a.currentFile().path != first {
		t.Fatal("replacing a preview removed the current document before the next was ready")
	}
	p4Wait(t, tester, queue, func() bool { return a.currentFile().path == last })
	if _, ok := tester.Find("文件内容 " + last); !ok || !a.currentFile().loaded {
		t.Fatal("replacement tab appeared before its content")
	}
	a.openFileWithPreview(root, filepath.Join(root, "missing"), true)
	p4Wait(t, tester, queue, func() bool { return a.currentFile().err != "" })
	if a.currentFile().path != filepath.Join(root, "missing") {
		t.Fatal("read error appeared on the previous file")
	}
}

func TestFilePreviewIsolatedByPaneAndRestored(t *testing.T) {
	a := New(stubBackend{}, "test", t.TempDir(), nil)
	store := imageStore(t, a)
	root := a.activeWorkspace
	left := focusedPane(a.layoutFor(root))
	a.openFileWithPreview(root, filepath.Join(root, "left.txt"), true)
	first := a.currentFile()
	right := a.splitPane(root, left, "horizontal")
	left = paneLeaves(a.layoutFor(root).Root)[0]
	a.openFileWithPreview(root, filepath.Join(root, "right.txt"), true)
	second := a.currentFile()
	if !first.preview || !second.preview || left.Active != first.id || right.Active != second.id {
		t.Fatal("opening a preview in a split disturbed the other pane")
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	leaves := paneLeaves(restored.layoutFor(root).Root)
	if len(leaves) != 2 || !restored.files[first.id].preview || !restored.files[second.id].preview {
		t.Fatal("restart lost the per-pane previews")
	}
	restored.openFileWithPreview(root, filepath.Join(root, "replacement.txt"), true)
	if restored.files[second.id] != nil || leaves[0].Active != first.id {
		t.Fatal("replacing the restored preview disturbed the other pane")
	}
	restored.openFileWithPreview(root, first.path, true)
	if restored.currentFile().id != first.id || leaves[0].Active != first.id || slices.Contains(leaves[1].Tabs, first.id) {
		t.Fatal("previewing an existing file moved it out of its pane")
	}
}
