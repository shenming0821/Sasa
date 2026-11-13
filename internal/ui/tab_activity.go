package ui

import (
	"math"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
)

func (s *session) tabActivity() (label string, waiting bool) {
	if s == nil || !s.State.Busy || s.State.Disconnected || s.State.Error != "" {
		return "", false
	}
	if s.State.Stopping() {
		return "正在停止", false
	}
	if len(s.State.PendingInteractions()) > 0 {
		return "待确认", true
	}
	if s.State.Phase == "正在发送" {
		return "等待响应", false
	}
	if strings.Contains(s.State.Phase, "正在重试") {
		return "重试中", false
	}
	return "执行中", false
}

func activeTabFace(c *native.Context, row native.Element) {
	tabFace(c, row, true, nil)
}

func tabFace(c *native.Context, row native.Element, selected bool, s *session) {
	theme := c.Theme()
	if selected {
		row.Background(theme.Background)
	}
	label, waiting := s.tabActivity()
	if !selected && label == "" {
		return
	}
	phase := float32(.5)
	if label != "" && !waiting && !c.Preferences().ReduceMotion {
		phase = row.Loop("tab-activity", 1200*time.Millisecond, native.Linear)
	}
	row.DrawOver(func(painter *native.Painter, bounds native.Rect) {
		line := native.Rect{X: bounds.X, Y: bounds.Y + bounds.H - 2, W: bounds.W, H: 2}
		if selected {
			painter.Fill(line, theme.Accent, 0)
		}
		if waiting {
			painter.Fill(line, theme.Warning, 0)
		} else if label != "" {
			if line.W <= 0 {
				return
			}
			width := line.W * .4
			bright := theme.Accent.Mix(native.RGB(255, 255, 255), .8)
			// Wrap at the edge so the band stays visible throughout the cycle.
			// Only the ends fade; the wide, solid centre remains easy to see.
			for x := float32(0); x < line.W; x++ {
				offset := float32(math.Mod(float64(x+.5-phase*line.W+line.W), float64(line.W)))
				if offset >= width {
					continue
				}
				strength := min(float32(1), offset/(width*.15), (width-offset)/(width*.15))
				color := theme.Accent.Alpha(strength)
				if selected {
					color = theme.Accent.Mix(bright, strength)
				}
				painter.Fill(native.Rect{X: line.X + x, Y: line.Y, W: min(1, line.W-x), H: line.H}, color, 0)
			}
		}
	})
}
