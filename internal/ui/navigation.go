package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	native "github.com/egoist/mygo/ui"
)

type projectGroup struct {
	path     string
	sessions []*session
}

func matchesQuery(query string, values ...string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	return query == "" || strings.Contains(strings.ToLower(strings.Join(values, "\n")), query)
}

func (a *App) projectGroups() []projectGroup {
	var groups []projectGroup
	indices := make(map[string]int)
	for i := len(a.sessions) - 1; i >= 0; i-- {
		s := a.sessions[i]
		if !matchesQuery(a.sessionQuery, s.title(), s.State.Cwd) {
			continue
		}
		path := s.State.Cwd
		if path != "" {
			path = filepath.Clean(path)
		}
		index, ok := indices[path]
		if !ok {
			index = len(groups)
			indices[path] = index
			groups = append(groups, projectGroup{path: path})
		}
		groups[index].sessions = append(groups[index].sessions, s)
	}
	return groups
}

func projectName(path string) string {
	if path == "" {
		return "未选择项目"
	}
	return filepath.Base(path)
}

func (s *session) navigationStatus() string {
	if !s.loaded && !s.recovering && !s.State.Disconnected {
		if s.saved.Active {
			return "上次任务待核验"
		}
		return ""
	}
	if n := len(s.State.PendingInteractions()); n > 0 {
		return fmt.Sprintf("待处理 · %d", n)
	}
	if s.State.Busy || s.State.Disconnected || s.recoveryBlocked || s.State.Error != "" {
		return s.State.Phase
	}
	return ""
}

func (a *App) sidebar(c *native.Context) {
	t := c.Theme()
	native.Column(c).Fill().Background(t.Surface).Children(func() {
		native.Row(c).FillWidth().Height(46).Padding(0, 12).Gap(4).AlignItems(native.Center).Children(func() {
			native.Text(c, "Fuyu").FontSize(15).Bold()
			native.Spacer(c)
			b := quietButton(c, "新建会话").Size(28, 28)
			b.Children(func() { native.Icon(c, newIcon).Size(16, 16) })
			if b.Clicked() {
				a.newSession(a.State.Cwd)
			}
			a.sidebarToggle(c, true)
		})
		native.Column(c).FillWidth().Padding(8, 12).Gap(10).Children(func() {
			native.TextInput(c, &a.sessionQuery).Label("搜索会话").Placeholder("搜索项目与会话").FillWidth().FontSize(12)
			b := quietButton(c, "打开项目").FillWidth().Gap(8)
			b.Children(func() {
				native.Icon(c, folderIcon).Size(16, 16)
				native.Text(c, "打开项目").FontSize(12)
			})
			if b.Clicked() {
				owner, cwd := a.session, a.State.Cwd
				go a.chooseDirectory(owner, cwd)
			}
		})
		native.Text(c, "项目").FontSize(11).TextColor(t.TextMuted).Padding(12, 18, 6, 18)
		native.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			native.Column(c).FillWidth().Padding(4, 8).Gap(12).Children(func() {
				groups := a.projectGroups()
				for _, group := range groups {
					gc := c.Key(group.path)
					native.Column(gc).FillWidth().Gap(3).Children(func() {
						b := quietButton(gc, "展开项目 "+group.path).FillWidth().Padding(8).Gap(7)
						b.Tooltip(group.path).Children(func() {
							glyph := chevron
							if a.collapsedProjects[group.path] && a.sessionQuery == "" {
								glyph = iconRight
							}
							native.Icon(gc, glyph).Size(11, 11)
							native.Icon(gc, folderIcon).Size(14, 14)
							native.Text(gc, projectName(group.path)).FontSize(12).Bold().TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
							native.Spacer(gc)
							native.Text(gc, fmt.Sprint(len(group.sessions))).FontSize(10).TextColor(t.TextMuted)
						})
						if b.Clicked() {
							if a.collapsedProjects == nil {
								a.collapsedProjects = make(map[string]bool)
							}
							a.collapsedProjects[group.path] = !a.collapsedProjects[group.path]
						}
						if a.collapsedProjects[group.path] && a.sessionQuery == "" {
							return
						}
						for _, s := range group.sessions {
							b := quietButton(gc.Key(s.id), fmt.Sprintf("切换会话 %d", s.id)).FillWidth().Padding(9, 12, 9, 28)
							if a.session == s {
								b.Background(t.SurfacePressed)
							}
							b.Children(func() {
								native.Column(gc).FillWidth().Gap(4).Children(func() {
									native.Text(gc, s.title()).FontSize(12).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
									if status := s.navigationStatus(); status != "" {
										color := t.TextMuted
										if s.State.Error != "" {
											color = t.Danger
										}
										native.Text(gc, status).FontSize(10).TextColor(color).SingleLine()
									}
								})
							})
							if b.Clicked() {
								a.selectSession(s)
							}
						}
					})
				}
				if len(groups) == 0 {
					native.Text(c, "没有匹配的会话").FontSize(12).TextColor(t.TextMuted).Padding(12)
				}
			})
		})
		native.Text(c, "Codex 工作空间").FontSize(11).TextColor(t.TextMuted).Padding(14, 18).
			BorderWidth(1, 0, 0, 0).BorderColor(t.Border.Alpha(0.5))
	})
}

