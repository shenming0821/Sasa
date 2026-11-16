package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
)

func (a *App) loadAuxiliaryModels(force bool) {
	runtime := a.preferences.AuxiliaryRuntime
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
			if a.preferences.AuxiliaryRuntime != runtime {
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

func (a *App) auxiliarySettings(c *native.Context) {
	native.Column(c).FillWidth().Gap(10).Children(func() {
		native.Text(c, "辅助模型").FontSize(14).FontWeight(600)
		runtimes := []string{}
		for key := range a.backends {
			if key.kind == agent.Claude {
				runtimes = append(runtimes, key.runtime)
			}
		}
		slices.Sort(runtimes)
		runtimeLabel := "选择 Claude Code 配置"
		if a.preferences.AuxiliaryRuntime != "" {
			runtimeLabel = filepath.Base(a.preferences.AuxiliaryRuntime)
		}
		a.auxiliaryChoice(c, "Claude Code 配置", "runtime", runtimeLabel, func() {
			if len(runtimes) == 0 {
				native.Text(c, "暂无可用的 Claude Code 配置").FontSize(12).TextColor(c.Theme().TextMuted)
			}
			for _, runtime := range runtimes {
				b := treeButton(c.Key(runtime), "辅助配置 "+runtime).FillWidth().Padding(8).Justify(native.Start)
				b.Children(func() {
					native.Text(c, runtime).FontSize(12).SingleLine().Grow(1).MinWidth(0)
					if a.preferences.AuxiliaryRuntime == runtime {
						native.Text(c, "✓").FontSize(12)
					}
				})
				if b.Clicked() {
					previous := a.preferences
					if previous.AuxiliaryRuntime != runtime {
						a.preferences.AuxiliaryModel, a.preferences.AuxiliaryModelName = "", ""
						a.preferences.AutoSessionTitle = false
					}
					a.preferences.AuxiliaryRuntime = runtime
					a.savePreferences(previous)
					if a.preferences.AuxiliaryRuntime == runtime {
						a.auxiliaryModels = nil
						if a.auxiliaryCatalogCancel != nil {
							a.auxiliaryCatalogCancel()
						}
						a.auxiliaryCatalogEpoch++
						a.auxiliaryLoading = false
						a.loadAuxiliaryModels(false)
					}
					a.auxiliaryPicker = ""
				}
			}
		})
		modelLabel := a.preferences.AuxiliaryModelName
		if modelLabel == "" {
			modelLabel = "选择辅助模型"
		}
		a.auxiliaryChoice(c, "模型", "model", modelLabel, func() {
			for _, model := range a.auxiliaryModels {
				name := model.DisplayName
				if name == "" {
					name = model.Model
				}
				b := treeButton(c.Key(model.Model), "辅助模型 "+name).FillWidth().Padding(8).Justify(native.Start)
				b.Children(func() {
					native.Text(c, name).FontSize(12).SingleLine().Grow(1).MinWidth(0)
					if a.preferences.AuxiliaryModel == model.Model {
						native.Text(c, "✓").FontSize(12)
					}
				})
				if b.Clicked() {
					previous := a.preferences
					a.preferences.AuxiliaryModel, a.preferences.AuxiliaryModelName = model.Model, name
					a.savePreferences(previous)
					a.auxiliaryPicker = ""
				}
			}
			if a.auxiliaryLoading {
				native.Text(c, "正在读取模型…").FontSize(12).TextColor(c.Theme().TextMuted)
			}
			if actionButton(c, "刷新辅助模型列表").Disabled(a.auxiliaryLoading).Clicked() {
				a.loadAuxiliaryModels(true)
			}
		})
		if a.preferences.AuxiliaryRuntime != "" {
			native.Text(c, a.preferences.AuxiliaryRuntime).FontSize(11).TextColor(c.Theme().TextMuted).FillWidth()
		}
		native.Row(c).FillWidth().Gap(16).AlignItems(native.Center).Children(func() {
			native.Text(c, "自动生成会话标题").FontSize(12).Grow(1).MinWidth(0)
			previous := a.preferences
			if native.Switch(c, &a.preferences.AutoSessionTitle).Label("切换自动生成会话标题").Changed() {
				if a.preferences.AutoSessionTitle && (a.preferences.AuxiliaryModel == "" || a.auxiliaryBackend() == nil) {
					a.preferences.AutoSessionTitle = previous.AutoSessionTitle
					a.settingsError = "请先选择 Claude Code 配置和辅助模型。"
				} else {
					a.savePreferences(previous)
				}
			}
		})
		if a.titleJob != nil {
			native.Text(c, "正在生成会话标题…").FontSize(11).TextColor(c.Theme().TextMuted)
		}
		if a.auxiliaryError != "" {
			native.Text(c, a.auxiliaryError).FontSize(11).TextColor(c.Theme().Danger).FillWidth()
		}
	})
}

func (a *App) auxiliaryChoice(c *native.Context, label, key, value string, content func()) {
	native.Row(c.Key(key)).FillWidth().Gap(12).AlignItems(native.Center).Children(func() {
		native.Text(c, label).FontSize(12).SingleLine().Grow(1).MinWidth(0)
		access := "选择" + label
		if key == "model" {
			access = "选择辅助模型"
		}
		b := treeButton(c, access).Width(190).Shrink(0).Height(32).Padding(0, 10).Border(1, c.Theme().Border)
		b.Children(func() {
			native.Text(c, value).FontSize(12).SingleLine().Grow(1).MinWidth(0)
			native.Icon(c, chevron).Size(12, 12)
		})
		if b.Clicked() {
			if a.auxiliaryPicker == key {
				a.auxiliaryPicker = ""
			} else {
				a.auxiliaryPicker = key
				a.awakePicker, a.terminalPicker = false, false
				if key == "model" {
					a.loadAuxiliaryModels(false)
				}
			}
		}
		open := a.auxiliaryPicker == key
		native.PopoverBase(c.Key("options"), b, &open, func(panel native.Element) {
			t := c.Theme()
			panel.Width(320).Padding(4).Margin(4, 0, 0, 0).Radius(8).Background(t.Background).Border(1, t.Border).
				Shadow(0, 6, 20, 0, native.RGBA(0, 0, 0, .18))
			native.Scroll(c).FillWidth().MaxHeight(220).Children(func() {
				native.Column(c).FillWidth().Gap(2).Children(content)
			})
		})
		if !open && a.auxiliaryPicker == key {
			a.auxiliaryPicker = ""
		}
	})
}
