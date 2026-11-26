package ui

import (
	"slices"
	"strings"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/power"
	"github.com/shenming0821/Sasa/internal/storage"
	"github.com/shenming0821/Sasa/internal/terminal"
)

type powerSettings interface {
	Set(string) error
	CurrentState() (power.State, error)
	RestoreState(power.State) error
	Sync() (string, error)
	Err() error
	PendingBaseline() (power.Baseline, bool)
	ConfirmBaseline()
	RestoreBaseline(power.Baseline)
	Close() error
}

type settingsState struct {
	powerController    powerSettings
	awakeSaving        bool
	awakePicker        bool
	terminalPicker     bool
	preferences        storage.Preferences
	settingsOpen       bool
	settingsWindow     *mygo.Window
	settingsSection    string
	settingsQuery      string
	settingsError      string
	fontFamilies       []string
	fontPicker         string
	fontSearch         string
	notifyError        string
	notificationSender func(mygo.NotificationOptions) error
	windowFocused      func() bool
}

var settingsIcon = icon(`<path d="M9.59356 3.94014C9.68397 3.39768 10.1533 3.00009 10.7033 3.00009H13.2972C13.8472 3.00009 14.3165 3.39768 14.4069 3.94014L14.6204 5.22119C14.6828 5.59523 14.9327 5.9068 15.2645 6.09045C15.3387 6.13151 15.412 6.17393 15.4844 6.21766C15.8095 6.41393 16.2048 6.47495 16.5604 6.34175L17.7772 5.88587C18.2922 5.69293 18.8712 5.9006 19.1462 6.37687L20.4432 8.6233C20.7181 9.09957 20.6085 9.70482 20.1839 10.0544L19.1795 10.8812C18.887 11.122 18.742 11.4938 18.7491 11.8726C18.7498 11.915 18.7502 11.9575 18.7502 12.0001C18.7502 12.0427 18.7498 12.0852 18.7491 12.1275C18.742 12.5064 18.887 12.8782 19.1795 13.119L20.1839 13.9458C20.6085 14.2953 20.7181 14.9006 20.4432 15.3769L19.1462 17.6233C18.8712 18.0996 18.2922 18.3072 17.7772 18.1143L16.5604 17.6584C16.2048 17.5252 15.8095 17.5862 15.4844 17.7825C15.412 17.8263 15.3387 17.8687 15.2645 17.9097C14.9327 18.0934 14.6828 18.4049 14.6204 18.779L14.4069 20.06C14.3165 20.6025 13.8472 21.0001 13.2972 21.0001H10.7033C10.1533 21.0001 9.68397 20.6025 9.59356 20.06L9.38005 18.779C9.31771 18.4049 9.06774 18.0934 8.73597 17.9097C8.66179 17.8687 8.58847 17.8263 8.51604 17.7825C8.19101 17.5863 7.79568 17.5252 7.44011 17.6584L6.22325 18.1143C5.70826 18.3072 5.12926 18.0996 4.85429 17.6233L3.55731 15.3769C3.28234 14.9006 3.39199 14.2954 3.81657 13.9458L4.82092 13.119C5.11343 12.8782 5.25843 12.5064 5.25141 12.1276C5.25063 12.0852 5.25023 12.0427 5.25023 12.0001C5.25023 11.9575 5.25063 11.915 5.25141 11.8726C5.25843 11.4938 5.11343 11.122 4.82092 10.8812L3.81657 10.0544C3.39199 9.70484 3.28234 9.09958 3.55731 8.62332L4.85429 6.37688C5.12926 5.90061 5.70825 5.69295 6.22325 5.88588L7.4401 6.34176C7.79566 6.47496 8.19099 6.41394 8.51603 6.21767C8.58846 6.17393 8.66179 6.13151 8.73597 6.09045C9.06774 5.9068 9.31771 5.59523 9.38005 5.22119L9.59356 3.94014Z"/><path d="M15 12C15 13.6569 13.6569 15 12 15C10.3431 15 9 13.6569 9 12C9 10.3432 10.3431 9.00001 12 9.00001C13.6569 9.00001 15 10.3432 15 12Z"/>`)

// defaultPreferences sets the theme the app ships with: the default
// palette and the appearance following the system.
func defaultPreferences() storage.Preferences {
	return storage.Preferences{DiffViewMode: diffViewModeInline, KeepAwake: power.Never, Terminal: terminal.Ghostty, Theme: "system", EnglishFont: latinFont, ChineseFont: ".PingFang UI SC", CodeFont: latinFont, CompletionNotification: true}
}

