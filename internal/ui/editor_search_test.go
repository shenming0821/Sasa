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
	if !w.searchContent || w.searchInclude != "./internal" || !a.historyOpen || a.rightTab != "files" {
		t.Fatal("folder search did not arm content mode", w.searchContent, w.searchInclude)
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
	if !tt.HasText("println(\"你好 Fuyu\")") || !tt.HasText("1 个结果，位于 1 个文件") {
		t.Fatal("match preview or summary missing")
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
	w.searchInclude = ""
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && len(w.contentSearch.Matches) == 2 })
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

func TestP4ContentSearchOptionsGroupsAndCollapse(t *testing.T) {
	a, q, root := p4App(t)
	a.historyOpen, a.rightTab = true, "files"
	w := a.workspace(root)
	w.searchContent = true
	tt := native.NewTester(a.view, 1100, 760)
	p4Wait(t, tt, q, func() bool { return len(w.children[root]) > 0 })
	if err := tt.Click("搜索文件内容"); err != nil {
		t.Fatal(err)
	}
	tt.Type("fuyu")
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && len(w.contentSearch.Matches) == 2 })
	readme := filepath.Join(root, "README.md")
	if !tt.HasText("2 个结果，位于 2 个文件") || len(contentSearchRows(w.contentSearch.Matches, nil)) != 4 {
		t.Fatal("grouped results or summary missing")
	}
	if err := tt.Click("折叠 README.md"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.searchCollapsed[readme] || tt.HasText("# Fuyu") {
		t.Fatal("file heading did not collapse its matches")
	}
	if err := tt.Click("搜索文件内容"); err != nil {
		t.Fatal(err)
	}
	for w.contentSearch.Matches[w.searchIndex].Path != readme {
		tt.Key(0, native.KeyDown)
	}
	if w.searchCollapsed[readme] {
		t.Fatal("keyboard selection did not expand the collapsed file")
	}
	if err := tt.Click("全部折叠结果"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("全部展开结果") && !w.searchCollapsed[readme] {
		t.Fatal("collapse all did not collapse")
	}
	if err := tt.Click("区分大小写"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && w.searchQuery == w.searchKey() })
	if len(w.contentSearch.Matches) != 0 || w.searchCollapsed != nil {
		t.Fatal("match case did not rerun the search", w.contentSearch.Matches)
	}
	w.searchCase = false
	if err := tt.Click("使用正则表达式"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && len(w.contentSearch.Matches) == 2 })
	if err := tt.Click("排除的文件"); err != nil {
		t.Fatal(err)
	}
	tt.Type("*.md")
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && len(w.contentSearch.Matches) == 1 })
	if w.contentSearch.Matches[0].Path != filepath.Join(root, "internal", "main.go") {
		t.Fatal("exclude pattern kept README", w.contentSearch.Matches)
	}
	w.query = "("
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && w.error != "" })
	if !tt.HasText("正则表达式无效") {
		t.Fatal("invalid regex error not shown", w.error)
	}
}

func TestRenamedSearchIncludeFollowsFolder(t *testing.T) {
	root := "/w"
	if got := renamedSearchInclude(root, "./a/b", "/w/a", "/w/c"); got != "./c/b" {
		t.Fatal(got)
	}
	if got := renamedSearchInclude(root, "./a", "/w/a", ""); got != "" {
		t.Fatal(got)
	}
	if got := renamedSearchInclude(root, "*.go, ./a", "/w/a", "/w/c"); got != "*.go, ./a" {
		t.Fatal(got)
	}
	if got := renamedSearchInclude(root, "./other", "/w/a", "/w/c"); got != "./other" {
		t.Fatal(got)
	}
}
