package ui

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/codex"
)

func commandMessage(id, status string) *app.Message {
	return &app.Message{ID: id, Role: "tool", TurnID: "turn", Tool: &app.Tool{Kind: "commandExecution", Command: "git status --short", Status: status}}
}

func TestCommandGroupingPreservesBoundariesAndReadingAnchor(t *testing.T) {
	a := New(nil, "test", "/tmp", nil)
	one, two, three, four := commandMessage("one", "completed"), commandMessage("two", "completed"), commandMessage("three", "inProgress"), commandMessage("four", "completed")
	four.TurnID = "next"
	a.State.Messages = []*app.Message{one, two, {ID: "text", Role: "assistant", Text: "explanation"}, three, four, {ID: "approval", Role: "interaction", Interaction: &app.Interaction{}}, commandMessage("five", "completed"), {ID: "other", Role: "tool", Tool: &app.Tool{Kind: "fileChange"}}, commandMessage("six", "completed")}
	rows := a.conversationRows()
	if len(rows) != 8 || len(rows[0].commands) != 2 || rows[1].message.ID != "text" || len(rows[4].commands) != 0 || len(rows[6].commands) != 0 {
		t.Fatalf("incorrect groups: %+v", rows)
	}
	a.saved.Anchor = "two"
	a.saved.AnchorOffset = 80
	a.restoreReading()
	if a.saved.Anchor != "one" || a.saved.AnchorOffset != 0 || !a.restoreOffset {
		t.Fatalf("legacy child anchor lost: %+v", a.saved)
	}
	*a.commandGroupOpen("one") = true
	two.Tool.Expanded = true
	a.saved.Expanded = map[string]bool{"older-tool": true}
	saved := a.snapshot().Sessions[0]
	if !saved.Expanded["command-group:one"] || !saved.Expanded["two"] || !saved.Expanded["older-tool"] {
		t.Fatalf("expansion lost: %+v", saved.Expanded)
	}
	b := New(nil, "test", "/tmp", nil)
	b.saved = saved
	if !*b.commandGroupOpen("one") || *b.commandGroupOpen("three") {
		t.Fatal("group persistence not independent")
	}
}

func TestCommandHoverAndAnimatedDisclosure(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprintf("reduced=%v", reduced), func(t *testing.T) {
			a := New(nil, "test", "/tmp", nil)
			commands := []*app.Message{commandMessage("one", "completed"), commandMessage("two", "inProgress")}
			tt := native.NewTester(func(c *native.Context) {
				native.Column(c).Fill().Background(c.Theme().Background).Children(func() { a.commandGroup(c, commands, false); native.Text(c, "after").Label("after") })
			}, 600, 400)
			tt.SetPreferences(native.Preferences{ReduceMotion: reduced})
			tt.Move(590, 390)
			arrow, _ := tt.Find("命令组 one箭头")
			before := copyRegion(tt.Image(), arrow)
			header, _ := tt.Find("命令组 one")
			tt.Move(header.X+30, header.Y+15)
			if equalPixels(before, copyRegion(tt.Image(), arrow)) {
				t.Fatal("hover did not reveal arrow")
			}
			tt.Move(590, 390)
			if !equalPixels(before, copyRegion(tt.Image(), arrow)) {
				t.Fatal("arrow remained after hover ended")
			}
			initial, _ := tt.Find("after")
			if err := tt.Click("命令组 one"); err != nil {
				t.Fatal(err)
			}
			tt.Advance(60 * time.Millisecond)
			middle, _ := tt.Find("after")
			tt.Advance(250 * time.Millisecond)
			full, _ := tt.Find("after")
			if full.Y <= initial.Y || (!reduced && (middle.Y <= initial.Y || middle.Y >= full.Y)) {
				t.Fatalf("no height animation: %v %v %v", initial, middle, full)
			}
			if !tt.HasText(`Bash "git status --short"`) || !tt.HasText(`正在执行 Bash "git status --short"`) {
				t.Fatal("individual statuses lost")
			}
			tt.Move(590, 390)
			if !equalPixels(before, copyRegion(tt.Image(), arrow)) {
				t.Fatal("pointer click kept arrow visible after leaving")
			}
			if err := tt.Click("命令组 one"); err != nil {
				t.Fatal(err)
			}
			tt.Advance(60 * time.Millisecond)
			middle, _ = tt.Find("after")
			if !reduced && (middle.Y <= initial.Y || middle.Y >= full.Y) {
				t.Fatalf("no collapse animation: %v", middle)
			}
			tt.Advance(250 * time.Millisecond)
			closed, _ := tt.Find("after")
			if closed.Y != initial.Y || *a.commandGroupOpen("one") {
				t.Fatal("did not collapse")
			}
			commands[1].Tool.Status = "completed"
			commands = append(commands, commandMessage("three", "completed"))
			tt.Frame()
			if *a.commandGroupOpen("one") || !tt.HasText("执行了命令") {
				t.Fatal("updates overrode manual choice")
			}
			tt.Key(0, native.KeySpace)
			tt.Advance(300 * time.Millisecond)
			if !*a.commandGroupOpen("one") {
				t.Fatal("Space did not open the focused disclosure")
			}
			tt.Key(0, native.KeyEnter)
			tt.Advance(300 * time.Millisecond)
			if *a.commandGroupOpen("one") {
				t.Fatal("Enter did not close the focused disclosure")
			}
		})
	}
}

