package ui

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/project"
)

func mergeRepository(repos []project.Repository, repo project.Repository) []project.Repository {
	for i := range repos {
		if repos[i].CommonDir == repo.CommonDir {
			repos[i] = repo
			return repos
		}
	}
	return append(repos, repo)
}

func (a *App) projectContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	go func() {
		select {
		case <-a.shutdown:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (a *App) chooseProject(defaultPath string) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "添加项目", Directory: true, DefaultPath: defaultPath, ButtonLabel: "添加项目"})
	a.update(func() {
		if err != nil {
			a.projectsError = err.Error()
			return
		}
		if len(paths) > 0 {
			a.addProject(paths[0])
		}
	})
}

func (a *App) addProject(path string) {
	if a.projectsLoading {
		return
	}
	a.projectsLoading, a.projectsError = true, ""
	go func() {
		ctx, cancel := a.projectContext(10 * time.Second)
		defer cancel()
		path = project.Canonical(path)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			if err == nil {
				err = fmt.Errorf("所选路径不是文件夹：%s", path)
			}
			a.update(func() { a.projectsLoading = false; a.projectsError = "添加项目失败：" + err.Error() })
			return
		}
		repo, err := project.Discover(ctx, path)
		a.update(func() {
			a.projectsLoading = false
			if errors.Is(err, project.ErrNotRepository) {
				a.restoreProject(directoryProjectID(path))
				delete(a.collapsedProjects, path)
				for _, s := range a.sessions {
					if filepath.Clean(s.State.Cwd) == path {
						a.selectSession(s)
						return
					}
				}
				a.newSession(path)
				return
			}
			if err != nil {
				a.projectsError = "添加项目失败：" + err.Error()
				return
			}
			for _, w := range repo.Worktrees {
				delete(a.hiddenProjects, directoryProjectID(w.Path))
			}
			delete(a.hiddenProjects, directoryProjectID(path))
			delete(a.hiddenProjects, directoryProjectID(repo.Path))
			a.restoreProject(repositoryProjectID(repo.CommonDir))
			a.repositories = mergeRepository(a.repositories, repo)
			delete(a.projectErrors, repo.CommonDir)
			delete(a.collapsedProjects, repo.CommonDir)
		})
	}()
}

func (a *App) refreshProjects() {
	if a.projectsLoading {
		return
	}
	a.projectsLoading = true
	a.projectsRefreshed = time.Now()
	repos := append([]project.Repository(nil), a.repositories...)
	hidden := maps.Clone(a.hiddenProjects)
	// Upgrade existing directory sessions to repository groups without starting a thread.
	var paths []string
	seen := make(map[string]bool)
	for _, s := range a.sessions {
		if s.State.Cwd != "" && !seen[s.State.Cwd] && !hidden[directoryProjectID(s.State.Cwd)] && a.worktreeFor(s.State.Cwd) == nil {
			seen[s.State.Cwd] = true
			paths = append(paths, s.State.Cwd)
		}
	}
	go func() {
		ctx, cancel := a.projectContext(20 * time.Second)
		defer cancel()
		failures := make(map[string]string)
		promotions := make(map[string]project.Repository)
		for i, repo := range repos {
			if hidden[repositoryProjectID(repo.CommonDir)] {
				continue
			}
			updated, err := project.Discover(ctx, repo.Path)
			if err != nil {
				// A missing primary directory can still be scanned through a linked worktree.
				for _, w := range repo.Worktrees {
					if w.Path == repo.Path {
						continue
					}
					updated, err = project.Discover(ctx, w.Path)
					if err == nil {
						break
					}
				}
			}
			if err != nil {
				failures[repo.CommonDir] = err.Error()
				continue
			}
			if updated.CommonDir != repo.CommonDir {
				failures[repo.CommonDir] = "仓库标识已变化，请核对路径后重新导入"
				continue
			}
			repos[i] = updated
		}
		for _, path := range paths {
			if ctx.Err() != nil {
				break
			}
			if repo, err := project.Discover(ctx, path); err == nil {
				repos = mergeRepository(repos, repo)
				promotions[path] = repo
			}
		}
		a.update(func() {
			for path, repo := range promotions {
				a.promoteDirectoryProject(path, repo)
			}
			a.repositories, a.projectErrors, a.projectsLoading = repos, failures, false
		})
	}()
}

