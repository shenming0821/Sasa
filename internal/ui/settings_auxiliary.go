package ui

import (
	"context"
	"fmt"
	"slices"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
)

func (a *App) loadAuxiliaryModels(force bool) {
	runtime := a.auxiliaryRuntime()
	if a.auxiliaryLoading || !a.canAsync() || a.auxiliaryClosed ||
		!force && a.auxiliaryCatalogRuntime == runtime && len(a.auxiliaryModels) > 0 {
		return
	}
	backend, ok := a.auxiliaryBackend().(modelCatalog)
	if !ok {
		a.auxiliaryError = "当前 Claude Code 配置不可用。"
		return
	}
	a.auxiliaryLoading = true
	a.auxiliaryError = ""
	a.auxiliaryCatalogEpoch++
	epoch := a.auxiliaryCatalogEpoch
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	a.auxiliaryCatalogCancel = cancel
	a.auxiliaryWorkers.Add(1)
	go func() {
		defer a.auxiliaryWorkers.Done()
		defer cancel()
		var models []agent.Model
		var err error
		if reload, ok := backend.(interface {
			ReloadModelsForDirectory(context.Context, string) ([]agent.Model, error)
		}); force && ok {
			models, err = reload.ReloadModelsForDirectory(ctx, "")
		} else {
			models, err = backend.ListModels(ctx)
		}
		models = slices.DeleteFunc(models, func(m agent.Model) bool { return m.Hidden || m.Model == "" })
		if err == nil && len(models) == 0 {
			err = fmt.Errorf("当前配置没有可用模型")
		}
		select {
		case <-a.shutdown:
			return
		default:
		}
		a.update(func() {
			if a.auxiliaryClosed || a.auxiliaryCatalogEpoch != epoch {
				return
			}
			a.auxiliaryLoading = false
			a.auxiliaryCatalogCancel = nil
			if a.auxiliaryRuntime() != runtime {
				return
			}
			if err != nil {
				a.auxiliaryError = "读取辅助模型失败：" + err.Error()
				return
			}
			a.auxiliaryCatalogRuntime, a.auxiliaryModels = runtime, models
		})
	}()
}

// auxiliaryRuntime returns the Claude runtime used for auxiliary tasks. The
// preference stays authoritative once set; when unset the first connected
// Claude backend wins so upgrades from the multi-runtime picker keep working.
func (a *App) auxiliaryRuntime() string {
	if a.preferences.AuxiliaryRuntime != "" {
		return a.preferences.AuxiliaryRuntime
	}
	var runtime string
	for key := range a.backends {
		if key.kind == agent.Claude && (runtime == "" || key.runtime < runtime) {
			runtime = key.runtime
		}
	}
	return runtime
}

func (a *App) auxiliarySettings(c *native.Context) {
	native.Column(c).FillWidth().Gap(10).Children(func() {
		native.Text(c, "辅助模型").FontSize(14).FontWeight(600)
		a.auxiliaryModelRow(c)
		a.auxiliaryTitleRow(c)
	})
}

func (a *App) auxiliaryModelRow(c *native.Context) {
	modelLabel := a.preferences.AuxiliaryModelName
	if modelLabel == "" {
		modelLabel = "选择模型"
	}
	native.Row(c).FillWidth().Gap(12).AlignItems(native.Center).Children(func() {
		native.Text(c, "模型").FontSize(12).SingleLine().Grow(1).MinWidth(0)
		trigger := treeButton(c, "选择辅助模型").Width(190).Shrink(0).Height(32).Padding(0, 10).Border(1, c.Theme().Border)
		trigger.Children(func() {
			native.Text(c, modelLabel).FontSize(12).SingleLine().Grow(1).MinWidth(0)
			native.Icon(c, chevron).Size(12, 12)
		})
		if trigger.Clicked() {
			if a.auxiliaryPicker == "model" {
				a.auxiliaryPicker = ""
			} else {
				a.auxiliaryPicker = "model"
				a.awakePicker, a.terminalPicker = false, false
				a.loadAuxiliaryModels(false)
			}
		}
		open := a.auxiliaryPicker == "model"
		native.PopoverBase(c.Key("options"), trigger, &open, func(panel native.Element) {
			t := c.Theme()
			panel.Width(280).Padding(4).Margin(4, 0, 0, 0).Radius(8).Background(t.Background).Border(1, t.Border).
				Shadow(0, 6, 20, 0, native.RGBA(0, 0, 0, .18))
			native.Column(c).FillWidth().Gap(4).Children(func() {
				a.auxiliaryModelMenuHead(c)
				a.auxiliaryModelMenuList(c)
				a.auxiliaryModelMenuStatus(c)
			})
		})
		if !open && a.auxiliaryPicker == "model" {
			a.auxiliaryPicker = ""
		}
	})
}

