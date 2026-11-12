package ui

import (
	"slices"
	"strings"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/power"
	"github.com/shenming0821/Fuyu/internal/storage"
	"github.com/shenming0821/Fuyu/internal/terminal"
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
	settingsSection    string
	settingsError      string
	fontFamilies       []string
	fontPicker         string
	fontSearch         string
	notifyError        string
	notificationSender func(mygo.NotificationOptions) error
	windowFocused      func() bool
}

var settingsIcon = icon(`<path d="M9 3h6l1 3 3 1 2 5-2 5-3 1-1 3H9l-1-3-3-1-2-5 2-5 3-1Z"/><circle cx="12" cy="12" r="3"/>`)

func defaultPreferences() storage.Preferences {
	return storage.Preferences{KeepAwake: power.Never, Terminal: terminal.Ghostty, Theme: "system", EnglishFont: latinFont, ChineseFont: ".PingFang UI SC", CodeFont: latinFont, CompletionNotification: true}
}

func (a *App) normalizePreferences() {
	p := &a.preferences
	d := defaultPreferences()
	p.KeepAwake = power.Normalize(p.KeepAwake)
	p.Terminal = terminal.Normalize(p.Terminal)
	if p.Theme != "light" && p.Theme != "dark" {
		p.Theme = "system"
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
	if !t.Dark {
		// Codex-style light grays: near-white sidebar, barely darker
		// selection, and a hairline border.
		t.Background = native.Hex("#fefefe")
		t.Surface = native.Hex("#fafafa")
		t.SurfaceHover = native.Hex("#efefef")
		t.SurfacePressed = native.Hex("#e4e4e4")
		t.Border = native.Hex("#e4e4e4")
	}
	t.Font = root.preferences.EnglishFont + ", " + root.preferences.ChineseFont + ", system-ui"
	c.SetTheme(&t)
	root.md.Theme.Font, root.md.Theme.CodeFont = t.Font, root.codeFontFamily()
}

// userBubbleColor fills the user's message bubble: one step off-white in
// light mode, the control face in dark mode.
func userBubbleColor(t *native.Theme) native.Color {
	if t.Dark {
		return t.Surface
	}
	return native.Hex("#f0f0f1")
}

// composerBackground fills the chat input card: pure white on the light
// theme so it lifts off the #fefefe conversation floor.
func composerBackground(t *native.Theme) native.Color {
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
	a.settingsOpen, a.fontPicker = true, ""
	a.awakePicker = false
	a.terminalPicker = false
	if a.settingsSection == "" {
		a.settingsSection = "通用"
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
}

func (a *App) savePreferences(previous storage.Preferences) {
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
	if a.win != nil && previous.Theme != a.preferences.Theme {
		a.applySystemTheme()
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
		{Label: "Fuyu", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout}, mygo.Separator(),
			{Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { a.win.Update(func() { a.openSettings() }); a.win.Show() }},
			mygo.Separator(), {Role: mygo.RoleServices}, mygo.Separator(),
			{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide}, mygo.Separator(), {Role: mygo.RoleQuit},
		}},
		{Label: "文件", Submenu: []*mygo.MenuItem{
			{ID: "quick-open", Label: "快速打开文件", Accelerator: "CmdOrCtrl+P", Click: func(*mygo.MenuItem, *mygo.Window) { a.update(a.openQuickOpen) }},
			{ID: "close-current-page", Label: "关闭当前页签", Accelerator: "CmdOrCtrl+W", Click: func(*mygo.MenuItem, *mygo.Window) { a.update(a.closeCurrentPage) }},
		}},
		{Role: mygo.RoleEditMenu}, {Role: mygo.RoleViewMenu}, {Role: mygo.RoleWindowMenu},
	})
}

func (a *App) settingsView(c *native.Context) {
	native.Modal(c.Key("settings"), &a.settingsOpen, func() {
		if c.Shortcut(native.Cmd, native.KeyW) {
			a.settingsOpen = false
		}
		w, h := c.Size()
		t := c.Theme()
		native.Column(c).Width(min(w-48, 640)).Height(min(h-64, 530)).Padding(16).Gap(16).Children(func() {
			native.Row(c).FillWidth().AlignItems(native.Center).Children(func() {
				native.Text(c, "设置").FontSize(17).FontWeight(600).Grow(1)
				close := treeButton(c, "关闭设置").Size(28, 28).Padding(0)
				close.Children(func() { native.Text(c, "×").FontSize(20) })
				if close.Clicked() {
					a.settingsOpen = false
				}
			})
			native.Row(c).FillWidth().Grow(1).MinHeight(0).Gap(20).Children(func() {
				native.Column(c).Width(82).FillHeight().Gap(4).Children(func() {
					for _, section := range []string{"通用", "外观", "通知"} {
						b := treeButton(c, "设置分类 "+section).FillWidth().Padding(8).Justify(native.Start)
						if a.settingsSection == section {
							b.Background(t.SurfacePressed).TextColor(t.Text)
						}
						b.Children(func() { native.Text(c, section).FontSize(12) })
						if b.Clicked() {
							a.settingsSection, a.fontPicker = section, ""
							a.awakePicker = false
							a.terminalPicker = false
						}
					}
				})
				native.Box(c).Width(1).FillHeight().Background(t.Border.Alpha(.6))
				native.Scroll(c.Key(a.settingsSection)).Grow(1).MinWidth(0).FillHeight().Children(func() {
					native.Column(c).FillWidth().Gap(18).Children(func() {
						if a.settingsSection == "通用" {
							a.generalSettings(c)
						} else if a.settingsSection == "通知" {
							a.notificationSettings(c)
						} else {
							a.appearanceSettings(c)
						}
						if a.settingsError != "" {
							native.Text(c, a.settingsError).FontSize(11).TextColor(t.Danger).FillWidth()
						}
					})
				})
			})
		})
	})
}

