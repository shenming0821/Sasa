package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
)

func TestP4MixedTabsAndLargeTextPerformance(t *testing.T) {
	if os.Getenv("FUYU_P4_PERF") != "1" {
		t.Skip("opt-in P4 performance sample")
	}
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1280, 800)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var openTimes []time.Duration
	for i := range 10 {
		if i > 0 {
			a.newSession(root)
		}
		path := filepath.Join(root, fmt.Sprintf("document-%d.txt", i))
		lines := 10000
		if i == 1 {
			lines = 100000
		}
		text := strings.Repeat("performance fixture line\n", lines-1) + "last line"
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		a.openFile(root, path)
		p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
		openTimes = append(openTimes, time.Since(start))
	}
	p4Wait(t, tt, q, func() bool {
		for _, d := range a.files {
			if d.highlighting {
				return false
			}
		}
		return true
	})
	if len(a.tabs) != 20 {
		t.Fatal("sample does not contain 20 mixed tabs", len(a.tabs))
	}
	path := filepath.Join(root, "document-1.txt")
	a.openFile(root, path)
	tt.Frame()
	if err := tt.Click("文件内容 " + path); err != nil {
		t.Fatal(err)
	}
	var frames, inputs []time.Duration
	for i := range 40 {
		a.currentFile().scroll.Y = float32(i * 40)
		start := time.Now()
		tt.Frame()
		frames = append(frames, time.Since(start))
	}
	for range 20 {
		start := time.Now()
		tt.Type("x")
		inputs = append(inputs, time.Since(start))
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	p95 := func(values []time.Duration) time.Duration {
		slices.Sort(values)
		return values[(len(values)-1)*95/100]
	}
	t.Logf("MyGo memory renderer, 1280x800, warm filesystem, 10 file + 10 chat tabs: 10k-line open=%s; 100k-line open=%s; scroll-frame p95=%s; input-event p95=%s; Go live heap before=%d after=%d bytes (not process RSS)", openTimes[0], openTimes[1], p95(frames), p95(inputs), before.HeapAlloc, after.HeapAlloc)
}
