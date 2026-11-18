package ui

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
)

func textEnvelope(text string) string {
	raw, _ := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}})
	return string(raw)
}

func TestToolResultReadableWithoutLosingUnknownData(t *testing.T) {
	raw := "First line\nSecond line\twith tab"
	wrapped := raw
	for i := 0; i < 6; i++ {
		wrapped = textEnvelope(wrapped)
	}
	if got := toolResult(wrapped, 0); got != raw {
		t.Fatal("nested history remains escaped", got)
	}

	if got := toolResult(textEnvelope(textEnvelope(raw)), 0); got != raw {
		t.Fatal(got)
	}
	for _, value := range []string{
		`{"content":[{"type":"image","data":"abc","mimeType":"image/png"}]}`,
		`{"content":[{"type":"text","text":"ok"}],"structuredContent":{"count":3}}`,
		`{"type":"text","text":"ok","annotations":{"audience":["user"]}}`,
	} {
		var before, after any
		json.Unmarshal([]byte(value), &before)
		got := toolResult(value, 0)
		// A non-text block can lose only its recognized outer content envelope.
		if strings.Contains(value, `"structuredContent"`) {
			if !strings.Contains(got, `"count": 3`) || !strings.HasPrefix(got, "ok\n") {
				t.Fatal("structured metadata lost", got)
			}
			continue
		}
		if strings.Contains(value, `"type":"image"`) {
			if !strings.Contains(got, `"data": "abc"`) {
				t.Fatal(got)
			}
			continue
		}
		if json.Unmarshal([]byte(got), &after) != nil {
			t.Fatal("unknown metadata lost", got)
		}
		b, _ := json.Marshal(before)
		a, _ := json.Marshal(after)
		if string(a) != string(b) {
			t.Fatal("unknown fields lost", got)
		}
	}
	if got := toolResult(`{"content":[{"type":"text","text":"ok"}],"id":9007199254740993}`, 0); !strings.Contains(got, "9007199254740993") {
		t.Fatal("metadata precision lost", got)
	}
	truncated := `{"content":[{"type":"text","text":"cut`
	if toolResult(truncated, 0) != truncated {
		t.Fatal("truncated text changed")
	}
	code, params := toolInput(`{"code":"await app.getState();\nawait app.click(1)","title":"Inspect window","timeout_ms":5000}`)
	if !strings.Contains(code, "\n") || !strings.Contains(params, "5000") || strings.Contains(params, "title") {
		t.Fatal(code, params)
	}
}

func TestToolDisclosureLayoutAndCopy(t *testing.T) {
	for _, width := range []int{320, 700} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/dark=%v", width, dark), func(t *testing.T) {
				a := New(nil, "test", "/workspace", nil)
				duration := int64(90)
				tool := &app.Tool{Kind: "mcpToolCall", Name: "cua_repl/js", Status: "completed", DurationMs: &duration, Input: `{"title":"核验长文件名与路径的显示优先级","code":"await app.getState()"}`, Output: textEnvelope(textEnvelope(strings.Repeat("line with readable text\n", 40)))}
				tt := native.NewTester(func(c *native.Context) {
					native.Column(c).Fill().Background(c.Theme().Background).Children(func() { a.toolCard(c, tool); native.Text(c, "after").Label("after") })
				}, width, 900)
				tt.SetDark(dark)
				tt.Advance(200 * time.Millisecond)
				closed, _ := tt.Find("after")
				if closed.Y > 40 || tt.HasText("已完成") || tt.HasText("展开输出") || tt.HasText("输入") {
					t.Fatal("tool not compact", tt.Texts())
				}
				if err := tt.Click("工具记录 cua_repl/js"); err != nil {
					t.Fatal(err)
				}
				tt.Advance(300 * time.Millisecond)
				opened, _ := tt.Find("after")
				if !tool.Expanded || !tt.HasText("输入") || !tt.HasText("结果") || opened.Y < closed.Y || opened.Y > 450 {
					t.Fatal("details not bounded", opened, tt.Texts())
				}
				for _, label := range []string{"工具输入", "工具结果"} {
					r, ok := tt.Find(label)
					if !ok || r.H > 280.5 || r.X+r.W > float32(width)+.5 {
						t.Fatal("detail overflow", label, r)
					}
				}
				if err := tt.Click("复制结果"); err != nil {
					t.Fatal(err)
				}
				if tt.Clipboard() != strings.Repeat("line with readable text\n", 40) {
					t.Fatal("copied escaped envelope")
				}
				if dir := os.Getenv("SASA_TOOL_PREVIEW"); dir != "" {
					os.MkdirAll(dir, 0700)
					f, err := os.Create(filepath.Join(dir, fmt.Sprintf("tool-%d-%v.png", width, dark)))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := tt.Click("工具记录 cua_repl/js"); err != nil {
					t.Fatal(err)
				}
				tt.Advance(300 * time.Millisecond)
				tool.Output = `{"isError":true,"content":[{"type":"text","text":"Window unavailable"}]}`
				tt.Frame()
				if !tt.HasText("执行失败") || !tt.HasText("工具错误摘要") {
					t.Fatal("completed error hidden", tt.Texts())
				}
				tool.Output = ""
				tool.Status = "inProgress"
				tt.Frame()
				if !tt.HasText("执行中") {
					t.Fatal("running state hidden")
				}
			})
		}
	}
}

