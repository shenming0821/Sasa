package ui

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
	"github.com/shenming0821/Sasa/internal/attachment"
)

func navigationTickPixels(picture *image.RGBA, rail native.Rect, center float32) int {
	ink := picture.RGBAAt(int(rail.X+rail.W/2), int(center))
	background := picture.RGBAAt(int(rail.X+1), int(center))
	contrast := math.Abs(float64(int(ink.R) - int(background.R)))
	count := 0
	for column := int(rail.X + 2); column < int(rail.X+rail.W-2); column++ {
		pixel := picture.RGBAAt(column, int(center))
		if math.Abs(float64(int(pixel.R)-int(background.R))) >= contrast*.5 {
			count++
		}
	}
	return count
}

func navigationApp(kind agent.AgentKind, count int) *App {
	a := New(stubBackend{}, "test", "/project", nil)
	a.sidebarOpen = false
	a.kind, a.State.Agent = kind, kind
	a.State.Draft = "保留原草稿"
	for index := 0; index < count; index++ {
		turn := fmt.Sprintf("turn-%d", index)
		a.State.Messages = append(a.State.Messages,
			&app.Message{ID: "user-" + turn, TurnID: turn, Role: "user", Text: fmt.Sprintf("%s 第 %d 个问题", kind.String(), index+1), Done: true},
			&app.Message{ID: "tool-" + turn, TurnID: turn, Role: "tool", Tool: &app.Tool{Kind: "commandExecution", Command: "pwd", Status: "completed"}, Done: true},
			&app.Message{ID: "tool2-" + turn, TurnID: turn, Role: "tool", Tool: &app.Tool{Kind: "commandExecution", Command: "ls", Status: "completed"}, Done: true},
			&app.Message{ID: "reply-" + turn, TurnID: turn, Role: "assistant", Text: fmt.Sprintf("第 %d 个回答\n\n", index+1) + strings.Repeat("这是用于验证消息定位和阅读位置保持的内容。\n\n", 6), Done: true},
		)
	}
	return a
}

