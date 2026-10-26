package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
)

func highlightFixture(t *testing.T, text string) (*App, chan func(), *filePage) {
	t.Helper()
	a, q, root := p4App(t)
	d := &filePage{id: "highlight", root: root, path: filepath.Join(root, "source.go"), kind: "file", loaded: true, buffer: native.NewTextBuffer(text)}
	d.observed = d.buffer.Snapshot()
	d.savedText = text
	a.files = map[string]*filePage{d.id: d}
	return a, q, d
}

func highlightUpdate(t *testing.T, q chan func()) {
	t.Helper()
	select {
	case fn := <-q:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("highlight update did not arrive")
	}
}

func TestP4CanceledHighlightLeavesQueueBeforeWorkerIsFree(t *testing.T) {
	a, q, d := highlightFixture(t, "package original\n")
	a.highlightWorkers = make(chan struct{}, 1)
	a.highlightWorkers <- struct{}{}
	a.requestHighlight(d)
	d.buffer.Set("package changed\n")
	d.observe()
	highlightUpdate(t, q)
	if d.highlighting || d.highlightVersion == d.buffer.Version() || len(d.highlights) > 0 {
		t.Fatal("canceled queued highlight retained work or published stale colors")
	}
	<-a.highlightWorkers
	a.requestHighlight(d)
	for d.highlighting {
		highlightUpdate(t, q)
	}
	if d.highlightVersion != d.buffer.Version() || len(d.highlights) == 0 {
		t.Fatal("latest text could not be highlighted after cancellation")
	}
}

func TestP4HighlightPublishesViewportBeforeWholeDocument(t *testing.T) {
	a, q, d := highlightFixture(t, strings.Repeat("var value = \"Fuyu\"\n", 2000))
	d.scroll.Y = 20000
	a.requestHighlight(d)
	highlightUpdate(t, q)
	first, _ := d.buffer.Snapshot().LineRange(950)
	if !d.highlighting || d.highlightVersion != d.buffer.Version() || len(d.highlights) == 0 || d.highlights[0].Start < first {
		t.Fatal("first update did not prioritize the viewport")
	}
	highlightUpdate(t, q)
	if d.highlighting || len(d.highlights) == 0 || d.highlights[0].Start != 0 {
		t.Fatal("whole-document context did not replace the preview")
	}
}

func TestP4ReadonlyLargeFileSkipsHighlight(t *testing.T) {
	a, q, d := highlightFixture(t, "package readonly\n")
	d.file.Reason = "大文件只读模式"
	a.requestHighlight(d)
	highlightUpdate(t, q)
	if d.highlighting || len(d.highlights) != 0 || d.contentWidth == 0 {
		t.Fatal("read-only fallback did not retain width without decoration")
	}
}
