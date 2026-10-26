package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shenming0821/Fuyu/internal/app"
)

const (
	readingWidth     float32 = 800
	returnButtonSize float32 = 32
	returnButtonGap  float32 = 20
	scrollFadeHeight float32 = 80
)

var (
	folderIcon  = icon(`<path d="M3 7V5a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Zm0 1h18"/>`)
	newIcon     = icon(`<path d="M12 5H5a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-7M15 3l6 6M10 14l2-5 7-7 4 4-7 7-6 1Z"/>`)
	upIcon      = icon(`<path d="M12 19V5m-6 6 6-6 6 6"/>`)
	downIcon    = icon(`<path d="M12 5v14m-6-6 6 6 6-6"/>`)
	stopIcon    = icon(`<rect x="6" y="6" width="12" height="12" rx="1" fill="currentColor" stroke="none"/>`)
	copyIcon    = icon(`<rect x="8" y="8" width="12" height="13" rx="2"/><path d="M15 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>`)
	checkIcon   = icon(`<path d="m5 12 4 4L19 6"/>`)
	chevron     = icon(`<path d="m7 10 5 5 5-5"/>`)
	sidebarIcon = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>`)
	historyIcon = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/>`)
)

func icon(paths string) *native.SVG {
	return native.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

func quietButton(c *native.Context, label string) native.Element {
	t := c.Theme()
	b := native.ButtonBase(c).Label(label).Tooltip(label).Radius(8).Padding(6).TextColor(t.TextMuted)
	if b.Pressed() {
		b.Background(t.SurfacePressed)
	} else if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	return b
}

// Each message and the composer share this width independently of list scrolling.
func readingColumn(c *native.Context, build func()) {
	native.Row(c).FillWidth().Justify(native.Center).Children(func() {
		native.Column(c).FillWidth().MaxWidth(readingWidth).MinWidth(0).Children(build)
	})
}

func (a *App) view(c *native.Context) {
	if !a.quitDialog && c.Shortcut(native.Cmd, native.KeyW) {
		if d := a.currentFile(); d != nil {
			a.closeFile(d)
		} else if a.open {
			a.closeSession(a.session)
		}
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
			native.Row(c).Fill().Children(func() {
				if d := a.currentFile(); d != nil {
					a.fileView(c.Key(d.id), d, leftShown, rightShown)
				} else if a.open {
					a.sessionView(c.Key(a.session.id), leftShown, rightShown)
				} else {
					a.emptyWorkspace(c.Key("empty-workspace"), leftShown, rightShown)
				}
				if rightShown {
					a.rightPanel(c)
				}
			})
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
}

func (a *App) emptyWorkspace(c *native.Context, leftShown, rightShown bool) {
	t := c.Theme()
	native.Column(c).Grow(1).FillHeight().MinWidth(0).Background(t.Background).Children(func() {
		native.Row(c).FillWidth().Height(46).Padding(0, 16).Gap(12).AlignItems(native.Center).Children(func() {
			if !leftShown {
				a.sidebarToggle(c, true)
			}
			native.Spacer(c)
			create := quietButton(c, "新会话").Size(28, 28)
			create.Children(func() { native.Icon(c, newIcon).Size(15, 15) })
			if create.Clicked() {
				a.newSession(a.workspaceCwd())
			}
			if !rightShown {
				a.sidebarToggle(c, false)
			}
		})
		a.fileExitNotice(c)
	})
}

func (a *App) sessionView(c *native.Context, leftShown, rightShown bool) {
	t := c.Theme()
	a.list.FollowEnd = true
	native.Column(c).Grow(1).FillHeight().MinWidth(0).Background(t.Background).Children(func() {
		a.toolbar(c, leftShown, rightShown)
		a.sessionTabs(c)
		a.fileExitNotice(c)
		if a.historyCursor != nil {
			native.Row(c).FillWidth().Padding(4, 24).Justify(native.Center).Children(func() {
				if actionButton(c, "加载更早消息").Disabled(a.historyLoading || a.recovering).Clicked() {
					a.loadOlder()
				}
			})
		}
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
				native.Text(c, "Codex 需要你的决定").FontSize(12).TextColor(t.TextMuted)
			})
		}
		if len(a.State.Messages) == 0 {
			native.Column(c).Grow(1).FillWidth().Center().Gap(12).Padding(24).Children(func() {
				native.Text(c, "从一个清晰的任务开始").FontSize(25)
				native.Text(c, "与 Codex 一起，把想法变成现实。").FontSize(14).TextColor(t.TextMuted)
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
				if a.State.Error != "" {
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
				if a.State.Phase == "执行失败" && !a.State.Disconnected && !a.State.Busy {
					if actionButton(c, "重试本次任务").Clicked() {
						for i := len(a.State.Messages) - 1; i >= 0; i-- {
							if a.State.Messages[i].Role == "user" {
								a.State.Draft = a.State.Messages[i].Text
								a.submit()
								break
							}
						}
					}
				}
				a.composer(c)
			})
		})
	})
}

