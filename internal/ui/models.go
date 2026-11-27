package ui

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/storage"
)

type modelSender = agent.ModelSender

type optionSender = agent.OptionSender

type modelCatalog = agent.ModelCatalog

type modelCatalogEntry struct {
	models             []agent.Model
	loading            bool
	loaded             bool
	manual             bool
	error              string
	checked            time.Time
	cachedAt           time.Time
	cachedDefaultModel string
}

func (a *App) loadModels() {
	a.loadModelCatalog(false)
}

func (a *App) loadModelCatalog(manual bool) {
	owner, root := a.session, a.application()
	requestKey := owner.completionCacheKey()
	key := requestKey.ModelCatalogKey()
	a.bindModelCatalog()
	entry := root.modelCatalogs[key]
	if entry != nil && entry.loading {
		entry.manual = entry.manual || manual
		root.applyModelCatalog(key, entry)
		return
	}
	backend, ok := a.sessionBackend().(modelCatalog)
	if !ok || owner.State.Disconnected || requestKey.Cwd == "" {
		return
	}
	if root.modelCatalogs == nil {
		root.modelCatalogs = make(map[agent.CacheKey]*modelCatalogEntry)
	}
	if entry == nil {
		if len(root.modelCatalogs) >= completionCatalogLimit {
			var oldest agent.CacheKey
			var candidate *modelCatalogEntry
			for k, e := range root.modelCatalogs {
				if !e.loading && (candidate == nil || e.cachedAt.Before(candidate.cachedAt)) {
					oldest, candidate = k, e
				}
			}
			if candidate == nil {
				return
			}
			delete(root.modelCatalogs, oldest)
		}
		entry = &modelCatalogEntry{cachedAt: time.Now()}
		if owner.modelsLoaded {
			entry.models, entry.loaded = owner.models, true
		}
		root.modelCatalogs[key] = entry
	}
	if !manual && entry.loaded && entry.error == "" && time.Since(entry.checked) < completionCatalogTTL {
		return
	}
	force := manual || entry.error != "" || entry.loaded
	entry.loading, entry.manual, entry.error = true, manual, ""
	root.applyModelCatalog(key, entry)
	ctx, cancel := root.projectContext(30 * time.Second)
	go func() {
		defer cancel()
		var models []agent.Model
		var err error
		if reloader, ok := backend.(interface {
			ReloadModelsForDirectory(context.Context, string) ([]agent.Model, error)
		}); force && ok {
			models, err = reloader.ReloadModelsForDirectory(ctx, requestKey.Cwd)
		} else if scoped, ok := backend.(interface {
			ListModelsForDirectory(context.Context, string) ([]agent.Model, error)
		}); ok {
			models, err = scoped.ListModelsForDirectory(ctx, requestKey.Cwd)
		} else {
			models, err = backend.ListModels(ctx)
		}
		if err == nil && !slices.ContainsFunc(models, availableModel) {
			err = errors.New("当前运行时未返回可用模型，请刷新模型列表重试")
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
				entry.cachedDefaultModel = catalogDefaultModel(models)
				entry.cachedAt = entry.checked
				if root.store != nil {
					record := storage.Catalog{Key: requestKey, Models: models, Checked: entry.checked}
					root.catalogWrites.Add(1)
					go func() {
						defer root.catalogWrites.Done()
						_ = root.store.SaveModelCatalog(record)
					}()
				}
			}
			root.applyModelCatalog(key, entry)
		})
	}()
}

func availableModel(model agent.Model) bool {
	return model.Model != "" && !model.Hidden
}

func catalogDefaultModel(models []agent.Model) string {
	for _, model := range models {
		if model.IsDefault && availableModel(model) {
			return model.Model
		}
	}
	return ""
}

func (a *App) bindModelCatalog() {
	owner, key := a.session, a.completionCacheKey()
	if owner.modelsCwd != key.Cwd || owner.modelsRuntime != key.Runtime {
		owner.models, owner.modelsLoaded, owner.modelsLoading, owner.modelsError = nil, false, false, ""
		owner.modelsCwd, owner.modelsRuntime = key.Cwd, key.Runtime
	}
	if entry := a.application().modelCatalogs[key.ModelCatalogKey()]; entry != nil && !owner.modelsLoaded {
		a.application().applyModelCatalog(key.ModelCatalogKey(), entry)
	}
}

