package ui

import (
	"cmp"
	"time"

	native "github.com/egoist/mygo/ui"
)

// Paint the face independently of the hit target; disabled can be set by callers
// after construction, so check it again when painting.
func buttonFace(c *native.Context, b native.Element, base, hover, pressed native.Color, radius, inset float32) native.Element {
	target := base
	if b.Pressed() {
		target = pressed
	} else if b.Hovered() {
		target = hover
	}
	shown := target
	if !c.Preferences().ReduceMotion {
		shown = b.AnimateColor("button-face", target, native.Animation{Duration: 120 * time.Millisecond})
	}
	return b.Draw(func(p *native.Painter, r native.Rect) {
		color := shown
		if b.IsDisabled() {
			color = base
		}
		r.X, r.Y = r.X+inset, r.Y+inset
		r.W, r.H = r.W-2*inset, r.H-2*inset
		p.Fill(r, color, radius)
	})
}

func quietButton(c *native.Context, label string) native.Element {
	t := c.Theme()
	b := native.ButtonBase(c).Label(label).Tooltip(label).Radius(8).Padding(6).TextColor(t.TextMuted)
	return buttonFace(c, b, native.Color{}, t.SurfaceHover, t.SurfacePressed, 8, 0)
}

func quietIconButton(c *native.Context, label string) native.Element {
	return quietButton(c, label).Padding(0).AlignItems(native.Center).Justify(native.Center)
}

func outlinedButton(c *native.Context, label string) native.Element {
	return selectableActionButton(c, label, false)
}

func selectableActionButton(c *native.Context, label string, selected bool) native.Element {
	t := c.Theme()
	b := native.ButtonBase(c).Label(label).Tooltip(label).Radius(8).Clip().
		Border(1, t.Border).TextColor(t.Text)
	if selected {
		return selectedButtonFace(c, b, true, 8, 0)
	}
	return buttonFace(c, b, t.Background, t.SurfaceHover, t.SurfacePressed, 8, 0)
}

func selectedButtonFace(c *native.Context, b native.Element, selected bool, radius, inset float32) native.Element {
	t := c.Theme()
	if !selected {
		return buttonFace(c, b, native.Color{}, t.SurfaceHover, t.SurfacePressed, radius, inset)
	}
	b.TextColor(t.Text)
	return buttonFace(c, b, t.SurfacePressed, t.SurfacePressed.Mix(t.Text, .04),
		t.SurfacePressed.Mix(t.Text, .08), radius, inset)
}

func panelTabButton(c *native.Context, label string, selected bool) native.Element {
	b := native.ButtonBase(c).Label(label).Tooltip(label).Size(36, 36).
		Radius(7).Padding(0).TextColor(c.Theme().TextMuted)
	return selectedButtonFace(c, b, selected, 7, 3)
}

func (a *App) pickerRowFace(theme *native.Theme, row native.Element, selected bool) {
	palette := a.activePalette(theme)
	if selected {
		row.Background(cmp.Or(palette.pickerSelected, theme.SurfaceHover))
	} else if row.Hovered() {
		row.Background(cmp.Or(palette.pickerHover, theme.SurfaceHover))
	}
}

func (a *App) listRowHover(theme *native.Theme) native.Color {
	return cmp.Or(a.activePalette(theme).listRowHover, theme.SurfaceHover)
}

func (a *App) emphasizedRowButton(c *native.Context, label string) native.Element {
	button := treeButton(c, label)
	if button.Hovered() || button.FocusVisible() {
		if hover := a.activePalette(c.Theme()).actionRowHover; hover != (native.Color{}) {
			button.Background(hover)
		}
	}
	return button
}