func (a *App) conversation(c *native.Context) {
	t := c.Theme()
	a.session.restoreRowOffset(c)
	rows := a.session.conversationRows()
	messages := a.session.displayMessages()
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
		native.List(c, &a.list, count, func(i int) {
			if i == len(messages) {
				readingColumn(c, func() { thinkingIndicator(c) })
			} else {
				native.Column(c).FillWidth().Bind(a.rowHandle(messages[i].ID)).Children(func() {
					if len(rows[i].commands) > 0 {
						readingColumn(c, func() { a.commandGroup(c.Key(messages[i].ID), rows[i].commands) })
					} else {
						a.message(c, messages[i])
					}
				})
			}
		}).Fill().Padding(24, 24, scrollFadeHeight, 24).Gap(28).TrackScroll(&a.scroll)
		if !a.list.AtEnd() {
			// The tint fades into the composer surface without intercepting scrolling.
			native.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(scrollFadeHeight).
				PassThrough().Gradient(t.Background.Alpha(0), t.Background, 180)
			native.Row(c).Absolute().Left(0).Right(0).Bottom(returnButtonGap).
				Height(returnButtonSize).Justify(native.Center).PassThrough().Children(func() {
				b := quietButton(c, "返回最新消息").Size(returnButtonSize, returnButtonSize).
					Radius(returnButtonSize/2).Background(t.Background).TextColor(t.Text).
					Border(1, t.Border).Shadow(0, 1, 4, 0, native.RGBA(0, 0, 0, 0.06))
				b.Children(func() { native.Icon(c, downIcon).Size(18, 18) })
				if b.Clicked() {
					a.list.ScrollToEnd()
				}
			})
		}
	})
}

func (a *App) toolbar(c *native.Context, leftShown, rightShown bool) {
	t := c.Theme()
	native.Row(c).FillWidth().Height(46).Padding(0, 16).Gap(12).
		AlignItems(native.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.5)).Children(func() {
		if !leftShown {
			a.sidebarToggle(c, true)
		}
		project := "Fuyu"
		if a.State.Cwd != "" {
			project = filepath.Base(a.State.Cwd)
		}
		b := quietButton(c, "工作目录").MinWidth(0).Shrink(1).Gap(8)
		b.Children(func() {
			native.Icon(c, folderIcon).Size(17, 17)
			native.Text(c, project).FontSize(13).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
			native.Icon(c, chevron).Size(12, 12)
		})
		if b.Clicked() {
			a.projectOpen = !a.projectOpen
		}
		native.Popover(c, b, &a.projectOpen, func() {
			native.Column(c).Width(360).Gap(10).Children(func() {
				path := a.State.Cwd
				if path == "" {
					path = "选择工作目录后开始任务"
				}
				native.Text(c, path).FontSize(13).Selectable()
				if native.Button(c, "选择目录").Disabled(a.State.Disconnected).Clicked() {
					a.projectOpen = false
					go a.chooseDirectory(a.session, a.State.Cwd)
				}
			})
		})
		native.Spacer(c)
		native.Text(c, a.State.Phase).FontSize(12).TextColor(t.TextMuted).SingleLine()
		if !rightShown {
			a.sidebarToggle(c, false)
		}
	})
}

