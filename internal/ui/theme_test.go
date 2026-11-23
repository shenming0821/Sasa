package ui

import (
	"testing"

	"github.com/alecthomas/chroma/v2"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
)

// dominantPixel samples the frame and returns its most common color:
// the theme's background by area, whatever corner rounding or sidebars
// paint over.
func dominantPixel(tt *native.Tester) native.Color {
	img := tt.Image()
	counts := map[native.Color]int{}
	best, bestN := native.Color{}, -1
	for y := 0; y < img.Bounds().Max.Y; y += 4 {
		for x := 0; x < img.Bounds().Max.X; x += 4 {
			p := img.RGBAAt(x, y)
			c := native.Color{R: p.R, G: p.G, B: p.B, A: p.A}
			counts[c]++
			if counts[c] > bestN {
				best, bestN = c, counts[c]
			}
		}
	}
	return best
}

// TestThemeRegistryCoversBothAppearances checks that every theme has a
// light palette and that default-theme light matches Sasa's
// Codex-style grays, so the palette table is never half-filled.
func TestThemeRegistryCoversBothAppearances(t *testing.T) {
	for _, theme := range themes {
		if theme.light == (palette{}) {
			t.Fatalf("theme %q has no light palette", theme.id)
		}
	}
	if got := themeByID("").light.background; got != native.Hex("#fefefe") {
		t.Fatal("default light background changed", got)
	}
	if themeByID("no-such-theme").id != "" {
		t.Fatal("unknown id does not resolve to default")
	}
}

// settleTheme advances frames until the window's background stops
// changing, because a palette switch blends to its colors over frames.
func settleTheme(tt *native.Tester) native.Color {
	prev := dominantPixel(tt)
	for i := 0; i < 30; i++ {
		tt.Frame()
		cur := dominantPixel(tt)
		if cur == prev {
			return cur
		}
		prev = cur
	}
	return prev
}

// TestThemePickChangesChromeHot checks the settings picker end to end:
// picking Claude repaints the window background with the theme's warm
// neutral within the same appearance, no restart needed.
func TestThemePickChangesChromeHot(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	tt := native.NewTester(a.view, 1000, 760)
	a.openSettings()
	tt.Frame()
	tt.Click("设置分类 外观")
	before := settleTheme(tt)
	for _, choice := range themes[1:] {
		if err := tt.Click("配色 " + choice.label); err != nil {
			t.Fatal(err)
		}
		if a.preferences.ThemeID != choice.id {
			t.Fatal("theme pick not saved", a.preferences.ThemeID)
		}
		after := settleTheme(tt)
		if after == before || after != choice.light.background {
			t.Fatalf("%s window background = %v, want %v", choice.label, after, choice.light.background)
		}
		before = after
	}
	// Switching back repaints too: same appearance, different palette.
	if err := tt.Click("配色 默认"); err != nil {
		t.Fatal(err)
	}
	if settleTheme(tt) != themeByID("").light.background {
		t.Fatal("switching back did not restore the default palette")
	}
}

func TestQoderAndCursorPaletteContrast(t *testing.T) {
	for _, id := range []string{"qoder", "cursor"} {
		for _, appearance := range []struct {
			name string
			base *native.Theme
		}{{"light", native.LightTheme()}, {"dark", native.DarkTheme()}} {
			t.Run(id+"/"+appearance.name, func(t *testing.T) {
				theme := themeByID(id)
				colors := theme.paletteFor(appearance.base.Dark)
				editor := colors.editorFor(appearance.base.Dark)
				if theme.id != id || editor.style.Name != id+"-"+appearance.name || editor.codeColors == nil {
					t.Fatal("theme or syntax palette did not resolve")
				}
				chrome := *appearance.base
				colors.apply(&chrome)
				colors.status.apply(&chrome, chrome.Background, chrome.Surface, chrome.SurfaceHover)
				for _, background := range []native.Color{chrome.Accent, chrome.AccentHover, chrome.AccentPressed} {
					if ratio := contrast(chrome.AccentText, background); ratio < 4.5 {
						t.Errorf("accent label contrast = %.2f", ratio)
					}
				}
				for _, token := range []chroma.TokenType{chroma.Keyword, chroma.LiteralString, chroma.Comment, chroma.GenericInserted, chroma.GenericDeleted} {
					for _, background := range []native.Color{editor.background, editor.selection, editor.addedBackground, editor.deletedBackground} {
						if ratio := contrast(editor.syntaxColor(token), background); ratio < 4.5 {
							t.Errorf("%s syntax contrast = %.2f against %v", token, ratio, background)
						}
					}
				}
			})
		}
	}
}