func TestProcessRecordsShareHierarchyAndCompactSpacing(t *testing.T) {
	for _, width := range []int{320, 700} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/dark=%v", width, dark), func(t *testing.T) {
				a := New(nil, "test", "/workspace", nil)
				duration := int64(90)
				first := &app.Message{ID: "tool-one", Role: "tool", TurnID: "turn", Tool: &app.Tool{Kind: "mcpToolCall", Name: "cua_repl/js", Status: "completed", Input: `{"title":"检查界面"}`, Output: textEnvelope("Window ready"), DurationMs: &duration}}
				second := &app.Message{ID: "tool-two", Role: "tool", TurnID: "turn", Tool: &app.Tool{Kind: "mcpToolCall", Name: "cua_repl/js", Status: "completed", Input: `{"title":"核验结果"}`}}
				a.State.Messages = []*app.Message{commandMessage("before", "completed"), first, second, commandMessage("after", "completed"), {ID: "answer", Role: "assistant", Text: "任务完成", Done: true}}
				tt := native.NewTester(func(c *native.Context) {
					native.Column(c).Fill().Background(c.Theme().Background).Children(func() { a.conversation(c) })
				}, width, 700)
				tt.SetDark(dark)
				tt.SetPreferences(native.Preferences{ReduceMotion: true})
				tt.Frame()
				if tt.HasText("检查界面") || len(a.conversationRows()) != 2 {
					t.Fatal("mixed tools were not folded into one group")
				}
				if err := tt.Click("命令组 before"); err != nil {
					t.Fatal(err)
				}
				tt.Advance(300 * time.Millisecond)
				labels := []string{"命令详情 before", "检查界面", "核验结果", "命令详情 after"}
				var previous native.Rect
				for i, label := range labels {
					r, ok := tt.Find(label)
					if !ok || r.H <= 0 {
						t.Fatal("missing record", label, tt.Texts())
					}
					distance := r.Y + r.H/2 - previous.Y - previous.H/2
					if i > 0 && (distance < 29 || distance > 31) {
						t.Fatal("records too far apart", previous, r)
					}
					if i == 1 || i == 2 {
						command, _ := tt.Find("命令详情 before")
						if r.X != command.X {
							t.Fatal("tool caption indented", r, command)
						}
					}
					previous = r
				}
				if dir := os.Getenv("SASA_PROCESS_PREVIEW"); dir != "" {
					os.MkdirAll(dir, 0700)
					f, err := os.Create(filepath.Join(dir, fmt.Sprintf("process-%d-%v.png", width, dark)))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
				tt.Move(310, 650)
				arrow, _ := tt.Find("工具记录 cua_repl/js箭头")
				before := copyRegion(tt.Image(), arrow)
				caption, _ := tt.Find("检查界面")
				tt.Move(caption.X+5, caption.Y+10)
				if equalPixels(before, copyRegion(tt.Image(), arrow)) {
					t.Fatal("tool arrow does not follow command hover")
				}
				if err := tt.Click("检查界面"); err != nil {
					t.Fatal(err)
				}
				if !first.Tool.Expanded || !tt.HasText("结果") {
					t.Fatal("tool did not expand")
				}
				tt.Key(0, native.KeyEnter)
				if first.Tool.Expanded {
					t.Fatal("keyboard did not collapse tool")
				}
			})
		}
	}
}
