package ui

import (
	"context"
	"encoding/json"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
)

type modelSender = agent.ModelSender

type modelCatalog = agent.ModelCatalog

func (a *App) loadModels() {
	root := a.session
	if root.modelsCwd != a.State.Cwd || root.modelsRuntime != a.runtimeKey {
		root.models, root.modelsLoaded = nil, false
		root.modelsCwd, root.modelsRuntime = a.State.Cwd, a.runtimeKey
	}
	if root.modelsLoading {
		return
	}
	backend, ok := a.sessionBackend().(modelCatalog)
	if !ok {
		root.modelsError = "当前连接不支持读取模型列表"
		return
	}
	root.modelsLoading, root.modelsError = true, ""
	cwd, runtime, kind := root.State.Cwd, root.runtimeKey, root.kind.Normalized()
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		var models []agent.Model
		var err error
		if scoped, ok := backend.(interface {
			ListModelsForDirectory(context.Context, string) ([]agent.Model, error)
		}); ok {
			models, err = scoped.ListModelsForDirectory(ctx, cwd)
		} else {
			models, err = backend.ListModels(ctx)
		}
		a.update(func() {
			if root.kind.Normalized() != kind || root.runtimeKey != runtime {
				return
			}
			root.modelsLoading = false
			if root.State.Cwd != cwd || root.runtimeKey != runtime {
				root.models, root.modelsLoaded = nil, false
				return
			}
			if err != nil {
				root.modelsError = "读取模型失败：" + err.Error()
				return
			}
			root.models, root.modelsLoaded = models, true
			if kind == agent.Claude && len(models) > 0 && (root.selectedModel == "" || root.selectedModel == "default" || strings.HasSuffix(root.selectedModel, "[1m]")) {
				root.selectedModel = models[0].Model
				for _, model := range models {
					if !model.Hidden && (root.State.Model != "" && model.DisplayName == root.State.Model || model.Model == a.application().lastClaudeModels[runtime]) {
						root.selectedModel = model.Model
						break
					}
				}
				if err := a.persist(); err != nil {
					root.modelError = "保存模型选择失败：" + err.Error()
				}
			}
		})
	}()
}

func shortModel(model string) string { return model[strings.LastIndex(model, "/")+1:] }

func (a *App) rememberClaudeModel(model string) {
	if a.kind.Normalized() != agent.Claude || model == "" || model == "default" {
		return
	}
	root := a.application()
	if root.lastClaudeModels == nil {
		root.lastClaudeModels = map[string]string{}
	}
	root.lastClaudeModels[a.runtimeKey] = model
}

func (a *App) resolveInitialModel() {
	if a.selectedModel != "" || a.modelResolving || a.modelResolvedCwd == a.State.Cwd {
		return
	}
	backend, ok := a.sessionBackend().(modelSender)
	if !ok {
		return
	}
	s, cwd, kind, runtime := a.session, a.State.Cwd, a.kind.Normalized(), a.runtimeKey
	a.modelResolving = true
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		model, err := backend.DefaultModel(ctx, cwd)
		a.update(func() {
			if s.kind.Normalized() != kind || s.runtimeKey != runtime {
				return
			}
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
	root := a.session
	if root.modelsCwd != root.State.Cwd || root.modelsRuntime != root.runtimeKey {
		root.models, root.modelsLoaded, root.modelsError = nil, false, ""
		root.modelsCwd, root.modelsRuntime = root.State.Cwd, root.runtimeKey
	}
	if a.win != nil || a.dispatch != nil {
		if a.kind.Normalized() == agent.Claude {
			if !root.modelsLoaded && !root.modelsLoading && root.modelsError == "" {
				a.loadModels()
			}
		} else {
			a.resolveInitialModel()
		}
	}
	model := "选择模型"
	if a.selectedModel != "" {
		model = a.selectedModel
	}
	if a.kind.Normalized() == agent.Claude {
		model = "选择模型"
		for _, entry := range root.models {
			if entry.Model == a.selectedModel {
				model = entry.DisplayName
				break
			}
		}
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
	root := a.session
	disabled := a.State.Busy || a.State.Disconnected || a.recovering || a.recoveryBlocked || a.forking
	if _, ok := a.sessionBackend().(modelSender); !ok {
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
				previousLast := a.application().lastClaudeModels[a.runtimeKey]
				a.selectedModel, a.modelError = value, ""
				a.rememberClaudeModel(value)
				if err := a.persist(); err != nil {
					a.selectedModel = previous
					if a.kind.Normalized() == agent.Claude {
						a.application().lastClaudeModels[a.runtimeKey] = previousLast
					}
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
				if a.selectedModel != "" && !seen[a.selectedModel] && a.kind.Normalized() != agent.Claude {
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
	case "default":
		return "Claude 标准审批"
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
	case "claudePermissions":
		return "由 Claude Code 工具权限控制"
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
