package ui

import (
	"testing"

	native "github.com/egoist/mygo/ui"
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
	if err := tt.Click("配色 Claude"); err != nil {
		t.Fatal(err)
	}
	if a.preferences.ThemeID != "claude" {
		t.Fatal("theme pick not saved", a.preferences.ThemeID)
	}
	after := settleTheme(tt)
	if after == before {
		t.Fatal("picking a theme did not repaint the window")
	}
	if after != themeByID("claude").light.background {
		t.Fatal("window background is not the Claude palette's", after)
	}
	// Switching back repaints too: same appearance, different palette.
	if err := tt.Click("配色 默认"); err != nil {
		t.Fatal(err)
	}
	if settleTheme(tt) != themeByID("").light.background {
		t.Fatal("switching back did not restore the default palette")
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
	a.preferences.ThemeID = "claude"
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "/workspace", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.preferences.ThemeID != "claude" {
		t.Fatal("theme id not restored", restored.preferences.ThemeID)
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
