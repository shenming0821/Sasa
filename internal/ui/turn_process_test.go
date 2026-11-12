package ui

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
)

func TestCompletedTurnProcessDisclosure(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{640, 1000} {
			t.Run(fmt.Sprintf("dark=%v/width=%d", dark, width), func(t *testing.T) {
				desktop := New(stubBackend{}, "test", "/tmp", nil)
				desktop.sidebarOpen = false
				desktop.State.Begin("检查文件")
				desktop.State.Messages = append(desktop.State.Messages,
					&app.Message{ID: "progress", Role: "assistant", Text: "准备读取文件", Done: true},
					commandMessage("read", "completed"),
					&app.Message{ID: "result", Role: "assistant", Text: "文件检查完成", Done: true},
				)
				tester := native.NewTester(desktop.view, width, 760)
				tester.SetDark(dark)
				tester.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
				tester.Frame()
				if !tester.HasText("准备读取文件") || !tester.HasText("命令组 read") || tester.HasText("展开") {
					t.Fatal("running process was hidden or offered a completion disclosure")
				}
				desktop.State.Busy = false
				desktop.State.FinishTiming()
				tester.Frame()
				assertCollapsed := func() {
					t.Helper()
					_, arrowVisible := tester.Find("展开执行过程 user-1")
					if !arrowVisible || !tester.HasText("文件检查完成") || tester.HasText("准备读取文件") || tester.HasText("命令组 read") || slices.Contains(tester.Texts(), "展开") || slices.Contains(tester.Texts(), "收起") {
						t.Fatalf("collapsed turn did not keep only its result: %v", tester.Texts())
					}
				}
				assertCollapsed()
				if err := tester.Click("展开执行过程 user-1"); err != nil {
					t.Fatal(err)
				}
				tester.Frame()
				_, arrowVisible := tester.Find("收起执行过程 user-1")
				if !arrowVisible || !tester.HasText("准备读取文件") || !tester.HasText("命令组 read") || !tester.HasText("文件检查完成") || slices.Contains(tester.Texts(), "展开") || slices.Contains(tester.Texts(), "收起") {
					t.Fatalf("expanded process is incomplete: %v", tester.Texts())
				}
				if err := tester.Click("收起执行过程 user-1"); err != nil {
					t.Fatal(err)
				}
				tester.Frame()
				assertCollapsed()
				if len(desktop.State.Messages) != 4 {
					t.Fatal("folding changed native messages")
				}
			})
		}
	}
}