func (a *App) auxiliaryModelMenuHead(c *native.Context) {
	native.Row(c).FillWidth().Padding(0, 4, 0, 8).AlignItems(native.Center).Children(func() {
		native.Text(c, "选择模型").FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
		label := "刷新模型列表"
		if a.auxiliaryError != "" {
			label = "重试读取模型"
		}
		if a.auxiliaryLoading {
			label = "正在刷新模型列表"
		}
		refresh := quietIconButton(c.Key("auxiliary-refresh"), label).Size(26, 26).Disabled(a.auxiliaryLoading)
		refresh.Children(func() {
			if a.auxiliaryLoading {
				native.Spinner(c).Size(14, 14)
			} else {
				native.Icon(c, refreshProjectIcon).Size(14, 14)
			}
		})
		if refresh.Clicked() {
			a.loadAuxiliaryModels(true)
		}
	})
}

func (a *App) auxiliaryModelMenuList(c *native.Context) {
	if len(a.auxiliaryModels) == 0 {
		return
	}
	native.Scroll(c).FillWidth().MaxHeight(240).Children(func() {
		native.Column(c).FillWidth().Children(func() {
			for _, model := range a.auxiliaryModels {
				name := model.DisplayName
				if name == "" {
					name = model.Model
				}
				b := treeButton(c.Key(model.Model), "辅助模型 "+name).FillWidth().Padding(8, 4, 8, 8).Justify(native.Start)
				b.Children(func() {
					native.Row(c).FillWidth().AlignItems(native.Center).Gap(8).Children(func() {
						native.Text(c, name).FontSize(12).Grow(1).MinWidth(0).SingleLine()
						native.Box(c).Width(26).Shrink(0).AlignItems(native.Center).Justify(native.Center).Children(func() {
							if a.preferences.AuxiliaryModel == model.Model {
								native.Text(c, "✓").FontSize(12)
							}
						})
					})
				})
				if b.Clicked() {
					previous := a.preferences
					a.preferences.AuxiliaryModel, a.preferences.AuxiliaryModelName = model.Model, name
					a.savePreferences(previous)
					a.auxiliaryPicker = ""
				}
			}
		})
	})
}

func (a *App) auxiliaryModelMenuStatus(c *native.Context) {
	t := c.Theme()
	switch {
	case a.auxiliaryError != "":
		native.Text(c, a.auxiliaryError).FontSize(11).TextColor(t.Danger).Padding(4, 8)
	case a.auxiliaryLoading:
		native.Text(c, "读取模型列表…").FontSize(11).TextColor(t.TextMuted).Padding(4, 8)
	case len(a.auxiliaryModels) == 0:
		native.Text(c, "暂无可用模型").FontSize(11).TextColor(t.TextMuted).Padding(4, 8)
	}
}

func (a *App) auxiliaryTitleRow(c *native.Context) {
	native.Row(c).FillWidth().Gap(16).AlignItems(native.Center).Children(func() {
		native.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			native.Text(c, "自动生成会话标题").FontSize(12)
			native.Text(c, "空闲时为新会话生成简短标题").FontSize(11).TextColor(c.Theme().TextMuted).FillWidth()
		})
		disabled := a.preferences.AuxiliaryModel == "" || a.auxiliaryBackend() == nil
		previous := a.preferences
		sw := native.Switch(c, &a.preferences.AutoSessionTitle).Label("切换自动生成会话标题")
		if sw.Changed() {
			if a.preferences.AutoSessionTitle && disabled {
				a.preferences.AutoSessionTitle = previous.AutoSessionTitle
				a.settingsError = "请先选择辅助模型。"
			} else {
				a.savePreferences(previous)
			}
		}
	})
	if a.titleJob != nil {
		native.Text(c, "正在生成会话标题…").FontSize(11).TextColor(c.Theme().TextMuted)
	}
}