func TestConversationNavigationBothAgents(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{480, 1000} {
				t.Run(fmt.Sprintf("%s/dark=%t/width=%d", kind, dark, width), func(t *testing.T) {
					a := navigationApp(kind, 14)
					tt := native.NewTester(a.view, width, 760)
					tt.SetDark(dark)
					rail, ok := tt.Find("对话定位导航")
					if !ok || rail.W < 24 || rail.H < 100 {
						t.Fatalf("navigation missing: %+v", rail)
					}
					composer, _ := tt.Find("任务输入")
					before := a.scroll.Y
					start, step := navigationGeometry(rail.H, 14)
					assertLatestHighlight := func() {
						t.Helper()
						if !a.list.AtEnd() {
							t.Fatal("expected list at bottom")
						}
						picture := tt.Image()
						firstColor := picture.RGBAAt(int(rail.X+rail.W/2), int(rail.Y+start+.5*step))
						lastColor := picture.RGBAAt(int(rail.X+rail.W/2), int(rail.Y+start+13.5*step))
						firstBrightness := int(firstColor.R) + int(firstColor.G) + int(firstColor.B)
						lastBrightness := int(lastColor.R) + int(lastColor.G) + int(lastColor.B)
						if dark && lastBrightness <= firstBrightness || !dark && lastBrightness >= firstBrightness {
							t.Fatalf("last tick is not highlighted at bottom: first=%v last=%v", firstColor, lastColor)
						}
						if navigationTickPixels(picture, rail, rail.Y+start+.5*step) != navigationTickPixels(picture, rail, rail.Y+start+13.5*step) {
							t.Fatal("active tick is longer without hover")
						}
					}
					assertLatestHighlight()
					x, y := rail.X+rail.W/2, rail.Y+start+2.5*step
					beforePixels := navigationTickPixels(tt.Image(), rail, y)
					leftEdge := func(picture *image.RGBA) int {
						background := picture.RGBAAt(int(rail.X+1), int(y))
						for column := int(rail.X + 2); column < int(rail.X+rail.W-2); column++ {
							if picture.RGBAAt(column, int(y)) != background {
								return column
							}
						}
						return -1
					}
					beforeLeft := leftEdge(tt.Image())
					tt.Move(x, y)
					tt.Advance(50 * time.Millisecond)
					if leftEdge(tt.Image()) != beforeLeft {
						t.Fatal("tick left edge moved during animation")
					}
					tt.Advance(150 * time.Millisecond)
					selectedPixels := navigationTickPixels(tt.Image(), rail, y)
					nearPixels := navigationTickPixels(tt.Image(), rail, y-step)
					farPixels := navigationTickPixels(tt.Image(), rail, y+5*step)
					if selectedPixels <= beforePixels || selectedPixels-beforePixels > 11 || nearPixels <= farPixels || leftEdge(tt.Image()) != beforeLeft {
						t.Fatalf("marks did not magnify near pointer: before=%d selected=%d near=%d far=%d", beforePixels, selectedPixels, nearPixels, farPixels)
					}
					preview, shown := tt.Find("对话预览")
					if !shown || !tt.HasText(fmt.Sprintf("%s 第 3 个问题", kind.String())) || !tt.HasText("第 3 个回答") {
						t.Fatalf("hover preview missing: %v", tt.Texts())
					}
					if preview.X < rail.X+rail.W || preview.X+preview.W > float32(width) || preview.Y < rail.Y-16 || preview.Y+preview.H > rail.Y+rail.H+24 {
						t.Fatalf("preview clipped: rail=%+v preview=%+v", rail, preview)
					}
					titleBounds, foundTitle := tt.Find(a.navigationEntries[2].title())
					if !foundTitle || math.Abs(float64(titleBounds.Y+titleBounds.H+4-y)) > 1 || math.Abs(float64(preview.X-(rail.X+14+18)-9)) > 1 {
						t.Fatalf("preview is not aligned with tick: preview=%+v rail=%+v tickY=%v", preview, rail, y)
					}
					reply := a.navigationEntries[2].reply
					replyText := reply.Text
					reply.Text = ""
					tt.Frame()
					if shortPreview, _ := tt.Find("对话预览"); math.Abs(float64(shortPreview.Y+24.1-y)) > 1 {
						t.Fatal("question-only preview is not centered on title")
					}
					reply.Text = replyText
					tt.Frame()
					user := a.navigationEntries[2].user
					userText := user.Text
					user.Text = strings.Repeat("验证两行问题标题与回复之间空白的对齐位置。", 5)
					tt.Frame()
					wrappedTitle, foundTitle := tt.Find(a.navigationEntries[2].title())
					if !foundTitle || wrappedTitle.H < 30 || math.Abs(float64(wrappedTitle.Y+wrappedTitle.H+4-y)) > 1 {
						t.Fatalf("wrapped title gap is not aligned: %+v", wrappedTitle)
					}
					user.Text = userText
					tt.Frame()
					if after, _ := tt.Find("任务输入"); after != composer || a.scroll.Y != before {
						t.Fatal("hover changed the layout or reading position")
					}
					tt.ClickAt(x, y)
					tt.Frame()
					first, last := a.list.Visible()
					entry := a.navigationEntries[2]
					if entry.row < first || entry.row > last || a.list.AtEnd() {
						t.Fatalf("click missed prompt: row=%d visible=%d..%d", entry.row, first, last)
					}
					tt.Move(float32(width)-4, 200)
					tt.Advance(150 * time.Millisecond)
					if tt.HasText("对话预览") || a.State.Draft != "保留原草稿" {
						t.Fatal("preview did not dismiss or draft changed")
					}
					a.State.Messages[len(a.State.Messages)-1].Text += "后台继续输出"
					a.State.Messages[len(a.State.Messages)-1].Done = false
					tt.Frame()
					if firstAfter, _ := a.list.Visible(); firstAfter != first {
						t.Fatal("streaming pulled navigation away from selected prompt")
					}
					if err := tt.Click("返回最新消息"); err != nil {
						t.Fatal(err)
					}
					if !a.list.AtEnd() {
						t.Fatal("return to latest did not restore following")
					}
					assertLatestHighlight()
				})
			}
		}
	}
}