func TestCollapsedTurnKeepsChangeCard(t *testing.T) {
	desktop := New(stubBackend{}, "test", "/tmp", nil)
	desktop.sidebarOpen = false
	started := time.Now().Add(-time.Minute)
	timing := &app.TurnTiming{Started: started, Finished: started.Add(time.Minute)}
	desktop.State.Messages = []*app.Message{
		{ID: "question", Role: "user", Text: "修改文件", Timing: timing},
		commandMessage("read", "completed"),
		changeMessage("edit", "turn", "/tmp/a.go", "@@ -1 +1 @@\n-old\n+new\n"),
		{ID: "result", Role: "assistant", Text: "修改完成", Done: true},
	}
	tester := native.NewTester(desktop.view, 1000, 700)
	tester.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	tester.Frame()
	assertFolded := func() {
		t.Helper()
		if !tester.HasText("已编辑 1 个文件") || !tester.HasText("a.go") || !tester.HasText("修改完成") {
			t.Fatalf("collapsed turn dropped the change summary: %v", tester.Texts())
		}
		if tester.HasText("命令组 read") {
			t.Fatal("collapsed turn leaked the command group")
		}
		reply, replyOK := tester.Find("修改完成")
		card, cardOK := tester.Find("已编辑 1 个文件")
		if !replyOK || !cardOK || card.Y <= reply.Y {
			t.Fatal("summary card is not placed after the final reply")
		}
	}
	assertFolded()
	if err := tester.Click("展开执行过程 question"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("命令组 read") || !tester.HasText("已编辑 1 个文件") {
		t.Fatal("expansion lost the card or the process")
	}
	if err := tester.Click("收起执行过程 question"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	assertFolded()
}

func TestExpandedTurnPinsReplyAfterProcess(t *testing.T) {
	// Devin accumulates one assistant message from its first chunk, so tool
	// records can land after the reply in Messages; expansion must still
	// restore the process between the duration line and the final reply.
	desktop := New(stubBackend{}, "test", "/tmp", nil)
	started := time.Now().Add(-time.Minute)
	desktop.State.Messages = []*app.Message{
		{ID: "question", Role: "user", Text: "检查", Timing: &app.TurnTiming{Started: started, Finished: started.Add(time.Minute)}},
		{ID: "reply", Role: "assistant", Text: "最终回复", Done: true},
		commandMessage("read", "completed"),
	}
	tester := native.NewTester(desktop.view, 1000, 700)
	tester.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	tester.Frame()
	if tester.HasText("命令组 read") {
		t.Fatal("collapsed turn leaked the process")
	}
	if err := tester.Click("展开执行过程 question"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	command, commandOK := tester.Find("命令组 read")
	reply, replyOK := tester.Find("最终回复")
	if !commandOK || !replyOK || command.Y >= reply.Y {
		t.Fatal("expanded process did not return between the duration line and the final reply")
	}
	rows := desktop.conversationRows()
	if len(rows) != 3 || len(rows[1].commands) == 0 || rows[2].message.ID != "reply" {
		t.Fatalf("expanded projection did not pin the reply last: %+v", rows)
	}
}

func TestChangeOnlyTurnHasNoProcessDisclosure(t *testing.T) {
	desktop := New(stubBackend{}, "test", "/tmp", nil)
	started := time.Now().Add(-time.Minute)
	question := &app.Message{ID: "question", Role: "user", Timing: &app.TurnTiming{Started: started, Finished: started.Add(time.Minute)}}
	desktop.State.Messages = []*app.Message{
		question,
		changeMessage("edit", "turn", "/tmp/a.go", "@@ -1 +1 @@\n-old\n+new\n"),
		{ID: "result", Role: "assistant", Text: "修改完成", Done: true},
	}
	tester := native.NewTester(func(c *native.Context) {
		desktop.session.turnTiming(c, question)
	}, 400, 100)
	tester.Frame()
	if !tester.HasText("用时 1分钟 0秒") {
		t.Fatal("duration disappeared")
	}
	if _, found := tester.Find("展开执行过程 question"); found {
		t.Fatal("an always-visible change card was offered as a folded process")
	}
}

func TestTurnProcessDisclosureRequiresContent(t *testing.T) {
	for _, example := range []struct {
		name     string
		messages []*app.Message
		want     bool
	}{
		{name: "no reply"},
		{name: "final reply", messages: []*app.Message{{Role: "assistant", Text: "嗨，我在。"}}},
		{name: "empty placeholders", messages: []*app.Message{{Role: "assistant"}, {Role: "assistant", Text: "嗨，我在。"}, {Role: "assistant"}}},
		{name: "intermediate reply", messages: []*app.Message{{Role: "assistant", Text: "正在检查"}, {Role: "assistant", Text: "完成"}}, want: true},
		{name: "tool without reply", messages: []*app.Message{commandMessage("read", "completed")}, want: true},
		{name: "tool and reply", messages: []*app.Message{commandMessage("read", "completed"), {Role: "assistant", Text: "完成"}}, want: true},
		{name: "next turn process", messages: []*app.Message{{Role: "assistant", Text: "嗨"}, {Role: "user", Text: "检查文件"}, commandMessage("read", "completed")}},
	} {
		for _, expanded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/expanded=%v", example.name, expanded), func(t *testing.T) {
				desktop := New(stubBackend{}, "test", "/tmp", nil)
				started := time.Now().Add(-6 * time.Second)
				message := &app.Message{ID: "question", Role: "user", Timing: &app.TurnTiming{Started: started, Finished: started.Add(6 * time.Second)}}
				desktop.State.Messages = append([]*app.Message{message}, example.messages...)
				desktop.expandedProcesses = map[string]bool{"question": expanded}
				tester := native.NewTester(func(c *native.Context) {
					desktop.session.turnTiming(c, message)
				}, 400, 100)
				tester.Frame()
				if !tester.HasText("用时 6秒") {
					t.Fatal("duration disappeared")
				}
				_, arrow := tester.Find("执行过程箭头 question")
				_, open := tester.Find("展开执行过程 question")
				_, close := tester.Find("收起执行过程 question")
				if arrow != example.want || (open || close) != example.want {
					t.Fatalf("disclosure content mismatch: arrow=%v open=%v close=%v want=%v", arrow, open, close, example.want)
				}
			})
		}
	}
}

