package ui

import (
	"fmt"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
)

func timingText(timing *app.TurnTiming, now time.Time) string {
	if timing == nil || timing.Started.IsZero() {
		return ""
	}
	prefix, end := "已处理 ", now
	if !timing.Finished.IsZero() {
		prefix, end = "用时 ", timing.Finished
	}
	seconds := max(0, int(end.Sub(timing.Started)/time.Second))
	if seconds >= 3600 {
		return fmt.Sprintf("%s%d小时 %d分钟 %d秒", prefix, seconds/3600, seconds/60%60, seconds%60)
	}
	if seconds >= 60 {
		return fmt.Sprintf("%s%d分钟 %d秒", prefix, seconds/60, seconds%60)
	}
	return fmt.Sprintf("%s%d秒", prefix, seconds)
}

func turnTiming(c *native.Context, timing *app.TurnTiming) {
	if text := timingText(timing, time.Now()); text != "" {
		native.Column(c).FillWidth().Gap(10).Padding(0, 0, 12, 0).Children(func() {
			native.Text(c, text).FontSize(13).TextColor(c.Theme().TextMuted).SingleLine()
			native.Box(c).FillWidth().Height(1).Background(c.Theme().Border)
		})
	}
}

func (a *App) conversationTimings(rows []conversationRow) (map[int]*app.TurnTiming, *app.TurnTiming) {
	completed := make(map[int]*app.TurnTiming)
	var current *app.TurnTiming
	last, reply := -1, -1
	flush := func() {
		if current != nil && !current.Finished.IsZero() {
			if reply >= 0 {
				completed[reply] = current
			} else if last >= 0 {
				completed[last] = current
			}
		}
	}
	for index, row := range rows {
		if row.message.Role == "user" {
			flush()
			current, last, reply = row.message.Timing, index, -1
		} else {
			last = index
			if row.message.Role == "assistant" {
				reply = index
			}
		}
	}
	flush()
	if !a.State.Busy || current == nil || !current.Finished.IsZero() {
		current = nil
	}
	return completed, current
}
