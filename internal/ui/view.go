package ui

import (
	"fmt"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
)

const (
	readingWidth     float32 = 800
	returnButtonSize float32 = 32
	returnButtonGap  float32 = 20
	scrollFadeHeight float32 = 80
)

var (
	folderIcon      = icon(`<path d="M3 7V5a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Zm0 1h18"/>`)
	restartIcon     = icon(`<path d="M17 2.1 21 6l-4 3.9"/><path d="M3 12.9V9a3 3 0 0 1 3-3h15"/><path d="m7 21.9-4-3.9 4-3.9"/><path d="M21 11.1V15a3 3 0 0 1-3 3H3"/>`)
	upIcon          = icon(`<path d="M12 19V5m-6 6 6-6 6 6"/>`)
	downIcon        = icon(`<path d="M12 5v14m-6-6 6 6 6-6"/>`)
	stopIcon        = icon(`<rect x="6" y="6" width="12" height="12" rx="1" fill="currentColor" stroke="none"/>`)
	copyIcon        = icon(`<rect x="8" y="8" width="12" height="13" rx="2"/><path d="M15 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>`)
	checkIcon       = icon(`<path d="m5 12 4 4L19 6"/>`)
	chevron         = icon(`<path d="m7 10 5 5 5-5"/>`)
	collapseAllIcon = icon(`<path fill="currentColor" stroke="none" d="M21 6.406C21.9 6.931 22.5 7.89 22.5 9V16.5C22.5 19.815 19.815 22.5 16.5 22.5H9C7.89 22.5 6.931 21.9 6.406 21H16.5C18.975 21 21 18.975 21 16.5V6.406Z"/><rect x="3" y="3" width="16.5" height="16.5" rx="3"/><rect x="8.25" y="10.5" width="6" height="1.5" rx="0.75" fill="currentColor" stroke="none"/>`)
	sidebarIcon     = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>`)
	historyIcon     = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/>`)
)

