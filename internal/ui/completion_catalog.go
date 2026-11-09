package ui

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/storage"
)

const completionCatalogTTL = 10 * time.Minute
const completionRetryDelay = 30 * time.Second
const completionCatalogLimit = 32

type completionCatalogState struct {
	completionCatalogs map[agent.CacheKey]*completionCatalog
	catalogWrites      sync.WaitGroup
}

type completionCatalog struct {
	skills          []agent.Skill
	commands        []agent.Command
	loaded, loading bool
	error           string
	checked, used   time.Time
	generation      uint64
}

func (s *session) completionCacheKey() agent.CacheKey {
	return agent.CacheKey{Agent: s.kind.Normalized(), Runtime: s.runtimeKey, Cwd: s.State.Cwd}
}

func (a *App) prewarmCompletionSkills() {
	if a.session == nil || !a.open || a.State.Cwd == "" || a.State.Disconnected || a.anyBusy() || !a.canAsync() || a.quitting || a.quitStopping {
		return
	}
	for _, entry := range a.completionCatalogs {
		if entry.loading {
			return
		}
	}
	a.loadCompletionSkills()
}

func (a *App) applyCompletionCatalog(key agent.CacheKey, entry *completionCatalog) {
	for _, owner := range a.application().sessions {
		if owner.completionCacheKey() == key {
			owner.completion.cwd = key.Cwd
			owner.completion.skills, owner.completion.commands = entry.skills, entry.commands
			owner.completion.loading, owner.completion.loaded = entry.loading, entry.loaded
			owner.completion.error = entry.error
			owner.completion.checked = entry.checked
			owner.completion.cacheKey = key
		}
	}
}

func (a *App) loadCompletionCatalog(force bool) {
	root, key := a.application(), a.session.completionCacheKey()
	entry := root.completionCatalogs[key]
	if entry == nil {
		if root.completionCatalogs == nil {
			root.completionCatalogs = make(map[agent.CacheKey]*completionCatalog)
		}
		root.trimCompletionCatalogs()
		if len(root.completionCatalogs) >= completionCatalogLimit {
			return
		}
		entry = &completionCatalog{}
		if a.completion.loaded && a.completion.cacheKey == key {
			entry.skills, entry.commands, entry.loaded = a.completion.skills, a.completion.commands, true
			entry.error, entry.checked = a.completion.error, a.completion.checked
		}
		root.completionCatalogs[key] = entry
	}
	now := time.Now()
	entry.used = now
	ttl := completionCatalogTTL
	if entry.error != "" {
		ttl = completionRetryDelay
	}
	if entry.loading || !force && entry.loaded && now.Sub(entry.checked) < ttl {
		root.applyCompletionCatalog(key, entry)
		return
	}
	backend := a.sessionBackend()
	if a.State.Disconnected || backend == nil {
		root.applyCompletionCatalog(key, entry)
		return
	}
	_, commandsSupported := backend.(agent.CommandCatalog)
	_, skillsSupported := backend.(interface {
		ListSkills(context.Context, string) ([]agent.SkillsEntry, error)
	})
	commandsAgent := key.Agent == agent.Claude || key.Agent == agent.Devin
	if commandsAgent && !commandsSupported || !commandsAgent && !skillsSupported {
		entry.loaded, entry.error, entry.checked = true, "当前连接不支持读取技能", now
		root.applyCompletionCatalog(key, entry)
		return
	}
	force = force || entry.loaded
	entry.loading, entry.error = true, ""
	root.applyCompletionCatalog(key, entry)
	ctx, cancel := root.projectContext(30 * time.Second)
	generation := entry.generation
	go func() {
		defer cancel()
		skills, commands, err := readCompletionCatalog(ctx, backend, key, force)
		select {
		case <-root.shutdown:
			return
		default:
		}
		root.update(func() {
			entry.loading, entry.loaded, entry.checked = false, true, time.Now()
			if entry.generation != generation {
				entry.checked = time.Time{}
			}
			if err != nil {
				entry.error = err.Error()
				if len(entry.skills) == 0 {
					entry.skills = skills
				}
			} else {
				entry.skills, entry.commands = skills, commands
				if root.store != nil && entry.generation == generation {
					record := storage.Catalog{Key: key, Skills: skills, Commands: commands, Checked: entry.checked}
					root.catalogWrites.Add(1)
					go func() {
						defer root.catalogWrites.Done()
						_ = root.store.SaveCatalog(record)
					}()
				}
			}
			root.applyCompletionCatalog(key, entry)
		})
	}()
}

