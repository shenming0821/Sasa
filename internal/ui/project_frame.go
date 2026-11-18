package ui

import (
	"path/filepath"

	"github.com/shenming0821/Sasa/internal/project"
)

// Only live for one sidebar render. History and repository changes are picked
// up on the next frame without maintaining an application-wide invalidation key.
type projectFrame struct {
	paths    map[string]*project.Worktree
	owners   map[string]*project.Worktree
	sessions map[string][]*session
}

func (a *App) beginProjectFrame() func() {
	previous := a.projectFrame
	frame := &projectFrame{
		paths:  make(map[string]*project.Worktree),
		owners: make(map[string]*project.Worktree),
	}
	for i := range a.repositories {
		for j := range a.repositories[i].Worktrees {
			w := &a.repositories[i].Worktrees[j]
			path := filepath.Clean(w.Path)
			if frame.paths[path] == nil {
				frame.paths[path] = w
			}
		}
	}
	a.projectFrame = frame
	return func() { a.projectFrame = previous }
}

func (frame *projectFrame) worktreeFor(cwd string) *project.Worktree {
	if owner, ok := frame.owners[cwd]; ok {
		return owner
	}
	var owner *project.Worktree
	if cwd != "" {
		// Discovery/import already canonicalizes worktree paths. Walking parent
		// directories finds the closest owner without testing every worktree.
		for path := filepath.Clean(cwd); ; path = filepath.Dir(path) {
			if owner = frame.paths[path]; owner != nil {
				break
			}
			if parent := filepath.Dir(path); parent == path {
				break
			}
		}
	}
	frame.owners[cwd] = owner
	return owner
}

func (a *App) indexedWorktreeSessions(path string) []*session {
	frame := a.projectFrame
	if frame.sessions == nil {
		frame.sessions = make(map[string][]*session)
		for i := len(a.sessions) - 1; i >= 0; i-- {
			s := a.sessions[i]
			if owner := a.worktreeFor(s.State.Cwd); owner != nil {
				frame.sessions[owner.Path] = append(frame.sessions[owner.Path], s)
			}
		}
	}
	return frame.sessions[path]
}
