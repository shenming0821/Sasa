package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

func BenchmarkToolDetailsFormatting(b *testing.B) {
	input, _ := json.Marshal(map[string]string{"code": strings.Repeat("console.log('sample');\n", 6000)})
	tool := &app.Tool{Kind: "mcpToolCall", Name: "cua_repl/js", Input: string(input), Output: textEnvelope(strings.Repeat("sample output\n", 10000))}
	b.Run("Uncached", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			toolInput(tool.Input)
			toolResult(tool.Output, 0)
		}
	})
	b.Run("Cached", func(b *testing.B) {
		ensureToolInputCache(tool)
		ensureToolFormattedOutputCache(tool)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			ensureToolInputCache(tool)
			ensureToolFormattedOutputCache(tool)
		}
	})
}

func TestDisclosureMovesOnFirstRefreshAndKeepsSiblingsInFlow(t *testing.T) {
	for _, initiallyOpen := range []bool{false, true} {
		t.Run(map[bool]string{false: "opening", true: "closing"}[initiallyOpen], func(t *testing.T) {
			open := initiallyOpen
			tt := native.NewTester(func(c *native.Context) {
				native.Column(c).FillWidth().Children(func() {
					parts := newCollapsible(c, &open)
					parts.Trigger.Label("toggle").Height(30).FillWidth()
					parts.Panel(func() { native.Box(c).Height(120).FillWidth().Label("body") })
					native.Box(c).Height(20).FillWidth().Label("after")
				})
			}, 300, 300)
			after := func() float32 {
				r, ok := tt.Find("after")
				if !ok {
					t.Fatal("following sibling disappeared")
				}
				return r.Y
			}
			start := after()
			if err := tt.Click("toggle"); err != nil {
				t.Fatal(err)
			}
			if got := after(); got != start {
				t.Fatalf("click jumped the sibling: %g -> %g", start, got)
			}
			tt.Advance(time.Second / 60)
			first := after()
			if initiallyOpen && first >= start || !initiallyOpen && first <= start {
				t.Fatalf("first refresh stalled: %g -> %g", start, first)
			}
			previous := first
			for frame := 0; frame < 24; frame++ {
				tt.Advance(time.Second / 60)
				current := after()
				if initiallyOpen && current > previous+.01 || !initiallyOpen && current < previous-.01 {
					t.Fatalf("sibling reversed unexpectedly: %g -> %g", previous, current)
				}
				if current < 30 || current > 150 {
					t.Fatalf("height overshot: %g", current)
				}
				previous = current
			}
			want := float32(150)
			if initiallyOpen {
				want = 30
			}
			if after() != want {
				t.Fatalf("did not settle: got %g want %g", after(), want)
			}
		})
	}
}

func TestGitGroupDisclosureAnimatesFollowingGroup(t *testing.T) {
	a := New(nil, "test", "/repo", nil)
	w := a.workspace("/repo")
	tt := native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Children(func() {
			a.gitGroup(c.Key("changes"), w, "更改", []gitview.Entry{{Path: "a", XY: ".M"}, {Path: "b", XY: ".M"}}, false)
			native.Text(c, "after")
		})
	}, 300, 300)
	start, _ := tt.Find("after")
	if err := tt.Click("折叠 更改"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second / 60)
	first, _ := tt.Find("after")
	if first.Y >= start.Y || first.Y <= start.Y-56 {
		t.Fatalf("Git group skipped the intermediate layout: %v -> %v", start, first)
	}
	tt.Advance(300 * time.Millisecond)
	last, _ := tt.Find("after")
	if last.Y != start.Y-56 || tt.HasText("Git 更改 a") {
		t.Fatal("Git group did not finish collapsing", last)
	}
}

func TestTurnDisclosureMovesResultOnFirstRefresh(t *testing.T) {
	a := New(nil, "test", "/repo", nil)
	a.State.Messages = []*app.Message{
		{ID: "question", Role: "user", Text: "question", Timing: &app.TurnTiming{
			Started: time.Unix(1, 0), Finished: time.Unix(2, 0),
		}},
		commandMessage("read", "completed"),
		{ID: "result", Role: "assistant", Text: "result", Done: true},
	}
	tt := native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Children(func() { a.conversation(c) })
	}, 800, 700)
	before, _ := tt.Find("result")
	if err := tt.Click("展开执行过程 question"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second / 60)
	after, _ := tt.Find("result")
	if after.Y <= before.Y {
		t.Fatalf("process waited past the first refresh: %v -> %v", before, after)
	}
}

func TestToolDisplayCacheInvalidatesOnRuntimeUpdates(t *testing.T) {
	tool := &app.Tool{Kind: "mcpToolCall", Name: "Read", Input: `{"file_path":"a.go"}`, Output: `{"content":[{"type":"text","text":"old"}]}`}
	if toolCaption(tool) != `Read "a.go"` {
		t.Fatal("initial caption")
	}
	ensureToolInputCache(tool)
	ensureToolOutputCache(tool)
	if tool.DisplayCache.OutputFormatted || tool.DisplayCache.FormattedOutput != "" {
		t.Fatal("collapsed successful tool eagerly formatted its output")
	}
	tool.Input = `{"file_path":"b.go"}`
	tool.Output = `{"isError":true,"content":[{"type":"text","text":"new error"}]}`
	if toolCaption(tool) != `Read "b.go"` || !toolResultFailed(tool) {
		t.Fatal("runtime update left stale caption or result")
	}
	ensureToolFormattedOutputCache(tool)
	_, parameters := ensureToolInputCache(tool)
	if !strings.Contains(parameters, "b.go") || !strings.Contains(tool.DisplayCache.ErrorSummary, "new error") {
		t.Fatal("runtime update left stale detail text")
	}
	tool.Kind = "commandExecution"
	ensureToolOutputCache(tool)
	if tool.DisplayCache.ResultFailed {
		t.Fatal("result classification survived a kind change")
	}
	tool.Kind = "mcpToolCall"
	if toolCaption(tool) != `Read "b.go"` {
		t.Fatal("could not warm caption")
	}
	if allocations := testing.AllocsPerRun(100, func() {
		_ = toolCaption(tool)
		_ = toolOperationCaption(tool)
		ensureToolInputCache(tool)
		ensureToolFormattedOutputCache(tool)
	}); allocations != 0 {
		t.Fatalf("unchanged tool allocated per animation frame: %g", allocations)
	}
}
