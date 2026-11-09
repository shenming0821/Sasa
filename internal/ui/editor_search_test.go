package ui

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/workspace"
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
	if w.searchInclude != "./internal" || !a.historyOpen || a.rightTab != "files" {
		t.Fatal("folder search did not set the include pattern", w.searchInclude)
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
	if err := tt.Click("搜索文件内容"); err != nil {
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

func TestP4ContentSearchKeyboardAndOpen(t *testing.T) {
	a, q, root := p4App(t)
	a.historyOpen, a.rightTab = true, "files"
	tt := native.NewTester(a.view, 1100, 760)
	w := a.workspace(root)
	p4Wait(t, tt, q, func() bool { return len(w.children[root]) > 0 })
	if err := tt.Click("搜索文件内容"); err != nil {
		t.Fatal(err)
	}
	tt.Type("fuyu")
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && len(w.contentSearch.Matches) == 2 })
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
	w.searchDetails = true
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

func TestP4SearchFiltersCollapseAndFolderSearchReveal(t *testing.T) {
	a, q, root := p4App(t)
	a.historyOpen, a.rightTab = true, "files"
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1100, 760)
	p4Wait(t, tt, q, func() bool { return len(w.children[root]) > 0 })
	if _, ok := tt.Find("包含的文件"); ok {
		t.Fatal("include field visible while collapsed by default")
	}
	if err := tt.Click("展开搜索筛选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("包含的文件"); !ok {
		t.Fatal("expanded filters not visible")
	}
	if err := tt.Click("收起搜索筛选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("包含的文件"); ok {
		t.Fatal("collapsed filters still visible")
	}
	a.searchInFolder(root, filepath.Join(root, "internal"))
	tt.Frame()
	if _, ok := tt.Find("包含的文件"); !ok || w.searchInclude != "./internal" {
		t.Fatal("folder search did not reveal filters", w.searchInclude)
	}
}

func TestP4FindHighlightsAllMatchesAndSteps(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("fuyu one\nfuyu two\nFUYU three\n")
	d.observe()
	d.findOpen, d.query = true, "fuyu"
	a.findInFile(d, false)
	p4Wait(t, tt, q, func() bool { return d.findScanned && len(d.findMatches) == 3 })
	if d.findResult != "1/3" || d.anchor != 0 || d.caret != 4 {
		t.Fatal("first match not revealed", d.findResult, d.anchor, d.caret)
	}
	a.findInFile(d, false)
	if d.findIndex != 1 || d.findResult != "2/3" || d.anchor != 9 || d.caret != 13 {
		t.Fatal("step did not reach the second match", d.findIndex, d.findResult, d.anchor, d.caret)
	}
	a.findInFile(d, true)
	if d.findIndex != 0 || d.findResult != "1/3" {
		t.Fatal("previous did not go back", d.findIndex, d.findResult)
	}
	a.findInFile(d, true)
	if d.findIndex != 2 || d.findResult != "3/3" || d.anchor != 18 || d.caret != 22 {
		t.Fatal("previous did not wrap to the last match", d.findIndex, d.findResult, d.anchor, d.caret)
	}
	// Clearing the query blanks the counter and drops the cached matches.
	d.query = ""
	tt.Frame()
	if d.findResult != "" || d.findIndex != -1 || d.findScanned || len(d.findMatches) != 0 {
		t.Fatal("cleared query kept stale find state", d.findResult, d.findIndex, d.findScanned)
	}
	// The find bar floats over the editor: closing it does not shift content.
	withBar, _ := tt.Find("文件内容 " + path)
	d.findOpen = false
	tt.Frame()
	withoutBar, _ := tt.Find("文件内容 " + path)
	if withBar != withoutBar {
		t.Fatal("find bar shifted the editor content", withBar, withoutBar)
	}
	// Closing find falls back to the workspace content search: its keywords
	// highlight the file without touching the selection.
	d.findOpen = false
	w := a.workspace(root)
	w.query = "two"
	anchor, caret := d.anchor, d.caret
	p4Wait(t, tt, q, func() bool { return d.findScanned && len(d.findKey) > 0 && d.findKey[0] == 's' && len(d.findMatches) == 1 })
	if d.findMatches[0] != (findMatch{14, 17}) || d.pendingSelection != nil || d.anchor != anchor || d.caret != caret {
		t.Fatal("workspace highlight changed selection or mismatched", d.findMatches, d.anchor, d.caret)
	}
	w.query = ""
	tt.Frame()
	if d.findScanned {
		t.Fatal("cleared search kept match highlight state")
	}
}

