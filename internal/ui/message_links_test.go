package ui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shenming0821/Fuyu/internal/app"
)

func TestMessageFileLinkPaths(t *testing.T) {
	root := t.TempDir()
	for _, destination := range []string{"docs/实施 计划.md", "./docs/实施%20计划.md#当前接续点", filepath.Join(root, "docs/实施 计划.md"), (&url.URL{Scheme: "file", Path: filepath.Join(root, "docs/实施 计划.md")}).String()} {
		t.Run(destination, func(t *testing.T) {
			gotRoot, path, err := messageFileLink(root, destination)
			if err != nil || gotRoot != root || path != filepath.Join(root, "docs/实施 计划.md") {
				t.Fatalf("root=%q path=%q err=%v", gotRoot, path, err)
			}
		})
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	gotRoot, path, err := messageFileLink(root, outside)
	if err != nil || gotRoot != filepath.Dir(outside) || path != outside {
		t.Fatalf("outside root=%q path=%q err=%v", gotRoot, path, err)
	}
	for _, destination := range []string{"javascript:alert(1)", "command:run", "#heading", ""} {
		_, path, err := messageFileLink(root, destination)
		if err != nil || path != "" {
			t.Fatalf("unexpected file for %q: %q %v", destination, path, err)
		}
	}
	for _, destination := range []string{"file://server/path", "//server/path", "file:relative", "bad%zz", "bad%00.md"} {
		if _, _, err := messageFileLink(root, destination); err == nil {
			t.Errorf("invalid link accepted: %q", destination)
		}
	}
}

func TestMessageLinkClickOpensEditor(t *testing.T) {
	for _, done := range []bool{true, false} {
		t.Run(fmt.Sprint(done), func(t *testing.T) {
			a, queue, root := p4App(t)
			a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
			message := &app.Message{ID: "linked-file", Role: "assistant", Text: "查看[实施计划](README.md:5011:8)。", Done: done}
			a.State.Messages = []*app.Message{message}
			tt := native.NewTester(a.view, 1000, 760)
			if err := tt.Click("实施计划"); err != nil {
				t.Fatal(err)
			}
			p4Wait(t, tt, queue, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
			if a.currentFile().path != filepath.Join(root, "README.md") || len(tt.OpenedURLs()) != 0 {
				t.Fatal("file link did not open in the workspace editor")
			}
			if len(a.tabs) != 2 {
				t.Fatalf("expected session and file tabs, got %d", len(a.tabs))
			}
		})
	}
}

func TestMessageFileLinksWithLocations(t *testing.T) {
	root := t.TempDir()
	for _, destination := range []string{
		"src/pages/factoryWestDrug/westDrug.vue:5011",
		"src/pages/factoryWestDrug/westDrug.vue:5011:8",
		filepath.Join(root, "src/pages/factoryWestDrug/westDrug.vue:5011"),
		"file://" + filepath.Join(root, "src/pages/factoryWestDrug/westDrug.vue:5011:8"),
		"src/pages/factoryWestDrug/westDrug.vue:5011#L5011",
		"src/pages/factoryWestDrug/westDrug.vue%3A5011",
	} {
		t.Run(destination, func(t *testing.T) {
			gotRoot, path, err := messageFileLink(root, destination)
			if err != nil || gotRoot != root || path != filepath.Join(root, "src/pages/factoryWestDrug/westDrug.vue") {
				t.Fatalf("root=%q path=%q err=%v", gotRoot, path, err)
			}
		})
	}
	for _, destination := range []string{"README.md:12", "README.md:12:3"} {
		_, path, err := messageFileLink(root, destination)
		if err != nil || path != filepath.Join(root, "README.md") {
			t.Fatalf("destination=%q path=%q err=%v", destination, path, err)
		}
	}
	for _, destination := range []string{"https://example.com:8080", "command:run:12"} {
		_, path, err := messageFileLink(root, destination)
		if err != nil || path != "" {
			t.Fatalf("unexpected file for %q: %q %v", destination, path, err)
		}
	}
}

func TestMessageLinkLocatesLineAndPreservesBuffer(t *testing.T) {
	for _, done := range []bool{true, false} {
		t.Run(fmt.Sprint(done), func(t *testing.T) {
			a, queue, root := p4App(t)
			text := strings.Repeat("你好 Fuyu\n", 160)
			path := filepath.Join(root, "README.md")
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			a.md.Reveal = &markdown.Reveal{Unit: markdown.AtOnce}
			a.State.Messages = []*app.Message{{ID: "locations", Role: "assistant", Text: "[深处](README.md:120:3) [开头](README.md#L2) [末尾](README.md:9999:9999)", Done: done}}
			tt := native.NewTester(a.view, 1000, 760)
			if err := tt.Click("深处"); err != nil {
				t.Fatal(err)
			}
			p4Wait(t, tt, queue, func() bool {
				return a.currentFile() != nil && a.currentFile().loaded && a.currentFile().pendingLocation == nil
			})
			file := a.currentFile()
			tt.Frame()
			if file.caret != 119*8+2 || file.anchor != file.caret || file.scroll.Y <= 0 {
				t.Fatalf("line link caret=%d anchor=%d scroll=%v", file.caret, file.anchor, file.scroll.Y)
			}
			bounds, ok := tt.Find("文件内容 " + path)
			initialY := 8 + float32(119)*20 + 10 - file.scroll.Y
			if !ok || initialY < bounds.H/2-1 || initialY > bounds.H/2+1 {
				t.Fatalf("initial link target=%v viewport=%+v scroll=%v", initialY, bounds, file.scroll.Y)
			}
			for _, height := range []int{760, 480, 1000} {
				tt.SetSize(1000, height)
				a.selectSession(a.session)
				tt.Frame()
				if err := tt.Click("深处"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				tt.Frame()
				bounds, ok := tt.Find("文件内容 " + path)
				targetY := 8 + float32(119)*20 + 10 - file.scroll.Y
				if !ok || targetY < bounds.H/2-1 || targetY > bounds.H/2+1 {
					t.Fatalf("height=%d: target=%v viewport=%+v scroll=%v", height, targetY, bounds, file.scroll.Y)
				}
				file.scroll.Y -= 40
				tt.Frame()
				tt.Frame()
				if after := 8 + float32(119)*20 + 10 - file.scroll.Y; after < targetY+39 || after > targetY+41 {
					t.Fatalf("link centering overrode later scrolling: before=%v after=%v", targetY, after)
				}
			}
			file.buffer.Set(text + "未保存")
			file.observe()
			for _, target := range []struct {
				label string
				caret int
			}{
				{"开头", 8},
				{"末尾", 160*8 + 3},
			} {
				a.selectSession(a.session)
				tt.Frame()
				if err := tt.Click(target.label); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				tt.Frame()
				if a.currentFile() != file || len(a.files) != 1 || file.caret != target.caret || file.buffer.String() != text+"未保存" || !file.dirty() {
					t.Fatalf("%s: caret=%d expected=%d dirty=%v", target.label, file.caret, target.caret, file.dirty())
				}
				if target.label == "开头" && file.scroll.Y != 0 || target.label == "末尾" && file.scroll.Y != file.scroll.MaxY {
					t.Fatalf("%s: edge scroll=%v max=%v", target.label, file.scroll.Y, file.scroll.MaxY)
				}
			}
		})
	}
}

func TestMessageLinkLocationFormats(t *testing.T) {
	for _, test := range []struct {
		link         string
		line, column int
	}{
		{"file:///tmp/code.go:12:3", 12, 3},
		{"/tmp/code.go#L12C3", 12, 3},
		{"code.go#L12-L20", 12, 1},
		{"code.go%3A12%3A3", 12, 3},
		{"code.go:12#L20", 12, 1},
		{"code.go#heading", 0, 0},
		{"code.go:0", 0, 0},
		{"code.go:12:0", 0, 0},
		{"code.go:99999999999999999999999", 0, 0},
	} {
		t.Run(test.link, func(t *testing.T) {
			position := messageLinkLocation(test.link)
			if test.line == 0 {
				if position != nil {
					t.Fatalf("unexpected position: %+v", position)
				}
			} else if position == nil || position.line != test.line || position.column != test.column {
				t.Fatalf("position=%+v expected=%d:%d", position, test.line, test.column)
			}
		})
	}
}

func TestMessageLinksUseSelectedSessionAndReuseFile(t *testing.T) {
	a, queue, first := p4App(t)
	second := t.TempDir()
	path := filepath.Join(second, "README.md")
	if err := os.WriteFile(path, []byte("second session file"), 0600); err != nil {
		t.Fatal(err)
	}
	a.newSession(second)
	a.State.Messages = []*app.Message{{ID: "second", Role: "assistant", Text: "查看[会话文件](README.md)。", Done: true}}
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.Click("会话文件"); err != nil {
		t.Fatal(err)
	}
	if file := a.currentFile(); file == nil || file.path != path {
		t.Fatalf("clicked file=%+v cwd=%q error=%q", file, a.State.Cwd, a.State.Error)
	}
	p4Wait(t, tt, queue, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	if a.currentFile().path != path || a.currentFile().root == first {
		t.Fatal("link resolved against another session")
	}
	document := a.currentFile()
	document.buffer.Set("unsaved edits")
	document.observe()
	a.selectSession(a.session)
	tt.Frame()
	if err := tt.Click("会话文件"); err != nil {
		t.Fatal(err)
	}
	if len(a.files) != 1 || a.currentFile() != document || document.buffer.String() != "unsaved edits" {
		t.Fatal("reopening a file did not preserve its tab and unsaved buffer")
	}
}

func TestMessageExternalAndMissingLinks(t *testing.T) {
	a, queue, _ := p4App(t)
	a.State.Messages = []*app.Message{{ID: "links", Role: "assistant", Text: "[网页](https://example.com) [危险](javascript:alert) [缺失文件](missing.md)", Done: true}}
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.Click("网页"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("危险"); err != nil {
		t.Fatal(err)
	}
	if len(tt.OpenedURLs()) != 1 || tt.OpenedURLs()[0] != "https://example.com" || len(a.files) != 0 {
		t.Fatal("external link routing changed")
	}
	if err := tt.Click("缺失文件"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, queue, func() bool { return a.currentFile() != nil && a.currentFile().err != "" })
}
