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
		native.Column(c).FillWidth().Gap(10).Children(func() {
			native.Text(c, text).FontSize(13).TextColor(c.Theme().TextMuted).SingleLine()
			native.Box(c).FillWidth().Height(1).Background(c.Theme().Border)
		})
	}
}

func (a *App) conversationTimings(rows []conversationRow) map[int]*app.TurnTiming {
	timings := make(map[int]*app.TurnTiming)
	current := -1
	for index, row := range rows {
		if row.message.Role == "user" {
			current = index
			if timing := row.message.Timing; timing != nil && !timing.Finished.IsZero() {
				timings[index] = timing
			}
		}
	}
	if a.State.Busy && current >= 0 {
		if timing := rows[current].message.Timing; timing != nil && timing.Finished.IsZero() {
			timings[current] = timing
		}
	}
	return timings
}