func (a *App) appearanceSettings(c *native.Context) {
	t := c.Theme()
	native.Text(c, "外观").FontSize(14).FontWeight(600)
	native.Row(c).FillWidth().AlignItems(native.Center).Gap(12).Children(func() {
		settingLabel(c, "主题")
		native.Row(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			for _, choice := range [][2]string{{"system", "跟随系统"}, {"light", "浅色"}, {"dark", "深色"}} {
				b := treeButton(c.Key(choice[0]), "主题 "+choice[1]).Grow(1).MinWidth(0).Height(32).Padding(0, 4)
				if a.preferences.Theme == choice[0] {
					b.Background(t.SurfacePressed).TextColor(t.Text)
				}
				b.Children(func() { native.Text(c, choice[1]).FontSize(12).SingleLine() })
				if b.Clicked() {
					previous := a.preferences
					a.preferences.Theme = choice[0]
					a.savePreferences(previous)
				}
			}
		})
	})
	defaults := defaultPreferences()
	for _, entry := range []struct {
		label        string
		value        *string
		defaultValue string
	}{
		{"英文字体", &a.preferences.EnglishFont, defaults.EnglishFont},
		{"中文字体", &a.preferences.ChineseFont, defaults.ChineseFont},
		{"代码字体", &a.preferences.CodeFont, defaults.CodeFont},
	} {
		a.fontSetting(c.Key(entry.label), entry.label, entry.value, entry.defaultValue)
	}
	native.Column(c).FillWidth().Padding(12).Gap(8).Radius(10).Background(t.Surface).Children(func() {
		native.Text(c, "字体预览").FontSize(11).TextColor(t.TextMuted)
		family := a.preferences.EnglishFont + ", " + a.preferences.ChineseFont + ", system-ui"
		native.Text(c, "让想法逐步成形，保持清晰与专注。").Font(family).FontSize(15).FillWidth()
		native.Text(c, "Make room for your next idea. 0123456789").Font(family).FontSize(14).FillWidth()
		native.Text(c, "const message = \"你好，Fuyu\";").Font(a.codeFontFamily()).FontSize(13).FillWidth()
	})
}

func (a *App) fontSetting(c *native.Context, label string, value *string, defaultValue string) {
	native.Row(c).FillWidth().AlignItems(native.Center).Gap(12).Children(func() {
		settingLabel(c, label)
		b := treeButton(c, "选择"+label).Grow(1).MinWidth(0).Height(32).Padding(0, 10).Border(1, c.Theme().Border)
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
		native.PopoverBase(c.Key("font-list"), b, &open, func(panel native.Element) {
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

func settingLabel(c *native.Context, label string) {
	native.Text(c, label).Label("设置标签 " + label).Width(64).Shrink(0).FontSize(12).TextColor(c.Theme().TextMuted)
}

func (a *App) notificationSettings(c *native.Context) {
	native.Text(c, "通知").FontSize(14).FontWeight(600)
	a.preferenceSwitch(c, "后台任务完成通知", "应用或会话处于后台时，任务完成后提醒。", &a.preferences.CompletionNotification, false)
	a.preferenceSwitch(c, "提示音", "任务完成通知播放系统提示音。", &a.preferences.NotificationSound, !a.preferences.CompletionNotification)
	native.Text(c, "系统通知的显示方式由 macOS 通知设置控制。").FontSize(11).TextColor(c.Theme().TextMuted).FillWidth()
	if a.notifyError != "" {
		native.Text(c, a.notifyError).FontSize(11).TextColor(c.Theme().Danger).FillWidth()
	}
}

func (a *App) preferenceSwitch(c *native.Context, label, detail string, value *bool, disabled bool) {
	native.Row(c.Key(label)).FillWidth().Gap(16).AlignItems(native.Center).Children(func() {
		native.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			native.Text(c, label).FontSize(12)
			native.Text(c, detail).FontSize(11).TextColor(c.Theme().TextMuted).FillWidth()
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