func TestCaretRepaintsWithThemeAndAppearance(t *testing.T) {
	for _, label := range []string{"任务输入", "搜索设置", "文件内容"} {
		t.Run(label, func(t *testing.T) {
			app := New(stubBackend{}, "test", t.TempDir(), nil)
			buffer := native.NewTextBuffer("")
			var textColor native.Color
			tester := native.NewTester(func(c *native.Context) {
				if label == "任务输入" {
					app.view(c)
					textColor = c.Theme().Text
					return
				}
				app.applyAppearance(c)
				textColor = c.Theme().Text
				if label == "搜索设置" {
					app.settingsSearch(c, false)
					return
				}
				editor := app.activeEditorPalette(c.Theme())
				textColor = editor.text
				editor.area(native.TextAreaBufferBase(c, buffer)).Label(label).Fill().Padding(10)
			}, 1000, 760)
			tester.SetDark(false)
			if err := tester.Click(label); err != nil {
				t.Fatal(err)
			}
			checkCaret := func(want native.Color) {
				t.Helper()
				caret, ok := tester.TextCaret()
				if !ok {
					t.Fatal("focused input lost its caret")
				}
				pixel := tester.Image().RGBAAt(int(caret.X), int(caret.Y+caret.H/2))
				if pixel.R != want.R || pixel.G != want.G || pixel.B != want.B {
					t.Fatalf("caret pixel = %v, want %v", pixel, want)
				}
			}
			checkCaret(textColor)
			app.preferences.ThemeID = "claude"
			tester.Frame()
			checkCaret(native.Hex("#c4613f"))
			tester.SetDark(true)
			checkCaret(native.Hex("#d77757"))
			app.preferences.ThemeID = ""
			tester.Frame()
			checkCaret(textColor)
		})
	}
}

// frameTheme builds one frame of a's view with the given appearance
// and returns the theme applyAppearance set, the chain every widget
// reads.
func frameTheme(a *App, dark bool) native.Theme {
	var got native.Theme
	tt := native.NewTester(func(c *native.Context) {
		a.view(c)
		got = *c.Theme()
	}, 100, 100)
	tt.SetDark(dark)
	tt.Frame()
	return got
}

// TestThemeAccentReachesFrame checks the palette's accent chain —
// accent, selection and focus — lands in the frame theme, so links,
// focus rings and text selection follow the theme, not the system
// blue of MyGo's default.
func TestThemeAccentReachesFrame(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	a.preferences.ThemeID = "claude"
	want := themeByID("claude").light
	if want.accent == native.LightTheme().Accent {
		t.Fatal("test theme shares the default accent; nothing to assert")
	}
	got := frameTheme(a, false)
	if got.Accent != legible(want.status.accent, false, want.background, want.surface, want.surfaceHover) {
		t.Fatal("accent did not follow the theme", got.Accent)
	}
	if got.Selection != want.selection || got.Focus != want.focus {
		t.Fatal("selection or focus did not follow the theme", got.Selection, got.Focus)
	}
	if got.Background != want.background {
		t.Fatal("background did not follow the theme", got.Background)
	}
}

// TestThemeStatusColorsFollowTheme checks the session-status colors the
// badge dot shows resolve from the palette: Claude's working dot is
// terracotta, not the default violet.
func TestThemeStatusColorsFollowTheme(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	a.preferences.ThemeID = "claude"
	got := frameTheme(a, false)
	want := themeByID("claude").light.status
	pal := themeByID("claude").light
	if got.Accent != legible(want.accent, false, pal.background, pal.surface, pal.surfaceHover) {
		t.Fatal("working color does not follow theme", got.Accent)
	}
	if got.Danger != legible(want.danger, false, pal.background, pal.surface, pal.surfaceHover) {
		t.Fatal("danger color does not follow theme", got.Danger)
	}
}

// through the store, so the picked theme survives a restart.
func TestThemeIDPersistsAcrossReload(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	store := imageStore(t, a)
	restored := New(stubBackend{}, "test", "/workspace", nil)
	for _, choice := range themes[1:] {
		a.preferences.ThemeID = choice.id
		if err := a.persist(); err != nil {
			t.Fatal(err)
		}
		if err := restored.AttachStore(store); err != nil {
			t.Fatal(err)
		}
		if restored.preferences.ThemeID != choice.id {
			t.Fatalf("restored %q, want %q", restored.preferences.ThemeID, choice.id)
		}
	}
	// An unknown stored id normalizes to the default theme.
	restored.preferences.ThemeID = "bogus"
	restored.normalizePreferences()
	if restored.preferences.ThemeID != "" {
		t.Fatal("unknown id not normalized", restored.preferences.ThemeID)
	}
}