func TestTurnProcessDisclosureKeepsReadingPosition(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		for _, longResult := range []bool{false, true} {
			for _, busy := range []bool{false, true} {
				t.Run(fmt.Sprintf("reduce=%v/longResult=%v/busy=%v", reduce, longResult, busy), func(t *testing.T) {
					desktop := New(stubBackend{}, "test", "/tmp", nil)
					desktop.sidebarOpen = false
					desktop.State.Busy = busy
					started := time.Now().Add(-time.Minute)
					result := "最终结果"
					if longResult {
						result = strings.Repeat("最终结果。\n\n", 40)
					}
					desktop.State.Messages = []*app.Message{
						{ID: "question", Role: "user", Text: "检查", Timing: &app.TurnTiming{Started: started, Finished: started.Add(time.Minute)}},
						{ID: "progress", Role: "assistant", Text: strings.Repeat("执行记录。\n\n", 40), Done: true},
						{ID: "result", Role: "assistant", Text: result, Done: true},
					}
					tester := native.NewTester(desktop.view, 1000, 600)
					tester.SetPreferences(native.Preferences{ReduceMotion: reduce, TextScale: 1})
					if longResult {
						desktop.list.ScrollTo(0, native.Start)
						tester.Frame()
						desktop.scroll.Y = 30
						tester.Frame()
					} else if !desktop.list.AtEnd() {
						t.Fatal("short collapsed conversation should already show its end")
					}
					before := desktop.scroll.Y
					button, found := tester.Find("展开执行过程 question")
					if !found {
						t.Fatal("duration disclosure is missing")
					}
					for _, label := range []string{"展开执行过程 question", "收起执行过程 question"} {
						if err := tester.Click(label); err != nil {
							t.Fatal(err)
						}
						for frame := 0; frame < 16; frame++ {
							tester.Advance(20 * time.Millisecond)
							after, found := tester.Find("执行过程箭头 question")
							if !found || after.Y < button.Y || after.Y+after.H > button.Y+button.H+1 || math.Abs(float64(desktop.scroll.Y-before)) > .5 {
								t.Fatalf("%s moved the reading position at frame %d: offset %g -> %g, button=%+v arrow=%+v", label, frame, before, desktop.scroll.Y, button, after)
							}
						}
					}
					if desktop.processScrollHold || !desktop.list.FollowEnd {
						t.Fatal("disclosure did not release its temporary scroll hold")
					}
					desktop.list.ScrollToEnd()
					tester.Frame()
					desktop.State.Messages[len(desktop.State.Messages)-1].Text += strings.Repeat("新增输出。\n\n", 40)
					tester.Frame()
					if !desktop.list.AtEnd() {
						t.Fatal("returning to latest did not restore normal output following")
					}
				})
			}
		}
	}
}

