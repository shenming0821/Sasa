package ui

import (
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/terminal"
)

// palette holds the colors one appearance of a theme paints the app
// with. The chrome fields project onto native.Theme in
// applyAppearance; the chat fields carry the app-specific surfaces the
// conversation view needs, kept apart because they sit on top of the
// conversation floor. A zero color means "keep the frame theme's
// value", so a palette lists only what it changes: the default theme's
// dark palette is entirely zero except status and leaves MyGo's dark
// theme alone, the way the appearance settings did before themes
// existed.
type palette struct {
	background     native.Color
	surface        native.Color
	surfaceHover   native.Color
	surfacePressed native.Color
	border         native.Color
	text           native.Color
	textMuted      native.Color
	accent         native.Color
	accentHover    native.Color
	accentPressed  native.Color
	selection      native.Color
	focus          native.Color
	search         searchPalette
	pickerHover    native.Color
	pickerSelected native.Color
	listRowHover   native.Color
	// status holds the session-state colors applyStatusTheme writes:
	// the accent for a working session, the modified color for waiting
	// on the user, danger for failure, and added for success.
	status statusPalette
	chat   paletteChat
	editor editorPalette
	term   terminal.Theme
}

// paletteChat holds the chat-only surfaces: the user's message bubble
// and the composer card.
type paletteChat struct {
	userBubble   native.Color
	composerCard native.Color
}

type searchPalette struct {
	match, current, ring native.Color
}

var claudeLightSearch = searchPalette{
	match:   native.RGBA(212, 162, 64, 0.24),
	current: native.RGBA(212, 162, 64, 0.44),
	ring:    native.RGBA(150, 108, 30, 0.9),
}

func (p palette) searchFor(accent native.Color, currentAlpha float32) searchPalette {
	if p.search != (searchPalette{}) {
		return p.search
	}
	return searchPalette{match: accent.Alpha(.25), current: accent.Alpha(currentAlpha), ring: accent.Alpha(.9)}
}

func (p searchPalette) overlay() native.Color {
	color := p.current
	color.A = 255
	return color.Alpha(float32(p.current.A-p.match.A) / float32(255-p.match.A))
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
// the default: the light appearance keeps Sasa's Codex-style grays
// with the mirrored Mayoi status colors, and the dark appearance
// follows MyGo's dark theme untouched. Claude mirrors the same
// structure in Anthropic's own neutrals — oat and cream grounds, the
// terracotta book-cloth accent — so the whole chain, chrome, accent,
// selection and chat surfaces, switches with one setting.
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
			status:         statusPaletteFor(false),
			chat:           paletteChat{userBubble: native.Hex("#f0f0f1"), composerCard: native.Hex("#ffffff")},
		},
		dark: palette{
			status: statusPaletteFor(true),
		},
	},
	{
		id:    "claude",
		label: "Claude",
		light: palette{
			// Mirrors Mayoi's claudeLightPalette: off-white grounds,
			// Claude's orange accent with near-black on it, and the
			// CLI's blue-violet for what is chosen.
			background:     native.Hex("#f5f4f1"),
			surface:        native.Hex("#f9f8f6"),
			surfaceHover:   native.Hex("#f4f3ef"),
			surfacePressed: native.Hex("#ebe9e4"),
			border:         native.Hex("#dcdad4"),
			text:           native.Hex("#1a1918"),
			textMuted:      native.Hex("#66645f"),
			accent:         native.Hex("#c4613f"),
			accentHover:    native.Hex("#b65a3c"),
			accentPressed:  native.Hex("#a04f31"),
			selection:      native.RGBA(87, 105, 247, 0.16),
			focus:          native.RGBA(196, 97, 63, 0.5),
			search:         claudeLightSearch,
			pickerHover:    native.Hex("#eeebe5"),
			pickerSelected: native.Hex("#e2e4f8"),
			listRowHover:   native.Hex("#f0ede7"),
			status:         statusPalette{accent: native.Hex("#c4613f"), modified: native.Hex("#966c1e"), danger: native.Hex("#ab2b3f"), added: native.Hex("#2c7a39")},
			chat:           paletteChat{userBubble: native.Hex("#eeebe5"), composerCard: native.Hex("#fdfcfa")},
			editor:         claudeEditorPalette(false),
			term: terminal.Theme{
				Background: native.Hex("#f9f8f6"), Foreground: native.Hex("#1a1918"), Cursor: native.Hex("#c4613f"),
				Selection: native.RGBA(87, 105, 247, 0.16),
				Search:    claudeLightSearch.match, SearchSelected: claudeLightSearch.current,
				ANSI: [16]uint32{
					0x1a1918, 0xab2b3f, 0x2c7a39, 0x966c1e, 0x3a63c8, 0xc8207c, 0x0f6e6e, 0x86847e,
					0x66645f, 0xc8374f, 0x3c9a4c, 0xb08428, 0x5769f7, 0xe0409a, 0x1f8a8a, 0x4a4843,
				},
			},
		},
		dark: palette{
			// Mirrors Mayoi's claudeDarkPalette: warm near-black, the
			// orange for the accent and periwinkle for what is chosen,
			// rose for errors and what is removed.
			background:     native.Hex("#141312"),
			surface:        native.Hex("#201f1d"),
			surfaceHover:   native.Hex("#272523"),
			surfacePressed: native.Hex("#2e2c29"),
			border:         native.Hex("#34322f"),
			accent:         native.Hex("#d77757"),
			accentHover:    native.Hex("#e08a67"),
			accentPressed:  native.Hex("#c05f3c"),
			selection:      native.RGBA(177, 185, 247, 0.24),
			focus:          native.RGBA(215, 119, 87, 0.6),
			status:         statusPalette{accent: native.Hex("#d77757"), modified: native.Hex("#f2c14e"), danger: native.Hex("#ff6b80"), added: native.Hex("#4eba65")},
			chat:           paletteChat{userBubble: native.Hex("#201f1d"), composerCard: native.Hex("#171615")},
			editor:         claudeEditorPalette(true),
			term: terminal.Theme{
				Background: native.Hex("#171615"), Foreground: native.Hex("#ecebe6"), Cursor: native.Hex("#d77757"),
				Selection: native.RGBA(177, 185, 249, 0.24),
				Search:    native.RGBA(242, 193, 78, 0.20), SearchSelected: native.RGBA(242, 193, 78, 0.38),
				ANSI: [16]uint32{
					0x34322f, 0xff6b80, 0x4eba65, 0xf2c14e, 0x6a9fe0, 0xfd5db1, 0x5fb3a6, 0xc9c8c3,
					0x62605b, 0xff8a9b, 0x6fd685, 0xffd451, 0x8db8f0, 0xff85c6, 0x7fcfc2, 0xffffff,
				},
			},
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
	set(&t.Text, p.text)
	set(&t.TextMuted, p.textMuted)
	set(&t.Accent, p.accent)
	set(&t.AccentHover, p.accentHover)
	set(&t.AccentPressed, p.accentPressed)
	set(&t.Selection, p.selection)
	set(&t.Focus, p.focus)
	t.Caret = p.accent
}

// apply writes the palette's status colors into the theme's status
// slots the same way applyStatusTheme does, adjusting each through
// legible on the backs the badge sits on.
func (p statusPalette) apply(t *native.Theme, backs ...native.Color) {
	t.Accent = legible(p.accent, t.Dark, backs...)
	t.Warning = legible(p.modified, t.Dark, backs...)
	t.Danger = legible(p.danger, t.Dark, backs...)
	t.Success = legible(p.added, t.Dark, backs...)
}