func (a *App) normalizePreferences() {
	p := &a.preferences
	d := defaultPreferences()
	if p.DiffViewMode != diffViewModeInline && p.DiffViewMode != diffViewModeSplit {
		p.DiffViewMode = diffViewModeInline
	}
	p.KeepAwake = power.Normalize(p.KeepAwake)
	p.Terminal = terminal.Normalize(p.Terminal)
	if p.Theme != "light" && p.Theme != "dark" {
		p.Theme = "system"
	}
	if !slices.ContainsFunc(themes, func(t themeDefinition) bool { return t.id == p.ThemeID }) {
		p.ThemeID = themes[0].id
	}
	if p.EnglishFont == "" {
		p.EnglishFont = d.EnglishFont
	}
	if p.ChineseFont == "" {
		p.ChineseFont = d.ChineseFont
	}
	if p.CodeFont == "" {
		p.CodeFont = d.CodeFont
	}
}

func (a *App) codeFontFamily() string {
	root := a.application()
	p := root.preferences
	if p.CodeFont == "" {
		p = defaultPreferences()
	}
	return p.CodeFont + ", " + p.ChineseFont + ", monospace"
}

func (a *App) applyAppearance(c *native.Context) {
	root := a.application()
	root.normalizePreferences()
	t := *c.Theme()
	// System mode keeps the host's accent and accessibility preferences.
	if root.win == nil && root.preferences.Theme == "light" {
		t = *native.LightTheme()
	}
	if root.win == nil && root.preferences.Theme == "dark" {
		t = *native.DarkTheme()
	}
	// The configured theme's palette paints the chrome; zero fields of
	// the default theme's dark palette leave MyGo's dark theme alone.
	pal := themeByID(root.preferences.ThemeID).paletteFor(t.Dark)
	pal.apply(&t)
	t.Font = root.preferences.EnglishFont + ", " + root.preferences.ChineseFont + ", system-ui"
	// The status badge reads as a small dot on chrome, surface, and the
	// editor ground. The default theme keeps the mirrored Mayoi status
	// colors through applyStatusTheme; a palette that carries its own
	// status colors skips that, so Claude's dot follows its accent.
	if pal.status == (statusPalette{}) {
		applyStatusTheme(&t, t.Background, t.Surface, t.SurfaceHover)
	} else {
		pal.status.apply(&t, t.Background, t.Surface, t.SurfaceHover)
	}
	c.SetTheme(&t)
	root.md.Theme.Font, root.md.Theme.CodeFont = t.Font, root.codeFontFamily()
}

// activePalette resolves the configured theme's palette for the given
// appearance, so view code that draws app-specific surfaces reads the
// same values applyAppearance painted the chrome with.
func (a *App) activePalette(t *native.Theme) palette {
	return themeByID(a.application().preferences.ThemeID).paletteFor(t.Dark)
}

// userBubbleColor fills the user's message bubble: the palette's bubble
// color when it carries one, else the existing defaults.
func (a *App) userBubbleColor(t *native.Theme) native.Color {
	if c := a.activePalette(t).chat.userBubble; c != (native.Color{}) {
		return c
	}
	if t.Dark {
		return t.Surface
	}
	return native.Hex("#f0f0f1")
}

// composerBackground fills the chat input card: the palette's card
// color when it carries one, else the existing defaults.
func (a *App) composerBackground(t *native.Theme) native.Color {
	if c := a.activePalette(t).chat.composerCard; c != (native.Color{}) {
		return c
	}
	if t.Dark {
		return t.Background
	}
	return native.Hex("#ffffff")
}

func (a *App) applySystemTheme() {
	mygo.Theme.SetSource(mygo.ThemeSource(a.preferences.Theme))
}

func (a *App) openSettings() {
	a = a.application()
	if a.quitting || a.quitDialog || a.quitStopping {
		return
	}
	if a.settingsWindow != nil {
		a.showSettingsWindow()
		return
	}
	a.resetSettingsPickers()
	a.settingsQuery = ""
	if a.settingsSection == "" {
		a.settingsSection = "外观"
	}
	if a.powerController != nil && !a.awakeSaving {
		if mode, err := a.powerController.Sync(); err != nil {
			a.settingsError = "读取系统电源配置失败：" + err.Error()
		} else if mode != a.preferences.KeepAwake {
			a.preferences.KeepAwake = mode
			if err := a.persist(); err != nil {
				a.settingsError = "设置未保存：" + err.Error()
			}
		}
	}
	if a.fontFamilies == nil {
		for _, f := range native.FontFamilies() {
			a.fontFamilies = append(a.fontFamilies, f.Name)
		}
	}
	if a.win == nil {
		a.settingsOpen = true
		return
	}
	a.settingsWindow = mygo.NewWindow(mygo.WindowOptions{
		Title: "设置", Width: 940, Height: 680, MinWidth: 640, MinHeight: 420,
		StateKey: "settings", BackgroundColor: "light-dark(#fefefe, #18181b)",
		Content: native.View(func(c *native.Context) {
			a.applyAppearance(c)
			a.settingsPanel(c, false)
		}),
	})
	window := a.settingsWindow
	window.OnClosed(func() {
		if a.settingsWindow == window {
			a.settingsWindow = nil
			a.resetSettingsPickers()
		}
	})
	a.showSettingsWindow()
}

