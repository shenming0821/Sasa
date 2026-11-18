package ui

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/shenming0821/Sasa/internal/workspace"
)

func workspacePathChanged(paths map[string]bool, path string) bool {
	if paths[""] || paths[path] {
		return true
	}
	for changed := range paths {
		if workspace.Contains(changed, path) {
			return true
		}
	}
	return false
}

func (a *App) applyWorkspaceChanges(w *workspacePage, paths map[string]bool) {
	if a.branchMetadataChanged(w.root, paths) {
		a.invalidateBranchCatalog(w.root)
		if a.worktreeCreate != nil && a.branchCatalogKey(a.worktreeCreate.root) == a.branchCatalogKey(w.root) {
			a.refreshBranchCatalog(w.root)
		}
	}
	directories := make(map[string]bool)
	for path := range paths {
		if path != "" {
			directories[filepath.Dir(path)] = true
		}
		if skillCatalogPathChanged(path) {
			for key, entry := range a.completionCatalogs {
				if workspace.Contains(w.root, key.Cwd) {
					entry.checked = time.Time{}
					entry.generation++
				}
			}
		}
	}
	for path, expanded := range w.expanded {
		if expanded && (paths[""] || directories[path] || workspacePathChanged(paths, path)) {
			a.loadDirectory(w, path)
		}
	}
	for _, document := range a.files {
		if document.root == w.root && document.kind == "file" && workspacePathChanged(paths, document.path) {
			a.checkFile(document)
		}
	}
	if a.gitWorkspaceVisible(w) {
		if w.gitLoading {
			w.gitRefreshPending = true
		} else {
			a.refreshGit(w)
		}
	}
}

func skillCatalogPathChanged(path string) bool {
	if path == "" || filepath.Base(path) == "SKILL.md" {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".claude" || part == ".codex" || part == ".agents" {
			return true
		}
	}
	return false
}
