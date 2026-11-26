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
	shimmerText(c, "正在思考", 15).AlignSelf(native.Start)
}

// shimmerText gives running process labels the same moving highlight as the
// waiting indicator; reduce motion keeps the plain muted text.
func shimmerText(c *native.Context, label string, size float32) native.Element {
	t := c.Theme()
	base, highlight := shimmerColors(t)
	if c.Preferences().ReduceMotion {
		return native.Text(c, label).FontSize(size).TextColor(base)
	}
	indicator := native.Box(c)
	phase := indicator.Loop("shimmer", 1800*time.Millisecond, native.Linear)
	const halfWidth float32 = 18
	width, _ := c.MeasureText(0, native.Span{Text: label, Size: size})
	center := phase*(width+4*halfWidth) - 2*halfWidth
	var spans []native.Span
	var x float32
	for _, character := range label {
		glyph := string(character)
		advance, _ := c.MeasureText(0, native.Span{Text: glyph, Size: size})
		distance := float64((x + advance/2 - center) / halfWidth)
		strength := float32(0)
		if math.Abs(distance) < 1 {
			strength = float32((1 + math.Cos(math.Pi*distance)) / 2)
		}
		spans = append(spans, native.Span{Text: glyph, Color: base.Mix(highlight, strength)})
		x += advance
	}
	// One shaped text run keeps the animation on the theme's glyphs and baseline.
	indicator.Children(func() { native.RichText(c, spans...).FontSize(size).TextColor(base) })
	return indicator
}

func shimmerColors(t *native.Theme) (base, highlight native.Color) {
	return t.Background.Mix(t.TextMuted, 0.78), t.Background.Mix(t.Text, 0.9)
}
