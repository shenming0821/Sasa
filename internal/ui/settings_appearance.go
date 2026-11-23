package ui

import (
	"slices"

	"github.com/alecthomas/chroma/v2"
	native "github.com/egoist/mygo/ui"
)

func (a *App) themeCards(c *native.Context) {
	theme := c.Theme()
	current := a.preferences.ThemeID
	selection := max(0, slices.IndexFunc(themes, func(choice themeDefinition) bool { return choice.id == current }))
	cards := native.SegmentedBase(c.Key("palettes"), &selection, len(themes))
	cards.Track.Label("配色").FillWidth().Wrap().Gap(12).Children(func() {
		for index, choice := range themes {
			selected := choice.id == current
			border := theme.Border
			weight := 400
			if selected {
				border = theme.Accent
				weight = 600
			}
			tile := cards.Segment(index).Column().Label("配色 " + choice.label).Width(122).Shrink(0).
				AlignItems(native.Center).Padding(3).Gap(7).Radius(8)
			settingsButtonBackground(tile, theme, native.Color{})
			tile.Children(func() {
				native.Box(c).FillWidth().Height(72).Radius(9).Border(2, border).Children(func() {
					themeSample(c, choice.paletteFor(theme.Dark), theme.Dark)
					if selected {
						native.Box(c).Absolute().Top(-4).Right(-4).Size(18, 18).Radius(9).
							Background(theme.Accent).Border(1, theme.Surface).
							AlignItems(native.Center).Justify(native.Center).Children(func() {
							native.Icon(c, checkIcon).Size(11, 11).TextColor(theme.AccentText)
						})
					}
				})
				native.Text(c, choice.label).FontSize(12).FontWeight(weight).SingleLine()
			})
		}
	})
	if id := themes[selection].id; id != current {
		previous := a.preferences
		a.preferences.ThemeID = id
		a.savePreferences(previous)
		c.Invalidate()
	}
}

func themeSample(c *native.Context, colors palette, dark bool) {
	theme := *native.LightTheme()
	if dark {
		theme = *native.DarkTheme()
	}
	colors.apply(&theme)
	editor := colors.editorFor(dark)
	syntax := editor.ranges([]syntaxToken{
		{Start: 0, End: 1, Type: chroma.Keyword},
		{Start: 1, End: 2, Type: chroma.LiteralString},
	}, 0, 2)
	native.Box(c).Absolute().Top(2).Right(2).Bottom(2).Left(2).Radius(6).Clip().Draw(func(p *native.Painter, bounds native.Rect) {
		p.Fill(bounds, editor.background, 6)
		p.Fill(native.Rect{X: bounds.X + 3, Y: bounds.Y + 3, W: 21, H: bounds.H - 6}, theme.Surface, 4)
		p.Fill(native.Rect{X: bounds.X + 7, Y: bounds.Y + 11, W: 11, H: 3}, editor.accent, 1.5)
		p.Fill(native.Rect{X: bounds.X + 7, Y: bounds.Y + 18, W: 8, H: 3}, editor.muted, 1.5)
		for index, width := range []float32{19, 28, 16, 22} {
			y := bounds.Y + 13 + float32(index)*10
			x := bounds.X + 31
			col := []native.Color{syntax[0].Color, syntax[1].Color, editor.text, editor.muted}[index]
			p.Fill(native.Rect{X: x, Y: y, W: width, H: 4}, col, 2)
			if index < 3 {
				p.Fill(native.Rect{X: x + width + 4, Y: y, W: 15, H: 4}, syntax[(index+1)%2].Color, 2)
			}
		}
		p.Fill(native.Rect{X: bounds.X + 69, Y: bounds.Y + 31, W: 2, H: 8}, editor.accent, 1)
	})
}

func (a *App) appearanceMode(c *native.Context) {
	choices := []struct{ id, label string }{{"system", "跟随系统"}, {"light", "浅色"}, {"dark", "深色"}}
	current := a.preferences.Theme
	selection := max(0, slices.IndexFunc(choices, func(choice struct{ id, label string }) bool { return choice.id == current }))
	native.Row(c).FillWidth().AlignItems(native.Center).Gap(12).Children(func() {
		native.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			settingsFieldLabel(c, "明暗模式", "跟随 macOS，或固定使用浅色、深色。")
		})
		theme := c.Theme()
		modes := native.SegmentedBase(c.Key("appearance-mode"), &selection, len(choices))
		modes.Track.Label("明暗模式").Shrink(0).Padding(2).Gap(2).Radius(8).
			Background(theme.Text.Alpha(.05)).Border(1, theme.Border).Children(func() {
			for index, choice := range choices {
				segment := modes.Segment(index).Label("主题 "+choice.label).Height(28).Padding(0, 9).
					Radius(5).TextColor(theme.TextMuted)
				settingsSelectionFace(segment, theme, index == selection)
				segment.Children(func() { native.Text(c, choice.label).FontSize(12).SingleLine() })
			}
		})
	})
	if id := choices[selection].id; id != current {
		previous := a.preferences
		a.preferences.Theme = id
		a.savePreferences(previous)
		c.Invalidate()
	}
}

func settingsSelectionFace(button native.Element, theme *native.Theme, selected bool) {
	base := native.Color{}
	if selected {
		base = theme.Background
		if theme.Dark {
			base = theme.SurfacePressed
		}
		button.TextColor(theme.Text).Border(1, theme.Border)
	}
	settingsButtonBackground(button, theme, base)
}

func settingsButtonBackground(button native.Element, theme *native.Theme, base native.Color) {
	switch {
	case button.Pressed():
		base = theme.SurfacePressed
	case button.Hovered():
		base = theme.SurfaceHover
	}
	button.Background(base)
}