func TestCommandGroupLongContentIsClippedAndDetailsScrollable(t *testing.T) {
	for _, width := range []int{320, 800} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("width=%d/dark=%v", width, dark), func(t *testing.T) {
				a := New(nil, "test", "/tmp", nil)
				m := commandMessage("long", "completed")
				m.Tool.Command = strings.Repeat("print('long command 中文内容')\n", 90)
				m.Tool.Output = strings.Repeat("output text 中文\n", 200)
				*a.commandGroupOpen("long") = true
				tt := native.NewTester(func(c *native.Context) {
					native.Column(c).Fill().Gap(12).Background(c.Theme().Background).Children(func() {
						a.commandGroup(c, []*app.Message{m}, false)
						native.Text(c, "following message").Label("following message")
					})
				}, width, 720)
				tt.SetDark(dark)
				row, _ := tt.Find("命令详情 long")
				if row.H != 30 {
					t.Fatalf("command preview wrapped: %+v", row)
				}
				if err := tt.Click("命令详情 long"); err != nil {
					t.Fatal(err)
				}
				tt.Advance(300 * time.Millisecond)
				if !m.Tool.Expanded || !tt.HasText("完整命令") {
					t.Fatal("missing full command")
				}
				details, ok := tt.Find("命令输出 long")
				if !ok || details.H > 180 {
					t.Fatalf("unbounded details: %+v", details)
				}
				marker, _ := tt.Find("following message")
				assertBlankBelow(t, tt.Image(), int(marker.Y+marker.H)+10)
				tt.Scroll(details.X+details.W/2, details.Y+details.H/2, 0, -400)
				marker, _ = tt.Find("following message")
				assertBlankBelow(t, tt.Image(), int(marker.Y+marker.H)+10)
				if err := tt.Click("命令详情 long"); err != nil {
					t.Fatal(err)
				}
				tt.Advance(60 * time.Millisecond)
				marker, _ = tt.Find("following message")
				assertBlankBelow(t, tt.Image(), int(marker.Y+marker.H)+10)
				tt.Advance(300 * time.Millisecond)
				m.Tool.Command += strings.Repeat("stream 中文\n", 30)
				tt.SetSize(480, 720)
				marker, _ = tt.Find("following message")
				assertBlankBelow(t, tt.Image(), int(marker.Y+marker.H)+10)
			})
		}
	}
}

func copyRegion(img *image.RGBA, r native.Rect) []byte {
	var pixels []byte
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			p := img.RGBAAt(x, y)
			pixels = append(pixels, p.R, p.G, p.B, p.A)
		}
	}
	return pixels
}
func equalPixels(a, b []byte) bool { return string(a) == string(b) }