func (a *App) showSettingsWindow() {
	if a.settingsWindow.IsMinimized() {
		a.settingsWindow.Restore()
	}
	a.settingsWindow.Show()
	a.settingsWindow.Focus()
	a.settingsWindow.Invalidate()
}

func (a *App) resetSettingsPickers() {
	a.fontPicker, a.auxiliaryPicker = "", ""
	a.awakePicker, a.terminalPicker = false, false
}

func (a *App) redrawSettings() {
	if a.settingsWindow != nil {
		a.settingsWindow.Invalidate()
	}
}

func (a *App) savePreferences(previous storage.Preferences) {
	defer a.redrawSettings()
	awakeChanged := a.powerController != nil && previous.KeepAwake != a.preferences.KeepAwake
	if awakeChanged {
		mode := a.preferences.KeepAwake
		a.preferences.KeepAwake = previous.KeepAwake
		a.saveKeepAwake(mode)
		return
	}
	if err := a.persist(); err != nil {
		a.preferences = previous
		a.settingsError = "设置未保存：" + err.Error()
		return
	}
	a.settingsError = ""
	a.auxiliaryPreferencesChanged(previous)
	if a.win != nil {
		if previous.Theme != a.preferences.Theme {
			a.applySystemTheme()
		}
		a.win.Invalidate()
	}
	// A same-appearance theme switch changes only the palette the next
	// frame paints, so invalidating the open windows is enough; the
	// appearance itself keeps going through applySystemTheme.
	if previous.ThemeID != a.preferences.ThemeID {
		if a.quickOpen.window != nil {
			a.quickOpen.window.Invalidate()
		}
	}
	if previous.CodeFont != a.preferences.CodeFont || previous.ChineseFont != a.preferences.ChineseFont {
		for _, d := range a.files {
			if d.highlightCancel != nil {
				d.highlightCancel()
			}
			d.contentWidth, d.gitLeftWidth, d.gitRightWidth = 0, 0, 0
		}
	}
}

func (a *App) saveKeepAwake(mode string) {
	if a.awakeSaving || !a.canAsync() {
		return
	}
	a.awakeSaving = true
	previous := a.preferences.KeepAwake
	controller := a.powerController
	go func() {
		original, err := controller.CurrentState()
		if err == nil {
			err = controller.Set(mode)
		}
		a.update(func() {
			defer a.redrawSettings()
			if err != nil {
				a.awakeSaving = false
				a.settingsError = "保持唤醒未生效：" + err.Error()
				return
			}
			oldBaseline := a.preferences.KeepAwakeBaseline
			a.preferences.KeepAwake = mode
			if baseline, ok := controller.PendingBaseline(); ok {
				a.preferences.KeepAwakeBaseline = &storage.SleepBaseline{AC: baseline.AC, Battery: baseline.Battery}
			}
			if err := a.persist(); err != nil {
				a.preferences.KeepAwake, a.preferences.KeepAwakeBaseline = previous, oldBaseline
				a.settingsError = "设置未保存：" + err.Error()
				go func() {
					rollbackErr := controller.RestoreState(original)
					a.update(func() {
						defer a.redrawSettings()
						a.awakeSaving = false
						if rollbackErr != nil {
							a.settingsError += "；恢复保持唤醒失败：" + rollbackErr.Error()
						}
					})
				}()
				return
			}
			controller.ConfirmBaseline()
			a.awakeSaving = false
			a.settingsError = ""
		})
	}()
}

func (a *App) installSettingsMenu() {
	mygo.App.SetMenu(a.applicationMenu())
}

func (a *App) applicationMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Sasa", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout}, mygo.Separator(),
			{Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings() }},
			mygo.Separator(), {Role: mygo.RoleServices}, mygo.Separator(),
			{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide}, mygo.Separator(), {Role: mygo.RoleQuit},
		}},
		{Label: "文件", Submenu: []*mygo.MenuItem{
			{ID: "quick-open", Label: "快速打开文件", Accelerator: "CmdOrCtrl+P", Click: func(*mygo.MenuItem, *mygo.Window) { a.update(a.openQuickOpen) }},
			{ID: "go-to-line", Label: "跳转到行", Accelerator: "Ctrl+G", Click: func(*mygo.MenuItem, *mygo.Window) { a.update(a.openGoToLine) }},
			{ID: "close-current-page", Label: "关闭当前页签", Accelerator: "CmdOrCtrl+W", Click: func(_ *mygo.MenuItem, w *mygo.Window) {
				if w != nil && w == a.settingsWindow {
					w.Close()
					return
				}
				a.update(a.closeCurrentPage)
			}},
		}},
		{Role: mygo.RoleEditMenu}, {Role: mygo.RoleViewMenu}, {Role: mygo.RoleWindowMenu},
	})
}

