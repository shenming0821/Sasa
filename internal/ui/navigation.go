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
	return a.projectGroupsFor(a.sessionQuery)
}

func (a *App) projectGroupsFor(query string) []projectGroup {
	var groups []projectGroup
	indices := make(map[string]int)
	for i := len(a.sessions) - 1; i >= 0; i-- {
		s := a.sessions[i]
		if s.State.Cwd == "" || a.worktreeFor(s.State.Cwd) != nil {
			continue
		}
		if !matchesQuery(query, s.title(), s.State.Cwd) {
			continue
		}
		path := a.workspaceRoot(s.State.Cwd)
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
	defer a.beginProjectFrame()()
	t := c.Theme()
	bar := c.TitleBar()
	native.Column(c).Fill().Background(t.Surface).Children(func() {
		native.Row(c).FillWidth().Height(max(52, bar.Height)).Padding(0, 12, 0, max(12, bar.Left)).Gap(4).AlignItems(native.Center).DragWindow().Children(func() {
			native.Spacer(c)
			settings := quietIconButton(c, "设置").Size(28, 28)
			settings.Children(func() { native.Icon(c, settingsIcon).Size(16, 16) })
			if settings.Clicked() {
				a.openSettings()
			}
			b := quietIconButton(c, "重启 Fuyu").Size(28, 28).Tooltip("重启 Fuyu（保留当前工作区）")
			b.Children(func() { native.Icon(c, restartIcon).Size(16, 16) })
			if b.Clicked() {
				a.restartApp()
			}
			a.sidebarToggle(c, true)
		})
		native.Column(c).FillWidth().Padding(8, 12).Gap(10).Children(func() {
			native.TextInput(c, &a.sessionQuery).Label("搜索会话").Placeholder("搜索项目与会话").FillWidth().FontSize(12)
		})
		native.Row(c).FillWidth().Padding(4, 12, 4, 18).Gap(2).AlignItems(native.Center).Children(func() {
			native.Text(c, "项目").FontSize(11).TextColor(t.TextMuted)
			native.Spacer(c)
			add := quietIconButton(c, "添加项目").Size(26, 26).Tooltip("添加项目文件夹（自动识别 Git 工作区）").Disabled(a.projectsLoading)
			add.Children(func() { native.Icon(c, addProjectIcon).Size(14, 14) })
			if add.Clicked() {
				go a.chooseProject(a.State.Cwd)
			}
			refresh := quietIconButton(c, "刷新项目").Size(26, 26).Disabled(a.projectsLoading)
			if a.projectsLoading {
				refresh.Tooltip("正在刷新项目…")
			}
			refresh.Children(func() { native.Icon(c, refreshProjectIcon).Size(14, 14) })
			if refresh.Clicked() {
				a.refreshProjects()
			}
		})
		if a.projectsError != "" {
			native.Text(c, a.projectsError).FontSize(11).TextColor(t.Danger).Padding(4, 12).MaxLines(3)
		}
		native.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			native.Column(c).FillWidth().Padding(4, 8).Gap(12).Children(func() {
				items := a.sidebarProjectItems()
				for _, item := range items {
					if item.repo != nil {
						a.repositoryRow(c, *item.repo)
					} else {
						a.directoryProjectRow(c, item.group)
					}
				}
				matched := len(items) > 0
				if !matched {
					native.Text(c, "没有匹配的会话").FontSize(12).TextColor(t.TextMuted).Padding(12)
				}
			})
		})
	})
}

var addProjectIcon = icon(`<path d="M12 5v14M5 12h14"/>`)
var refreshProjectIcon = icon(`<path d="M20 7v5h-5M4 17v-5h5M6 7a7 7 0 0 1 12-1l2 3M4 15l2 3a7 7 0 0 0 12-1"/>`)

var tabCloseIcon = icon(`<path d="m7 7 10 10M17 7 7 17"/>`)

func activeTabFace(c *native.Context, row native.Element) {
	theme := c.Theme()
	row.Background(theme.Background).DrawOver(func(painter *native.Painter, bounds native.Rect) {
		painter.Fill(native.Rect{X: bounds.X, Y: bounds.Y + bounds.H - 2, W: bounds.W, H: 2}, theme.Accent, 0)
	})
}

