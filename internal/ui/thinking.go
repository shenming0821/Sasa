package ui

import (
	"math"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
)

type thinkingKey struct{ sequence int }

// Only the current request's visible output ends the initial waiting indicator.
func (a *App) thinking() bool {
	if !a.State.Busy || a.State.Disconnected || a.State.Error != "" ||
		a.State.Phase == "正在停止" || a.State.Phase == "停止待确认" {
		return false
	}
	for i := len(a.State.Messages) - 1; i >= 0; i-- {
		m := a.State.Messages[i]
		if m.Role == "user" {
			return true
		}
		if m.Role == "tool" || (m.Role == "assistant" && strings.TrimSpace(m.Text) != "") {
			return false
		}
	}
	return false
}

func thinkingIndicator(c *native.Context) {
	const label = "正在思考"
	t := c.Theme()
	base := t.Background.Mix(t.TextMuted, 0.65)
	highlight := t.Background.Mix(t.TextMuted, 0.82)
	text := native.Text(c, label).FontSize(15).TextColor(base).AlignSelf(native.Start)
	if c.Preferences().ReduceMotion {
		return
	}
	phase := text.Loop("thinking-shimmer", 1800*time.Millisecond, native.Linear)
	text.DrawOver(func(p *native.Painter, r native.Rect) {
		// Paint a narrow moving band through the glyphs, leaving the background clear.
		const halfWidth float32 = 18
		center := phase*(r.W+4*halfWidth) - 2*halfWidth
		for x := float32(0); x < r.W; x++ {
			distance := float64((x + 0.5 - center) / halfWidth)
			if math.Abs(distance) >= 1 {
				continue
			}
			strength := float32((1 + math.Cos(math.Pi*distance)) / 2)
			color := base.Mix(highlight, strength)
			p.Clip(native.Rect{X: r.X + x, Y: r.Y, W: 1, H: r.H}, 0, func() {
				p.Text(r.X, r.Y, label, 15, color)
			})
		}
	})
}