func TestScanBufferContentMatches(t *testing.T) {
	snap := native.NewTextBuffer("Fuyu 你好\nfuyu 世界\nFUYU\n").Snapshot()
	matches, err := scanBufferContentMatches(context.Background(), snap, workspace.ContentQuery{Text: "fuyu"})
	if err != nil || len(matches) != 3 {
		t.Fatal("case-insensitive matches", matches, err)
	}
	if matches[0] != (findMatch{0, 4}) || matches[1] != (findMatch{8, 12}) || matches[2] != (findMatch{16, 20}) {
		t.Fatal("wrong rune offsets", matches)
	}
	matches, err = scanBufferContentMatches(context.Background(), snap, workspace.ContentQuery{Text: "fuyu", MatchCase: true})
	if err != nil || len(matches) != 1 || matches[0] != (findMatch{8, 12}) {
		t.Fatal("case-sensitive matches", matches, err)
	}
	matches, err = scanBufferContentMatches(context.Background(), snap, workspace.ContentQuery{Text: "uyu", WholeWord: true})
	if err != nil || len(matches) != 0 {
		t.Fatal("whole word matched inside a word", matches, err)
	}
	matches, err = scanBufferContentMatches(context.Background(), snap, workspace.ContentQuery{Text: "世界", WholeWord: true})
	if err != nil || len(matches) != 1 || matches[0] != (findMatch{13, 15}) {
		t.Fatal("whole word missed a CJK match", matches, err)
	}
	if _, err = scanBufferContentMatches(context.Background(), snap, workspace.ContentQuery{Text: "(", Regex: true}); err == nil {
		t.Fatal("invalid regex reported no error")
	}
}

func TestFindBracketPair(t *testing.T) {
	snap := native.NewTextBuffer("f(a {b} c)\nd(e[f])g\n").Snapshot()
	pair := func(caret int) [2]int {
		if a, b, ok := findBracketPair(snap, caret); ok {
			return [2]int{a, b}
		}
		return [2]int{-1, -1}
	}
	for _, tc := range []struct {
		caret int
		want  [2]int
	}{
		{1, [2]int{1, 9}},   // on "("
		{2, [2]int{1, 9}},   // just after "("
		{9, [2]int{1, 9}},   // on ")"
		{10, [2]int{1, 9}},  // just after ")"
		{4, [2]int{4, 6}},   // on "{"
		{7, [2]int{4, 6}},   // just after "}"
		{5, [2]int{4, 6}},   // between "{" and "b"
		{12, [2]int{12, 17}}, // "(" with a [] pair inside
		{14, [2]int{14, 16}},
		{18, [2]int{12, 17}},
		{0, [2]int{-1, -1}},
		{3, [2]int{-1, -1}},
		{8, [2]int{-1, -1}},
		{11, [2]int{-1, -1}},
		{20, [2]int{-1, -1}},
	} {
		if got := pair(tc.caret); got != tc.want {
			t.Fatalf("caret %d: got %v, want %v", tc.caret, got, tc.want)
		}
	}
	if _, _, ok := findBracketPair(native.NewTextBuffer("a(b").Snapshot(), 1); ok {
		t.Fatal("unmatched open bracket reported a pair")
	}
	if _, _, ok := findBracketPair(native.NewTextBuffer("a)b").Snapshot(), 1); ok {
		t.Fatal("unmatched close bracket reported a pair")
	}
}

func TestP4BracketPairHighlightFollowsCaret(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("f(a {b} c)\n")
	d.observe()
	d.pendingSelection = &[2]int{1, 1}
	tt.Frame()
	if d.bracketStart != 1 || d.bracketEnd != 9 {
		t.Fatal("bracket pair not highlighted", d.bracketStart, d.bracketEnd)
	}
	d.pendingSelection = &[2]int{3, 3}
	tt.Frame()
	if d.bracketEnd != 0 {
		t.Fatal("stale bracket pair kept", d.bracketStart, d.bracketEnd)
	}
	d.pendingSelection = &[2]int{5, 5}
	tt.Frame()
	if d.bracketStart != 4 || d.bracketEnd != 6 {
		t.Fatal("inner pair not highlighted", d.bracketStart, d.bracketEnd)
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
