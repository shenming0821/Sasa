package ui

import (
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/power"
	"github.com/shenming0821/Fuyu/internal/terminal"
)

var awakeChoices = []struct{ mode, label string }{
	{power.Never, "永不"},
	{power.Always, "始终"},
	{power.ACOnly, "仅使用电源适配器时"},
}

func (a *App) generalSettings(c *native.Context) {
	native.Text(c, "通用").FontSize(14).FontWeight(600)
	native.Column(c).FillWidth().Gap(10).Children(func() {
		native.Row(c).FillWidth().AlignItems(native.Center).Gap(12).Children(func() {
			native.Text(c, "默认终端").FontSize(12).SingleLine().Grow(1).MinWidth(0)
			label := terminal.Label(a.preferences.Terminal)
			button := treeButton(c, "默认终端 "+label).Width(190).Shrink(0).Height(32).Padding(0, 10).Border(1, c.Theme().Border)
			button.Children(func() {
				native.Row(c).FillWidth().AlignItems(native.Center).Gap(6).Children(func() {
					native.Text(c, label).FontSize(12).SingleLine().Grow(1).MinWidth(0)
					native.Icon(c, chevron).Size(12, 12)
				})
			})
			if button.Clicked() {
				a.terminalPicker = !a.terminalPicker
				a.awakePicker = false
			}
			native.PopoverBase(c.Key("terminal-options"), button, &a.terminalPicker, func(panel native.Element) {
				theme := c.Theme()
				panel.Width(button.Bounds().W).Padding(4).Margin(4, 0, 0, 0).Radius(8).Background(theme.Background).Border(1, theme.Border).
					Shadow(0, 6, 20, 0, native.RGBA(0, 0, 0, .18))
				native.Column(c).FillWidth().Children(func() {
					for _, choice := range terminal.Choices {
						row := treeButton(c.Key(choice.ID), "默认终端选项 "+choice.Label).FillWidth().Padding(8).Justify(native.Start)
						row.Children(func() {
							native.Text(c, choice.Label).FontSize(12).SingleLine().Grow(1).MinWidth(0)
							if terminal.Normalize(a.preferences.Terminal) == choice.ID {
								native.Text(c, "✓").FontSize(12)
							}
						})
						if row.Clicked() {
							previous := a.preferences
							a.preferences.Terminal = choice.ID
							a.savePreferences(previous)
							a.terminalPicker = false
						}
					}
				})
			})
		})
		native.Row(c).FillWidth().AlignItems(native.Center).Gap(12).Children(func() {
			native.Text(c, "让这台 Mac 保持唤醒状态").FontSize(12).SingleLine().Grow(1).MinWidth(0)
			label := "永不"
			for _, choice := range awakeChoices {
				if choice.mode == a.preferences.KeepAwake {
					label = choice.label
				}
			}
			button := treeButton(c, "保持唤醒 "+label).Width(190).Shrink(0).Height(32).Padding(0, 10).Border(1, c.Theme().Border)
			button.Children(func() {
				native.Row(c).FillWidth().AlignItems(native.Center).Gap(6).Children(func() {
					native.Text(c, label).FontSize(12).SingleLine().Grow(1).MinWidth(0)
					native.Icon(c, chevron).Size(12, 12)
				})
			})
			if button.Clicked() {
				a.awakePicker = !a.awakePicker
				a.terminalPicker = false
			}
			native.PopoverBase(c.Key("awake-options"), button, &a.awakePicker, func(panel native.Element) {
				theme := c.Theme()
				panel.Width(button.Bounds().W).Padding(4).Margin(4, 0, 0, 0).Radius(8).Background(theme.Background).Border(1, theme.Border).
					Shadow(0, 6, 20, 0, native.RGBA(0, 0, 0, .18))
				native.Column(c).FillWidth().Children(func() {
					for _, choice := range awakeChoices {
						row := treeButton(c.Key(choice.mode), "保持唤醒选项 "+choice.label).FillWidth().Padding(8).Justify(native.Start)
						row.Children(func() {
							native.Text(c, choice.label).FontSize(12).SingleLine().Grow(1).MinWidth(0)
							if a.preferences.KeepAwake == choice.mode {
								native.Text(c, "✓").FontSize(12)
							}
						})
						if row.Clicked() && !a.awakeSaving {
							previous := a.preferences
							a.preferences.KeepAwake = choice.mode
							a.savePreferences(previous)
							a.awakePicker = false
						}
					}
				})
			})
		})
		native.Text(c, "选择会写入 macOS 电源设置并需要管理员授权，退出 Fuyu 后仍然生效。").FontSize(11).TextColor(c.Theme().TextMuted).FillWidth()
		if a.awakeSaving {
			native.Text(c, "正在等待系统授权并更新电源设置…").FontSize(11).TextColor(c.Theme().TextMuted).FillWidth()
		}
		if a.powerController != nil {
			if err := a.powerController.Err(); err != nil {
				native.Text(c, "保持唤醒异常："+err.Error()).FontSize(11).TextColor(c.Theme().Danger).FillWidth()
			}
		}
	})
}
