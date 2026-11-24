package ui

import (
	"slices"
	"strings"

	native "github.com/egoist/mygo/ui"
)

var (
	settingsAppearanceIcon = icon(`<circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M4.93 4.93l1.42 1.42m11.3 11.3 1.42 1.42M2 12h2m16 0h2M4.93 19.07l1.42-1.42m11.3-11.3 1.42-1.42"/>`)
	settingsFontIcon       = icon(`<path d="M4 7V4h16v3M12 4v16m-4 0h8"/>`)
	settingsSearchIcon     = icon(`<circle cx="10.5" cy="10.5" r="5.5"/><path d="m15 15 4 4"/>`)
	settingsBellIcon       = icon(`<path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9m-8 12a2 2 0 0 0 4 0"/>`)
)

type settingsField struct {
	title, keywords string
	draw            func(*native.Context)
}

type settingsCategory struct {
	title  string
	icon   *native.SVG
	fields []settingsField
}

func (a *App) settingsCategories() []settingsCategory {
	defaults := defaultPreferences()
	fonts := make([]settingsField, 0, 3)
	for _, font := range []struct {
		title     string
		value     *string
		defaultID string
	}{
		{"英文字体", &a.preferences.EnglishFont, defaults.EnglishFont},
		{"中文字体", &a.preferences.ChineseFont, defaults.ChineseFont},
		{"代码字体", &a.preferences.CodeFont, defaults.CodeFont},
	} {
		fonts = append(fonts, settingsField{font.title, "font", func(c *native.Context) {
			a.fontSetting(c, font.title, font.value, font.defaultID)
		}})
	}
	return []settingsCategory{
		{"外观", settingsAppearanceIcon, []settingsField{
			{"配色", "主题 Claude 默认 火星乐园 Mars theme palette", func(c *native.Context) {
				settingsFieldLabel(c, "配色", "应用、编辑器、差异视图和内置终端共用的颜色。")
				a.themeCards(c)
			}},
			{"明暗模式", "主题 跟随系统 浅色 深色 dark light system", a.appearanceMode},
		}},
		{"字体", settingsFontIcon, fonts},
		{"通用", settingsIcon, []settingsField{
			{"默认终端", "Ghostty terminal", a.terminalSetting},
			{"让这台 Mac 保持唤醒状态", "电源 睡眠 awake", a.awakeSetting},
			{"辅助模型", "Claude Code model", a.auxiliaryModelRow},
			{"自动生成会话标题", "辅助模型 title", a.auxiliaryTitleRow},
		}},
		{"通知", settingsBellIcon, []settingsField{
			{"后台任务完成通知", "notification", func(c *native.Context) {
				a.preferenceSwitch(c, "后台任务完成通知", "应用或会话处于后台时，任务完成后提醒。", &a.preferences.CompletionNotification, false)
			}},
			{"提示音", "sound", func(c *native.Context) {
				a.preferenceSwitch(c, "提示音", "任务完成通知播放系统提示音。", &a.preferences.NotificationSound, !a.preferences.CompletionNotification)
			}},
		}},
	}
}

func (a *App) settingsPanel(c *native.Context, modal bool) {
	t := c.Theme()
	width, height := c.Size()
	layout := native.Row(c).Fill().Background(t.Background)
	if modal {
		layout.Width(min(width-24, 940)).Height(min(height-24, 640)).Radius(12).Clip()
	}
	categories := a.settingsCategories()
	find := c.Shortcut(native.Cmd, native.KeyF)
	layout.Children(func() {
		native.Column(c).Width(min(220, width*.29)).Shrink(0).FillHeight().Padding(14, 10).
			Gap(10).Background(t.Surface).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
			a.settingsSearch(c, find)
			native.Column(c).FillWidth().Gap(2).Children(func() {
				for _, category := range categories {
					selected := a.settingsSection == category.title && strings.TrimSpace(a.settingsQuery) == ""
					button := native.ButtonBase(c.Key(category.title)).Label("设置分类 "+category.title).
						FillWidth().Height(32).Padding(0, 8).Radius(6).Gap(9).Justify(native.Start).TextColor(t.Text)
					background := native.Color{}
					if selected {
						background = t.Accent.Alpha(.14)
					}
					settingsButtonBackground(button, t, background)
					button.Children(func() {
						native.Icon(c, category.icon).Size(16, 16).TextColor(t.Accent)
						native.Text(c, category.title).FontSize(13).SingleLine()
					})
					if button.Clicked() {
						a.settingsSection, a.settingsQuery = category.title, ""
						a.resetSettingsPickers()
						c.Invalidate()
					}
				}
			})
		})
		native.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
			a.settingsContent(c, categories, modal)
		})
	})
}