func (a *App) worktreeFor(cwd string) *project.Worktree {
	if cwd == "" {
		return nil
	}
	// Paths are canonicalized during discovery/import, not during view rendering.
	cwd = filepath.Clean(cwd)
	var best *project.Worktree
	for i := range a.repositories {
		for j := range a.repositories[i].Worktrees {
			w := &a.repositories[i].Worktrees[j]
			relative, err := filepath.Rel(w.Path, cwd)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
				(best == nil || len(w.Path) > len(best.Path)) {
				best = w
			}
		}
	}
	return best
}

func (a *App) worktreeSessions(w project.Worktree) []*session {
	var result []*session
	for i := len(a.sessions) - 1; i >= 0; i-- {
		s := a.sessions[i]
		if owner := a.worktreeFor(s.State.Cwd); owner != nil && owner.Path == w.Path {
			result = append(result, s)
		}
	}
	return result
}

func (a *App) openWorktree(w project.Worktree) {
	if !w.Available() {
		return
	}
	sessions := a.worktreeSessions(w)
	for _, s := range sessions {
		if s.hasProjectConversation() {
			a.selectSession(s)
			return
		}
	}
	for _, s := range sessions {
		if filepath.Clean(s.State.Cwd) == w.Path {
			a.selectSession(s)
			return
		}
	}
	a.newSession(w.Path)
}

func (s *session) hasProjectConversation() bool {
	return s.saved.CustomTitle != "" || len(s.State.Messages) > 0 || s.State.Busy || s.reviewRequired || s.saved.Active ||
		(!s.loaded && s.State.ThreadID != "") ||
		(s.saved.Title != "" && s.saved.Title != "新会话")
}

func (a *App) repositoryMatches(repo project.Repository) bool {
	if matchesQuery(a.sessionQuery, repo.Name, repo.Path) {
		return true
	}
	for _, w := range repo.Worktrees {
		if w.Missing || w.Prunable {
			continue
		}
		if matchesQuery(a.sessionQuery, w.Path, w.Branch, a.worktreeName(w.Path)) {
			return true
		}
		for _, s := range a.worktreeSessions(w) {
			if s.hasProjectConversation() && matchesQuery(a.sessionQuery, s.title(), s.State.Cwd) {
				return true
			}
		}
	}
	return false
}

// projectDisclosure keeps descendants mounted until the height transition finishes.
func (a *App) projectDisclosure(c *native.Context, key, label string, heading, content func(), projectIDs ...string) {
	var header, arrow native.Element
	managed := len(projectIDs) > 0 && projectIDs[0] != directoryProjectID("")
	var progress float32
	target := float32(1)
	if a.collapsedProjects[key] && a.sessionQuery == "" {
		target = 0
	}
	row := native.Row(c.Key("project-heading")).FillWidth().Radius(8).AlignItems(native.Center)
	row.Children(func() {
		header = native.ButtonBase(c).Label(label).TextColor(c.Theme().TextMuted).Radius(8).Grow(1).MinWidth(0).Padding(8).Gap(7).Justify(native.Start)
		if header.Clicked() {
			if a.collapsedProjects == nil {
				a.collapsedProjects = make(map[string]bool)
			}
			a.collapsedProjects[key] = !a.collapsedProjects[key]
			if a.collapsedProjects[key] && a.sessionQuery == "" {
				target = 0
			} else {
				target = 1
			}
		}
		progress = header.Animate("project-open", target, 200*time.Millisecond)
		header.Children(func() {
			heading()
			arrow = native.Icon(c, chevron).Size(11, 11).Rotate(-90 * (1 - progress))
		})
		if managed {
			a.projectHeadingActions(c, row, header, projectIDs[0])
		}
	})
	opacity := float32(0)
	if row.Hovered() || header.FocusVisible() {
		row.Background(c.Theme().SurfaceHover)
		opacity = 1
	}
	arrow.Opacity(opacity)
	if progress == 0 && target == 0 {
		return
	}
	panel := native.Column(c).FillWidth().Shrink(0)
	var inner native.Element
	panel.Children(func() {
		inner = native.Column(c).FillWidth().Shrink(0).Gap(3)
		inner.Children(content)
	})
	if progress < 1 {
		panel.ClipY().Height(progress * inner.Bounds().H)
	}
}

