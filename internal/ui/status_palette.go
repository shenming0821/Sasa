package ui

import (
	"math"

	native "github.com/egoist/mygo/ui"
)

// statusPalette holds the colors that say what a session is doing,
// mirrored from Mayoi's palette for a dark or light window: the accent
// for working, the modified color for waiting on the user, the danger
// color for failure, and the added color kept for callers that need a
// success. These are the colors as text, already adjusted by legible
// toward black or white until they read at 4.5:1 on the surfaces the
// badge can sit on.
type statusPalette struct {
	accent   native.Color
	modified native.Color
	danger   native.Color
	added    native.Color
}

// statusPaletteFor returns the palette of the current appearance,
// mirrored from Mayoi's default dark and light themes. As more of
// Mayoi's themes land, this grows to pick among them.
func statusPaletteFor(dark bool) statusPalette {
	if dark {
		return statusPalette{
			accent:   native.Hex("#8b7cf6"),
			modified: native.Hex("#e8b86d"),
			danger:   native.Hex("#f0718b"),
			added:    native.Hex("#4fa86d"),
		}
	}
	return statusPalette{
		accent:   native.Hex("#6d5bd0"),
		modified: native.Hex("#c4862a"),
		danger:   native.Hex("#d0365a"),
		added:    native.Hex("#3fa363"),
	}
}

// applyStatusTheme writes the palette into the theme's status slots, the
// same way Mayoi's applyTheme does: accent is what a working session
// shows, warning is what waits for the user, danger is what failed, and
// success is kept for parallel use. Each color is run through legible on
// the surfaces the badge is likely drawn on so it reads at 4.5:1.
func applyStatusTheme(t *native.Theme, backs ...native.Color) {
	p := statusPaletteFor(t.Dark)
	t.Accent = legible(p.accent, t.Dark, backs...)
	t.Warning = legible(p.modified, t.Dark, backs...)
	t.Danger = legible(p.danger, t.Dark, backs...)
	t.Success = legible(p.added, t.Dark, backs...)
}

// legible returns col as text that reads at 4.5:1, WCAG's AA, on each of
// the backgrounds: col itself when it does, or else col mixed toward
// black on a light window, or white on a dark one, no further than it
// must. Mirrors Mayoi's legible.
func legible(col native.Color, dark bool, backs ...native.Color) native.Color {
	to := native.Color{R: 0, G: 0, B: 0, A: 255}
	if dark {
		to = native.Color{R: 255, G: 255, B: 255, A: 255}
	}
	reads := func(c native.Color) bool {
		for _, b := range backs {
			if contrast(c, b) < 4.5 {
				return false
			}
		}
		return true
	}
	for i := 0; i <= 40; i++ {
		if c := col.Mix(to, float32(i)/40); reads(c) || i == 40 {
			return c
		}
	}
	return col
}

// contrast is the WCAG contrast ratio of two opaque colors. Mirrors
// Mayoi's contrast.
func contrast(x, y native.Color) float64 {
	lum := func(c native.Color) float64 {
		ch := func(v uint8) float64 {
			f := float64(v) / 255
			if f <= 0.03928 {
				return f / 12.92
			}
			return math.Pow((f+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
	}
	a, b := lum(x)+0.05, lum(y)+0.05
	return math.Max(a, b) / math.Min(a, b)
}
