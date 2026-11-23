package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
)

func TestQuickOpenGoToLineInCurrentFile(t *testing.T) {
	a, queue, root := p4App(t)
	path := filepath.Join(root, "long.vue")
	if err := os.WriteFile(path, []byte(strings.Repeat("你好世界\n", 4000)+"末行"), 0600); err != nil {
		t.Fatal(err)
	}
	tt := native.NewTester(a.view, 1280, 800)
	a.openFile(root, path)
	p4Wait(t, tt, queue, func() bool {
		return a.currentFile() != nil && a.currentFile().loaded
	})
	d := a.currentFile()
	if err := tt.Click("文件内容 " + path); err != nil {
		t.Fatal(err)
	}
	tt.Key(native.Ctrl, native.KeyG)
	if !a.quickOpen.open || a.quickOpen.lineTarget != d {
		t.Fatal("Ctrl+G did not open the line picker")
	}
	tt.Type("3500:3")
	tt.Key(0, native.KeyEnter)
	snapshot := d.buffer.Snapshot()
	start, _ := snapshot.LineRange(3499)
	p4Wait(t, tt, queue, func() bool { return d.caret == start+2 })
	if a.quickOpen.open || d.anchor != d.caret || d.scroll.Y < 60000 || d.dirty() {
		t.Fatalf("line jump did not reveal a collapsed caret: open=%v anchor=%d caret=%d scroll=%f dirty=%v",
			a.quickOpen.open, d.anchor, d.caret, d.scroll.Y, d.dirty())
	}
	tt.Key(0, native.KeyRight)
	if d.caret != start+3 {
		t.Fatal("focus did not return to the editor after jumping")
	}

	if err := tt.Click("跳转到行"); err != nil {
		t.Fatal(err)
	}
	tt.Type("123:wrong")
	tt.Key(0, native.KeyEnter)
	if !a.quickOpen.open || d.caret != start+3 {
		t.Fatal("invalid column changed the editor or dismissed the picker")
	}
	tt.Key(0, native.KeyEscape)
	if a.quickOpen.open {
		t.Fatal("Escape did not dismiss the line picker")
	}

	tt.Key(native.Cmd, native.KeyP)
	tt.Type("：9999：9999")
	tt.Key(0, native.KeyEnter)
	p4Wait(t, tt, queue, func() bool { return d.caret == snapshot.Len() })
	if a.quickOpen.open || a.currentFile() != d {
		t.Fatal("quick open did not jump to the last position in the current file")
	}
}

type testQuickOpenWindow struct {
	closed      func()
	destroyed   bool
	invalidated int
	width       int
	height      int
}

func (w *testQuickOpenWindow) Destroy() {
	if !w.destroyed {
		w.destroyed = true
		w.closed()
	}
}
func (w *testQuickOpenWindow) Invalidate()       { w.invalidated++ }
func (w *testQuickOpenWindow) SetSize(wd, h int) { w.width, w.height = wd, h }

func TestQuickOpenIndependentSurfaceDismissAndReopen(t *testing.T) {
	a, queue, root := p4App(t)
	var windows []*testQuickOpenWindow
	a.quickOpen.newWindow = func(_ mygo.FlyoutOptions, closed func()) quickOpenWindow {
		w := &testQuickOpenWindow{closed: closed}
		windows = append(windows, w)
		return w
	}
	main := native.NewTester(a.view, 1280, 800)
	main.Key(native.Cmd, native.KeyP)
	p4Wait(t, main, queue, func() bool { return !a.quickOpen.filesLoading })
	if len(windows) != 1 || windows[0].invalidated == 0 || a.quickOpen.root != root {
		t.Fatal("picker surface did not open or receive the file-index update")
	}
	if _, found := main.Find("快速打开文件"); found {
		t.Fatal("picker was also drawn under the native terminal")
	}
	panel := native.NewTester(a.quickOpenWindowView, 1280, 800)
	if err := panel.Click("快速打开文件"); err != nil {
		t.Fatal(err)
	}
	panel.Type("README")
	if a.quickOpen.query != "README" {
		t.Fatalf("independent picker did not receive input: %q", a.quickOpen.query)
	}
	// Index updates can arrive between frames, while the installed key
	// callback still holds the old rows. Enter must use the current results.
	files := a.quickOpen.files
	a.quickOpen.files = nil
	a.quickOpen.filesVersion++
	panel.Key(0, native.KeyEnter)
	if !a.quickOpen.open || a.currentFile() != nil {
		t.Fatal("Enter accepted a stale search result")
	}
	a.quickOpen.files = files
	a.quickOpen.filesVersion++
	panel.Frame()
	panel.Key(0, native.KeyEscape)
	main.Frame()
	if a.quickOpen.open || a.quickOpen.window != nil || !windows[0].destroyed {
		t.Fatal("Escape did not release the independent picker surface")
	}
	main.Key(native.Cmd, native.KeyP)
	if len(windows) != 2 || a.quickOpen.query != "" {
		t.Fatal("reopening did not create a fresh picker")
	}
	main.SetSize(1000, 700)
	if windows[1].width != 1000 || windows[1].height != 700 {
		t.Fatal("picker did not follow the parent size")
	}
	panel = native.NewTester(a.quickOpenWindowView, 1000, 700)
	if err := panel.Click("快速打开文件"); err != nil {
		t.Fatal(err)
	}
	panel.Type("README")
	if !panel.HasText("README.md") {
		t.Fatal("reopened picker did not render matching files")
	}
	panel.Key(native.Cmd, native.KeyW)
	main.Frame()
	if a.quickOpen.open || !windows[1].destroyed {
		t.Fatal("Cmd+W did not dismiss the independent picker")
	}
}