func (a *App) repositoryRow(c *native.Context, repo project.Repository) {
	t := c.Theme()
	rc := c.Key(repo.CommonDir)
	native.Column(rc).FillWidth().Gap(3).Children(func() {
		a.projectDisclosure(rc, repo.CommonDir, "展开仓库 "+repo.CommonDir, func() {
			native.Icon(rc, folderIcon).Size(14, 14)
			native.Text(rc, repo.Name).FontSize(14).Bold().TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
			native.Spacer(rc)
		}, func() {
			if err := a.projectErrors[repo.CommonDir]; err != "" {
				native.Text(rc, "刷新失败，保留上次列表").FontSize(10).TextColor(t.Danger).Tooltip(err).Padding(4, 12)
			}
			whole := matchesQuery(a.sessionQuery, repo.Name, repo.Path)
			for _, w := range repo.Worktrees {
				if w.Missing || w.Prunable {
					continue
				}
				sessions := a.worktreeSessions(w)
				match := whole || matchesQuery(a.sessionQuery, w.Path, w.Branch, a.worktreeName(w.Path))
				if !match {
					for _, s := range sessions {
						if s.hasProjectConversation() && matchesQuery(a.sessionQuery, s.title(), s.State.Cwd) {
							match = true
							break
						}
					}
				}
				if !match {
					continue
				}
				var latest *session
				for _, s := range sessions {
					if s.hasProjectConversation() && (whole || matchesQuery(a.sessionQuery, w.Path, w.Branch, a.worktreeName(w.Path), s.title(), s.State.Cwd)) {
						latest = s
						break
					}
				}
				wc := rc.Key(w.Path)
				card := native.Column(wc).FillWidth().Gap(0).Radius(10)
				if current := a.worktreeFor(a.State.Cwd); current != nil && current.Path == w.Path && a.open {
					card.Background(t.SurfacePressed)
				} else if card.Hovered() {
					card.Background(t.SurfaceHover)
				}
				card.Children(func() {
					b := native.ButtonBase(wc).Label("打开工作区 "+w.Path).FillWidth().MinWidth(0).Radius(10).Padding(10, 16, 10, 29).Disabled(!w.Available())
					b.ContextMenu(func(menu *native.Menu) {
						if menu.Item("重命名").Chosen() {
							a.beginWorktreeRename(w.Path)
						}
					})
					if b.FocusVisible() {
						card.Background(t.SurfacePressed)
					}
					b.Children(func() {
						native.Column(wc).FillWidth().Gap(3).Children(func() {
							native.Row(wc).FillWidth().Gap(6).AlignItems(native.Center).Children(func() {
								native.Text(wc, a.worktreeName(w.Path)).FontSize(13).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
								if w.Main {
									native.Text(wc, "主要").FontSize(9).TextColor(t.TextMuted).Padding(1, 4).BorderWidth(1).BorderColor(t.Border).Radius(3)
								}
							})
							label := w.Label()
							if !w.Available() {
								label += " · 不可用"
							} else if w.Locked {
								label += " · 已锁定"
							}
							native.Text(wc, label).FontSize(10).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
							if latest != nil {
								native.Text(wc, latest.title()).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0).Padding(5, 0, 0, 0)
								if status := latest.navigationStatus(); status != "" && !latest.writerOccupied {
									color := t.TextMuted
									if latest.State.Error != "" {
										color = t.Danger
									}
									native.Text(wc, status).FontSize(10).TextColor(color).SingleLine().Shrink(1).MinWidth(0)
								}
							}
						})
					})
					if b.Clicked() {
						if latest != nil {
							a.selectSession(latest)
						} else {
							a.openWorktree(w)
						}
					}
				})
			}
		}, repositoryProjectID(repo.CommonDir))
	})
}