func icon(paths string) *native.SVG {
	return native.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

// Each message and the composer share this width independently of list scrolling.
func readingColumn(c *native.Context, build func()) {
	native.Row(c).FillWidth().Justify(native.Center).Children(func() {
		native.Column(c).FillWidth().MaxWidth(readingWidth).MinWidth(0).Children(build)
	})
}

func (a *App) view(c *native.Context) {
	defer a.scheduleSave()
	a.applyAppearance(c)
	defer a.hideInactiveTerminals()
	a.syncActivePane()
	a.focusVisiblePane(c)
	a.markSelectedSessionRead()
	if c.Shortcut(native.Cmd, native.KeyComma) {
		a.openSettings()
	}
	if c.Shortcut(native.Cmd, native.KeyP) {
		a.openQuickOpen()
	}
	if c.Shortcut(native.Cmd, native.KeyW) {
		a.closeCurrentPage()
	}
	if a.sessionViewActive() && c.Shortcut(native.Cmd, native.KeyF) {
		a.openSessionFind()
	}
	width, _ := c.Size()
	a.sidebarWidth = min(max(a.sidebarWidth, 200), 320)
	sidebar := a.sidebarVisible(width)
	if sidebar {
		a.compactSidebar = false
	}
	leftShown, rightShown := sidebar || a.compactSidebar, a.historyOpen && width >= 640
	native.Box(c).Fill().Children(func() {
		center := func() {
			main := func() {
				a.workspacePanes(c, leftShown, rightShown)
			}
			if rightShown {
				available := width
				if sidebar {
					available -= a.sidebarWidth + 1
				}
				mainWidth := available - a.effectiveRightPanelWidth(available) - 1
				split := native.Split(c.Key("right-panel-split"), &mainWidth, main, func() { a.rightPanel(c) }).Fill()
				if split.Changed() {
					a.rightPanelWidth = min(max(available-mainWidth-1, 240), 520, max(240, available-401))
				}
			} else {
				native.Row(c).Fill().Children(main)
			}
		}
		if sidebar {
			native.Split(c, &a.sidebarWidth, func() { a.sidebar(c) }, center).Fill()
		} else {
			center()
		}
		if !sidebar && leftShown {
			native.Column(c.Key("project-overlay")).Absolute().Left(0).Top(46).Bottom(0).Width(240).
				Shadow(4, 0, 16, 0, native.RGBA(0, 0, 0, 0.12)).Children(func() { a.sidebar(c) })
		}
	})
	a.imageViewer(c)
	a.settingsView(c)
	a.worktreeRenameView(c)
	a.worktreeCreateView(c)
	a.sessionRenameView(c)
	a.fileActionView(c)
	a.quickOpenView(c)
	if a.sessionDeleteError != "" {
		native.Column(c.Key("session-delete-error")).Absolute().Bottom(20).Left(20).Right(20).Padding(12).Gap(8).Background(c.Theme().Surface).BorderWidth(1).BorderColor(c.Theme().Danger).Radius(8).Children(func() {
			native.Text(c, a.sessionDeleteError).FontSize(12).TextColor(c.Theme().Danger)
			if actionButton(c, "关闭删除提示").Clicked() {
				a.sessionDeleteError = ""
			}
		})
	}
}

func (a *App) emptyWorkspace(c *native.Context, leftShown, rightShown bool) {
	t := c.Theme()
	native.Column(c).Grow(1).FillHeight().MinWidth(0).Background(t.Background).Children(func() {
		a.sessionTabs(c, leftShown, rightShown)
		if a.modelError != "" {
			native.Text(c, a.modelError).FontSize(12).TextColor(t.Danger).Padding(12, 16)
		}
		a.fileExitNotice(c)
		if a.isEmptyTab(a.activeTab) {
			a.emptyPane(c)
		}
	})
}

func (a *App) sessionView(c *native.Context, leftShown, rightShown bool) {
	t := c.Theme()
	a.list.FollowEnd = !a.processScrollHold && a.edit == nil && a.editScrollHold == 0
	if a.editScrollHold > 0 {
		a.editScrollHold--
		c.Invalidate()
	}
	if a.thinking() {
		a.list.ScrollToEnd()
	}
	native.Column(c).Grow(1).FillHeight().MinWidth(0).Background(t.Background).Children(func() {
		a.sessionTabs(c, leftShown, rightShown)
		a.fileExitNotice(c)
		if pending := a.State.PendingInteractions(); len(pending) > 0 {
			native.Row(c).FillWidth().Padding(6, 24).Children(func() {
				b := actionButton(c, fmt.Sprintf("处理待办（%d）", len(pending))).TextColor(t.Text)
				if b.Clicked() {
					row := 0
					for _, m := range a.session.displayMessages() {
						if m.Interaction == pending[0] {
							a.list.ScrollTo(row, native.Start)
							break
						}
						row++
					}
				}
				native.Text(c, a.kind.String()+" 需要你的决定").FontSize(12).TextColor(t.TextMuted)
			})
		}
		if len(a.State.Messages) == 0 {
			native.Column(c).Grow(1).FillWidth().Center().Gap(12).Padding(24).Children(func() {
				native.Text(c, "从一个清晰的任务开始").FontSize(25)
				native.Text(c, "与 "+a.kind.String()+" 一起，把想法变成现实。").FontSize(14).TextColor(t.TextMuted)
			})
		} else {
			a.conversation(c)
		}
		native.Column(c).FillWidth().Padding(0, 24, 16, 24).Children(func() {
			readingColumn(c, func() {
				if a.application().storageError != "" {
					native.Text(c, a.application().storageError).FontSize(12).TextColor(t.Danger)
					if actionButton(c, "重试保存").Clicked() {
						a.persist()
					}
				}
				if a.writerOccupied {
					a.occupiedCard(c)
					return
				}
				if a.recoveryBlocked && !a.recovering && !a.State.Disconnected {
					if a.State.ThreadID != "" && actionButton(c, "重新核验原生会话").Clicked() {
						a.recoverSession(a.session)
					}
					if a.reviewRequired && actionButton(c, "确认历史后继续").Clicked() {
						a.reviewRequired, a.recoveryBlocked = false, false
						a.saved.Active = false
						a.State.Phase = "就绪"
						a.State.Notice = "已确认历史，可输入下一项任务；未自动重发上次请求。"
					}
				}
				if a.State.Phase == "执行失败" && a.State.Error != "" {
					a.taskFailureCard(c)
				} else if a.State.Error != "" {
					native.Scroll(c).FillWidth().MaxHeight(54).Children(func() {
						native.Text(c, a.State.Error).Selectable().FontSize(12).TextColor(t.Danger).Padding(0, 4, 8, 4)
					})
				}
				if a.State.Notice != "" {
					native.Scroll(c).FillWidth().MaxHeight(38).Children(func() {
						native.Text(c, a.State.Notice).FontSize(11).TextColor(t.TextMuted).Padding(0, 4, 6, 4)
					})
				}
				if a.State.Incomplete && !a.State.Busy && !a.State.Disconnected {
					if actionButton(c, "重新核验结果").Disabled(a.snapshotPending).Clicked() {
						a.State.NeedsSnapshot = true
						a.reconcileIfNeeded()
					}
				}
				a.composer(c)
			})
		})
	})
}

func (a *App) conversation(c *native.Context) {
	t := c.Theme()
	processMotions := a.session.animateProcesses(c)
	a.session.restoreRowOffset(c)
	rows := a.session.conversationRows()
	messages := a.session.projectedMessages
	if a.find.open {
		a.sessionFindRescan(messages)
	}
	entries := a.session.navigationEntries
	padding := float32(24)
	if len(entries) > 1 {
		padding = 48
	}
	if len(a.rows) > len(messages) {
		visibleIDs := make(map[string]bool, len(messages))
		for _, m := range messages {
			visibleIDs[m.ID] = true
		}
		for id := range a.rows {
			if !visibleIDs[id] {
				delete(a.rows, id)
			}
		}
	}
	thinking := a.thinking()
	timings := a.conversationTimings(rows)
	lastCommands := -1
	for i := range rows {
		if len(rows[i].commands) > 0 {
			lastCommands = i
		}
	}
	count := len(messages)
	if thinking {
		count++
	}
	a.list.Key = func(i int) any {
		if i == len(messages) {
			return thinkingKey{a.State.Sequence}
		}
		return messages[i].ID
	}
	native.Box(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
		// Keep the latest content clear of the floating control, even while it is hidden.
		list := native.List(c, &a.list, count, func(i int) {
			gap := conversationSpacing(rows, i)
			if i == len(messages) {
				native.Column(c).FillWidth().Padding(gap, 0, 0, 0).Children(func() {
					readingColumn(c, func() { thinkingIndicator(c) })
				})
			} else {
				row := native.Column(c).FillWidth().Bind(a.rowHandle(messages[i].ID))
				var inner native.Element
				row.Children(func() {
					inner = native.Column(c).FillWidth().Shrink(0).Padding(gap, 0, 0, 0)
					inner.Children(func() {
						if len(rows[i].changes) > 0 {
							readingColumn(c, func() { a.changeCard(c.Key(messages[i].ID), rows[i].changes) })
						} else if len(rows[i].commands) > 0 {
							continuing := i == lastCommands && i == len(rows)-1 && a.State.Busy && !a.State.Stopping()
							readingColumn(c, func() { a.commandGroup(c.Key(messages[i].ID), rows[i].commands, continuing) })
						} else {
							a.message(c, messages[i])
						}
						if timings[i] != nil {
							readingColumn(c, func() {
								native.Column(c).FillWidth().Padding(16, 0, 0, 0).Children(func() {
									a.session.turnTiming(c.Key(messages[i].ID), messages[i], processMotions[messages[i].ID])
								})
							})
						}
					})
				})
				if id := rows[i].processID; id != "" {
					h := inner.LayoutBounds().H
					if h > 0 {
						if a.session.processHeights == nil {
							a.session.processHeights = make(map[string]float32)
						}
						a.session.processHeights[messages[i].ID] = h
					} else {
						h = a.session.processHeights[messages[i].ID]
					}
					if progress := a.session.processScale(id); progress < 1 {
						row.ClipY().Opacity(progress).Height(progress * h)
						if h == 0 && progress > 0 {
							c.AnimationFrame()
						}
					}
				}
			}
		})
		list.Selectable().Fill().Padding(24, padding, scrollFadeHeight, padding).Gap(0).TrackScroll(&a.scroll)
		if a.renderingPane != nil {
			// A Selectable container takes over its text's context menus:
			// give it the pane's, with the text's editing items on top.
			root, pane := a.activeWorkspace, a.renderingPane
			list.ContextMenu(func(menu *native.Menu) {
				menu.EditItems()
				a.paneMenu(root, pane)(menu)
			})
		}
		a.selSeed = findSeed(list.SelectedTexts())
		a.loadHistoryNearTop()
		if !a.list.AtEnd() {
			// The tint fades into the composer surface without intercepting scrolling.
			native.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(scrollFadeHeight).
				PassThrough().Gradient(t.Background.Alpha(0), t.Background, 180)
			native.Row(c).Absolute().Left(0).Right(0).Bottom(returnButtonGap).
				Height(returnButtonSize).Justify(native.Center).PassThrough().Children(func() {
				b := outlinedButton(c, "返回最新消息").Size(returnButtonSize, returnButtonSize).
					Radius(returnButtonSize/2).
					Border(1, t.Border).Shadow(0, 1, 4, 0, native.RGBA(0, 0, 0, 0.06))
				buttonFace(c, b, t.Background, t.SurfaceHover, t.SurfacePressed, returnButtonSize/2, 0)
				b.Children(func() { native.Icon(c, downIcon).Size(18, 18) })
				if b.Clicked() {
					a.list.ScrollToEnd()
				}
			})
		}
		a.conversationNavigator(c, entries)
		a.conversationFindBar(c)
	})
}