func TestTurnProcessAnimation(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, reduce := range []bool{false, true} {
			t.Run(fmt.Sprintf("dark=%v/reduce=%v", dark, reduce), func(t *testing.T) {
				desktop := New(stubBackend{}, "test", "/tmp", nil)
				desktop.sidebarOpen = false
				started := time.Now().Add(-time.Minute)
				desktop.State.Messages = []*app.Message{
					{ID: "question", Role: "user", Text: "检查", Timing: &app.TurnTiming{Started: started, Finished: started.Add(time.Minute)}},
					{ID: "progress", Role: "assistant", Text: "正在核对执行过程", Done: true},
					commandMessage("read", "completed"),
					{ID: "result", Role: "assistant", Text: "最终结果", Done: true},
				}
				tester := native.NewTester(desktop.view, 1000, 900)
				tester.SetDark(dark)
				tester.SetPreferences(native.Preferences{ReduceMotion: reduce, TextScale: 1})
				resultY := func() float32 {
					t.Helper()
					result, found := tester.Find("最终结果")
					if !found {
						t.Fatal("folding hid the final result")
					}
					return result.Y
				}
				collapsed := resultY()
				if desktop.processProgress["question"] != 0 || tester.HasText("正在核对执行过程") {
					t.Fatal("history did not initially settle in its collapsed state")
				}
				button, _ := tester.Find("展开执行过程 question")
				tester.Move(button.X+button.W/2, button.Y+button.H/2)
				arrowPixels := func() []byte {
					t.Helper()
					arrow, found := tester.Find("执行过程箭头 question")
					if !found {
						t.Fatal("process arrow is missing")
					}
					var pixels []byte
					image := tester.Image()
					for pixelY := int(arrow.Y); pixelY < int(arrow.Y+arrow.H); pixelY++ {
						for pixelX := int(arrow.X); pixelX < int(arrow.X+arrow.W); pixelX++ {
							pixel := image.RGBAAt(pixelX, pixelY)
							pixels = append(pixels, pixel.R, pixel.G, pixel.B, pixel.A)
						}
					}
					return pixels
				}
				closedArrow := arrowPixels()
				if err := tester.Click("展开执行过程 question"); err != nil {
					t.Fatal(err)
				}
				tester.Advance(80 * time.Millisecond)
				opening := desktop.processProgress["question"]
				middleArrow := arrowPixels()
				middle := resultY()
				tester.Advance(250 * time.Millisecond)
				opened := resultY()
				openArrow := arrowPixels()
				if bytes.Equal(closedArrow, openArrow) || !reduce && (bytes.Equal(middleArrow, closedArrow) || bytes.Equal(middleArrow, openArrow)) {
					t.Fatal("process arrow did not rotate through an intermediate pose")
				}
				if desktop.processProgress["question"] != 1 || opened <= collapsed || !tester.HasText("命令组 read") {
					t.Fatal("expansion did not finish with the complete process")
				}
				if reduce {
					if opening != 1 || middle != opened {
						t.Fatal("reduced motion did not open immediately")
					}
				} else if opening <= 0 || opening >= 1 || middle <= collapsed || middle >= opened {
					t.Fatalf("expansion skipped its intermediate height: progress=%g positions=%g/%g/%g", opening, collapsed, middle, opened)
				}
				if err := tester.Click("收起执行过程 question"); err != nil {
					t.Fatal(err)
				}
				tester.Advance(80 * time.Millisecond)
				closing := desktop.processProgress["question"]
				middle = resultY()
				if reduce {
					if closing != 0 || tester.HasText("命令组 read") {
						t.Fatal("reduced motion did not collapse immediately")
					}
				} else if closing <= 0 || closing >= 1 || middle <= collapsed || middle >= opened || !tester.HasText("命令组 read") {
					t.Fatalf("collapse removed the process before its intermediate height: progress=%g positions=%g/%g/%g", closing, collapsed, middle, opened)
				}
				tester.Advance(250 * time.Millisecond)
				if desktop.processProgress["question"] != 0 || tester.HasText("命令组 read") || math.Abs(float64(resultY()-collapsed)) > .5 {
					t.Fatal("collapse did not remove process rows and restore the result position")
				}
				if !reduce {
					if err := tester.Click("展开执行过程 question"); err != nil {
						t.Fatal(err)
					}
					tester.Advance(80 * time.Millisecond)
					beforeReverse := desktop.processProgress["question"]
					if err := tester.Click("收起执行过程 question"); err != nil {
						t.Fatal(err)
					}
					tester.Advance(40 * time.Millisecond)
					if progress := desktop.processProgress["question"]; progress <= 0 || progress >= beforeReverse {
						t.Fatal("reversing an animation jumped to an endpoint")
					}
					tester.Advance(250 * time.Millisecond)
					if tester.HasText("命令组 read") || desktop.processProgress["question"] != 0 {
						t.Fatal("reversed animation left process rows behind")
					}
				}
			})
		}
	}
}