// TestChatSurfacesFollowTheme checks the bubble and composer colors
// resolve from the palette when it carries them and fall back to the
// old defaults otherwise.
func TestChatSurfacesFollowTheme(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	light := *native.LightTheme()
	dark := *native.DarkTheme()
	if got := a.userBubbleColor(&light); got != native.Hex("#f0f0f1") {
		t.Fatal("default light bubble changed", got)
	}
	a.preferences.ThemeID = "claude"
	if got := a.userBubbleColor(&light); got != themeByID("claude").light.chat.userBubble {
		t.Fatal("bubble does not follow theme", got)
	}
	if got := a.composerBackground(&dark); got != themeByID("claude").dark.chat.composerCard {
		t.Fatal("dark composer does not follow theme", got)
	}
	if got := a.composerBackground(&light); got != themeByID("claude").light.chat.composerCard {
		t.Fatal("light composer does not follow theme", got)
	}
}

func TestClaudeLightCodeColorsRepaintInConversation(t *testing.T) {
	a := New(stubBackend{}, "test", t.TempDir(), nil)
	message := &app.Message{ID: "code", Role: "assistant", Done: true, Text: "```go\npackage sample\n// sample comment\n```"}
	a.md.Theme.FontSize = 22
	tester := native.NewTester(func(c *native.Context) {
		a.applyAppearance(c)
		a.messageMarkdown(c, message)
	}, 500, 160)
	tester.SetDark(false)
	hasColor := func(want native.Color) bool {
		image := tester.Image()
		for y := 0; y < image.Bounds().Dy(); y++ {
			for x := 0; x < image.Bounds().Dx(); x++ {
				pixel := image.RGBAAt(x, y)
				if pixel.R == want.R && pixel.G == want.G && pixel.B == want.B {
					return true
				}
			}
		}
		return false
	}
	palette := themeByID("claude").light.editor
	keyword, comment := palette.syntaxColor(chroma.Keyword), palette.syntaxColor(chroma.Comment)
	if hasColor(keyword) || hasColor(comment) {
		t.Fatal("default Markdown unexpectedly uses Claude syntax colors")
	}
	a.preferences.ThemeID = "claude"
	tester.Frame()
	if !hasColor(keyword) || !hasColor(comment) {
		t.Fatal("existing Markdown code block did not repaint with Claude syntax colors")
	}
	a.find.open, a.find.query = true, "sample"
	tester.Frame()
	if !hasColor(keyword) || !hasColor(comment) {
		t.Fatal("searched Markdown code block lost Claude syntax colors")
	}
	tester.SetDark(true)
	if hasColor(keyword) || hasColor(comment) {
		t.Fatal("Claude dark retained the light syntax colors")
	}
}

func TestClaudeLightTextAndSearchColors(t *testing.T) {
	a := New(stubBackend{}, "test", t.TempDir(), nil)
	a.preferences.ThemeID = "claude"
	theme := frameTheme(a, false)
	if theme.Text != native.Hex("#1a1918") || theme.TextMuted != native.Hex("#66645f") {
		t.Fatal("Claude light did not apply its text colors", theme.Text, theme.TextMuted)
	}
	palette := themeByID("claude").light
	editor := palette.editor
	if ratio := contrast(editor.syntaxColor(chroma.Comment), editor.background); ratio < 4.5 {
		t.Fatalf("editor comment contrast = %.2f, want at least 4.5", ratio)
	}
	search := palette.searchFor(theme.Accent, .55)
	if search.match != palette.term.Search || search.current != palette.term.SearchSelected {
		t.Fatal("conversation and terminal search colors differ")
	}
	got, want := search.overlay().Over(search.match.Over(theme.Background)), search.current.Over(theme.Background)
	if max(int(got.R)-int(want.R), int(want.R)-int(got.R),
		int(got.G)-int(want.G), int(want.G)-int(got.G),
		int(got.B)-int(want.B), int(want.B)-int(got.B)) > 1 {
		t.Fatalf("Markdown search overlay = %v, want %v", got, want)
	}
}

// TestThemeDarkFollowsAppearance checks the dark palette paints when
// the appearance says dark: picking Claude repaints the window with
// Claude's warm dark ground within the dark appearance.
func TestThemeDarkFollowsAppearance(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	tt := native.NewTester(a.view, 1000, 760)
	a.openSettings()
	tt.Frame()
	tt.Click("设置分类 外观")
	tt.Click("主题 深色")
	tt.Frame()
	if !tt.HasText("配色") {
		t.Fatal("theme picker missing")
	}
	// The default dark palette is all zero, so the pixel stays whatever
	// MyGo's dark theme painted; the switch to Claude is asserted after
	// the blend settles.
	defaultDark := settleTheme(tt)
	if err := tt.Click("配色 Claude"); err != nil {
		t.Fatal(err)
	}
	got := settleTheme(tt)
	if got == defaultDark {
		t.Fatal("dark theme pick did not repaint the window")
	}
	if got != themeByID("claude").dark.background {
		t.Fatal("Claude dark background not painted", got)
	}
}