func (a *App) sessionTabs(c *native.Context, leftShown, rightShown bool) {
	t := c.Theme()
	renderedActive := a.activeTab
	tabs := a.paneTabs()
	row := native.Row(c).FillWidth().Height(38).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(.5)).DragWindow()
	row.Background(t.Surface)
	if a.renderingPane != nil {
		row.ContextMenu(a.paneMenu(a.activeWorkspace, a.renderingPane))
	}
	row.Children(func() {
		if !leftShown {
			a.sidebarToggle(c, true)
		}
		scroll := native.ScrollHorizontal(c).Grow(1).FillHeight().MinWidth(0)
		scroll.Children(func() {
			native.Row(c).MinWidth(scroll.Bounds().W).Shrink(0).FillHeight().AlignItems(native.Center).Children(func() {
				for _, tab := range tabs {
					if tab.Kind == "empty" {
						a.emptyTab(c.Key(tab.ID), tab)
						continue
					}
					if tab.Kind == "session" {
						valid := false
						for _, s := range a.sessions {
							if s.id == tab.SessionID && s.open {
								valid = true
							}
						}
						if !valid {
							continue
						}
					}
					tc := c.Key(tab.ID)
					row := native.Row(tc).Width(168).FillHeight().Padding(0, 4, 0, 0).AlignItems(native.Center)
					focused := a.paneIsFocused()
					if focused && tab.ID == a.activeTab {
						activeTabFace(c, row)
						if a.tabsSelected != a.activeTab {
							row.ScrollIntoView()
						}
					}
					a.tabDropZone(c, row, tab.ID, focused && tab.ID == a.activeTab)
					row.Children(func() {
						label := "文件标签 " + tab.Path
						closeLabel := "关闭文件标签 " + tab.Path
						if tab.Kind == "session" {
							label = fmt.Sprintf("会话标签 %d", tab.SessionID)
							closeLabel = fmt.Sprintf("关闭会话标签 %d", tab.SessionID)
						} else if tab.Kind == "terminal" {
							label = "终端标签 " + tab.ID
							closeLabel = "关闭终端标签 " + tab.ID
						}
						b := native.ButtonBase(tc).Label(label).Tooltip(a.tabTooltip(tab)).Grow(1).MinWidth(0).FillHeight().Padding(0, 12)
						if pane := a.renderingPane; pane != nil {
							b.Drag(paneTabDrag{Workspace: a.activeWorkspace, Pane: pane.ID, Tab: tab.ID})
							if b.Dragging() {
								a.paneTabDragging = true
							}
						}
						if tab.Kind == "file" || tab.Kind == "diff" {
							b.ContextMenu(a.fileMenu(tab.Workspace, tab.Path, false, false))
						}
						if tab.Kind == "session" {
							for _, target := range a.sessions {
								if target.id == tab.SessionID {
									b.ContextMenu(a.sessionMenu(target))
								}
							}
						}
						if a.tabHandles == nil {
							a.tabHandles = map[string]*native.Handle{}
						}
						if a.tabHandles[tab.ID] == nil {
							a.tabHandles[tab.ID] = &native.Handle{}
						}
						b.Bind(a.tabHandles[tab.ID])
						b.Children(func() {
							native.Row(tc).FillWidth().Gap(6).AlignItems(native.Center).Children(func() {
								if tab.Kind == "session" {
									for _, target := range a.sessions {
										if target.id == tab.SessionID {
											agentIcon(tc, target.kind, 12).Label("会话 Agent " + target.kind.String())
											break
										}
									}
								} else if tab.Kind == "terminal" {
									native.Box(tc).Size(12, 16).Shrink(0).Center().Children(func() {
										native.Icon(tc, launchTerminalIcon).Size(16, 16).Shrink(0).TextColor(t.TextMuted)
									})
								} else {
									glyph := fileIcon
									if tab.Kind == "diff" {
										glyph = gitIcon
									}
									native.Icon(tc, glyph).Size(12, 12).TextColor(t.TextMuted)
								}
								native.Text(tc, a.tabTitle(tab)).FontSize(12).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
							})
						})
						if b.Clicked() {
							a.selectWorkspaceTab(tab)
							if d := a.currentFile(); d != nil {
								d.focusOwner = a.tabHandles[tab.ID]
							}
						}
						close := native.ButtonBase(tc).Label(closeLabel).Size(24, 24).Radius(8).Padding(6)
						close.Children(func() {
							if d := a.files[tab.ID]; d != nil && d.dirty() && !close.Hovered() && !close.FocusVisible() {
								native.Text(tc, "●").FontSize(9).TextColor(t.TextMuted)
							} else {
								native.Icon(tc, tabCloseIcon).Size(13, 13).TextColor(t.TextMuted)
							}
						})
						if close.Clicked() {
							if tab.Kind == "session" {
								for _, s := range a.sessions {
									if s.id == tab.SessionID {
										a.closeSession(s)
										break
									}
								}
							} else if page := a.terminals[tab.ID]; page != nil {
								a.closeTerminal(page)
							} else if d := a.files[tab.ID]; d != nil {
								a.closeFile(d)
							}
						}
					})
				}
				if pane := a.renderingPane; pane != nil {
					blank := native.Box(c.Key("tab-blank")).Label("新建标签页空白处 " + pane.ID).Grow(1).FillHeight()
					a.tabEndDropZone(c, blank, pane)
					if blank.DoubleClicked() {
						a.newEmptyTab(a.activeWorkspace, pane)
						c.AnimationFrame()
					}
				}
			})
		})
		a.agentSessionButtons(c)
		if pane := a.renderingPane; pane != nil {
			button := quietIconButton(c, "面板操作").Size(28, 28).Tooltip("分屏与面板操作")
			button.Children(func() { native.Icon(c, paneMenuIcon).Size(16, 16) })
			button.Menu(a.paneMenu(a.activeWorkspace, pane))
		}
		if !rightShown {
			a.sidebarToggle(c, false)
		}
	})
	// A click in this pass can change activeTab after its row was built.
	// Retain the rendered selection so the following pass reveals the new row.
	if a.paneIsFocused() {
		a.tabsSelected = renderedActive
	}
}