func TestCommandSummaryKeepsFailureAndAwaitingFinalVisible(t *testing.T) {
	one, two := commandMessage("one", "completed"), commandMessage("two", "inProgress")
	exit := 1
	one.Tool.ExitCode = &exit
	text, failed, running := commandSummary([]*app.Message{one, two}, false)
	if !failed || !running || text != "正在执行命令 · 1失败" {
		t.Fatalf("lost mixed state: %s %v", text, failed)
	}
	two.Tool.Status = "completed"
	two.Tool.AwaitingFinal = true
	text, _, _ = commandSummary([]*app.Message{one, two}, false)
	if !strings.HasPrefix(text, "等待确认 ") {
		t.Fatal(text)
	}
	two.Tool.AwaitingFinal = false
	text, failed, _ = commandSummary([]*app.Message{one, two}, false)
	if text != "执行了命令 · 1失败" || !failed {
		t.Fatal("failure disappeared")
	}
	one.Tool.Status = "failed"
	two.Tool.Status = "failed"
	text, failed, _ = commandSummary([]*app.Message{one, two}, false)
	if text != "命令执行失败" || !failed {
		t.Fatal("all-failed group lost failure summary")
	}
	one.Tool.Status = "completed"
	two.Tool.Status = "completed"
	one.Tool.ExitCode = nil
	one.Tool.Status, two.Tool.Status = "declined", "declined"
	text, _, _ = commandSummary([]*app.Message{one, two}, false)
	if text != "命令已拒绝" {
		t.Fatal("declined commands reported as executed")
	}
}

func TestBusyTurnKeepsLatestCommandGroupRunning(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprintf("reduced=%v", reduced), func(t *testing.T) {
			a := New(nil, "test", "/tmp", nil)
			a.State.Busy = true
			a.State.Messages = []*app.Message{
				{ID: "q", Role: "user", Text: "run"},
				commandMessage("one", "completed"),
				{ID: "note", Role: "assistant", Text: "partial reply", Done: true},
				commandMessage("two", "completed"),
			}
			tt := native.NewTester(a.view, 600, 600)
			tt.SetPreferences(native.Preferences{ReduceMotion: reduced})
			if !tt.HasText("正在执行命令") || !tt.HasText("执行了命令") {
				t.Fatalf("busy turn did not keep the latest group running: %v", tt.Texts())
			}
			a.State.Messages = append(a.State.Messages, &app.Message{ID: "conclusion", Role: "assistant", Text: "follow-up", Done: true})
			tt.Frame()
			if tt.HasText("正在执行命令") {
				t.Fatal("follow-up output kept the running header", tt.Texts())
			}
			a.State.Messages = a.State.Messages[:len(a.State.Messages)-1]
			a.State.Busy = false
			tt.Frame()
			if tt.HasText("正在执行命令") {
				t.Fatal("finished turn kept the running header", tt.Texts())
			}
		})
	}
}

func TestPendingInteractionUsesGroupedListIndex(t *testing.T) {
	a := New(nil, "test", "/tmp", nil)
	for i := range 40 {
		a.State.Messages = append(a.State.Messages, commandMessage(fmt.Sprint(i), "completed"))
	}
	request := &app.Interaction{Status: "待处理", Request: codex.Interaction{Method: "item/commandExecution/requestApproval", Command: "go test ./..."}}
	a.State.Messages = append(a.State.Messages,
		&app.Message{ID: "approval", Role: "interaction", Interaction: request},
		&app.Message{ID: "after", Role: "assistant", Text: strings.Repeat("later text\n\n", 100), Done: true})
	tt := native.NewTester(a.view, 640, 480)
	if err := tt.Click("处理待办（1）"); err != nil {
		t.Fatal(err)
	}
	first, _ := a.list.Visible()
	if first > 1 || !tt.HasText("需要批准命令") {
		t.Fatalf("pending interaction not reached after grouping: first=%d", first)
	}
}

func TestCommandGroupTitleKeepsContentWidthAndAdjacentArrow(t *testing.T) {
	for _, width := range []int{320, 800} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := New(nil, "test", "/tmp", nil)
			command := commandMessage("title", "completed")
			tt := native.NewTester(func(c *native.Context) {
				native.Column(c).Fill().Children(func() { a.commandGroup(c, []*app.Message{command}, false) })
			}, width, 240)
			caption, ok := tt.Find("执行了命令")
			arrow, _ := tt.Find("命令组 title箭头")
			if !ok || caption.W < 60 {
				t.Fatalf("short title has no content width: %+v", caption)
			}
			if arrow.X != caption.X+caption.W+8 || arrow.X+arrow.W > float32(width) {
				t.Fatalf("arrow not adjacent/in bounds: caption=%+v arrow=%+v", caption, arrow)
			}
			command.Tool.Status = "inProgress"
			command.Tool.Command = strings.Repeat("long command ", 100)
			tt.Frame()
			arrow, _ = tt.Find("命令组 title箭头")
			if arrow.X+arrow.W > float32(width) {
				t.Fatalf("long summary displaced arrow: %+v", arrow)
			}
			command.Tool.Status = "completed"
			tt.Frame()
			caption, _ = tt.Find("执行了命令")
			if caption.W < 60 {
				t.Fatalf("completed title remained compressed: %+v", caption)
			}
		})
	}
}