func (a *App) settingsSearch(c *native.Context, find bool) {
	t := c.Theme()
	native.Box(c).FillWidth().Height(30).Shrink(0).Children(func() {
		search := native.TextInput(c.Key("settings-search"), &a.settingsQuery).
			Label("搜索设置").Placeholder("搜索设置").Fill().Padding(0, 26, 0, 28).
			Radius(6).Background(t.Background).FontSize(13)
		native.Icon(c, settingsSearchIcon).Absolute().Top(7).Left(8).Size(16, 16).TextColor(t.TextMuted)
		if find {
			search.Focus()
		}
		if search.Changed() {
			a.resetSettingsPickers()
		}
		if a.settingsQuery != "" {
			clear := quietIconButton(c, "清除搜索").Absolute().Top(3).Right(3).Size(24, 24).KeepFocus()
			clear.Children(func() { native.Text(c, "×").FontSize(16) })
			if clear.Clicked() {
				a.settingsQuery = ""
				a.resetSettingsPickers()
				search.Focus()
				c.Invalidate()
			}
		}
	})
}

func (a *App) settingsContent(c *native.Context, categories []settingsCategory, modal bool) {
	t := c.Theme()
	query := strings.Fields(strings.ToLower(a.settingsQuery))
	page, title, glyph := a.settingsSection, a.settingsSection, settingsIcon
	if len(query) > 0 {
		page, title, glyph = "search", "搜索结果", settingsSearchIcon
	} else if index := slices.IndexFunc(categories, func(category settingsCategory) bool {
		return category.title == page
	}); index >= 0 {
		glyph = categories[index].icon
	}
	native.Row(c).FillWidth().Height(56).Shrink(0).Padding(0, 24).Gap(10).AlignItems(native.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		native.Box(c).Size(27, 27).Radius(7).AlignItems(native.Center).Justify(native.Center).
			Background(t.Accent.Alpha(.13)).Children(func() {
			native.Icon(c, glyph).Size(16, 16).TextColor(t.Accent)
		})
		native.Text(c, title).FontSize(17).FontWeight(600).SingleLine().Grow(1)
		if modal {
			close := quietIconButton(c, "关闭设置").Size(28, 28)
			close.Children(func() { native.Text(c, "×").FontSize(20) })
			if close.Clicked() {
				a.settingsOpen = false
			}
		}
	})
	native.Scroll(c.Key(page)).Grow(1).MinHeight(0).Children(func() {
		native.Column(c).FillWidth().MaxWidth(760).AlignSelf(native.Center).Padding(20, 24, 28, 24).
			Gap(12).Children(func() {
			if page == "字体" {
				a.settingsFontPreview(c)
			}
			found := false
			for _, category := range categories {
				if len(query) == 0 && category.title != page {
					continue
				}
				fields := matchingSettingsFields(category, query)
				if len(fields) == 0 {
					continue
				}
				found = true
				if len(query) > 0 {
					native.Text(c, category.title).FontSize(12).FontWeight(600).TextColor(t.TextMuted)
				}
				settingsCard(c, category.title, func() {
					for index, field := range fields {
						if index > 0 {
							native.Row(c).FillWidth().Padding(0, 16).Children(func() {
								native.Box(c).Grow(1).Height(1).Background(t.Border)
							})
						}
						native.Column(c.Key(field.title)).FillWidth().MinWidth(0).Padding(13, 16).Gap(10).Children(func() {
							field.draw(c)
						})
					}
				})
			}
			if !found {
				native.Text(c, "没有匹配的设置").FillWidth().Padding(24).TextAlign(native.Center).TextColor(t.TextMuted)
			}
			if page == "通知" {
				native.Text(c, "系统通知的显示方式由 macOS 通知设置控制。").FontSize(12).TextColor(t.TextMuted)
				if a.notifyError != "" {
					native.Text(c, a.notifyError).FontSize(12).TextColor(t.Danger).FillWidth()
				}
			}
			if a.settingsError != "" {
				native.Text(c, a.settingsError).FontSize(12).TextColor(t.Danger).FillWidth()
			}
		})
	})
}

func matchingSettingsFields(category settingsCategory, query []string) []settingsField {
	if len(query) == 0 {
		return category.fields
	}
	var matches []settingsField
	for _, field := range category.fields {
		text := strings.ToLower(category.title + " " + field.title + " " + field.keywords)
		if !slices.ContainsFunc(query, func(word string) bool { return !strings.Contains(text, word) }) {
			matches = append(matches, field)
		}
	}
	return matches
}

func settingsCard(c *native.Context, key string, draw func()) {
	t := c.Theme()
	native.Column(c.Key(key)).FillWidth().MinWidth(0).Radius(10).Background(t.Surface).
		Border(1, t.Border).Children(draw)
}

func settingsFieldLabel(c *native.Context, title, detail string) {
	native.Text(c, title).Label("设置标签 " + title).FontSize(13).FontWeight(600)
	if detail != "" {
		native.Text(c, detail).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
	}
}