func (a *App) settingsView(c *native.Context) {
	if a.win != nil {
		return
	}
	native.Modal(c.Key("settings"), &a.settingsOpen, func() {
		if c.Shortcut(native.Cmd, native.KeyW) {
			a.settingsOpen = false
		}
		a.settingsPanel(c, true)
	})
}

func (a *App) settingsFontPreview(c *native.Context) {
	t := c.Theme()
	settingsCard(c, "font-preview", func() {
		native.Column(c).FillWidth().Padding(14, 16).Gap(6).Children(func() {
			native.Text(c, "字体预览").FontSize(13).FontWeight(600).TextColor(t.Text)
			family := a.preferences.EnglishFont + ", " + a.preferences.ChineseFont + ", system-ui"
			native.Text(c, "让想法逐步成形，保持清晰与专注。").Font(family).FontSize(15).FillWidth()
			native.Text(c, "Make room for your next idea. 0123456789").Font(family).FontSize(14).FillWidth()
			native.Text(c, "const message = \"你好，Sasa\";").Font(a.codeFontFamily()).FontSize(13).FillWidth()
		})
	})
}

func (a *App) fontSetting(c *native.Context, label string, value *string, defaultValue string) {
	native.Row(c).FillWidth().AlignItems(native.Center).Gap(12).Children(func() {
		native.Text(c, label).Label("设置标签 " + label).Grow(1).MinWidth(0).FontSize(13).FontWeight(500)
		b := treeButton(c, "选择"+label).Width(216).Shrink(0).Height(32).Padding(0, 10).Border(1, c.Theme().Border)
		b.Children(func() {
			native.Row(c).FillWidth().AlignItems(native.Center).Gap(6).Children(func() {
				native.Text(c, fontDisplayName(*value)).FontSize(12).SingleLine().Grow(1).MinWidth(0)
				native.Icon(c, chevron).Size(12, 12)
			})
		})
		if b.Clicked() {
			if a.fontPicker == label {
				a.fontPicker = ""
			} else {
				a.fontPicker, a.fontSearch = label, ""
			}
		}
		open := a.fontPicker == label
		native.PopoverBase(c, b, &open, func(panel native.Element) {
			t := c.Theme()
			panel.Label("字体列表 "+label).Width(b.Bounds().W).Padding(4).Margin(4, 0, 0, 0).
				Radius(8).Background(t.Background).Border(1, t.Border).
				Shadow(0, 6, 20, 0, native.RGBA(0, 0, 0, .18))
			native.Column(c).FillWidth().MinWidth(0).Gap(6).Children(func() {
				native.TextInput(c, &a.fontSearch).Label("搜索" + label).Placeholder("搜索字体").FontSize(12).FillWidth()
				native.Scroll(c).FillWidth().MinWidth(0).Height(180).Children(func() {
					native.Column(c).FillWidth().Children(func() {
						names := slices.Clone(a.fontFamilies)
						for _, required := range []string{*value, defaultValue} {
							if !slices.Contains(names, required) {
								names = append([]string{required}, names...)
							}
						}
						matched := false
						for _, name := range names {
							if !strings.Contains(strings.ToLower(name), strings.ToLower(strings.TrimSpace(a.fontSearch))) {
								continue
							}
							matched = true
							row := treeButton(c.Key(name), label+" "+name).FillWidth().Padding(6).Justify(native.Start)
							row.Children(func() {
								native.Text(c, fontDisplayName(name)).FontSize(12).SingleLine().Grow(1).MinWidth(0)
								if *value == name {
									native.Text(c, "✓").FontSize(12)
								}
							})
							if row.Clicked() {
								previous := a.preferences
								*value = name
								a.savePreferences(previous)
								a.fontPicker = ""
							}
						}
						if !matched {
							native.Text(c, "没有匹配的字体").FontSize(12).TextColor(c.Theme().TextMuted).Padding(8)
						}
					})
				})
			})
		})
		if !open && a.fontPicker == label {
			a.fontPicker = ""
		}
	})
}

func (a *App) preferenceSwitch(c *native.Context, label, detail string, value *bool, disabled bool) {
	native.Row(c.Key(label)).FillWidth().Gap(16).AlignItems(native.Center).Children(func() {
		native.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			settingsFieldLabel(c, label, detail)
		})
		previous := a.preferences
		if native.Switch(c, value).Label("切换" + label).Disabled(disabled).Changed() {
			a.savePreferences(previous)
		}
	})
}

func fontDisplayName(name string) string {
	if name == ".PingFang UI SC" {
		return "系统中文字体（默认）"
	}
	return name
}