func TestConversationNavigationKeyboardAndHistory(t *testing.T) {
	a := navigationApp(agent.Claude, 20)
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.Click("对话定位导航"); err != nil {
		t.Fatal(err)
	}
	tt.Move(990, 200)
	tt.Key(0, native.KeyHome)
	if a.navigationID != "user-turn-0" || !tt.HasText("对话预览") {
		t.Fatalf("Home did not preview first prompt: id=%q focused=%t keyboard=%t texts=%v", a.navigationID, tt.Focused("对话定位导航"), a.navigationKeyboard, tt.Texts())
	}
	tt.Key(0, native.KeyDown)
	if a.navigationID != "user-turn-1" {
		t.Fatal("arrow did not preview adjacent prompt")
	}
	tt.Key(0, native.KeyEnter)
	first, last := a.list.Visible()
	if row := a.navigationEntries[1].row; row < first || row > last {
		t.Fatal("keyboard activation did not navigate")
	}
	tt.Key(0, native.KeyEscape)
	if tt.HasText("对话预览") {
		t.Fatal("Escape did not dismiss preview")
	}
	tt.Key(0, native.KeyEnd)
	selected := a.navigationID
	older := []*app.Message{
		{ID: "older-user", Role: "user", TurnID: "older", Text: "更早的问题", Done: true},
		{ID: "older-reply", Role: "assistant", TurnID: "older", Text: "更早的回复", Done: true},
	}
	a.State.Messages = append(older, a.State.Messages...)
	tt.Frame()
	if a.navigationID != selected || !tt.HasText("Claude Code 第 20 个问题") {
		t.Fatal("history prepend changed preview identity")
	}
	a.newSessionForAgent("/project", agent.Codex, "")
	tt.Frame()
	if tt.HasText("对话预览") || tt.HasText("对话定位导航") {
		t.Fatal("preview leaked into empty session")
	}
}

func TestConversationNavigationProjectionAndDensity(t *testing.T) {
	a := navigationApp(agent.Codex, 3)
	a.State.Messages[0].Text = ""
	a.State.Messages[0].Images = []attachment.Image{{Path: "/image.png"}}
	a.State.Messages[7].Text = "\n"
	rows := a.conversationRows()
	entries := a.navigationEntries
	if len(entries) != 3 || entries[0].title() != "图片消息" || entries[1].row != 3 || len(rows) != 9 {
		t.Fatalf("navigation disagrees with grouped rows: entries=%+v rows=%d", entries, len(rows))
	}
	a.State.Messages[7].Text = "**流式回复更新**"
	a.conversationRows()
	if conversationExcerpt(entries[1].reply.Text, 30) != "流式回复更新" {
		t.Fatal("preview cached stale reply text")
	}
	for _, count := range []int{2, 50, 500} {
		start, step := navigationGeometry(200, count)
		if start < 0 || step <= 0 || start+float32(count)*step > 200 || math.IsNaN(float64(step)) {
			t.Fatalf("invalid dense geometry for %d: %f %f", count, start, step)
		}
	}
}

func TestConversationNavigationDenseResizeAndReducedMotion(t *testing.T) {
	a := navigationApp(agent.Codex, 200)
	tt := native.NewTester(a.view, 1000, 760)
	tt.SetPreferences(native.Preferences{ReduceMotion: true})
	for _, size := range [][2]int{{1000, 760}, {480, 360}} {
		tt.SetSize(size[0], size[1])
		rail, ok := tt.Find("对话定位导航")
		if !ok {
			t.Fatal("dense conversation lost navigation")
		}
		start, step := navigationGeometry(rail.H, 200)
		for _, index := range []int{0, 199} {
			tt.Move(rail.X+rail.W/2, rail.Y+start+(float32(index)+.5)*step)
			preview, shown := tt.Find("对话预览")
			if !shown || preview.X < 0 || preview.Y < 0 || preview.X+preview.W > float32(size[0]) || preview.Y+preview.H > float32(size[1]) {
				t.Fatalf("edge preview outside resized window: %+v", preview)
			}
			if a.navigationID != fmt.Sprintf("user-turn-%d", index) {
				t.Fatal("dense navigation cannot reach first or last prompt")
			}
		}
	}
}
