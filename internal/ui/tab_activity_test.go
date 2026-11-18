package ui

import (
	"bytes"
	"fmt"
	"image"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
)

func TestSessionTabActivitySelectionAndCompletion(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		for _, dark := range []bool{false, true} {
			for _, reduce := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/dark=%v/reduce=%v", kind, dark, reduce), func(t *testing.T) {
					a := New(stubBackend{}, "test", "/project", nil)
					first := a.session
					first.kind = kind
					first.saved.CustomTitle = "执行任务"
					a.newSession("/project")
					second := a.session
					second.saved.CustomTitle = "另一个会话"
					pane := focusedPane(a.layoutFor("/project"))
					pane.Active = sessionTabID(first.id)
					tester := native.NewTester(func(c *native.Context) {
						a.withPane("/project", pane, func() {
							native.Column(c).Fill().Children(func() { a.sessionTabs(c, true, true) })
						})
					}, 800, 80)
					tester.SetDark(dark)
					tester.SetPreferences(native.Preferences{ReduceMotion: reduce})
					line := func(index int) []byte {
						t.Helper()
						img := tester.Image()
						var pixels []byte
						for y := 36; y < 38; y++ {
							for x := index * 168; x < (index+1)*168; x++ {
								color := img.RGBAAt(x, y)
								pixels = append(pixels, color.R, color.G, color.B, color.A)
							}
						}
						return pixels
					}
					selectedIdle := line(0)
					backgroundIdle := line(1)
					titleBefore, _ := tester.Find("执行任务")
					closeBefore, _ := tester.Find("关闭会话标签 1")
					first.State.Begin("请执行任务")
					tester.Frame()
					tester.Advance(400 * time.Millisecond)
					running := line(0)
					if bytes.Equal(running, selectedIdle) {
						t.Fatal("selected running tab has no visible highlight")
					}
					tester.Advance(400 * time.Millisecond)
					if bytes.Equal(running, line(0)) != reduce {
						t.Fatal("running highlight did not respect reduced motion")
					}
					if !bytes.Equal(backgroundIdle, line(1)) {
						t.Fatal("idle background tab acquired an underline")
					}
					// Visible output must not remove the running indication.
					beforeOutput := line(0)
					first.State.Messages = append(first.State.Messages, &app.Message{Role: "assistant", Text: "正在处理"})
					tester.Frame()
					if !bytes.Equal(beforeOutput, line(0)) {
						t.Fatal("assistant output changed the running indication")
					}
					if titleAfter, _ := tester.Find("执行任务"); titleAfter != titleBefore {
						t.Fatal("activity moved the tab title")
					}
					if closeAfter, _ := tester.Find("关闭会话标签 1"); closeAfter != closeBefore {
						t.Fatal("activity moved the close button")
					}
					pane.Active = sessionTabID(second.id)
					tester.Frame()
					tester.Advance(400 * time.Millisecond)
					backgroundRunning := line(0)
					if bytes.Equal(backgroundRunning, backgroundIdle) || bytes.Equal(backgroundRunning, selectedIdle) {
						t.Fatal("background running tab must show a short line rather than a full selection line")
					}
					if !bytes.Equal(selectedIdle, line(1)) {
						t.Fatal("newly selected idle tab lost its solid underline")
					}
					first.State.Messages = append(first.State.Messages, &app.Message{
						Role: "interaction", Interaction: &app.Interaction{Status: "待处理"},
					})
					tester.Frame()
					waiting := line(0)
					tester.Advance(400 * time.Millisecond)
					if !bytes.Equal(waiting, line(0)) || bytes.Equal(waiting, backgroundRunning) {
						t.Fatal("waiting for confirmation must show a distinct static line")
					}
					if got := a.tabTooltip(a.tabs[0]); got != "执行任务\n待确认" {
						t.Fatalf("missing pending status in tooltip: %q", got)
					}
					first.State.BeginStop()
					tester.Frame()
					tester.Advance(400 * time.Millisecond)
					if bytes.Equal(waiting, line(0)) || bytes.Equal(backgroundIdle, line(0)) {
						t.Fatal("stop request removed the indication before completion")
					}
					first.State.Busy = false
					tester.Frame()
					if !bytes.Equal(backgroundIdle, line(0)) {
						t.Fatal("completed background tab retained its running underline")
					}
					pane.Active = sessionTabID(first.id)
					tester.Frame()
					if !bytes.Equal(selectedIdle, line(0)) {
						t.Fatal("completed selected tab did not restore its solid underline")
					}
				})
			}
		}
	}
}

func TestSessionTabActivityStopsOnFailureAndDisconnect(t *testing.T) {
	for _, failure := range []string{"error", "disconnect"} {
		t.Run(failure, func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			tester := native.NewTester(func(c *native.Context) {
				row := native.Row(c).Width(168).Height(38)
				tabFace(c, row, false, a.session)
			}, 200, 80)
			idle := append([]byte(nil), tester.Image().Pix...)
			a.State.Begin("执行任务")
			tester.Frame()
			tester.Advance(400 * time.Millisecond)
			if failure == "error" {
				a.State.Error = "执行失败"
			} else {
				a.State.Disconnected = true
			}
			tester.Frame()
			if !bytes.Equal(idle, tester.Image().Pix) {
				t.Fatal("failed or disconnected session still appears to be executing")
			}
		})
	}
}

func TestSessionTabActivityPaintStaysInsideUnderline(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	tester := native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Background(c.Theme().Surface).Children(func() {
			row := native.Row(c).Width(168).Height(38)
			tabFace(c, row, false, a.session)
		})
	}, 240, 80)
	idle := image.NewRGBA(tester.Image().Bounds())
	copy(idle.Pix, tester.Image().Pix)
	a.State.Begin("执行任务")
	tester.Frame()
	for frame := 0; frame < 10; frame++ {
		tester.Advance(160 * time.Millisecond)
		got := tester.Image()
		visible := 0
		for x := 0; x < 168; x++ {
			if got.RGBAAt(x, 36) != idle.RGBAAt(x, 36) {
				visible++
			}
		}
		if visible < 50 {
			t.Fatalf("running band became too faint or disappeared: frame=%d visible=%d", frame, visible)
		}
		for y := 0; y < 80; y++ {
			for x := 0; x < 240; x++ {
				if x < 168 && y >= 36 && y < 38 {
					continue
				}
				if got.RGBAAt(x, y) != idle.RGBAAt(x, y) {
					t.Fatalf("activity painted outside its underline at (%d,%d)", x, y)
				}
			}
		}
	}
}