func (a *App) applyModelDiskCache(records []storage.Catalog) {
	if a.modelCatalogs == nil {
		a.modelCatalogs = make(map[agent.CacheKey]*modelCatalogEntry)
	}
	for _, record := range records {
		if !slices.ContainsFunc(record.Models, availableModel) {
			continue
		}
		key := record.Key.ModelCatalogKey()
		entry := a.modelCatalogs[key]
		if entry == nil {
			if len(a.modelCatalogs) >= completionCatalogLimit {
				continue
			}
			entry = &modelCatalogEntry{}
			a.modelCatalogs[key] = entry
		}
		if entry.loaded {
			continue
		}
		entry.models, entry.loaded, entry.cachedAt = record.Models, true, record.Checked
		entry.cachedDefaultModel = catalogDefaultModel(record.Models)
		a.applyModelCatalog(key, entry)
	}
	a.prewarmModels()
}

func (a *App) applyModelCatalog(key agent.CacheKey, entry *modelCatalogEntry) {
	key = key.ModelCatalogKey()
	var selected []*session
	for _, owner := range a.sessions {
		if owner.completionCacheKey().ModelCatalogKey() != key {
			if owner.kind.Normalized() == key.Agent && owner.modelsRuntime == key.Runtime && owner.modelsCwd == key.Cwd {
				owner.models, owner.modelsLoaded, owner.modelsLoading, owner.modelsError = nil, false, false, ""
			}
			continue
		}
		owner.modelsCwd, owner.modelsRuntime = owner.State.Cwd, key.Runtime
		owner.models, owner.modelsLoaded = entry.models, entry.loaded
		owner.modelsLoading = entry.loading && (entry.manual || !entry.loaded)
		owner.modelsError = ""
		if entry.manual || !entry.loaded {
			owner.modelsError = entry.error
		}
		a.forSession(owner).rememberModelDisplayName(owner.selectedModel)
		if key.Agent != agent.Claude || !entry.loaded {
			continue
		}
		if owner.selectedModel == "" || owner.selectedModel == "default" || strings.HasSuffix(owner.selectedModel, "[1m]") {
			fallback := ""
			for _, model := range entry.models {
				if !availableModel(model) {
					continue
				}
				if fallback == "" {
					fallback = model.Model
				}
				if owner.State.Model != "" && model.DisplayName == owner.State.Model || model.Model == a.lastModels[modelPreferenceKey(key.Agent, key.Runtime)] {
					fallback = model.Model
					break
				}
			}
			owner.selectedModel = fallback
			if owner.selectedEffort == "" {
				owner.selectedEffort = a.rememberedModelEffort(owner.kind, owner.runtimeKey, owner.selectedModel)
			}
			a.forSession(owner).rememberModelDisplayName(owner.selectedModel)
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
	if a.session == nil || !a.open || a.State.Cwd == "" || a.State.Disconnected || !a.canAsync() || a.quitting || a.quitStopping {
		return
	}
	a.bindModelCatalog()
	if a.anyBusy() {
		return
	}
	entry := a.application().modelCatalogs[a.completionCacheKey().ModelCatalogKey()]
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
	a.rememberModelDisplayName(model)
	if model == "" || model == "default" {
		return
	}
	root := a.application()
	if root.lastModels == nil {
		root.lastModels = map[string]string{}
	}
	root.lastModels[modelPreferenceKey(a.kind, a.runtimeKey)] = model
}

func modelDisplayNameKey(key agent.CacheKey, model string) string {
	return modelPreferenceKey(key.Agent, key.Runtime) + "\x00" + key.Cwd + "\x00" + model
}

func (a *App) rememberModelDisplayName(model string) {
	for _, entry := range a.models {
		if entry.Model != model || entry.DisplayName == "" || entry.Hidden {
			continue
		}
		root := a.application()
		if root.modelDisplayNames == nil {
			root.modelDisplayNames = map[string]string{}
		}
		root.modelDisplayNames[modelDisplayNameKey(a.completionCacheKey().ModelCatalogKey(), model)] = entry.DisplayName
		return
	}
}

func (a *App) selectedModelDisplayName() string {
	model := a.turnModel()
	for _, entry := range a.models {
		if entry.Model == model && entry.DisplayName != "" && !entry.Hidden {
			return entry.DisplayName
		}
	}
	key := a.completionCacheKey()
	names := a.application().modelDisplayNames
	if name := names[modelDisplayNameKey(key.ModelCatalogKey(), model)]; name != "" {
		return name
	}
	if name := names[modelDisplayNameKey(key, model)]; name != "" {
		return name
	}
	if a.modelsLoaded {
		return model
	}
	return ""
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

func defaultModelFromCatalog(ctx context.Context, backend modelSender, cwd string, models []agent.Model) (string, error) {
	if reader, ok := backend.(interface {
		DefaultModelWithCatalog(context.Context, string, []agent.Model) (string, error)
	}); ok && len(models) > 0 {
		return reader.DefaultModelWithCatalog(ctx, cwd, models)
	}
	return backend.DefaultModel(ctx, cwd)
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
	models := a.models
	a.modelResolving = true
	go func() {
		ctx, cancel := agent.RequestContext()
		defer cancel()
		model, err := defaultModelFromCatalog(ctx, backend, cwd, models)
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
			a.forSession(s).rememberModelDisplayName(model)
			if s.selectedEffort == "" {
				s.selectedEffort = a.rememberedModelEffort(s.kind, s.runtimeKey, model)
			}
		})
	}()
}

func (a *App) modelPicker(c *native.Context) {
	root := a.session
	a.bindModelCatalog()
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
	if name := a.selectedModelDisplayName(); name != "" {
		model = name
	} else if root.modelsLoading {
		model = "读取模型…"
	}
	effort := a.chosenModelEffort()
	for i := range root.models {
		if root.models[i].Model != a.turnModel() {
			continue
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
			entry := a.application().modelCatalogs[root.completionCacheKey().ModelCatalogKey()]
			if !root.modelsLoaded || root.modelsError != "" || entry != nil && time.Since(entry.checked) >= completionCatalogTTL {
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
	disabled := a.State.Disconnected || a.recovering || a.recoveryBlocked || a.writerOccupied || a.forking
	if _, ok := a.sessionBackend().(modelSender); !ok {
		disabled = true
	}
	native.Column(c).Width(280).Gap(4).Children(func() {
		native.Row(c).FillWidth().Padding(0, 4, 0, 8).AlignItems(native.Center).Children(func() {
			native.Text(c, "选择模型").FontSize(12).TextColor(c.Theme().TextMuted).Grow(1)
			label := "刷新模型列表"
			if root.modelsError != "" {
				label = "重试读取模型"
			}
			if root.modelsLoading {
				label = "正在刷新模型列表"
			}
			refresh := quietIconButton(c.Key("model-refresh"), label).Size(26, 26).
				Disabled(root.modelsLoading || a.State.Disconnected)
			refresh.Children(func() {
				if root.modelsLoading {
					native.Spinner(c).Size(14, 14)
				} else {
					native.Icon(c, refreshProjectIcon).Size(14, 14)
				}
			})
			if refresh.Clicked() {
				a.loadModelCatalog(true)
			}
		})
		if a.State.Busy && !disabled {
			native.Text(c, "选择的模型将在下一轮生效").FontSize(11).TextColor(c.Theme().TextMuted).Padding(4, 8)
		}
		row := func(label, value string) {
			b := a.emphasizedRowButton(c.Key(value), "选择模型 "+label).FillWidth().Padding(8).Disabled(disabled)
			b.Children(func() {
				native.Row(c).FillWidth().AlignItems(native.Center).Gap(8).Children(func() {
					native.Text(c, label).FontSize(12).Grow(1).MinWidth(0).SingleLine()
					if a.turnModel() == value {
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
					a.selectedTier = a.application().modelTiers[modelEffortKey(a.kind, a.runtimeKey, value)]
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
					name := a.selectedModelDisplayName()
					if name == "" {
						name = "当前模型"
					}
					row(modelButtonLabel(name)+"（当前选择）", a.selectedModel)
				}
			})
		})
		a.turnOptionsMenu(c, disabled)
		if root.modelsLoading && !root.modelsLoaded {
			native.Text(c, "读取模型列表…").FontSize(11).TextColor(c.Theme().TextMuted).Padding(4, 8)
		}
		if root.modelsError != "" {
			native.Text(c, root.modelsError).FontSize(11).TextColor(c.Theme().Danger)
		}
		if a.modelError != "" {
			native.Text(c, a.modelError).FontSize(11).TextColor(c.Theme().Danger)
			if a.selectedModel == "" && quietButton(c, "重试读取默认模型").Disabled(a.modelResolving).Clicked() {
				a.modelResolvedCwd = ""
				a.resolveInitialModel()
			}
		}
		native.Box(c).FillWidth().Height(1).Background(c.Theme().Border)
		details := a.emphasizedRowButton(c, "运行详情…").FillWidth().Height(24).Padding(0, 8)
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

func (a *App) selectedTurnOptions() agent.TurnOptions {
	return agent.TurnOptions{Model: a.turnModel(), Effort: a.chosenModelEffort(), ServiceTier: a.chosenModelTier(), Mode: a.selectedMode}
}

func (a *App) chosenModelTier() string {
	if a.selectedTier != "" {
		return a.selectedTier
	}
	return a.application().modelTiers[modelEffortKey(a.kind, a.runtimeKey, a.turnModel())]
}

func (a *App) chosenModelEffort() string {
	if a.selectedEffort != "" {
		return a.selectedEffort
	}
	return a.rememberedModelEffort(a.kind, a.runtimeKey, a.turnModel())
}

func (a *App) turnModel() string {
	if a.selectedModel == "" && a.kind.Normalized() == agent.Codex {
		if entry := a.application().modelCatalogs[a.completionCacheKey().ModelCatalogKey()]; entry != nil {
			return entry.cachedDefaultModel
		}
	}
	return a.selectedModel
}

func (a *App) supportedTurnOptions(options agent.TurnOptions) agent.TurnOptions {
	if !a.modelsLoaded {
		return options
	}
	for i := range a.models {
		if a.models[i].Model != options.Model {
			continue
		}
		if options.ServiceTier != "" && !modelHasTier(&a.models[i], options.ServiceTier) {
			options.ServiceTier = ""
		}
		break
	}
	return options
}

func effortLabel(effort string) string {
	return effort
}

// turnOptionsMenu renders per-model effort and service tier choices reported by
// the catalog. Picks apply to every send of this session; unset inherits the
// runtime default.
func (a *App) turnOptionsMenu(c *native.Context, disabled bool) {
	if _, ok := a.sessionBackend().(optionSender); !ok && !a.State.Disconnected {
		return
	}
	var current *agent.Model
	for i := range a.session.models {
		if a.session.models[i].Model == a.turnModel() {
			current = &a.session.models[i]
			break
		}
	}
	if current == nil || len(current.SupportedReasoningEfforts) == 0 && len(current.ServiceTiers) == 0 {
		return
	}
	t := c.Theme()
	native.Box(c).FillWidth().Height(1).Background(t.Border)
	choose := func(dst *string, remembered *map[string]string, value, what string) {
		key := modelEffortKey(a.kind, a.runtimeKey, current.Model)
		previousRemembered, existed := (*remembered)[key]
		previous := *dst
		*dst = value
		if *remembered == nil {
			*remembered = map[string]string{}
		}
		(*remembered)[key] = value
		if err := a.persist(); err != nil {
			*dst = previous
			if existed {
				(*remembered)[key] = previousRemembered
			} else {
				delete(*remembered, key)
			}
			a.modelError = "保存" + what + "失败：" + err.Error()
			return
		}
		a.modelError = ""
	}
	optionRow := func(label string, build func()) {
		native.Row(c).FillWidth().AlignItems(native.Center).Gap(4).Padding(3, 8).Children(func() {
			native.Text(c, label).FontSize(11).TextColor(t.TextMuted).Shrink(0).SingleLine()
			native.Row(c).Grow(1).MinWidth(0).Justify(native.End).Gap(4).Children(build)
		})
	}
	// picker is a dropdown: the trigger shows the current choice and opens
	// a popup of options above it, over the model list. Opening upward
	// keeps the other picker's trigger clear of the popup, so clicking it
	// closes this popup and opens its own in one click. The popover needs
	// no key: an unused key would leak onto the next element while the
	// popup is closed, shifting element ids and dropping that press.
	picker := func(label, current, display string, open, other *bool, options []agent.ReasoningEffortOption, labelOf func(string) string, pick func(value string)) {
		// A stable key keeps the trigger's state across the other picker
		// opening and closing: without it the toggle rebuilds the trigger
		// under a new id and drops the press that should open this popup.
		trigger := a.emphasizedRowButton(c.Key(label+"-trigger"), label+" "+display).Gap(4).Shrink(0).Padding(2, 6).Radius(6).Disabled(disabled)
		trigger.Children(func() {
			native.Text(c, display).FontSize(11).SingleLine()
			native.Icon(c, chevron).Size(12, 12)
		})
		if trigger.Clicked() {
			*open = !*open
			*other = false
		}
		native.PopoverBase(c, trigger, open, func(panel native.Element) {
			panel.AttachTo(trigger, native.AnchorTopEnd, native.AnchorBottomEnd).Margin(0, 0, 4, 0)
			panel.Padding(4).Radius(8).Background(t.Background).Border(1, t.Border).
				Shadow(0, 6, 20, 0, native.RGBA(0, 0, 0, .18))
			native.Column(c).FillWidth().Children(func() {
				for _, option := range options {
					value := option.ReasoningEffort
					row := a.emphasizedRowButton(c.Key(value), label+"选项 "+labelOf(value)).FillWidth().Padding(6).Justify(native.Start)
					v := value
					row.Children(func() {
						native.Row(c).FillWidth().AlignItems(native.Center).Gap(6).Children(func() {
							native.Text(c, labelOf(v)).FontSize(11).SingleLine().Grow(1).MinWidth(0)
							if v == current {
								native.Text(c, "✓").FontSize(11)
							}
						})
					})
					if row.Clicked() {
						pick(v)
						*open = false
					}
				}
			})
		})
	}
	if len(current.SupportedReasoningEfforts) > 0 {
		effort := a.chosenModelEffort()
		if effort == "" {
			effort = current.DefaultReasoningEffort
		}
		display := effortLabel(effort)
		if display == "" {
			display = "选择强度"
		}
		effortPick := func(value string) {
			choose(&a.selectedEffort, &a.application().modelEfforts, value, "推理强度")
		}
		optionRow("推理强度", func() {
			picker("推理强度", effort, display, &a.session.effortPickerOpen, &a.session.tierPickerOpen, current.SupportedReasoningEfforts, effortLabel, effortPick)
		})
		if a.session.effortPin != "" {
			native.Text(c, "已被 CLAUDE_CODE_EFFORT_LEVEL 固定为 "+a.session.effortPin+"，选择不会生效").FontSize(11).TextColor(t.Danger).Padding(0, 8)
		}
	}
	if len(current.ServiceTiers) > 0 {
		tier := a.chosenModelTier()
		if tier == "" || !modelHasTier(current, tier) {
			tier = current.DefaultServiceTier
		}
		if tier == "" {
			tier = "default"
		}
		tierOptions := []agent.ReasoningEffortOption{{ReasoningEffort: "default"}}
		for _, service := range current.ServiceTiers {
			tierOptions = append(tierOptions, agent.ReasoningEffortOption{ReasoningEffort: service.ID, Description: service.Description})
		}
		tierLabel := func(value string) string {
			for _, service := range current.ServiceTiers {
				if service.ID == value {
					return service.Name
				}
			}
			return value
		}
		optionRow("速度", func() {
			picker("速度", tier, tierLabel(tier), &a.session.tierPickerOpen, &a.session.effortPickerOpen, tierOptions, tierLabel,
				func(value string) { choose(&a.selectedTier, &a.application().modelTiers, value, "速度") })
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
