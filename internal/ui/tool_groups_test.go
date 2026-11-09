package ui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
)

func readMessage(id string) *app.Message {
	return &app.Message{ID: id, Role: "tool", TurnID: "turn", Tool: &app.Tool{Kind: "dynamicToolCall", Name: "Read", Input: `{"file_path":"/tmp/mayoi102/test.yml"}`, Output: "value: true", Status: "completed"}}
}

func TestToolOperationCaptions(t *testing.T) {
	for _, example := range []struct {
		name, input, expected string
	}{
		{"Read", `{"file_path":"/tmp/a b/test.yml","title":"Read file"}`, `Read "/tmp/a b/test.yml"`},
		{"Edit", `{"path":"/tmp/test.yml"}`, `Edit "/tmp/test.yml"`},
		{"Write", `{"filePath":"/tmp/test.yml"}`, `Write "/tmp/test.yml"`},
		{"Bash", `{"command":"go test ./internal/ui"}`, `Bash "go test ./internal/ui"`},
		{"ToolSearch", `{"query":"task output background wait"}`, `ToolSearch "task output background wait"`},
		{"WebSearch", `{"query":"Go native UI"}`, `WebSearch "Go native UI"`},
		{"WebFetch", `{"url":"https://example.com/docs"}`, `WebFetch "https://example.com/docs"`},
		{"Grep", `{"pattern":"hello.*world"}`, `Grep "hello.*world"`},
		{"Glob", `{"pattern":"**/*.go"}`, `Glob "**/*.go"`},
		{"Read", `{"file_path":42,"title":"misleading"}`, "Read"},
		{"Read", `{"file_path":`, "Read"},
		{"Read", `{}`, "Read"},
		{"Read", `null`, "Read"},
		{"other", `{"title":"Existing title"}`, "Existing title"},
	} {
		t.Run(example.name+example.input, func(t *testing.T) {
			tool := &app.Tool{Name: example.name, Input: example.input}
			if actual := toolCaption(tool); actual != example.expected {
				t.Fatalf("caption=%q, want %q", actual, example.expected)
			}
		})
	}
}

func TestMixedToolGroupsBoundariesAndRecovery(t *testing.T) {
	for _, boundary := range []*app.Message{
		{ID: "text", Role: "assistant", Text: "说明"},
		{ID: "user", Role: "user", Text: "继续"},
		{ID: "approval", Role: "tool", Tool: &app.Tool{Kind: "commandExecution"}, Interaction: &app.Interaction{}},
	} {
		t.Run(boundary.ID, func(t *testing.T) {
			desktop := New(nil, "test", "/tmp", nil)
			desktop.State.Messages = []*app.Message{commandMessage("bash", "completed"), readMessage("read"), boundary, readMessage("after")}
			rows := desktop.conversationRows()
			if len(rows) != 3 || len(rows[0].commands) != 2 || rows[1].message != boundary || len(rows[2].commands) != 1 {
				t.Fatalf("boundary lost: %+v", rows)
			}
			desktop.saved.Anchor = "read"
			desktop.restoreReading()
			if desktop.saved.Anchor != "bash" {
				t.Fatal("mixed tool reading anchor was not restored")
			}
			*desktop.commandGroupOpen("bash") = true
			desktop.State.Messages = append(desktop.State.Messages, readMessage("append"))
			desktop.conversationRows()
			if !*desktop.commandGroupOpen("bash") {
				t.Fatal("append reset expansion")
			}
			restored := New(nil, "test", "/tmp", nil)
			restored.saved = desktop.snapshot().Sessions[0]
			if !*restored.commandGroupOpen("bash") {
				t.Fatal("mixed group expansion did not persist")
			}
		})
	}
	for _, ownership := range []string{"turn", "parent"} {
		desktop := New(nil, "test", "/tmp", nil)
		first, second := commandMessage("bash", "completed"), readMessage("read")
		if ownership == "turn" {
			second.TurnID = "other"
		} else {
			second.Tool.ParentToolID = "child"
		}
		desktop.State.Messages = []*app.Message{first, second}
		if len(desktop.conversationRows()) != 2 {
			t.Fatal("merged across", ownership)
		}
	}
}

