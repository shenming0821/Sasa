package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/chroma/v2"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/editor"
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
	a, q, d := highlightFixture(t, strings.Repeat("var value = \"Sasa\"\n", 2000))
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

func TestP4ReadonlyLargeFileHighlightsViewport(t *testing.T) {
	a, q, d := highlightFixture(t, strings.Repeat("var value = 1\n", 2000))
	d.file.Reason = editor.LargeFileReadOnly
	d.scroll.Y = 1000 * 20
	a.requestHighlight(d)
	for d.highlighting {
		highlightUpdate(t, q)
	}
	if !d.highlightWindow.active || d.contentWidth == 0 || tokenAtHighlightLine(d, 1000) != chroma.KeywordDeclaration {
		t.Fatal("read-only large file did not retain its viewport highlighting")
	}
}

func TestP4DenseVueKeepsHighlightsWhenScrolling(t *testing.T) {
	const row = `    <div v-if="visible" class="entry" :title="label"><span>{{ label }}</span></div>` + "\n"
	text := "<template>\n  <section>\n" + strings.Repeat(row, 4800) + "  </section>\n</template>\n"
	a, queue, d := highlightFixture(t, text)
	d.path = filepath.Join(d.root, "source.vue")
	if d.buffer.Snapshot().LineCount() >= editorHighlightWholeLines {
		t.Fatal("fixture must exceed the token limit before the line threshold")
	}
	tester := native.NewTester(func(c *native.Context) {
		native.Row(c).Fill().Children(func() { a.fileView(c.Key(d.id), d, false, false) })
	}, 650, 400)
	tester.Frame()
	deadline := time.After(15 * time.Second)
	for d.highlighting {
		select {
		case update := <-queue:
			update()
		case <-deadline:
			t.Fatal("dense Vue scan did not settle")
		}
	}
	if d.contentWidth == 0 || !d.highlightWindow.active || len(d.highlights) == 0 {
		t.Fatal("whole-file token overflow cleared the visible Vue highlighting")
	}
	for _, line := range []int{2250, 4000, 2} {
		d.scroll.Y = float32(line * 20)
		p4Wait(t, tester, queue, func() bool {
			return int(d.scroll.Y/20) == line &&
				!d.highlighting && d.highlightWindow.covers(line, line, d.buffer.Snapshot().LineCount()) &&
				tokenAtHighlightLine(d, line) == chroma.Punctuation
		})
	}
	bounds, ok := tester.Find("文件内容 " + d.path)
	if !ok {
		t.Fatal("editor text area missing")
	}
	tester.ClickAt(bounds.X+80, bounds.Y+20)
	lastLine := d.buffer.Snapshot().LineCount() - 3
	for _, jump := range []struct {
		key  native.Key
		line int
	}{{native.KeyDown, lastLine}, {native.KeyUp, 2}} {
		tester.Key(native.Cmd, jump.key)
		if first := int(d.scroll.Y / 20); first > jump.line || first < jump.line-30 {
			t.Fatalf("shortcut did not scroll to line %d: first line is %d", jump.line, first)
		}
		if !d.highlighting || tokenAtHighlightLine(d, jump.line) != chroma.Text {
			t.Fatalf("expected the background cache to still be loading line %d", jump.line)
		}
		frame := tester.Image()
		colored := 0
		for y := int(bounds.Y) + 12; y < int(bounds.Y)+120; y++ {
			for x := int(bounds.X) + 62; x < int(bounds.X)+235; x++ {
				pixel := frame.RGBAAt(x, y)
				if int(max(pixel.R, pixel.G, pixel.B))-int(min(pixel.R, pixel.G, pixel.B)) > 40 {
					colored++
				}
			}
		}
		if colored < 60 {
			t.Fatalf("line %d rendered without syntax colors while the background scan was pending", jump.line)
		}
		for d.highlighting {
			highlightUpdate(t, queue)
		}
	}
}

func TestP4VueHighlightWindowInsideEmbeddedSection(t *testing.T) {
	const markup = "<template>\n<!-- <style lang=\"less\"> -->\n<p title=\"<style>\">你好</p>\n</template>\n"
	const script = "<script setup lang='ts'>\n"
	const style = "<style scoped lang=\"scss\">\n"
	text := markup + script + strings.Repeat("type Person = { name: string }\n", 3200) + "</script>\n" +
		style + strings.Repeat("$theme: #333;\n", 3200) + "</style>\n"
	a, queue, d := highlightFixture(t, text)
	d.path = filepath.Join(d.root, "source.vue")
	for _, sample := range []struct {
		line int
		kind chroma.TokenType
	}{{1600, chroma.KeywordReserved}, {4800, chroma.NameVariable}, {2000, chroma.KeywordReserved}} {
		d.scroll.Y = float32(sample.line * 20)
		a.requestHighlightAt(d, 400)
		for d.highlighting {
			highlightUpdate(t, queue)
		}
		if got := tokenAtHighlightLine(d, sample.line); got != sample.kind {
			t.Fatalf("line %d highlighted as %s, want %s", sample.line+1, got, sample.kind)
		}
		first := d.highlightWindow.first
		a.requestHighlightAt(d, 400)
		if d.highlighting || d.highlightWindow.first != first {
			t.Fatal("a viewport inside the cached area was scanned again")
		}
	}
}

func tokenAtHighlightLine(d *filePage, line int) chroma.TokenType {
	snap := d.buffer.Snapshot()
	start, end := snap.LineRange(line)
	for _, token := range d.highlights {
		if token.Start < end && token.End > start {
			return token.Type
		}
	}
	return chroma.Text
}
