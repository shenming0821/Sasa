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

type optionSender = agent.OptionSender

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
				if !model.Hidden && (owner.State.Model != "" && model.DisplayName == owner.State.Model || model.Model == a.lastModels[modelPreferenceKey(key.Agent, key.Runtime)]) {
					owner.selectedModel = model.Model
					break
				}
			}
			if owner.selectedEffort == "" {
				owner.selectedEffort = a.rememberedModelEffort(owner.kind, owner.runtimeKey, owner.selectedModel)
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

func modelPreferenceKey(kind agent.AgentKind, runtime string) string {
	return string(kind.Normalized()) + "\x00" + runtime
}

func (a *App) rememberModel(model string) {
	if model == "" || model == "default" {
		return
	}
	root := a.application()
	if root.lastModels == nil {
		root.lastModels = map[string]string{}
	}
	root.lastModels[modelPreferenceKey(a.kind, a.runtimeKey)] = model
}

func modelEffortKey(kind agent.AgentKind, runtime, model string) string {
	return string(kind.Normalized()) + "\x00" + runtime + "\x00" + model
}

func (a *App) rememberedModelEffort(kind agent.AgentKind, runtime, model string) string {
	if model == "" || model == "default" {
		return ""
	}
	return a.application().modelEfforts[modelEffortKey(kind, runtime, model)]
}

func (a *App) rememberModelEffort(model, effort string) {
	if model == "" || model == "default" || effort == "" {
		return
	}
	root := a.application()
	if root.modelEfforts == nil {
		root.modelEfforts = map[string]string{}
	}
	root.modelEfforts[modelEffortKey(a.kind, a.runtimeKey, model)] = effort
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
			if s.selectedEffort == "" {
				s.selectedEffort = a.rememberedModelEffort(s.kind, s.runtimeKey, model)
			}
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
	}
	for _, entry := range root.models {
		if entry.Model == a.selectedModel && entry.DisplayName != "" {
			model = entry.DisplayName
			break
		}
	}
	effort := a.selectedEffort
	for i := range root.models {
		if root.models[i].Model != a.selectedModel {
			continue
		}
		if effort != "" && !modelHasEffort(&root.models[i], effort) {
			effort = ""
		}
		if effort == "" {
			effort = root.models[i].DefaultReasoningEffort
		}
		break
	}
	label := modelButtonLabel(model)
	if effort != "" {
		label += " " + effortLabel(effort)
	}
	trigger := quietButton(c, "选择模型").Gap(5).MinWidth(0).Shrink(1)
	trigger.Children(func() {
		native.Text(c, label).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
		native.Icon(c, chevron).Size(12, 12)
	})
	if trigger.Clicked() {
		a.modelMenuOpen = !a.modelMenuOpen
		if a.modelMenuOpen {
			if !root.modelsLoaded {
				a.loadModels()
			}
			if pinner, ok := a.sessionBackend().(interface{ PinnedEffort(string) string }); ok {
				root.effortPin = pinner.PinnedEffort(root.State.Cwd)
			} else {
				root.effortPin = ""
			}
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
				previousEffort, previousTier := a.selectedEffort, a.selectedTier
				prefKey := modelPreferenceKey(a.kind, a.runtimeKey)
				previousLast := a.application().lastModels[prefKey]
				a.selectedModel, a.modelError = value, ""
				if value != previous {
					a.selectedEffort = a.rememberedModelEffort(a.kind, a.runtimeKey, value)
					a.selectedTier = ""
				}
				a.rememberModel(value)
				if err := a.persist(); err != nil {
					a.selectedModel = previous
					a.selectedEffort, a.selectedTier = previousEffort, previousTier
					if previousLast == "" {
						delete(a.application().lastModels, prefKey)
					} else {
						a.application().lastModels[prefKey] = previousLast
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
		a.turnOptionsMenu(c, disabled)
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

// modelButtonLabel shortens a catalog display name for the picker trigger:
// path prefixes and " · provider" suffixes are dropped. Menu rows keep the
// full name so providers can still disambiguate identical models.
func modelButtonLabel(name string) string {
	name = shortModel(name)
	if i := strings.LastIndex(name, " · "); i > 0 {
		name = name[:i]
	}
	return name
}

// modelDisplayName resolves a catalog model id to its display name; unknown or
// unnamed entries stay as reported.
func modelDisplayName(models []agent.Model, model string) string {
	for i := range models {
		if models[i].Model == model && models[i].DisplayName != "" {
			return models[i].DisplayName
		}
	}
	return model
}

func modelHasEffort(m *agent.Model, effort string) bool {
	for _, option := range m.SupportedReasoningEfforts {
		if option.ReasoningEffort == effort {
			return true
		}
	}
	return false
}

func modelHasTier(m *agent.Model, tier string) bool {
	if tier == "default" {
		return true
	}
	for _, service := range m.ServiceTiers {
		if service.ID == tier {
			return true
		}
	}
	return false
}

// sendTurnOptions drops stored choices the loaded catalog no longer offers for
// the selected model; without catalog evidence the stored value is sent as-is.
func (a *App) sendTurnOptions() (effort, tier string) {
	effort, tier = a.selectedEffort, a.selectedTier
	if !a.modelsLoaded {
		return effort, tier
	}
	for i := range a.models {
		if a.models[i].Model != a.selectedModel {
			continue
		}
		if effort != "" && !modelHasEffort(&a.models[i], effort) {
			effort = ""
		}
		if tier != "" && !modelHasTier(&a.models[i], tier) {
			tier = ""
		}
		break
	}
	return effort, tier
}

var effortLabels = map[string]string{
	"low": "低", "medium": "中", "high": "高",
	"xhigh": "极高", "max": "最大", "ultra": "极致",
}

func effortLabel(effort string) string {
	if label, ok := effortLabels[effort]; ok {
		return label
	}
	return effort
}

// turnOptionsMenu renders per-model effort and service tier choices reported by
// the catalog. Picks apply to every send of this session; unset inherits the
// runtime default.
func (a *App) turnOptionsMenu(c *native.Context, disabled bool) {
	if _, ok := a.sessionBackend().(optionSender); !ok {
		return
	}
	var current *agent.Model
	for i := range a.session.models {
		if a.session.models[i].Model == a.selectedModel {
			current = &a.session.models[i]
			break
		}
	}
	if current == nil || len(current.SupportedReasoningEfforts) == 0 && len(current.ServiceTiers) == 0 {
		return
	}
	t := c.Theme()
	native.Box(c).FillWidth().Height(1).Background(t.Border)
	choose := func(dst *string, value, what string) {
		previous := *dst
		*dst = value
		if err := a.persist(); err != nil {
			*dst = previous
			a.modelError = "保存" + what + "失败：" + err.Error()
			return
		}
		a.modelError = ""
	}
	chip := func(key, access, text, tip string, selected bool, pick func()) {
		b := native.ButtonBase(c.Key(key)).Label(access).Radius(6).Padding(2, 6).FontSize(11).
			TextColor(t.TextMuted).Disabled(disabled)
		if tip != "" {
			b.Tooltip(tip)
		}
		selectedButtonFace(c, b, selected, 6, 0)
		b.Children(func() { native.Text(c, text).FontSize(11).SingleLine() })
		if b.Clicked() {
			pick()
		}
	}
	optionRow := func(label string, build func()) {
		native.Row(c).FillWidth().AlignItems(native.Center).Gap(4).Padding(3, 8).Children(func() {
			native.Text(c, label).FontSize(11).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
			build()
		})
	}
	if len(current.SupportedReasoningEfforts) > 0 {
		effort := a.selectedEffort
		if effort != "" && !modelHasEffort(current, effort) {
			effort = ""
		}
		if effort == "" {
			effort = current.DefaultReasoningEffort
		}
		optionRow("推理强度", func() {
			for _, option := range current.SupportedReasoningEfforts {
				value := option.ReasoningEffort
				chip("effort-"+value, "推理强度 "+effortLabel(value), effortLabel(value), option.Description, value == effort,
					func() {
						previousEffort := a.selectedEffort
						key := modelEffortKey(a.kind, a.runtimeKey, a.selectedModel)
						previousRemembered, hadRemembered := a.application().modelEfforts[key]
						a.selectedEffort = value
						a.rememberModelEffort(a.selectedModel, value)
						if err := a.persist(); err != nil {
							a.selectedEffort = previousEffort
							if hadRemembered {
								a.application().modelEfforts[key] = previousRemembered
							} else {
								delete(a.application().modelEfforts, key)
							}
							a.modelError = "保存推理强度失败：" + err.Error()
							return
						}
						a.modelError = ""
					})
			}
		})
		if a.session.effortPin != "" {
			native.Text(c, "已被 CLAUDE_CODE_EFFORT_LEVEL 固定为 "+a.session.effortPin+"，选择不会生效").FontSize(11).TextColor(t.Danger).Padding(0, 8)
		}
	}
	if len(current.ServiceTiers) > 0 {
		tier := a.selectedTier
		if tier == "" {
			tier = current.DefaultServiceTier
		}
		if tier == "" {
			tier = "default"
		}
		optionRow("速度", func() {
			chip("tier-default", "速度 标准", "标准", "", tier == "default",
				func() { choose(&a.selectedTier, "default", "速度") })
			for _, service := range current.ServiceTiers {
				chip("tier-"+service.ID, "速度 "+service.Name, service.Name, service.Description, service.ID == tier,
					func() { choose(&a.selectedTier, service.ID, "速度") })
			}
		})
	}
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