func (a *App) composer(c *native.Context) {
	t := c.Theme()
	box := native.Column(c).FillWidth().Padding(14, 14, 10, 14).Gap(8).Radius(22).
		Border(1, t.Border.Alpha(0.7)).Background(composerBackground(t)).
		Shadow(0, 2, 12, 0, native.RGBA(0, 0, 0, 0.04))
	if box.FocusWithin() {
		box.Border(1, t.Accent).DrawOver(func(painter *native.Painter, bounds native.Rect) {
			painter.Stroke(native.Rect{X: bounds.X - 2, Y: bounds.Y - 2, W: bounds.W + 4, H: bounds.H + 4}, t.Accent.Alpha(.14), 24, 4)
		})
	}
	box.Children(func() {
		a.draftImageStrip(c)
		composer := native.TextAreaBase(c, &a.State.Draft).Label("任务输入").
			Placeholder("与 "+a.kind.String()+" 协作…").FontSize(14).FillWidth().Lines(2, 6).Padding(2, 4)
		for _, span := range filePathRanges(a.State.Draft) {
			composer.TextRanges(native.TextRange{Start: span[0], End: span[1], Color: t.Accent, Weight: 600})
		}
		a.focusPressedPane(composer)
		if a.composerFocus {
			a.composerFocus = false
			if a.paneIsFocused() {
				composer.Focus()
			}
		}
		if a.completion.focus && a.paneIsFocused() {
			composer.Focus().SetTextSelection(a.completion.caret, a.completion.caret)
			a.completion.focus = false
		}
		composer.OnPaste(a.panePaste(func(_ string) bool { return a.pasteImages() }))
		composing := composer.Composing()
		// Element handles are valid during build; key callbacks use the captured caret.
		anchor, caret := composer.TextAnchorCaret()
		query, show := queryCompletion(a.State.Draft, anchor, caret, a.kind)
		if show && !composing {
			// Claude 的斜杠命令来自运行时目录，与技能共用这份按会话缓存。
			if query.kind == '$' || query.kind == '/' && a.kind.Normalized() != agent.Codex {
				a.loadCompletionSkills()
			}
			if query.kind == '@' {
				a.loadCompletionFiles(false)
			}
			key := completionKey(query)
			if a.completion.key != key {
				a.completion.key = key
				a.completion.selected = 0
				a.completion.reveal = true
				a.completion.fileShown = completionFilePageSize
				a.completion.fileScroll.Y = 0
			}
			if a.completion.dismissed != key {
				native.Overlay(c, func() { a.completionPanel(c, composer, query) })
			}
		} else {
			a.completion.dismissed = ""
			a.completion.key = ""
		}

		composer.OnTextKey(a.paneTextKey(func(k native.TextKey) bool {
			if composing {
				return false
			}
			q, ok := queryCompletion(k.Text(), anchor, caret, a.kind)
			if ok && k.Mods == 0 && a.completion.dismissed != completionKey(q) {
				items := a.completionItems(q)
				switch k.Key {
				case native.KeyEscape:
					a.completion.dismissed = completionKey(q)
					return true
				case native.KeyDown, native.KeyUp:
					if len(items) > 0 {
						delta := 1
						if k.Key == native.KeyUp {
							delta = -1
						}
						a.completion.selected = (a.completion.selected + delta + len(items)) % len(items)
						a.completion.reveal = true
					}
					return true
				case native.KeyEnter, native.KeyTab:
					if len(items) > 0 {
						a.State.Draft = k.Text()
						a.acceptCompletion(q, items[a.completion.selected%len(items)])
						k.SetText(a.State.Draft)
					}
					return true
				}
			}
			if k.Key == native.KeyEscape && k.Mods == 0 {
				return a.composerEscape()
			}
			if k.Key == native.KeyEnter && k.Mods == 0 {
				a.State.Draft = k.Text()
				if a.canSend() {
					a.submit()
					k.SetText(a.State.Draft)
				}
				return true
			}
			return false
		}))
		native.Row(c).FillWidth().Gap(8).AlignItems(native.Center).Children(func() {
			a.imagePickerButton(c)
			a.modelPicker(c)
			a.planModeBadge(c)
			native.Spacer(c)
			hint := "Enter 发送 · ⌘ Enter 换行"
			if len(a.pendingInputs) >= 16 {
				hint = "最多保留 16 条待发送消息，请先处理已有消息"
			}
			stopping := a.State.Busy && strings.TrimSpace(a.State.Draft) == ""
			disabled := !a.canSend()
			if stopping {
				hint = "Esc 停止当前任务"
				disabled = a.State.TurnID == "" && a.submitCancel == nil || a.State.Stopping()
			}
			if taskSendButton(c.Key("task-action"), stopping, disabled, hint).Clicked() {
				if stopping {
					a.stop()
				} else {
					a.submit()
				}
			}
		})
	})
	a.receiveImageDrop(box)
}