func (a *App) effectiveRightPanelWidth(width float32) float32 {
	preferred := a.rightPanelWidth
	if preferred == 0 {
		preferred = 320
	}
	return min(max(preferred, 240), 520, max(240, width-401))
}

func (a *App) sidebarVisible(width float32) bool {
	return a.sidebarOpen && a.sidebarFits(width)
}

func (a *App) sidebarFits(width float32) bool {
	return width >= 900 &&
		(!a.historyOpen || width >= a.sidebarWidth+2+a.effectiveRightPanelWidth(width)+400)
}

func (a *App) toggleHistory() {
	a.historyOpen = !a.historyOpen
	a.compactSidebar = false
	if a.activeWorkspace != "" {
		w := a.workspace(a.activeWorkspace)
		if !a.gitWorkspaceVisible(w) {
			a.cancelGitRefresh(w)
		}
	}
	if a.historyOpen && a.rightTab == "history" {
		a.openThreadList()
	}
}

func (a *App) sidebarToggle(c *native.Context, left bool) {
	label, glyph := "切换右侧边栏", historyIcon
	if left {
		label, glyph = "切换左侧边栏", sidebarIcon
	}
	b := quietIconButton(c, label).Size(28, 28)
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
	native.Column(c.Key("history-panel")).Width(a.effectiveRightPanelWidth(width)).Shrink(0).FillHeight().Background(t.Surface).
		BorderWidth(0, 0, 0, 1).BorderColor(t.Border.Alpha(0.5)).Children(func() {
		native.Row(c).FillWidth().Height(46).Padding(0, 16).AlignItems(native.Center).Children(func() {
			native.Text(c, "会话历史").FontSize(13).Bold()
			native.Spacer(c)
			a.sidebarToggle(c, false)
		})
		native.Column(c).FillWidth().Padding(12).Gap(12).Children(func() {
			native.TextInput(c, &a.historyQuery).Label("搜索历史").Placeholder("搜索已加载的历史").FillWidth().FontSize(12)
			a.historyScopeControl(c)
		})
		a.historyBrowser(c)
	})
}

func (a *App) historyScopeControl(c *native.Context) {
	t := c.Theme()
	labels := []string{"当前项目", "全部项目"}
	parts := native.SegmentedBase(c, &a.historyScope, len(labels))
	parts.Track.Label("历史项目范围").FillWidth().AlignItems(native.Stretch).
		Padding(2).Gap(2).Radius(8).Background(t.Surface).Border(1, t.Border).Children(func() {
		for index, label := range labels {
			segment := parts.Segment(index).Label(label).Padding(6, 10).Radius(6).TextColor(t.Text)
			selectedButtonFace(c, segment, a.historyScope == index, 6, 0)
			segment.Children(func() { native.Text(c, label).SingleLine() })
		}
	})
}

// Tree rows use the same background on hover and mouse-down.
func treeButton(c *native.Context, label string) native.Element {
	t := c.Theme()
	b := native.ButtonBase(c).Label(label).Radius(8).Padding(6).TextColor(t.TextMuted)
	if b.Hovered() || b.FocusVisible() {
		b.Background(t.SurfaceHover)
	}
	return b
}

func (a *App) directoryProjectRow(c *native.Context, group projectGroup) {
	t := c.Theme()
	gc := c.Key(group.path)
	native.Column(gc).FillWidth().Gap(3).Children(func() {
		a.projectDisclosure(gc, group.path, "展开项目 "+group.path, func() {
			a.folderTabIndicator(gc, group.path, -2)
			native.Icon(gc, folderIcon).Size(14, 14)
			native.Text(gc, projectName(group.path)).FontSize(12).Bold().TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
			native.Spacer(gc)
		}, func() {
			for _, s := range group.sessions {
				b := treeButton(gc.Key(s.id), fmt.Sprintf("切换会话 %d", s.id)).FillWidth().Padding(9, 12, 9, 28)
				b.ContextMenu(a.sessionMenu(s))
				if a.activeTab == sessionTabID(s.id) && s.open {
					b.Background(t.SurfacePressed)
					b.DrawOver(func(painter *native.Painter, bounds native.Rect) {
						painter.Fill(native.Rect{X: bounds.X + 2, Y: bounds.Y + 6, W: 3, H: bounds.H - 12}, t.Accent, 1.5)
					})
				}
				b.Children(func() {
					native.Column(gc).FillWidth().Gap(4).Children(func() {
						native.Row(gc.Key("session-title")).FillWidth().Gap(6).AlignItems(native.Center).Children(func() {
							agentIcon(gc, s.kind, 12).Label("会话 Agent " + s.kind.String()).Shrink(0)
							native.Text(gc, s.title()).FontSize(12).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
						})
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
		}, directoryProjectID(group.path))
	})
}