func TestCommandGroupCaptionVerticallyCentersWithIcon(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, live := range []bool{false, true} {
			for _, open := range []bool{false, true} {
				t.Run(fmt.Sprintf("dark=%v/live=%v/open=%v", dark, live, open), func(t *testing.T) {
					assertCommandGroupCaptionCentered(t, dark, live, open)
				})
			}
		}
	}
}

func assertCommandGroupCaptionCentered(t *testing.T, dark, live, open bool) {
	t.Helper()
	a := New(nil, "test", "/tmp", nil)
	command := commandMessage("title", "completed")
	captionText := "执行了命令"
	if live {
		command.Tool.Status = "inProgress"
		captionText = "正在执行命令"
	}
	*a.commandGroupOpen("title") = open
	tt := native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Background(c.Theme().Background).Children(func() {
			a.commandGroup(c, []*app.Message{command}, false)
		})
	}, 400, 60)
	tt.SetDark(dark)
	tt.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Move(30, 15)
	img := tt.Image()
	caption, ok := tt.Find(captionText)
	if !ok {
		t.Fatal("caption missing")
	}
	row, _ := tt.Find("命令组 title")
	background := img.RGBAAt(399, 0)
	glyphCenter := func(x0, x1 int) float64 {
		top, bottom := int(row.Y+row.H), -1
		for y := int(row.Y); y < int(row.Y+row.H); y++ {
			for x := x0; x < x1; x++ {
				p := img.RGBAAt(x, y)
				contrast := max(math.Abs(float64(p.R)-float64(background.R)), math.Abs(float64(p.G)-float64(background.G)), math.Abs(float64(p.B)-float64(background.B)))
				if contrast > 50 {
					top = min(top, y)
					bottom = max(bottom, y)
				}
			}
		}
		if bottom < top {
			return -1
		}
		return float64(top+bottom) / 2
	}
	iconCenter := glyphCenter(int(caption.X)-16-8, int(caption.X)-8)
	iconBounds := image.Rectangle{}
	for vertical := int(row.Y); vertical < int(row.Y+row.H); vertical++ {
		for horizontal := int(caption.X) - 24; horizontal < int(caption.X)-8; horizontal++ {
			pixel := img.RGBAAt(horizontal, vertical)
			contrast := max(math.Abs(float64(pixel.R)-float64(background.R)), math.Abs(float64(pixel.G)-float64(background.G)), math.Abs(float64(pixel.B)-float64(background.B)))
			if contrast > 50 {
				iconBounds = iconBounds.Union(image.Rect(horizontal, vertical, horizontal+1, vertical+1))
			}
		}
	}
	if iconBounds.Dx() != 12 || iconBounds.Dy() != 12 {
		t.Fatalf("command icon must paint a 12x12 pixel square: %v", iconBounds)
	}
	textCenter := glyphCenter(int(caption.X), int(caption.X+caption.W))
	if iconCenter < 0 || textCenter < 0 {
		t.Fatalf("glyphs missing: icon=%v text=%v", iconCenter, textCenter)
	}
	if diff := textCenter - iconCenter; math.Abs(diff) > .5 {
		t.Fatalf("caption not centered on icon: icon=%.1f text=%.1f diff=%.1f", iconCenter, textCenter, diff)
	}
	arrow, ok := tt.Find("命令组 title箭头")
	if !ok {
		t.Fatal("arrow missing")
	}
	arrowCenter := glyphCenter(int(arrow.X), int(arrow.X+arrow.W))
	if arrowCenter < 0 || math.Abs(arrowCenter-textCenter-1) > .5 {
		t.Fatalf("arrow lost optical alignment below caption center: arrow=%.1f text=%.1f", arrowCenter, textCenter)
	}
}
