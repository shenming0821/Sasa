package ui

import (
	"context"
	"path/filepath"
	"slices"
	"time"

	gitview "github.com/shenming0821/Sasa/internal/git"
	"github.com/shenming0821/Sasa/internal/project"
	"github.com/shenming0821/Sasa/internal/workspace"
)

type branchCatalogState struct {
	branchCatalogs map[string]*branchCatalog
	readBranches   func(context.Context, string) ([]gitview.Branch, error)
}

type branchCatalog struct {
	branches               []gitview.Branch
	sources                []gitview.WorktreeSource
	loaded, loading, dirty bool
	version                uint64
	sourcesVersion         uint64
	checked                time.Time
	err                    error
}

func (a *App) branchCatalogKey(root string) string {
	for _, repo := range a.repositories {
		for _, tree := range repo.Worktrees {
			if tree.Path == root {
				return repo.CommonDir
			}
		}
	}
	canonical := project.Canonical(root)
	if canonical != root {
		for _, repo := range a.repositories {
			for _, tree := range repo.Worktrees {
				if tree.Path == canonical {
					return repo.CommonDir
				}
			}
		}
	}
	return canonical
}

func (a *App) branchCatalog(root string) *branchCatalog {
	a = a.application()
	if a.branchCatalogs == nil {
		a.branchCatalogs = make(map[string]*branchCatalog)
	}
	key := a.branchCatalogKey(root)
	if a.branchCatalogs[key] == nil && key != project.Canonical(root) {
		for _, repo := range a.repositories {
			if repo.CommonDir != key {
				continue
			}
			for _, tree := range repo.Worktrees {
				candidate := a.branchCatalogs[tree.Path]
				if candidate != nil && (a.branchCatalogs[key] == nil || candidate.loaded && !a.branchCatalogs[key].loaded) {
					a.branchCatalogs[key] = candidate
				}
				delete(a.branchCatalogs, tree.Path)
			}
		}
	}
	if a.branchCatalogs[key] == nil {
		a.branchCatalogs[key] = &branchCatalog{}
	}
	return a.branchCatalogs[key]
}

func (a *App) invalidateBranchCatalog(root string) {
	entry := a.branchCatalog(root)
	entry.dirty = true
	entry.version++
}

func (a *App) refreshBranchCatalog(root string) {
	a = a.application()
	entry := a.branchCatalog(root)
	if entry.loaded || entry.err != nil {
		a.publishBranchCatalog(root, entry)
	}
	if entry.loading || !a.canAsync() || !entry.dirty && time.Since(entry.checked) < 30*time.Second {
		return
	}
	entry.loading, entry.dirty = true, false
	version := entry.version
	read := a.readBranches
	if read == nil {
		read = gitview.RepositoryBranches
	}
	go func() {
		ctx, cancel := a.projectContext(10 * time.Second)
		defer cancel()
		branches, err := read(ctx, root)
		a.update(func() {
			entry.loading = false
			if a.branchCatalog(root) != entry {
				a.refreshBranchCatalog(root)
				return
			}
			if entry.version != version {
				a.refreshBranchCatalog(root)
				return
			}
			entry.checked, entry.err = time.Now(), err
			if err == nil {
				sources := make([]gitview.WorktreeSource, 0, len(branches))
				for _, branch := range branches {
					label := branch.Name
					if branch.Remote {
						label += "（远程）"
					}
					sources = append(sources, gitview.WorktreeSource{Ref: branch.Ref, Label: label})
				}
				if !entry.loaded || !slices.Equal(entry.sources, sources) {
					entry.sources = sources
					entry.sourcesVersion++
				}
				entry.branches, entry.loaded = branches, true
			}
			a.publishBranchCatalog(root, entry)
		})
	}()
}

func (a *App) publishBranchCatalog(root string, entry *branchCatalog) {
	key := a.branchCatalogKey(root)
	if draft := a.worktreeCreate; draft != nil && a.branchCatalogKey(draft.root) == key {
		draft.loading = !entry.loaded && entry.err == nil
		delete(draft.errors, "base")
		if entry.err != nil {
			draft.errors["base"] = "分支刷新失败：" + entry.err.Error()
		} else if draft.noCommit {
			draft.errors["base"] = "请先完成首次提交"
		}
		if entry.loaded && (draft.catalog != entry || draft.catalogVersion != entry.sourcesVersion) {
			draft.sources = entry.sources
			if draft.base == "HEAD" {
				draft.sources = append([]gitview.WorktreeSource{{Ref: "HEAD", Label: draft.headLabel}}, entry.sources...)
			}
			draft.filteredSources = nil
			draft.catalog, draft.catalogVersion = entry, entry.sourcesVersion
		}
	}
}

func (a *App) branchMetadataChanged(root string, paths map[string]bool) bool {
	common := a.branchCatalogKey(root)
	if common == project.Canonical(root) {
		common = filepath.Join(common, ".git")
	}
	for path := range paths {
		if path == "" {
			return true
		}
		path = filepath.Clean(path)
		if path == filepath.Join(common, "HEAD") || path == filepath.Join(common, "config") || path == filepath.Join(common, "packed-refs") || workspace.Contains(filepath.Join(common, "refs"), path) || workspace.Contains(filepath.Join(common, "worktrees"), path) {
			return true
		}
	}
	return false
}