func TestMixedToolGroupDetailsAndStates(t *testing.T) {
	desktop := New(nil, "test", "/tmp", nil)
	command, read := commandMessage("bash", "inProgress"), readMessage("read")
	messages := []*app.Message{command, read}
	tester := native.NewTester(func(c *native.Context) { desktop.commandGroup(c, messages, false) }, 650, 650)
	if !tester.HasText("正在执行命令") || tester.HasText(`Read "/tmp/mayoi102/test.yml"`) {
		t.Fatal("group did not start folded", tester.Texts())
	}
	if err := tester.Click("命令组 bash"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText(`Read "/tmp/mayoi102/test.yml"`) {
		t.Fatal("missing read path", tester.Texts())
	}
	if err := tester.Click("工具记录 Read"); err != nil {
		t.Fatal(err)
	}
	tester.Advance(300 * time.Millisecond)
	if !tester.HasText("value: true") || !tester.HasText("输入") {
		t.Fatal("tool detail lost", tester.Texts())
	}
	command.Tool.Status = "completed"
	tester.Frame()
	if !tester.HasText("执行了命令") || !*desktop.commandGroupOpen("bash") {
		t.Fatal("completion reset group")
	}
	read.Tool.Output = `{"isError":true,"content":[{"type":"text","text":"missing file"}]}`
	if summary, failed, _ := commandSummary(messages, false); summary != "执行了命令 · 1失败" || !failed {
		t.Fatal("tool result error hidden", summary)
	}
	command.Tool.AwaitingFinal = true
	if summary, _, _ := commandSummary(messages, false); !strings.HasPrefix(summary, "等待确认 Bash") {
		t.Fatal("verification hidden", summary)
	}
}

func TestMixedToolGroupKeepsReadingPosition(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprint(reduced), func(t *testing.T) {
			desktop := New(stubBackend{}, "test", "/tmp", nil)
			desktop.sidebarOpen = false
			for index := range 14 {
				desktop.State.Messages = append(desktop.State.Messages, &app.Message{ID: fmt.Sprint(index), Role: "assistant", Text: strings.Repeat("阅读正文。", 30)})
			}
			desktop.State.Messages = append(desktop.State.Messages, commandMessage("bash", "completed"), readMessage("read"))
			desktop.State.Messages = append(desktop.State.Messages, &app.Message{ID: "result", Role: "assistant", Text: strings.Repeat("后续正文。\n\n", 40)})
			tester := native.NewTester(desktop.view, 1000, 760)
			tester.SetPreferences(native.Preferences{ReduceMotion: reduced})
			tester.Frame()
			desktop.list.ScrollTo(14, native.Start)
			tester.Frame()
			before := desktop.scroll.Y
			for _, label := range []string{"命令组 bash", "工具记录 Read", "工具记录 Read", "命令组 bash"} {
				if err := tester.Click(label); err != nil {
					t.Fatal(err)
				}
				for frame := range 18 {
					tester.Advance(20 * time.Millisecond)
					if math.Abs(float64(desktop.scroll.Y-before)) > .5 {
						t.Fatalf("%s moved reading position at frame %d: %g -> %g", label, frame, before, desktop.scroll.Y)
					}
				}
			}
		})
	}
}

func TestMixedToolGroupAtEndKeepsReadingPosition(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprint(reduced), func(t *testing.T) {
			desktop := New(stubBackend{}, "test", "/tmp", nil)
			desktop.sidebarOpen = false
			desktop.State.Messages = []*app.Message{
				{ID: "intro", Role: "assistant", Text: strings.Repeat("阅读正文。\n\n", 40)},
				commandMessage("bash", "completed"), readMessage("read"),
			}
			tester := native.NewTester(desktop.view, 1000, 760)
			tester.SetPreferences(native.Preferences{ReduceMotion: reduced})
			tester.Frame()
			tester.Advance(300 * time.Millisecond)
			desktop.list.ScrollToEnd()
			tester.Frame()
			before := desktop.scroll.Y
			if err := tester.Click("命令组 bash"); err != nil {
				t.Fatal(err)
			}
			for frame := range 18 {
				tester.Advance(20 * time.Millisecond)
				if math.Abs(float64(desktop.scroll.Y-before)) > .5 {
					t.Fatalf("opening at end moved at frame %d: %g -> %g", frame, before, desktop.scroll.Y)
				}
			}
			if desktop.processScrollHold {
				t.Fatal("scroll hold did not release")
			}
			desktop.list.ScrollToEnd()
			tester.Frame()
			desktop.State.Messages = append(desktop.State.Messages, &app.Message{ID: "result", Role: "assistant", Text: strings.Repeat("新回复。\n\n", 10)})
			tester.Frame()
			if !desktop.list.AtEnd() {
				t.Fatal("normal output no longer follows after returning to end")
			}
		})
	}
}

func TestLongReadCaptionStaysOnOneLine(t *testing.T) {
	for _, width := range []int{320, 800} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			desktop := New(nil, "test", "/tmp", nil)
			read := readMessage("long")
			read.Tool.Input = `{"file_path":"/tmp/` + strings.Repeat("long directory/", 80) + `test.yml"}`
			*desktop.commandGroupOpen("long") = true
			tester := native.NewTester(func(c *native.Context) {
				native.Column(c).Fill().Children(func() {
					desktop.commandGroup(c, []*app.Message{read}, false)
					native.Text(c, "after").Label("after")
				})
			}, width, 500)
			tester.Advance(300 * time.Millisecond)
			caption, found := tester.Find("工具记录 Read")
			after, afterFound := tester.Find("after")
			if !found || !afterFound || caption.H != 30 || caption.X+caption.W > float32(width)+.5 || after.Y > 90 {
				t.Fatal("long path changed collapsed layout", caption, after)
			}
		})
	}
}
