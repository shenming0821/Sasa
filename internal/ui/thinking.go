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
		if m.Role == "tool" || m.Role == "interaction" || (m.Role == "assistant" && strings.TrimSpace(m.Text) != "") {
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
	if c.Preferences().ReduceMotion {
		native.Text(c, label).FontSize(15).TextColor(base).AlignSelf(native.Start)
		return
	}
	indicator := native.Box(c).AlignSelf(native.Start)
	phase := indicator.Loop("thinking-shimmer", 1800*time.Millisecond, native.Linear)
	const halfWidth float32 = 18
	width, _ := c.MeasureText(0, native.Span{Text: label, Size: 15})
	center := phase*(width+4*halfWidth) - 2*halfWidth
	var spans []native.Span
	var x float32
	for _, character := range label {
		glyph := string(character)
		advance, _ := c.MeasureText(0, native.Span{Text: glyph, Size: 15})
		distance := float64((x + advance/2 - center) / halfWidth)
		strength := float32(0)
		if math.Abs(distance) < 1 {
			strength = float32((1 + math.Cos(math.Pi*distance)) / 2)
		}
		spans = append(spans, native.Span{Text: glyph, Color: base.Mix(highlight, strength)})
		x += advance
	}
	// One shaped text run keeps the animation on the theme's glyphs and baseline.
	indicator.Children(func() { native.RichText(c, spans...).FontSize(15).TextColor(base) })
}
