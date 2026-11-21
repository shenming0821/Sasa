package ui

import (
	native "github.com/egoist/mygo/ui"
)

// palette holds the colors one appearance of a theme paints the app
// with. The chrome fields project onto native.Theme in
// applyAppearance; the chat fields carry the app-specific surfaces the
// conversation view needs, kept apart because they sit on top of the
// conversation floor. A zero color means "keep the frame theme's
// value", so a palette lists only what it changes: the default theme's
// dark palette is entirely zero and leaves MyGo's dark theme alone,
// the way the appearance settings did before themes existed.
type palette struct {
	background     native.Color
	surface        native.Color
	surfaceHover   native.Color
	surfacePressed native.Color
	border         native.Color
	chat           paletteChat
}

// paletteChat holds the chat-only surfaces: the user's message bubble
// and the composer card.
type paletteChat struct {
	userBubble   native.Color
	composerCard native.Color
}

// themeDefinition is one named color scheme: the palettes its light
// and dark appearances resolve to. The entry with the empty ID is the
// default theme; unknown IDs resolve to it.
type themeDefinition struct {
	id    string
	label string
	light palette
	dark  palette
}

// themes lists the built-in themes in picker order. The first entry is
// the default: the light appearance keeps Sasa's Codex-style grays and
// the dark appearance follows MyGo's dark theme untouched. Claude
// mirrors the same structure in Anthropic's warm neutrals, so the
// whole chain — chrome, bubble, composer — switches with one setting.
var themes = []themeDefinition{
	{
		id:    "",
		label: "默认",
		light: palette{
			background:     native.Hex("#fefefe"),
			surface:        native.Hex("#fafafa"),
			surfaceHover:   native.Hex("#efefef"),
			surfacePressed: native.Hex("#e4e4e4"),
			border:         native.Hex("#e4e4e4"),
			chat:           paletteChat{userBubble: native.Hex("#f0f0f1"), composerCard: native.Hex("#ffffff")},
		},
	},
	{
		id:    "claude",
		label: "Claude",
		light: palette{
			background:     native.Hex("#f9f5ef"),
			surface:        native.Hex("#f1ebdf"),
			surfaceHover:   native.Hex("#e9e1d2"),
			surfacePressed: native.Hex("#e0d5c1"),
			border:         native.Hex("#e0d5c1"),
			chat:           paletteChat{userBubble: native.Hex("#f1ebdf"), composerCard: native.Hex("#fdfcf9")},
		},
		dark: palette{
			background:     native.Hex("#262624"),
			surface:        native.Hex("#30302e"),
			surfaceHover:   native.Hex("#3a3937"),
			surfacePressed: native.Hex("#444240"),
			border:         native.Hex("#444240"),
			chat:           paletteChat{userBubble: native.Hex("#30302e"), composerCard: native.Hex("#262624")},
		},
	},
}

// themeByID returns the definition of the theme with the given ID, or
// the default theme when the ID is unknown.
func themeByID(id string) themeDefinition {
	for _, t := range themes {
		if t.id == id {
			return t
		}
	}
	return themes[0]
}

// themeLabels lists the themes for the settings picker: the ID to
// store and the label to show.
func themeLabels() [][2]string {
	list := make([][2]string, 0, len(themes))
	for _, t := range themes {
		list = append(list, [2]string{t.id, t.label})
	}
	return list
}

// paletteFor resolves the palette of the configured theme for the
// given appearance.
func (d themeDefinition) paletteFor(dark bool) palette {
	if dark {
		return d.dark
	}
	return d.light
}

// apply writes the palette's chrome colors over the theme. Zero
// colors leave the theme's value alone.
func (p palette) apply(t *native.Theme) {
	set := func(dst *native.Color, v native.Color) {
		if v != (native.Color{}) {
			*dst = v
		}
	}
	set(&t.Background, p.background)
	set(&t.Surface, p.surface)
	set(&t.SurfaceHover, p.surfaceHover)
	set(&t.SurfacePressed, p.surfacePressed)
	set(&t.Border, p.border)
}
