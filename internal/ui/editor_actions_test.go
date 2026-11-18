package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/editor"
	gitview "github.com/shenming0821/Sasa/internal/git"
	"github.com/shenming0821/Sasa/internal/storage"
	"github.com/shenming0821/Sasa/internal/workspace"
)

func TestEditorFileMenusAndRename(t *testing.T) {
	a, queue, root := p4App(t)
	a.historyOpen, a.rightTab = true, "files"
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	tester := native.NewTester(a.view, 1280, 800)
	page := a.currentFile()
	p4Wait(t, tester, queue, func() bool { return page.loaded && !page.checking && len(a.workspace(root).children[root]) > 0 })
	var copied string
	a.fileAction.copy = func(text string) error { copied = text; return nil }
	for _, label := range []string{"文件树 " + path, "文件标签 " + path, "文件内容 " + path} {
		for _, item := range []string{"复制路径", "复制相对路径"} {
			if err := tester.RightClick(label); err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"复制路径", "复制相对路径", "重命名", "删除"} {
				if !slices.Contains(tester.Menu(), action) {
					t.Fatal("missing action", label, action, tester.Menu())
				}
			}
			if err := tester.ChooseMenuItem(item); err != nil {
				t.Fatal(err)
			}
			expected := path
			if item == "复制相对路径" {
				expected = "README.md"
			}
			if copied != expected {
				t.Fatalf("copied %q, want %q", copied, expected)
			}
		}
	}
	if err := tester.RightClick("文件标签 " + path); err != nil {
		t.Fatal(err)
	}
	if err := tester.ChooseMenuItem("重命名"); err != nil {
		t.Fatal(err)
	}
	if !a.fileAction.open || a.fileAction.name != "README.md" {
		t.Fatal("rename did not prefill current name")
	}
	tester.Key(native.Cmd, native.KeyW)
	if a.fileAction.open || a.currentFile() != page {
		t.Fatal("closing rename dialog closed the file")
	}
	a.beginFileAction(root, path, "重命名")
	tester.Frame()
	if err := tester.Click("文件名称"); err != nil {
		t.Fatal(err)
	}
	tester.Key(native.Cmd, native.KeyA)
	tester.Type("说明.md")
	if err := tester.Click("确认重命名"); err != nil {
		t.Fatal(err)
	}
	if a.fileAction.open || page.path != filepath.Join(root, "说明.md") {
		t.Fatal("rename failed", a.fileAction.err)
	}
	if _, err := os.Stat(page.path); err != nil {
		t.Fatal(err)
	}
	a.openFile(root, page.path)
	if a.currentFile() != page || len(a.files) != 1 {
		t.Fatal("renamed file opened a duplicate tab")
	}
	if err := os.WriteFile(path, []byte("new old path"), 0600); err != nil {
		t.Fatal(err)
	}
	a.openFile(root, path)
	if a.currentFile() == page || a.currentFile().path != path {
		t.Fatal("recreated old path reused renamed tab")
	}
	p4Wait(t, tester, queue, func() bool { return a.currentFile().loaded })
}

func TestEditorCopyPathWithLineNumber(t *testing.T) {
	a, queue, root := p4App(t)
	a.historyOpen, a.rightTab = true, "files"
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	tester := native.NewTester(a.view, 1280, 800)
	page := a.currentFile()
	p4Wait(t, tester, queue, func() bool {
		return page.loaded && !page.checking && len(a.workspace(root).children[root]) > 0
	})
	var copied string
	a.fileAction.copy = func(text string) error { copied = text; return nil }
	r, ok := tester.Find("文件内容 " + path)
	if !ok {
		t.Fatal("editor area not found")
	}
	for _, line := range []int{2, 1} {
		tester.RightClickAt(r.X+24, r.Y+8+float32(line-1)*20+10)
		if err := tester.ChooseMenuItem("复制路径和行号"); err != nil {
			t.Fatal(err, tester.Menu())
		}
		if want := fmt.Sprintf("%s:%d", path, line); copied != want {
			t.Fatalf("copied %q, want %q", copied, want)
		}
	}
	if err := tester.RightClick("文件树 " + path); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(tester.Menu(), "复制路径和行号") {
		t.Fatal("file tree menu offered a line copy")
	}
	tester.CloseMenu()
}

