package ui

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
)

type modelSender = agent.ModelSender

type modelCatalog = agent.ModelCatalog

type modelCatalogEntry struct {
	models  []agent.Model
	loading bool
	loaded  bool
	error   string
	checked time.Time
}

func (a *App) loadModels() {
	owner, root := a.session, a.application()
	key := owner.completionCacheKey()
	if owner.modelsCwd != key.Cwd || owner.modelsRuntime != key.Runtime {
		owner.models, owner.modelsLoaded = nil, false
		owner.modelsCwd, owner.modelsRuntime = key.Cwd, key.Runtime
	}
	backend, ok := a.sessionBackend().(modelCatalog)
	if !ok || owner.State.Disconnected {
		return
	}
	if root.modelCatalogs == nil {
		root.modelCatalogs = make(map[agent.CacheKey]*modelCatalogEntry)
	}
	entry := root.modelCatalogs[key]
	if entry == nil {
		if len(root.modelCatalogs) >= completionCatalogLimit {
			var oldest agent.CacheKey
			var candidate *modelCatalogEntry
			for k, e := range root.modelCatalogs {
				if !e.loading && (candidate == nil || e.checked.Before(candidate.checked)) {
					oldest, candidate = k, e
				}
			}
			if candidate == nil {
				return
			}
			delete(root.modelCatalogs, oldest)
		}
		entry = &modelCatalogEntry{}
		root.modelCatalogs[key] = entry
	}
	if entry.loading || !owner.modelsLoaded && entry.loaded && entry.error == "" && time.Since(entry.checked) < completionCatalogTTL {
		root.applyModelCatalog(key, entry)
		return
	}
	force := entry.error != "" || entry.loaded
	entry.loading, entry.error = true, ""
	root.applyModelCatalog(key, entry)
	ctx, cancel := root.projectContext(30 * time.Second)
	go func() {
		defer cancel()
		var models []agent.Model
		var err error
		if reloader, ok := backend.(interface {
			ReloadModelsForDirectory(context.Context, string) ([]agent.Model, error)
		}); force && ok {
			models, err = reloader.ReloadModelsForDirectory(ctx, key.Cwd)
		} else if scoped, ok := backend.(interface {
			ListModelsForDirectory(context.Context, string) ([]agent.Model, error)
		}); ok {
			models, err = scoped.ListModelsForDirectory(ctx, key.Cwd)
		} else {
			models, err = backend.ListModels(ctx)
		}
		select {
		case <-root.shutdown:
			return
		default:
		}
		root.update(func() {
			entry.loading, entry.checked = false, time.Now()
			if err != nil {
				entry.error = "读取模型失败：" + err.Error()
			} else {
				entry.models, entry.loaded = models, true
			}
			root.applyModelCatalog(key, entry)
		})
	}()
}

func (a *App) applyModelCatalog(key agent.CacheKey, entry *modelCatalogEntry) {
	var selected []*session
	for _, owner := range a.sessions {
		if owner.completionCacheKey() != key {
			if owner.kind.Normalized() == key.Agent && owner.modelsRuntime == key.Runtime && owner.modelsCwd == key.Cwd {
				owner.models, owner.modelsLoaded, owner.modelsLoading = nil, false, false
			}
			continue
		}
		owner.modelsCwd, owner.modelsRuntime = key.Cwd, key.Runtime
		owner.models, owner.modelsLoaded = entry.models, entry.loaded
		owner.modelsLoading, owner.modelsError = entry.loading, entry.error
		if key.Agent != agent.Claude || !entry.loaded || len(entry.models) == 0 || entry.loading {
			continue
		}
		if owner.selectedModel == "" || owner.selectedModel == "default" || strings.HasSuffix(owner.selectedModel, "[1m]") {
			owner.selectedModel = entry.models[0].Model
			for _, model := range entry.models {
				if !model.Hidden && (owner.State.Model != "" && model.DisplayName == owner.State.Model || model.Model == a.lastClaudeModels[key.Runtime]) {
					owner.selectedModel = model.Model
					break
				}
			}
			selected = append(selected, owner)
		}
	}
	if len(selected) > 0 {
		if err := a.persist(); err != nil {
			for _, owner := range selected {
				owner.modelError = "保存模型选择失败：" + err.Error()
			}
		}
	}
}

func (a *App) prewarmModels() {
	if a.session == nil || !a.open || a.State.Cwd == "" || a.State.Disconnected || a.anyBusy() || !a.canAsync() || a.quitting || a.quitStopping {
		return
	}
	entry := a.application().modelCatalogs[a.completionCacheKey()]
	expired := false
	if entry != nil && !entry.loading {
		ttl := completionCatalogTTL
		if entry.error != "" {
			ttl = completionRetryDelay
		}
		expired = time.Since(entry.checked) >= ttl
	}
	if expired || !a.modelsLoaded && !a.modelsLoading && a.modelsError == "" {
		a.loadModels()
	}
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
	if !a.State.Disconnected && (a.win != nil || a.dispatch != nil) {
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
		if root.modelsLoading && !root.modelsLoaded {
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
