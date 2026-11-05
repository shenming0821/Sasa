package ui

import (
	"path/filepath"
	"slices"
	"testing"

	native "github.com/egoist/mygo/ui"
)

func TestP4FolderContentSearchMenuScopeAndOpen(t *testing.T) {
	a, q, root := p4App(t)
	a.historyOpen, a.rightTab = true, "files"
	tt := native.NewTester(a.view, 1100, 760)
	w := a.workspace(root)
	p4Wait(t, tt, q, func() bool { return len(w.children[root]) > 0 })
	dir := filepath.Join(root, "internal")
	if err := tt.RightClick("文件树 " + dir); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), "在文件夹中搜索") {
		t.Fatal("folder menu missing search item", tt.Menu())
	}
	if err := tt.ChooseMenuItem("在文件夹中搜索"); err != nil {
		t.Fatal(err)
	}
	if !w.searchContent || w.searchScope != dir || !a.historyOpen || a.rightTab != "files" {
		t.Fatal("folder search did not arm content mode", w.searchContent, w.searchScope)
	}
	if !tt.HasText("范围：internal") {
		t.Fatal("scope chip missing")
	}
	if err := tt.Click("搜索文件内容"); err != nil {
		t.Fatal(err)
	}
	tt.Type("fuyu")
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && len(w.contentSearch.Matches) == 1 })
	m := w.contentSearch.Matches[0]
	if m.Path != filepath.Join(dir, "main.go") || m.Line != 4 || m.Col != 14 {
		t.Fatal("unexpected match", m)
	}
	if !tt.HasText("println(\"你好 Fuyu\")") {
		t.Fatal("match preview missing")
	}
	tt.Key(0, native.KeyEnter)
	p4Wait(t, tt, q, func() bool {
		d := a.currentFile()
		return d != nil && d.loaded && d.pendingLocation == nil && d.path == m.Path
	})
	d := a.currentFile()
	if line := d.buffer.Snapshot().LineAt(d.caret); line != 3 {
		t.Fatal("match open did not land on the matched line", line)
	}
	if err := tt.Click("清除搜索范围"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && len(w.contentSearch.Matches) == 2 })
	if w.searchScope != "" {
		t.Fatal("scope clear kept folder restriction")
	}
	if err := tt.Click("全文搜索"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && !w.searchContent })
	if len(w.contentSearch.Matches) != 0 {
		t.Fatal("filename mode kept content matches")
	}
	if err := tt.Click("查找文件"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, native.KeyEscape)
	tt.Frame()
	if w.query != "" {
		t.Fatal("Escape did not clear search")
	}
	if err := tt.RightClick("文件树 " + filepath.Join(root, "README.md")); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(tt.Menu(), "在文件夹中搜索") {
		t.Fatal("file row shows folder-only search item", tt.Menu())
	}
}

func TestP4ContentSearchKeyboardAndModeToggle(t *testing.T) {
	a, q, root := p4App(t)
	a.historyOpen, a.rightTab = true, "files"
	tt := native.NewTester(a.view, 1100, 760)
	w := a.workspace(root)
	p4Wait(t, tt, q, func() bool { return len(w.children[root]) > 0 })
	if err := tt.Click("查找文件"); err != nil {
		t.Fatal(err)
	}
	tt.Type("fuyu")
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && w.searchQuery == w.searchKey() })
	if len(w.search.Entries) != 0 {
		t.Fatal("filename search matched content-only text")
	}
	if err := tt.Click("全文搜索"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && len(w.contentSearch.Matches) == 2 })
	if err := tt.Click("搜索文件内容"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, native.KeyDown)
	if w.searchIndex != 1 || w.selected != w.contentSearch.Matches[1].Path {
		t.Fatal("Down did not move to the second match")
	}
	tt.Key(0, native.KeyEnter)
	p4Wait(t, tt, q, func() bool {
		d := a.currentFile()
		return d != nil && d.loaded && d.pendingLocation == nil
	})
	second := w.contentSearch.Matches[1]
	if a.currentFile().path != second.Path {
		t.Fatal("Enter opened the wrong match", a.currentFile().path, second.Path)
	}
}