func (a *App) restoreCompletionCatalogs() {
	a.applyCompletionDiskCache(a.store.LoadCatalogs())
}

func (a *App) loadCompletionDiskCache() {
	if a.store == nil {
		return
	}
	a.catalogWrites.Add(1)
	go func() {
		defer a.catalogWrites.Done()
		records := a.store.LoadCatalogs()
		select {
		case <-a.shutdown:
			return
		default:
		}
		a.update(func() {
			select {
			case <-a.shutdown:
				return
			default:
				a.applyCompletionDiskCache(records)
			}
		})
	}()
}

func (a *App) applyCompletionDiskCache(records []storage.Catalog) {
	for _, record := range records {
		if a.completionCatalogs == nil {
			a.completionCatalogs = make(map[agent.CacheKey]*completionCatalog)
		}
		entry := a.completionCatalogs[record.Key]
		if entry != nil && entry.loaded {
			continue
		}
		if entry == nil {
			if len(a.completionCatalogs) >= completionCatalogLimit {
				continue
			}
			entry = &completionCatalog{}
			a.completionCatalogs[record.Key] = entry
		}
		entry.skills, entry.commands = record.Skills, record.Commands
		entry.loaded, entry.used = true, record.Checked
		a.applyCompletionCatalog(record.Key, entry)
	}
}

func (a *App) trimCompletionCatalogs() {
	if len(a.completionCatalogs) < completionCatalogLimit {
		return
	}
	var oldest agent.CacheKey
	var found *completionCatalog
	for key, entry := range a.completionCatalogs {
		if !entry.loading && (found == nil || entry.used.Before(found.used)) {
			oldest, found = key, entry
		}
	}
	if found != nil {
		delete(a.completionCatalogs, oldest)
	}
}

func readCompletionCatalog(ctx context.Context, backend Backend, key agent.CacheKey, force bool) ([]agent.Skill, []agent.Command, error) {
	if key.Agent == agent.Claude || key.Agent == agent.Devin {
		catalog, ok := backend.(agent.CommandCatalog)
		if !ok {
			return nil, nil, fmt.Errorf("当前连接不支持读取技能")
		}
		var commands []agent.Command
		var err error
		if reloader, ok := backend.(interface {
			ReloadCommands(context.Context, string) ([]agent.Command, error)
		}); force && ok {
			commands, err = reloader.ReloadCommands(ctx, key.Cwd)
		} else {
			commands, err = catalog.ListCommands(ctx, key.Cwd)
		}
		return nil, commands, err
	}
	catalog, ok := backend.(interface {
		ListSkills(context.Context, string) ([]agent.SkillsEntry, error)
	})
	if !ok {
		return nil, nil, fmt.Errorf("当前连接不支持读取技能")
	}
	var entries []agent.SkillsEntry
	var err error
	if reloader, ok := backend.(interface {
		ReloadSkills(context.Context, string) ([]agent.SkillsEntry, error)
	}); force && ok {
		entries, err = reloader.ReloadSkills(ctx, key.Cwd)
	} else {
		entries, err = catalog.ListSkills(ctx, key.Cwd)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("读取技能失败：%w", err)
	}
	var skills []agent.Skill
	var issue error
	for _, entry := range entries {
		if entry.Cwd == key.Cwd {
			if len(entry.Errors) > 0 {
				issue = fmt.Errorf("技能加载异常：%s", entry.Errors[0].Message)
			}
			skills = append(skills, entry.Skills...)
		}
	}
	return skills, nil, issue
}
