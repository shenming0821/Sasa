package ui

import (
	"context"
	"encoding/json"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/attachment"
	"github.com/shenming0821/Fuyu/internal/codex"
)

type modelSender interface {
	SendWithModel(context.Context, string, string, []codex.Skill, []attachment.Image, string) (codex.Turn, error)
	DefaultModel(context.Context, string) (string, error)
}

type modelCatalog interface {
	ListModels(context.Context) ([]codex.Model, error)
}

func (a *App) loadModels() {
	root := a.application()
	if root.modelsLoading {
		return
	}
	backend, ok := root.backend.(modelCatalog)
	if !ok {
		root.modelsError = "当前连接不支持读取模型列表"
		return
	}
	root.modelsLoading, root.modelsError = true, ""
	go func() {
		ctx, cancel := codex.RequestContext()
		defer cancel()
		models, err := backend.ListModels(ctx)
		root.update(func() {
			root.modelsLoading = false
			if err != nil {
				root.modelsError = "读取模型失败：" + err.Error()
				return
			}
			root.models, root.modelsLoaded = models, true
		})
	}()
}

func shortModel(model string) string { return model[strings.LastIndex(model, "/")+1:] }

func (a *App) resolveInitialModel() {
	if a.selectedModel != "" || a.modelResolving || a.modelResolvedCwd == a.State.Cwd {
		return
	}
	backend, ok := a.backend.(modelSender)
	if !ok {
		return
	}
	s, cwd := a.session, a.State.Cwd
	a.modelResolving = true
	go func() {
		ctx, cancel := codex.RequestContext()
		defer cancel()
		model, err := backend.DefaultModel(ctx, cwd)
		a.update(func() {
			s.modelResolving = false
			if s.State.Cwd != cwd || s.selectedModel != "" {
				return
			}
			s.modelResolvedCwd = cwd
			if err != nil {
				s.modelError = "读取默认模型失败：" + err.Error()
				return
			}
			s.selectedModel, s.modelError = model, ""
		})
	}()
}

func (a *App) modelPicker(c *native.Context) {
	root := a.application()
	if a.win != nil || a.dispatch != nil {
		a.resolveInitialModel()
	}
	model := "选择模型"
	if a.selectedModel != "" {
		model = a.selectedModel
	}
	trigger := quietButton(c, "选择模型").Gap(5).MinWidth(0).Shrink(1)
	trigger.Children(func() {
		native.Text(c, shortModel(model)).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
		native.Icon(c, chevron).Size(12, 12)
	})
	if trigger.Clicked() {
		a.modelMenuOpen = !a.modelMenuOpen
		if a.modelMenuOpen && !root.modelsLoaded {
			a.loadModels()
		}
	}
	native.Popover(c.Key("model-menu"), trigger, &a.modelMenuOpen, func() { a.modelMenu(c) })
	native.Popover(c.Key("runtime-details"), trigger, &a.detailsOpen, func() { a.runtimeDetails(c) })
}

func (a *App) modelMenu(c *native.Context) {
	root := a.application()
	disabled := a.State.Busy || a.State.Disconnected || a.recovering || a.recoveryBlocked || a.forking
	if _, ok := a.backend.(modelSender); !ok {
		disabled = true
	}
	native.Column(c).Width(280).Gap(4).Children(func() {
		native.Text(c, "选择模型").FontSize(12).TextColor(c.Theme().TextMuted).Padding(4, 8)
		row := func(label, value string) {
			b := treeButton(c.Key(value), "选择模型 "+label).FillWidth().Padding(8).Disabled(disabled)
			b.Children(func() {
				native.Row(c).FillWidth().AlignItems(native.Center).Gap(8).Children(func() {
					native.Text(c, label).FontSize(12).Grow(1).MinWidth(0).SingleLine()
					if a.selectedModel == value {
						native.Text(c, "✓").FontSize(12)
					}
				})
			})
			if b.Clicked() {
				previous := a.selectedModel
				a.selectedModel, a.modelError = value, ""
				if err := a.persist(); err != nil {
					a.selectedModel = previous
					a.modelError = "保存模型选择失败：" + err.Error()
					return
				}
				a.modelMenuOpen = false
			}
		}
		native.Scroll(c).FillWidth().MaxHeight(240).Children(func() {
			native.Column(c).FillWidth().Children(func() {
				seen := map[string]bool{}
				for _, m := range root.models {
					if m.Hidden || m.Model == "" || seen[m.Model] {
						continue
					}
					seen[m.Model] = true
					name := m.DisplayName
					if name == "" {
						name = m.Model
					}
					row(name, m.Model)
				}
				if a.selectedModel != "" && !seen[a.selectedModel] {
					row(shortModel(a.selectedModel)+"（当前选择）", a.selectedModel)
				}
			})
		})
		if root.modelsLoading {
			native.Text(c, "读取模型列表…").FontSize(11).TextColor(c.Theme().TextMuted).Padding(4, 8)
		}
		if root.modelsError != "" {
			native.Text(c, root.modelsError).FontSize(11).TextColor(c.Theme().Danger)
			if quietButton(c, "重试读取模型").Disabled(root.modelsLoading).Clicked() {
				a.loadModels()
			}
		}
		if a.modelError != "" {
			native.Text(c, a.modelError).FontSize(11).TextColor(c.Theme().Danger)
			if a.selectedModel == "" && quietButton(c, "重试读取默认模型").Disabled(a.modelResolving).Clicked() {
				a.modelResolvedCwd = ""
				a.resolveInitialModel()
			}
		}
		native.Box(c).FillWidth().Height(1).Background(c.Theme().Border)
		details := treeButton(c, "运行详情…").FillWidth().Height(24).Padding(0, 8)
		details.Children(func() {
			native.Row(c).FillWidth().AlignItems(native.Center).Children(func() {
				native.Text(c, "运行详情").FontSize(11).TextColor(c.Theme().TextMuted).Grow(1)
				native.Text(c, "›").FontSize(14).TextColor(c.Theme().TextMuted)
			})
		})
		if details.Clicked() {
			a.modelMenuOpen, a.detailsOpen = false, true
		}
	})
}

func readablePolicy(raw string) string {
	var value string
	if json.Unmarshal([]byte(raw), &value) != nil {
		return raw
	}
	switch value {
	case "never":
		return "无需审批"
	case "on-request":
		return "按需审批"
	case "untrusted":
		return "不可信操作需审批"
	case "on-failure":
		return "失败后审批"
	default:
		return value
	}
}

func readableSandbox(raw string) string {
	var value struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(raw), &value) != nil {
		return raw
	}
	switch value.Type {
	case "dangerFullAccess":
		return "完全访问"
	case "workspaceWrite":
		return "工作区可写"
	case "readOnly":
		return "只读"
	case "externalSandbox":
		return "外部沙箱"
	default:
		return raw
	}
}