func TestEditorDiffCopyPathWithLineNumber(t *testing.T) {
	a, queue, root := p4App(t)
	path := filepath.Join(root, "file.go")
	if err := os.WriteFile(path, []byte("ctx1\nadd1\nctx3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	patch := "diff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go\n@@ -1,3 +1,4 @@\n ctx1\n-del\n+add1\n ctx3\n+tail\n"
	review := gitview.ParsePatch(patch)
	d := &filePage{id: "diff", root: root, path: path, kind: "diff", compare: "更改",
		gitDiffState: gitDiffState{gitReview: &review}}
	a.gitSetDiffText(d, patch, &review)
	a.files = map[string]*filePage{d.id: d}
	tester := native.NewTester(func(c *native.Context) {
		native.Row(c).Fill().Children(func() { a.fileView(c, d, false, false) })
	}, 1000, 600)
	p4Wait(t, tester, queue, func() bool { return d.loaded && !d.highlighting })
	var copied string
	a.fileAction.copy = func(text string) error { copied = text; return nil }
	r, ok := tester.Find("文件内容 " + path)
	if !ok {
		t.Fatal("diff area not found")
	}
	// 行序：ctx1(next=1)、del(old=2)、add1(next=2)、ctx3(next=3)、tail(next=4)
	for _, tc := range []struct {
		row, want int
	}{
		{2, 2}, // 新增行取新文件行号
		{1, 2}, // 删除行取旧文件行号
		{0, 1}, // 上下文行
	} {
		tester.RightClickAt(r.X+24, r.Y+8+float32(tc.row)*20+10)
		if err := tester.ChooseMenuItem("复制路径和行号"); err != nil {
			t.Fatalf("row %d: %v menu=%v", tc.row, err, tester.Menu())
		}
		if want := fmt.Sprintf("%s:%d", path, tc.want); copied != want {
			t.Fatalf("row %d copied %q, want %q", tc.row, copied, want)
		}
	}
	// 合成路径（提交视图）不提供行号项
	d.path = filepath.Join(root, "提交 abc12345")
	tester.RightClickAt(r.X+24, r.Y+8+10)
	if slices.Contains(tester.Menu(), "复制路径和行号") {
		t.Fatal("synthetic diff path offered a line copy")
	}
	tester.CloseMenu()
}

func TestEditorRenameDirectoryPreservesDraftAndRecovery(t *testing.T) {
	a, queue, root := p4App(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.store = store
	path := filepath.Join(root, "internal", "main.go")
	a.openFile(root, path)
	tester := native.NewTester(a.view, 1100, 760)
	page := a.currentFile()
	p4Wait(t, tester, queue, func() bool { return page.loaded && !page.checking })
	page.buffer.Set("package main\n// local draft\n")
	page.observe()
	page.caret, page.anchor = 3, 1
	if err := a.renameEditorPath(root, filepath.Dir(path), "源码"); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(root, "源码", "main.go")
	if page.path != next || !page.dirty() || !strings.Contains(page.buffer.String(), "local draft") || page.caret != 3 || page.anchor != 1 {
		t.Fatal("rename lost editor state")
	}
	copies, err := editor.Recoveries(a.recoveryDir())
	if err != nil || len(copies) != 1 || copies[0].Path != next || copies[0].Base.Path != next {
		t.Fatal("recovery not moved", copies, err)
	}
	var snapshot storage.Snapshot
	a.snapshotWorkspaceTabs(&snapshot)
	restored := New(stubBackend{}, "test", root, nil)
	restored.store = store
	if err := restored.restoreWorkspaceTabs(snapshot, ""); err != nil {
		t.Fatal(err)
	}
	if len(restored.files) != 1 || restored.files[page.id].buffer.String() != page.buffer.String() {
		t.Fatal("restart duplicated or lost renamed draft")
	}
	a.saveFile(page, false, false, nil)
	p4Wait(t, tester, queue, func() bool { return !page.saving })
	content, err := os.ReadFile(next)
	if err != nil || string(content) != page.buffer.String() || page.dirty() {
		t.Fatal("saving renamed draft failed", page.err, err)
	}
}

func TestEditorPathFailuresAndDeleteDraft(t *testing.T) {
	a, queue, root := p4App(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.store = store
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	tester := native.NewTester(a.view, 1100, 760)
	page := a.currentFile()
	p4Wait(t, tester, queue, func() bool { return page.loaded && !page.checking })
	for _, name := range []string{"", ".", "..", "../escape", "bad/name", "bad\\name", "bad\nname", "internal"} {
		if err := a.renameEditorPath(root, path, name); err == nil {
			t.Fatal("accepted invalid or existing name", name)
		}
	}
	if err := a.renameEditorPath(root, root, "renamed-root"); err == nil {
		t.Fatal("renamed workspace root")
	}
	a.workspace(filepath.Join(root, "internal"))
	if err := a.renameEditorPath(root, filepath.Join(root, "internal"), "renamed-workspace"); err == nil {
		t.Fatal("renamed an opened nested workspace")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.deleteEditorPath(root, filepath.Join(root, "outside", "keep")); err == nil {
		t.Fatal("followed directory symlink outside workspace")
	}
	page.saving = true
	if err := a.renameEditorPath(root, path, "busy"); err == nil {
		t.Fatal("renamed while saving")
	}
	page.saving = false
	a.fileAction.trash = func(string) error { return errors.New("trash unavailable") }
	if err := a.deleteEditorPath(root, path); err == nil {
		t.Fatal("ignored trash failure")
	}
	if _, err := os.Stat(path); err != nil || a.files[page.id] != page {
		t.Fatal("trash failure changed file or tab", err)
	}
	page.buffer.Set("unsaved draft")
	page.observe()
	trash := filepath.Join(t.TempDir(), "trashed.md")
	a.fileAction.trash = func(source string) error { return os.Rename(source, trash) }
	a.beginFileAction(root, path, "删除")
	tester.Frame()
	if err := tester.Click("取消文件操作"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("cancel deleted file", err)
	}
	a.beginFileAction(root, path, "删除")
	tester.Frame()
	if err := tester.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	if a.fileAction.open || !page.missing || a.files[page.id] != page || page.buffer.String() != "unsaved draft" {
		t.Fatal("delete lost unsaved buffer", a.fileAction.err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file was not moved", err)
	}
	copies, err := editor.Recoveries(a.recoveryDir())
	if err != nil || len(copies) != 1 || copies[0].Text != "unsaved draft" {
		t.Fatal("delete did not preserve durable draft", copies, err)
	}
}

func TestEditorDeleteDirectoryClosesCleanTabsAndRefreshesTree(t *testing.T) {
	a, queue, root := p4App(t)
	a.historyOpen, a.rightTab = true, "files"
	path := filepath.Join(root, "internal", "main.go")
	a.openFile(root, path)
	tester := native.NewTester(a.view, 1280, 800)
	page := a.currentFile()
	state := a.workspace(root)
	p4Wait(t, tester, queue, func() bool { return page.loaded && !page.checking && len(state.children[filepath.Dir(path)]) > 0 })
	trash := filepath.Join(t.TempDir(), "directory")
	a.fileAction.trash = func(source string) error { return os.Rename(source, trash) }
	if err := a.deleteEditorPath(root, filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tester, queue, func() bool { return !state.loading[root] })
	if a.files[page.id] != nil || state.expanded[filepath.Dir(path)] || slices.ContainsFunc(a.tabs, func(tab storage.Tab) bool { return tab.ID == page.id }) {
		t.Fatal("delete left clean tab or expanded directory behind")
	}
	if slices.ContainsFunc(state.children[root], func(entry workspace.Entry) bool { return entry.Path == filepath.Dir(path) }) {
		t.Fatal("deleted directory still in file tree")
	}
}