func TestQuickOpenReopenedSurfaceOwnsRecentList(t *testing.T) {
	a, queue, root := p4App(t)
	a.quickOpen.newWindow = func(_ mygo.FlyoutOptions, closed func()) quickOpenWindow {
		return &testQuickOpenWindow{closed: closed}
	}
	main := native.NewTester(a.view, 1280, 800)
	a.openFile(root, filepath.Join(root, "README.md"))
	p4Wait(t, main, queue, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	for attempt := 0; attempt < 2; attempt++ {
		main.Key(native.Cmd, native.KeyP)
		panel := native.NewTester(a.quickOpenWindowView, 1280, 800)
		if !panel.HasText("README.md") {
			t.Fatal("picker surface did not show the recent file")
		}
		panel.Key(0, native.KeyEscape)
		main.Frame()
	}
}

func TestParseQuickQuery(t *testing.T) {
	cases := []struct {
		text         string
		name         string
		line, column int
		symbol       string
	}{
		{"", "", 0, 0, ""},
		{"main.go", "main.go", 0, 0, ""},
		{"internal/main.go", "internal/main.go", 0, 0, ""},
		{"main:3", "main", 3, 0, ""},
		{"main:3:9", "main", 3, 9, ""},
		{"main:", "main", 0, 0, ""},
		{"main:abc", "main:abc", 0, 0, ""},
		{"main:3:abc", "main:3:abc", 0, 0, ""},
		{"main:-3", "main:-3", 0, 0, ""},
		{"main:3:9:2", "main:3", 9, 2, ""},
		{"main@save", "main", 0, 0, "save"},
		{"main@save:3", "main", 3, 0, "save"},
	}
	for _, c := range cases {
		q := parseQuickQuery(c.text)
		if q.name != c.name || q.line != c.line || q.column != c.column || q.symbol != c.symbol {
			t.Fatalf("parseQuickQuery(%q) = {%q %d %d %q}", c.text, q.name, q.line, q.column, q.symbol)
		}
	}
}

func TestQuickOpenRecentFilterKeyboardAndLineJump(t *testing.T) {
	a, queue, root := p4App(t)
	main := filepath.Join(root, "internal", "main.go")
	readme := filepath.Join(root, "README.md")
	tt := native.NewTester(a.view, 1280, 800)
	a.openFile(root, readme)
	p4Wait(t, tt, queue, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	a.openFile(root, main)
	p4Wait(t, tt, queue, func() bool { return a.currentFile() != nil && a.currentFile().path == main && a.currentFile().loaded })

	tt.Key(native.Cmd, native.KeyP)
	if !a.quickOpen.open || a.quickOpen.root != root {
		t.Fatalf("picker did not open on the workspace: open=%v root=%q", a.quickOpen.open, a.quickOpen.root)
	}
	p4Wait(t, tt, queue, func() bool { return !a.quickOpen.filesLoading && a.quickOpen.filesRoot == root })
	if !tt.HasText("recently opened") || !tt.HasText("main.go") || !tt.HasText("README.md") {
		t.Fatalf("recent list missing: %v", tt.Texts())
	}
	items := a.quickOpenItems(parseQuickQuery(""))
	if len(items) < 2 || items[0].path != main || items[1].path != readme {
		t.Fatalf("MRU order wrong: %+v", items)
	}

	if err := tt.Click("快速打开文件"); err != nil {
		t.Fatal(err)
	}
	tt.Type("readme")
	filtered := a.quickOpenItems(parseQuickQuery("readme"))
	if len(filtered) != 1 || filtered[0].name != "README.md" {
		t.Fatalf("filter wrong: %+v", filtered)
	}
	tt.Key(0, native.KeyEnter)
	if a.quickOpen.open {
		t.Fatal("picker stayed open after Enter")
	}
	if d := a.currentFile(); d == nil || d.path != readme {
		t.Fatalf("Enter opened %v", d)
	}

	tt.Key(native.Cmd, native.KeyP)
	tt.Click("快速打开文件")
	tt.Type("main:3")
	tt.Key(0, native.KeyEnter)
	d := a.currentFile()
	if d == nil || d.path != main {
		t.Fatalf("line jump opened %v", d)
	}
	p4Wait(t, tt, queue, func() bool {
		return d.caret > 0 && d.buffer != nil && d.buffer.Snapshot().LineAt(d.caret) == 2
	})
}

func TestQuickOpenInputStaysPutWhileResultsChange(t *testing.T) {
	a, queue, root := p4App(t)
	main := filepath.Join(root, "internal", "main.go")
	readme := filepath.Join(root, "README.md")
	tt := native.NewTester(a.view, 1280, 800)
	a.openFile(root, readme)
	p4Wait(t, tt, queue, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	a.openFile(root, main)
	p4Wait(t, tt, queue, func() bool { return a.currentFile() != nil && a.currentFile().path == main && a.currentFile().loaded })

	tt.Key(native.Cmd, native.KeyP)
	p4Wait(t, tt, queue, func() bool { return !a.quickOpen.filesLoading && a.quickOpen.filesRoot == root })
	before, ok := tt.Find("快速打开文件")
	if !ok {
		t.Fatal("input not found")
	}
	tt.Click("快速打开文件")
	for _, query := range []string{"readme", "main", "zzz"} {
		a.quickOpen.query = query
		tt.Frame()
		after, _ := tt.Find("快速打开文件")
		if before.X != after.X || before.Y != after.Y {
			t.Fatalf("input moved while filtering %q: %+v -> %+v", query, before, after)
		}
	}
}

func TestQuickOpenDismissPaths(t *testing.T) {
	a, queue, root := p4App(t)
	readme := filepath.Join(root, "README.md")
	tt := native.NewTester(a.view, 1280, 800)
	a.openFile(root, readme)
	p4Wait(t, tt, queue, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	for _, how := range []string{"escape", "outside", "cmdw"} {
		tt.Key(native.Cmd, native.KeyP)
		if !a.quickOpen.open {
			t.Fatal("picker did not open before", how)
		}
		switch how {
		case "escape":
			tt.Key(0, native.KeyEscape)
		case "outside":
			tt.ClickAt(10, 10)
		case "cmdw":
			tt.Key(native.Cmd, native.KeyW)
		}
		if a.quickOpen.open {
			t.Fatal("picker survived", how)
		}
	}
	if d := a.currentFile(); d == nil || d.path != readme {
		t.Fatal("dismiss closed the underlying file")
	}
}

func TestQuickOpenKeyboardSelectionAndClick(t *testing.T) {
	a, queue, root := p4App(t)
	a.preferences.Theme, a.preferences.ThemeID = "light", "claude"
	main := filepath.Join(root, "internal", "main.go")
	readme := filepath.Join(root, "README.md")
	tt := native.NewTester(a.view, 1280, 800)
	a.openFile(root, readme)
	p4Wait(t, tt, queue, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	a.openFile(root, main)
	p4Wait(t, tt, queue, func() bool { return a.currentFile() != nil && a.currentFile().path == main && a.currentFile().loaded })

	tt.Key(native.Cmd, native.KeyP)
	p4Wait(t, tt, queue, func() bool { return !a.quickOpen.filesLoading && a.quickOpen.filesRoot == root })
	candidate := func(label string) (float32, float32) {
		t.Helper()
		bounds, ok := tt.Find("打开 " + label)
		if !ok {
			t.Fatalf("missing result %q", label)
		}
		return bounds.X + bounds.W - 12, bounds.Y + bounds.H/2
	}
	checkBackground := func(label string, want native.Color) {
		t.Helper()
		x, y := candidate(label)
		got := tt.Image().RGBAAt(int(x), int(y))
		if got.R != want.R || got.G != want.G || got.B != want.B {
			t.Fatalf("result %q background = %v, want %v", label, got, want)
		}
	}
	selected := native.Hex("#e2e4f8")
	checkBackground("internal/main.go", selected)
	tt.Move(candidate("README.md"))
	checkBackground("README.md", native.Hex("#eeebe5"))
	tt.Key(0, native.KeyDown)
	checkBackground("README.md", selected)
	if a.quickOpen.selected != 1 {
		t.Fatalf("selection = %d", a.quickOpen.selected)
	}
	tt.Key(0, native.KeyUp)
	if a.quickOpen.selected != 0 {
		t.Fatalf("selection = %d", a.quickOpen.selected)
	}
	tt.Key(0, native.KeyDown)
	tt.Key(0, native.KeyEnter)
	if d := a.currentFile(); d == nil || d.path != readme {
		t.Fatal("keyboard selection opened the wrong file")
	}

	tt.Key(native.Cmd, native.KeyP)
	if err := tt.Click("打开 README.md"); err != nil {
		t.Fatal(err)
	}
	if a.quickOpen.open {
		t.Fatal("picker stayed open after row click")
	}
	if d := a.currentFile(); d == nil || d.path != readme {
		t.Fatal("row click opened the wrong file")
	}
}