func (a *App) runtimeDetails(c *native.Context) {
	t := c.Theme()
	native.Column(c).Width(300).Padding(12).Gap(12).Children(func() {
		native.Row(c).FillWidth().AlignItems(native.Center).Gap(12).Children(func() {
			native.Text(c, "运行详情").FontSize(12).FontWeight(600).Grow(1)
			version := a.version
			if a.kind.Normalized() != agent.Codex {
				version = a.kind.String()
				if a.State.RuntimeVersion != "" {
					version += " " + a.State.RuntimeVersion
				}
			}
			native.Text(c, version).FontSize(10).TextColor(t.TextMuted).SingleLine().Shrink(1).MinWidth(0)
		})
		model := a.State.Model
		if model == "" {
			model = "会话建立后确认"
		} else {
			model = modelDisplayName(a.session.models, model)
		}
		native.Column(c).FillWidth().Padding(10, 12).Gap(5).Radius(8).Background(t.Surface).Children(func() {
			native.Text(c, "当前模型").FontSize(11).TextColor(t.TextMuted)
			native.Text(c, model).FontSize(12).FontWeight(500).Selectable().FillWidth().MinWidth(0)
		})
		policy := readablePolicy(a.State.Policy)
		values := [][2]string{{"审批", policy}, {"文件访问", readableSandbox(a.State.Sandbox)}}
		native.Column(c).FillWidth().Gap(8).Children(func() {
			for _, value := range values {
				text := value[1]
				if text == "" {
					text = "会话建立后确认"
				}
				native.Row(c).FillWidth().Gap(12).AlignItems(native.Center).Children(func() {
					native.Text(c, value[0]).Width(60).Shrink(0).FontSize(11).TextColor(t.TextMuted)
					native.Text(c, text).FontSize(12).Selectable().Grow(1).MinWidth(0)
				})
			}
		})
		native.Box(c).FillWidth().Height(1).Background(t.Border.Alpha(0.6))
		native.Row(c).FillWidth().Children(func() {
			native.Spacer(c)
			reload := treeButton(c, "重新加载会话").Height(24).Padding(0, 6).
				Disabled(a.State.ThreadID == "" || a.State.Busy || a.State.Disconnected || a.recovering)
			reload.Children(func() { native.Text(c, "重新加载会话").FontSize(11).TextColor(t.TextMuted) })
			if reload.Clicked() {
				a.detailsOpen = false
				a.recoverSession(a.session)
			}
		})
	})
}

