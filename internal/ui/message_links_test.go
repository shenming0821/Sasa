package ui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
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
			message := &app.Message{ID: "linked-file", Role: "assistant", Text: "查看[实施计划](README.md)。", Done: done}
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