var iconRight = icon(`<path d="m10 7 5 5-5 5"/>`)
var tabCloseIcon = icon(`<path d="m7 7 10 10M17 7 7 17"/>`)

func (a *App) sessionTabs(c *native.Context) {
	t := c.Theme()
	selected := a.session.id
	native.Row(c).FillWidth().Height(38).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.5)).Children(func() {
		native.ScrollHorizontal(c).Grow(1).FillHeight().MinWidth(0).Children(func() {
			native.Row(c).FillHeight().Children(func() {
				for _, s := range a.sessions {
					if !s.open {
						continue
					}
					tc := c.Key(s.id)
					row := native.Row(tc).Width(168).FillHeight().Padding(0, 4, 0, 0).AlignItems(native.Center)
					if s == a.session {
						row.Background(t.Surface).BorderWidth(0, 0, 2, 0).BorderColor(t.TextMuted)
						if a.tabsSelected != selected {
							row.ScrollIntoView()
						}
					}
					row.Children(func() {
						b := quietButton(tc, fmt.Sprintf("会话标签 %d", s.id)).
							Grow(1).MinWidth(0).FillHeight().Radius(0).Padding(0, 14).
							Tooltip(s.title() + "\n" + s.State.Cwd)
						b.Children(func() {
							native.Text(tc, s.title()).FontSize(12).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
						})
						if b.Clicked() {
							a.selectSession(s)
						}
						close := quietButton(tc, fmt.Sprintf("关闭会话标签 %d", s.id)).Size(24, 24).
							Tooltip("关闭标签，保留会话和后台任务")
						close.Children(func() { native.Icon(tc, tabCloseIcon).Size(13, 13).TextColor(t.TextMuted) })
						if close.Clicked() {
							a.closeSession(s)
						}
					})
				}
			})
		})
		b := quietButton(c, "新会话").Size(38, 36)
		b.Children(func() { native.Icon(c, newIcon).Size(15, 15) })
		if b.Clicked() {
			a.newSession(a.State.Cwd)
		}
	})
	a.tabsSelected = selected
}

func historyPanelWidth(width float32) float32 {
	return min(320, max(240, width-400))
}

func (a *App) sidebarVisible(width float32) bool {
	return a.sidebarOpen && a.sidebarFits(width)
}

func (a *App) sidebarFits(width float32) bool {
	return width >= 900 &&
		(!a.historyOpen || width >= a.sidebarWidth+1+historyPanelWidth(width)+400)
}

func (a *App) toggleHistory() {
	a.historyOpen = !a.historyOpen
	a.compactSidebar = false
	if a.historyOpen && len(a.nativeThreads) == 0 {
		a.loadThreadList()
	}
}

func (a *App) sidebarToggle(c *native.Context, left bool) {
	label, glyph := "切换右侧边栏", historyIcon
	if left {
		label, glyph = "切换左侧边栏", sidebarIcon
	}
	b := quietButton(c, label).Size(28, 28)
	b.Children(func() { native.Icon(c, glyph).Size(17, 17) })
	if b.Clicked() {
		if !left {
			a.toggleHistory()
		} else if a.compactSidebar {
			a.compactSidebar = false
		} else {
			width, _ := c.Size()
			if a.sidebarFits(width) {
				a.sidebarOpen = !a.sidebarOpen
			} else {
				a.compactSidebar = true
			}
		}
	}
}

func (a *App) historyPanel(c *native.Context) {
	t := c.Theme()
	width, _ := c.Size()
	native.Column(c.Key("history-panel")).Width(historyPanelWidth(width)).Shrink(0).FillHeight().Background(t.Surface).
		BorderWidth(0, 0, 0, 1).BorderColor(t.Border.Alpha(0.5)).Children(func() {
		native.Row(c).FillWidth().Height(46).Padding(0, 16).AlignItems(native.Center).Children(func() {
			native.Text(c, "会话历史").FontSize(13).Bold()
			native.Spacer(c)
			a.sidebarToggle(c, false)
		})
		native.Column(c).FillWidth().Padding(12).Gap(12).Children(func() {
			native.TextInput(c, &a.historyQuery).Label("搜索历史").Placeholder("搜索已加载的历史").FillWidth().FontSize(12)
			native.Segmented(c, &a.historyScope, "当前项目", "全部项目").FillWidth()
			scope := "全部项目"
			if a.historyScope == 0 {
				scope = projectName(a.State.Cwd)
			}
			native.Text(c, scope).FontSize(11).TextColor(t.TextMuted).SingleLine().Tooltip(a.State.Cwd)
		})
		a.historyBrowser(c)
	})
}
