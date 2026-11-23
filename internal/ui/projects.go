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
	"github.com/shenming0821/Sasa/internal/project"
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
	a.addProjectPaths([]string{path})
}

// addProjectPaths reserves the import before inspecting paths off the UI thread.
// Only the first directory is imported, using the same flow as the file picker.
func (a *App) addProjectPaths(paths []string) {
	if a.projectsLoading {
		return
	}
	a.projectsLoading, a.projectsError = true, ""
	paths = append([]string(nil), paths...)
	go func() {
		ctx, cancel := a.projectContext(10 * time.Second)
		defer cancel()
		var path string
		var statError error
		for _, candidate := range paths {
			candidate = project.Canonical(candidate)
			info, err := os.Stat(candidate)
			if err != nil {
				statError = err
				continue
			}
			if info.IsDir() {
				path = candidate
				break
			}
		}
		if path == "" {
			err := errors.New("所选路径中没有可添加的文件夹")
			if statError != nil {
				err = fmt.Errorf("%w：%w", err, statError)
			}
			a.update(func() { a.projectsLoading = false; a.projectsError = "添加项目失败：" + err.Error() })
			return
		}
		repo, err := project.Discover(ctx, path)
		a.update(func() {
			defer a.persist()
			a.projectsLoading = false
			if errors.Is(err, project.ErrNotRepository) {
				a.restoreProject(directoryProjectID(path))
				delete(a.collapsedProjects, path)
				root := a.workspaceRoot(path)
				for _, tab := range a.tabs {
					if tab.Kind == "empty" && a.workspaceRoot(tab.Workspace) == root {
						a.selectWorkspaceTab(tab)
						return
					}
				}
				a.newEmptyTab(root, focusedPane(a.layoutFor(root)))
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
			a.refreshFolderSelection()
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
			a.refreshFolderSelection()
			a.persist()
		})
	}()
}

func (a *App) worktreeFor(cwd string) *project.Worktree {
	if frame := a.projectFrame; frame != nil {
		return frame.worktreeFor(cwd)
	}
	return a.findWorktree(cwd)
}

func (a *App) findWorktree(cwd string) *project.Worktree {
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
	if a.projectFrame != nil {
		return a.indexedWorktreeSessions(w.Path)
	}
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
	if a.restoreFolderTab(w.Path) {
		return
	}
	a.openWorkspacePicker(w.Path)
}

func (s *session) hasProjectConversation() bool {
	return s.saved.CustomTitle != "" || len(s.State.Messages) > 0 || s.State.Busy || s.reviewRequired || s.saved.Active ||
		(!s.loaded && s.State.ThreadID != "") ||
		(s.saved.Title != "" && s.saved.Title != "新会话")
}

// projectDisclosure keeps descendants mounted until the height transition finishes.
func (a *App) projectDisclosure(c *native.Context, key, label string, heading, content func(), projectIDs ...string) {
	var header, arrow native.Element
	managed := len(projectIDs) > 0 && projectIDs[0] != directoryProjectID("")
	var progress float32
	open := !a.collapsedProjects[key]
	row := native.Row(c.Key("project-heading")).FillWidth().Radius(8).AlignItems(native.Center)
	row.Children(func() {
		header = native.ButtonBase(c).Label(label).TextColor(c.Theme().TextMuted).Radius(8).Grow(1).MinWidth(0).Padding(8).Gap(7).Justify(native.Start)
		if header.Clicked() {
			if a.collapsedProjects == nil {
				a.collapsedProjects = make(map[string]bool)
			}
			a.collapsedProjects[key] = !a.collapsedProjects[key]
			open = !a.collapsedProjects[key]
		}
		progress = animateDisclosure(header, "project-open", open)
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
		row.Background(a.listRowHover(c.Theme()))
		opacity = 1
	}
	arrow.Opacity(opacity)
	disclosurePanel(c, "project-panel", open, progress, func() {
		native.Column(c).FillWidth().Shrink(0).Gap(3).Children(content)
	})
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
			for _, w := range repo.Worktrees {
				if w.Missing || w.Prunable {
					continue
				}
				sessions := a.worktreeSessions(w)
				var latest *session
				for _, s := range sessions {
					if s.hasProjectConversation() {
						latest = s
						break
					}
				}
				wc := rc.Key(w.Path)
				card := native.Column(wc).FillWidth().Gap(0).Radius(10)
				on := a.activeWorkspace == w.Path && a.activeTab != ""
				card.Children(func() {
					b := native.ButtonBase(wc).Label("打开工作区 "+w.Path).FillWidth().MinWidth(0).Radius(10).Padding(10, 16, 10, 29).Disabled(!w.Available())
					b.ContextMenu(func(menu *native.Menu) {
						a.projectFinderMenu(menu, w.Path)
						if menu.Item("重命名").Chosen() {
							a.beginWorktreeRename(w.Path)
						}
					})
					// Mirror Mayoi's railTab: a pill of accent grows at
					// the left edge as the card is on or hovered, the
					// card face fills at a partial alpha, and an accent
					// ring outlines it while it is on. The three states
					// ease over a short duration. A muted short pill
					// also stays while the folder keeps open tabs, so
					// the list shows which folders hold pages.
					hovered := b.Hovered()
					hasTabs := len(a.folderTabs(w.Path)) > 0
					var pill, fill float32
					switch {
					case on:
						pill, fill = 20, 0.30
					case hovered:
						pill, fill = 8, 0.22
					case hasTabs:
						pill = 8
					}
					pill = b.Animate("pill", pill, 160*time.Millisecond)
					fill = b.Animate("fill", fill, 120*time.Millisecond)
					outline := b.Animate("ring", projectFocusUnit(on || b.FocusVisible()), 160*time.Millisecond)
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
								native.Row(wc.Key("session-summary")).FillWidth().Padding(5, 0, 0, 0).Gap(6).AlignItems(native.Center).Children(func() {
									agentBadge(wc, latest.kind, 12, t.Surface, sessionStatusDot(wc, latest)).Label("工作区会话 Agent " + latest.kind.String()).Shrink(0)
									native.Text(wc, latest.title()).FontSize(11).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
								})
							}
						})
					})
					if b.Clicked() {
						a.openWorktree(w)
					}
					b.DrawOver(func(painter *native.Painter, bounds native.Rect) {
						paintProjectFocus(painter, bounds, t, pill, fill, outline, on)
					})
				})
			}
		}, repositoryProjectID(repo.CommonDir))
	})
}

// paintProjectFocus paints the pill, translucent fill, and accent ring
// of a project card as it is on or hovered, mirrored from Mayoi's
// railTab. The pill grows at the left edge in the accent when on, or in
// the muted color while only hovered; the fill and ring fade in with
// the card's alpha.
func paintProjectFocus(painter *native.Painter, bounds native.Rect, t *native.Theme, pill, fill, outline float32, on bool) {
	pc := t.TextMuted
	if on {
		pc = t.Accent
	}
	painter.Fill(native.Rect{X: bounds.X + 2, Y: bounds.Y + (bounds.H-pill)/2, W: 3, H: pill}, pc, 1.5)
	painter.Fill(bounds, t.Accent.Alpha(fill), 10)
	painter.Stroke(bounds, t.Accent.Alpha(0.75*outline), 10, 1.25)
}

// projectFocusUnit returns 1 when cond is true and 0 otherwise, the way
// Mayoi's unit does for its animated on/off states.
func projectFocusUnit(cond bool) float32 {
	if cond {
		return 1
	}
	return 0
}