func (a *App) composer(c *native.Context) {
	t := c.Theme()
	native.Column(c).FillWidth().Padding(14, 14, 10, 14).Gap(8).Radius(22).
		Border(1, t.Border.Alpha(0.7)).Background(t.Background).
		Shadow(0, 2, 12, 0, native.RGBA(0, 0, 0, 0.04)).Children(func() {
		composer := native.TextAreaBase(c, &a.State.Draft).Label("任务输入").
			Placeholder("与 Codex 协作…").FontSize(14).FillWidth().Lines(2, 6).Padding(2, 4)
		if a.completion.focus {
			composer.Focus().SetTextSelection(a.completion.caret, a.completion.caret)
			a.completion.focus = false
		}
		composing := composer.Composing()
		// Element handles are valid during build; key callbacks use the captured caret.
		anchor, caret := composer.TextAnchorCaret()
		query, show := queryCompletion(a.State.Draft, anchor, caret)
		if show && !composing {
			if query.kind == '$' {
				a.loadCompletionSkills()
			}
			key := completionKey(query)
			if a.completion.key != key {
				a.completion.key = key
				a.completion.selected = 0
				a.completion.reveal = true
			}
			if a.completion.dismissed != key {
				native.Overlay(c, func() { a.completionPanel(c, composer, query) })
			}
		} else {
			a.completion.dismissed = ""
			a.completion.key = ""
		}

		composer.OnTextKey(func(k native.TextKey) bool {
			if composing {
				return false
			}
			q, ok := queryCompletion(k.Text(), anchor, caret)
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
			if k.Key == native.KeyEnter && k.Mods == 0 {
				a.State.Draft = k.Text()
				if a.canSend() {
					a.submit()
					k.SetText(a.State.Draft)
				}
				return true
			}
			return false
		})
		native.Row(c).FillWidth().Gap(8).AlignItems(native.Center).Children(func() {
			model := "Codex"
			if a.State.Model != "" {
				model = a.State.Model[strings.LastIndex(a.State.Model, "/")+1:]
			}
			details := quietButton(c, "运行详情").Gap(5).MinWidth(0).Shrink(1)
			details.Children(func() {
				native.Text(c, model).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
				native.Icon(c, chevron).Size(12, 12)
			})
			if details.Clicked() {
				a.detailsOpen = !a.detailsOpen
			}
			native.Popover(c, details, &a.detailsOpen, func() { a.runtimeDetails(c) })
			native.Spacer(c)
			action, glyph := "发送", upIcon
			hint := "Enter 发送 · ⌘ Enter 换行"
			disabled := !a.canSend()
			if a.State.Busy {
				action, glyph = "停止", stopIcon
				hint = "停止当前任务"
				disabled = a.State.TurnID == "" || a.State.Phase == "正在停止"
			}
			button := native.ButtonBase(c).Label(action).Tooltip(hint).
				Size(34, 34).Radius(17).Background(t.Text).TextColor(t.Background).Disabled(disabled)
			if button.Hovered() {
				button.Opacity(0.8)
			}
			button.Children(func() { native.Icon(c, glyph).Size(20, 20) })
			if button.Clicked() {
				if a.State.Busy {
					a.stop()
				} else {
					a.submit()
				}
			}
		})
	})
}

func (a *App) runtimeDetails(c *native.Context) {
	t := c.Theme()
	native.Column(c).Width(420).Gap(10).Children(func() {
		native.Text(c, "运行详情").FontSize(14).Bold()
		values := [][2]string{
			{"版本", a.version}, {"模型", a.State.Model},
			{"审批", a.State.Policy}, {"沙箱", a.State.Sandbox},
		}
		for _, value := range values {
			text := value[1]
			if text == "" {
				text = "会话建立后确认"
			}
			native.Column(c).Gap(3).Children(func() {
				native.Text(c, value[0]).FontSize(11).TextColor(t.TextMuted)
				native.Text(c, text).FontSize(12).Selectable()
			})
		}
		native.Text(c, "Enter 发送 · ⌘ Enter 换行").FontSize(11).TextColor(t.TextMuted)
		if native.Button(c, "刷新原生历史").Disabled(a.State.ThreadID == "" || a.State.Busy || a.State.Disconnected || a.recovering).Clicked() {
			a.detailsOpen = false
			a.recoverSession(a.session)
		}
	})
}

func (a *App) message(c *native.Context, m *app.Message) {
	t := c.Theme()
	readingColumn(c, func() {
		native.Column(c.Key(m.ID)).FillWidth().Gap(8).Children(func() {
			if m.Role == "user" {
				native.Row(c).FillWidth().Justify(native.End).Children(func() {
					native.Text(c, m.Text).Selectable().FontSize(15).
						MaxWidthPercent(85).Padding(12, 16).Radius(20).Background(t.Surface)
				})
			} else if m.Role == "assistant" {
				if m.Done {
					markdown.View(c, m.Text, &a.md)
				} else {
					markdown.Stream(c, m.Text, &a.md)
				}
				if m.Done && m.Text != "" {
					native.Row(c).Children(func() {
						glyph, label := copyIcon, "复制回复"
						if a.copiedID == m.ID {
							glyph, label = checkIcon, "已复制"
						}
						b := quietButton(c, label).Size(28, 28)
						b.Children(func() { native.Icon(c, glyph).Size(16, 16) })
						if b.Clicked() {
							c.WriteClipboard(m.Text)
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
					native.Text(c, m.Text).Selectable().Font("monospace").FontSize(12)
				})
			}
		})
	})
}