func (a *App) message(c *native.Context, m *app.Message) {
	t := c.Theme()
	readingColumn(c, func() {
		native.Column(c.Key(m.ID)).FillWidth().Gap(4).Children(func() {
			if m.Role == "user" {
				a.userMessage(c, m)
			} else if m.Role == "assistant" {
				if m.Plan {
					native.Text(c, "计划").FontSize(12).TextColor(t.TextMuted)
				}
				a.messageMarkdown(c, m)
				if text := a.replyCopyText(m); text != "" {
					native.Row(c).Gap(8).Children(func() {
						a.planActions(c, m)
						glyph, label := copyIcon, "复制回复"
						if a.copiedID == m.ID {
							glyph, label = checkIcon, "已复制"
						}
						b := quietIconButton(c, label).Size(24, 24)
						b.Children(func() { native.Icon(c, glyph).Size(16, 16) })
						if b.Clicked() {
							c.WriteClipboard(text)
							a.copiedID = m.ID
						}
					})
				}
			} else if m.Interaction != nil {
				a.interactionCard(c, m.Interaction)
			} else if m.Tool != nil {
				a.toolCard(c, m.Tool)
			} else {
				native.Column(c).FillWidth().Gap(6).Padding(12, 14).Radius(10).Background(t.Surface).Children(func() {
					native.Text(c, "工具").FontSize(11).TextColor(t.TextMuted)
					a.markedMessageText(c, m, m.Text).Selectable().Font(a.codeFontFamily()).FontSize(12)
				})
			}
		})
	})
}
