package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
)

type conversationNavigation struct {
	navigationID       string
	navigationPointer  float32
	navigationHovering bool
	navigationKeyboard bool
}

type conversationEntry struct {
	row   int
	user  *app.Message
	reply *app.Message
}

func conversationEntries(rows []conversationRow) []conversationEntry {
	var entries []conversationEntry
	for index, row := range rows {
		message := row.message
		if message.Role == "user" {
			entries = append(entries, conversationEntry{row: index, user: message})
		} else if len(entries) > 0 && message.Role == "assistant" {
			entry := &entries[len(entries)-1]
			if message.TurnID == "" || message.TurnID == entry.user.TurnID {
				entry.reply = message
			}
		}
	}
	return entries
}

func conversationExcerpt(text string, limit int) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimLeft(strings.TrimSpace(line), "#> ")
	}
	text = strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
	for _, marker := range []string{"**", "__", "`"} {
		text = strings.ReplaceAll(text, marker, "")
	}
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return text
}

func (entry conversationEntry) title() string {
	if title := conversationExcerpt(entry.user.Text, 100); title != "" {
		return title
	}
	if len(entry.user.Images) > 0 {
		return "图片消息"
	}
	return "用户消息"
}

func navigationGeometry(height float32, count int) (float32, float32) {
	step := min(float32(10), max(float32(0), height-16)/float32(max(1, count)))
	return (height - step*float32(count)) / 2, step
}

func navigationIndex(entries []conversationEntry, id string) int {
	for index, entry := range entries {
		if entry.user.ID == id {
			return index
		}
	}
	return -1
}

func (a *App) conversationNavigator(c *native.Context, entries []conversationEntry) {
	if len(entries) < 2 {
		a.navigationID = ""
		a.navigationHovering, a.navigationKeyboard = false, false
		return
	}
	t := c.Theme()
	area := native.ButtonBase(c.Key("conversation-navigation")).Label("对话定位导航").
		Absolute().Left(4).Top(16).Bottom(24).Width(36).Padding(0).Radius(6)
	bounds := area.Bounds()
	start, step := navigationGeometry(bounds.H, len(entries))
	first, _ := a.list.Visible()
	current := 0
	for index, entry := range entries {
		if entry.row <= first {
			current = index
		}
	}
	if a.list.AtEnd() {
		current = len(entries) - 1
	}
	selected := navigationIndex(entries, a.navigationID)
	move := func(index int) {
		selected = max(0, min(len(entries)-1, index))
		a.navigationID = entries[selected].user.ID
		a.navigationKeyboard = true
		c.Announce(fmt.Sprintf("第 %d 轮：%s", selected+1, entries[selected].title()))
	}
	switch {
	case area.Shortcut(0, native.KeyDown):
		if selected < 0 {
			move(current)
		} else {
			move(selected + 1)
		}
	case area.Shortcut(0, native.KeyUp):
		if selected < 0 {
			move(current)
		} else {
			move(selected - 1)
		}
	case area.Shortcut(0, native.KeyHome):
		move(0)
	case area.Shortcut(0, native.KeyEnd):
		move(len(entries) - 1)
	case area.Shortcut(0, native.KeyEscape):
		a.navigationID, a.navigationKeyboard = "", false
		selected = -1
	}
	_, pointer, over := area.PointerPosition()
	if over && step > 0 && pointer >= start-8 && pointer <= bounds.H-start+8 {
		if !a.navigationHovering || pointer != a.navigationPointer {
			selected = max(0, min(len(entries)-1, int((pointer-start)/step)))
			a.navigationID = entries[selected].user.ID
			a.navigationKeyboard = false
		}
		a.navigationHovering, a.navigationPointer = true, pointer
	} else {
		a.navigationHovering = false
		if !area.Focused() || !a.navigationKeyboard {
			a.navigationID, selected = "", -1
		}
	}
	if selected >= 0 {
		area.Value(fmt.Sprintf("第 %d 轮：%s", selected+1, entries[selected].title()))
	}
	if area.Clicked() {
		if selected < 0 {
			selected = current
		}
		a.restoreOffset = false
		a.list.ScrollTo(entries[selected].row, native.Start)
	}
	widths := make([]float32, len(entries))
	for index, entry := range entries {
		width := float32(8)
		if selected >= 0 {
			distance := math.Abs(float64(index - selected))
			width += float32(8 * math.Exp(-distance*distance/3))
		}
		if !c.Preferences().ReduceMotion {
			width = area.Animate(entry.user.ID, width, 100*time.Millisecond)
		}
		widths[index] = width
	}
	area.Draw(func(painter *native.Painter, rect native.Rect) {
		for index, width := range widths {
			color := t.TextMuted.Alpha(.38)
			if index == selected || selected < 0 && index == current {
				color = t.Text
			}
			height := min(float32(2), step*.6)
			painter.Fill(native.Rect{X: rect.X + (rect.W-8)/2, Y: rect.Y + start + (float32(index)+.5)*step - height/2, W: width, H: height}, color, height/2)
		}
	})
	if selected < 0 {
		return
	}
	container := a.list.Handle.Bounds(c)
	width := min(float32(340), max(float32(0), container.W-60))
	if width < 100 {
		return
	}
	height := min(float32(172), max(float32(0), container.H-16))
	top := min(max(float32(8), 16+start+(float32(selected)+.5)*step-height/2), max(float32(8), container.H-height-8))
	entry := entries[selected]
	native.Column(c.Key("conversation-preview")).Label("对话预览").Absolute().Left(44).Top(top).
		Width(width).MaxHeight(height).Padding(14).Gap(8).Radius(12).PassThrough().
		Background(t.Background).Border(1, t.Border.Alpha(.8)).Shadow(0, 5, 16, 0, native.RGBA(0, 0, 0, .12)).Clip().Children(func() {
		native.Text(c, entry.title()).FontSize(13).FontWeight(600).LineHeight(1.4).MaxLines(2).FillWidth()
		if entry.reply != nil && strings.TrimSpace(entry.reply.Text) != "" {
			native.Text(c, conversationExcerpt(entry.reply.Text, 260)).FontSize(12).LineHeight(1.5).TextColor(t.TextMuted).MaxLines(4).FillWidth()
		}
	})
}