func TestTurnProcessProjectionKeepsTurnsIndependent(t *testing.T) {
	desktop := New(stubBackend{}, "test", "/tmp", nil)
	finished := &app.TurnTiming{Started: time.Now().Add(-time.Minute), Finished: time.Now()}
	desktop.State.Messages = []*app.Message{
		{ID: "old", Role: "user", Text: "旧历史"},
		{ID: "old-tool", Role: "tool", Tool: &app.Tool{Kind: "webSearch"}},
		{ID: "first", Role: "user", Timing: finished},
		{ID: "first-tool", Role: "tool", Tool: &app.Tool{Kind: "webSearch"}},
		{ID: "first-result", Role: "assistant", Text: "first result"},
		{ID: "second", Role: "user", Timing: finished},
		{ID: "second-tool", Role: "tool", Tool: &app.Tool{Kind: "webSearch"}},
		{ID: "third", Role: "user", Timing: &app.TurnTiming{Started: time.Now()}},
		{ID: "third-tool", Role: "tool", Tool: &app.Tool{Kind: "webSearch"}},
	}
	rows := desktop.conversationRows()
	if len(rows) != 7 || rows[3].message.ID != "first-result" || rows[5].message.ID != "third" {
		t.Fatalf("unexpected default projection: %+v", rows)
	}
	desktop.expandedProcesses = map[string]bool{"second": true}
	rows = desktop.conversationRows()
	if len(rows) != 8 || rows[5].message.ID != "second-tool" {
		t.Fatal("expanding one turn changed another turn or failed to invalidate projection")
	}
	other := New(stubBackend{}, "test", "/tmp", nil)
	other.State.Messages = desktop.State.Messages
	if len(other.conversationRows()) != 7 {
		t.Fatal("new session inherited another session's expanded process")
	}
	other.saved.Anchor, other.saved.AnchorOffset = "second-tool", 120
	other.restoreReading()
	if other.saved.Anchor != "second" || other.saved.AnchorOffset != 0 || other.State.Notice != "" {
		t.Fatal("restoring a hidden process anchor did not return to its user question")
	}
}

func TestTurnTimingArrowVisualAlignment(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprintf("dark=%v", dark), func(t *testing.T) {
			desktop := New(stubBackend{}, "test", "/tmp", nil)
			started := time.Now().Add(-time.Minute)
			message := &app.Message{ID: "question", Role: "user", Timing: &app.TurnTiming{Started: started, Finished: started.Add(time.Minute)}}
			desktop.State.Messages = []*app.Message{message, commandMessage("read", "completed")}
			tester := native.NewTester(func(c *native.Context) {
				desktop.applyAppearance(c)
				native.Column(c).Fill().Padding(10).Background(c.Theme().Background).Children(func() {
					desktop.session.turnTiming(c, message)
				})
			}, 400, 100)
			tester.SetDark(dark)
			tester.SetScale(2)
			tester.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
			for _, expanded := range []bool{false, true} {
				desktop.expandedProcesses = map[string]bool{"question": expanded}
				tester.Frame()
				image := tester.Image()
				background := image.RGBAAt(0, 0)
				inkCenter := func(label string) float64 {
					t.Helper()
					bounds, found := tester.Find(label)
					if !found {
						t.Fatalf("missing %s", label)
					}
					top, bottom := image.Bounds().Max.Y, -1
					for pixelY := int(bounds.Y * 2); pixelY < int((bounds.Y+bounds.H)*2); pixelY++ {
						for pixelX := int(bounds.X * 2); pixelX < int((bounds.X+bounds.W)*2); pixelX++ {
							pixel := image.RGBAAt(pixelX, pixelY)
							contrast := math.Abs(float64(pixel.R)-float64(background.R)) + math.Abs(float64(pixel.G)-float64(background.G)) + math.Abs(float64(pixel.B)-float64(background.B))
							if contrast > 60 {
								top, bottom = min(top, pixelY), max(bottom, pixelY)
							}
						}
					}
					if bottom < top {
						t.Fatalf("missing visible ink for %s", label)
					}
					return float64(top+bottom) / 4
				}
				textCenter, arrowCenter := inkCenter("用时 1分钟 0秒"), inkCenter("执行过程箭头 question")
				if math.Abs(textCenter-arrowCenter) > 0.3 {
					t.Errorf("expanded=%v: arrow ink center %g differs from text ink center %g", expanded, arrowCenter, textCenter)
				}
			}
		})
	}
}

func TestTurnTimingTextClickAndHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprintf("dark=%v", dark), func(t *testing.T) {
			desktop := New(stubBackend{}, "test", "/tmp", nil)
			started := time.Now().Add(-time.Minute)
			message := &app.Message{ID: "question", Role: "user", Timing: &app.TurnTiming{Started: started, Finished: started.Add(time.Minute)}}
			desktop.State.Messages = []*app.Message{message, commandMessage("read", "completed")}
			var muted, primary native.Color
			tester := native.NewTester(func(c *native.Context) {
				muted, primary = c.Theme().TextMuted, c.Theme().Text
				native.Column(c).Fill().Padding(10).Background(c.Theme().Background).Children(func() {
					desktop.session.turnTiming(c, message)
				})
			}, 400, 100)
			tester.SetDark(dark)
			tester.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
			tester.Move(390, 90)
			text, found := tester.Find("用时 1分钟 0秒")
			if !found {
				t.Fatal("duration text is missing")
			}
			button, found := tester.Find("展开执行过程 question")
			if !found || button.X > text.X || button.X+button.W < text.X+text.W {
				t.Fatal("duration text is outside the disclosure hit target")
			}
			assertInk := func(want native.Color) {
				t.Helper()
				distance := math.MaxFloat64
				image := tester.Image()
				for pixelY := int(text.Y); pixelY < int(text.Y+text.H); pixelY++ {
					for pixelX := int(text.X); pixelX < int(text.X+text.W); pixelX++ {
						pixel := image.RGBAAt(pixelX, pixelY)
						difference := math.Abs(float64(pixel.R)-float64(want.R)) + math.Abs(float64(pixel.G)-float64(want.G)) + math.Abs(float64(pixel.B)-float64(want.B))
						distance = min(distance, difference)
					}
				}
				if distance > 12 {
					t.Fatalf("duration ink does not match expected theme color: distance=%g", distance)
				}
			}
			assertInk(muted)
			background := tester.Image().RGBAAt(0, 0)
			assertBackground := func() {
				t.Helper()
				for _, sample := range [][2]float32{{button.X + 1, button.Y + 1}, {text.X + text.W + 4, button.Y + button.H/2}} {
					if pixel := tester.Image().RGBAAt(int(sample[0]), int(sample[1])); pixel != background {
						t.Fatal("duration disclosure paints a background")
					}
				}
			}
			pointerX, pointerY := text.X+text.W/2, text.Y+text.H/2
			tester.Move(pointerX, pointerY)
			assertInk(primary)
			assertBackground()
			tester.Press(pointerX, pointerY)
			assertBackground()
			tester.Release(pointerX, pointerY)
			if !desktop.expandedProcesses[message.ID] {
				t.Fatal("clicking duration text did not expand the process")
			}
			tester.ClickAt(pointerX, pointerY)
			if desktop.expandedProcesses[message.ID] {
				t.Fatal("clicking duration text again did not collapse the process")
			}
			tester.Move(390, 90)
			assertInk(muted)
			assertBackground()
			if after, _ := tester.Find("展开执行过程 question"); after != button {
				t.Fatal("hover or folding moved the duration hit target")
			}
		})
	}
}
