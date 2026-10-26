package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
)

var gitPlusIcon = icon(`<path d="M12 5v14M5 12h14"/>`)
var gitMinusIcon = icon(`<path d="M5 12h14"/>`)
var gitFetchIcon = icon(`<path d="M12 3v12m-4-4 4 4 4-4M4 17v4h16v-4"/>`)
var gitPushIcon = icon(`<path d="M12 16V4m-4 4 4-4 4 4M4 17v4h16v-4"/>`)
var gitMoreIcon = icon(`<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>`)
var gitUndoIcon = icon(`<path d="M4 5v6h6M4 11a8 8 0 1 1 2 8"/>`)
var gitOpenFileIcon = icon(`<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8M14 2v6h6M9 15h7m-3-3 3 3-3 3"/>`)

func gitIconButton(c *native.Context, label string, glyph *native.SVG, disabled bool) native.Element {
	b := quietButton(c, label).Size(24, 24).Padding(4).Disabled(disabled)
	b.Children(func() { native.Icon(c, glyph).Size(13, 13) })
	return b
}

func (a *App) gitPanel(c *native.Context, w *workspacePage) {
	a.startWatcher(w)
	t := c.Theme()
	if w.gitTime.IsZero() && !w.gitLoading {
		a.refreshGit(w)
	}
	initialLoading := w.gitLoading && w.gitFingerprint == ""
	busy := w.gitBusy != "" || w.gitPreparing || initialLoading || a.gitQuitPending || a.quitStopping
	native.Row(c.Key("git-toolbar")).FillWidth().Height(36).Padding(0, 12).Gap(2).AlignItems(native.Center).Children(func() {
		branch := w.git.Branch
		if branch == "" {
			branch = projectName(w.root)
		}
		if branch == "(detached)" && len(w.git.Head) >= 8 {
			branch = w.git.Head[:8]
		}
		button := treeButton(c, "选择 Git 分支").Height(28).Padding(0, 4).Grow(1).MinWidth(0).Disabled(busy)
		button.Children(func() {
			native.Row(c).Fill().Gap(5).AlignItems(native.Center).Children(func() {
				native.Icon(c, gitIcon).Size(13, 13)
				native.Text(c, branch).FontSize(12).Bold().SingleLine().Shrink(1).MinWidth(0)
				native.Icon(c, chevron).Size(10, 10).Rotate(90)
			})
		})
		if button.Clicked() {
			w.gitBranchPicker = !w.gitBranchPicker
		}
		if len(w.gitRemotes) > 0 {
			if gitIconButton(c.Key("fetch"), "获取远程更新", gitFetchIcon, busy).Clicked() {
				a.gitRemoteAction(w, "fetch")
			}
			if w.git.Upstream != "" {
				if gitIconButton(c.Key("pull"), fmt.Sprintf("拉取 %d 个提交", w.git.Behind), gitFetchIcon, busy).Clicked() {
					a.gitRemoteAction(w, "pull")
				}
				if gitIconButton(c.Key("push"), fmt.Sprintf("推送 %d 个提交", w.git.Ahead), gitPushIcon, busy).Clicked() {
					a.gitRemoteAction(w, "push")
				}
			} else if w.git.Head != "(initial)" && w.git.Head != "" {
				if gitIconButton(c.Key("publish"), "发布分支", gitPushIcon, busy).Clicked() {
					a.gitRemoteAction(w, "publish")
				}
			}
		}
		if gitIconButton(c.Key("refresh"), "刷新 Git", refreshProjectIcon, busy).Clicked() {
			a.refreshGit(w)
		}
	})
	if w.git.Upstream != "" {
		native.Row(c).FillWidth().Padding(0, 16, 5, 16).Gap(8).Children(func() {
			native.Text(c, "→ "+w.git.Upstream).FontSize(10).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
			native.Text(c, fmt.Sprintf("%d↓ %d↑", w.git.Behind, w.git.Ahead)).FontSize(10).TextColor(t.TextMuted).SingleLine()
		})
	}
	if w.gitBranchPicker {
		a.gitBranchPickerView(c.Key("branches"), w)
	}
	if w.gitError != "" && w.gitFingerprint == "" {
		message := "读取失败：" + w.gitError
		if strings.Contains(w.gitError, "not a git repository") {
			message = "当前文件夹未使用 Git"
		}
		native.Text(c, message).FontSize(11).TextColor(t.TextMuted).MaxLines(4).Padding(12)
		return
	}
	if initialLoading {
		native.Text(c, "正在读取 Git…").FontSize(11).TextColor(t.TextMuted).Padding(8, 14)
	}
	if w.gitFingerprint != "" {
		a.gitCommitBox(c.Key("commit"), w, busy)
		a.gitRefreshStats(w)
	}
	if w.gitDecision != nil {
		a.gitDecisionView(c.Key("decision"), w)
	}
	if w.gitOperation != "" {
		native.Column(c).FillWidth().Padding(6, 12).Gap(6).Children(func() {
			native.Text(c, "正在进行 "+w.gitOperation).FontSize(11).TextColor(t.Warning)
			native.Row(c).Gap(6).Children(func() {
				if actionButton(c, "继续").Disabled(busy || len(gitEntries(w, "冲突")) > 0).Clicked() {
					op := w.gitOperation
					a.gitWithSavedFiles(w, nil, func() {
						a.gitAction(w, "继续 "+op, func(ctx context.Context) error { return gitview.ContinueOperation(ctx, w.root, op, false) }, nil)
					})
				}
				if actionButton(c, "中止").Disabled(busy).Clicked() {
					a.gitAbort(w)
				}
			})
		})
	}
	message, col := w.gitNotice, t.TextMuted
	if w.gitBusy != "" {
		message = "正在" + w.gitBusy + "…"
	}
	if w.gitActionError != "" {
		message, col = w.gitActionError, t.Danger
	} else if w.gitError != "" {
		message, col = w.gitError, t.Danger
	}
	if message != "" {
		native.Text(c, message).FontSize(10).TextColor(col).MaxLines(3).Padding(4, 14)
	}
	native.Scroll(c.Key("git-list")).TrackScroll(&w.gitScroll).FillWidth().Grow(1).MinHeight(0).Children(func() {
		native.Column(c).FillWidth().Padding(4, 8).Gap(8).Children(func() {
			for _, group := range []string{"冲突", "已暂存的更改", "更改"} {
				entries := gitEntries(w, group)
				if group == "冲突" && len(entries) == 0 {
					continue
				}
				a.gitGroup(c.Key(group), w, group, entries, busy)
			}
			a.gitHistoryView(c.Key("history"), w)
		})
	})
}

func (a *App) gitCommitBox(c *native.Context, w *workspacePage, busy bool) {
	t := c.Theme()
	native.Column(c).FillWidth().Padding(4, 12, 8, 12).Gap(6).Children(func() {
		area := native.TextArea(c.Key("message"), &w.gitMessage).Label("Git 提交信息").
			Placeholder("描述这次改动…").FontSize(12).Lines(1, 5).MinHeight(34).MaxHeight(110).
			FillWidth().Padding(7, 9).Radius(8).Disabled(busy)
		staged := len(gitEntries(w, "已暂存的更改"))
		label := fmt.Sprintf("提交 %d 个文件", staged)
		all := staged == 0
		if all {
			label = "全部暂存并提交"
		}
		ready := !busy && strings.TrimSpace(w.gitMessage) != "" && len(gitEntries(w, "冲突")) == 0 &&
			(staged > 0 || len(gitEntries(w, "更改")) > 0)
		native.Row(c).FillWidth().Gap(6).AlignItems(native.Center).Children(func() {
			b := native.ButtonBase(c.Key("submit")).Label(label).Grow(1).Height(30).Radius(8).Disabled(!ready)
			buttonFace(c, b, t.Text, t.Text.Mix(t.Background, .15), t.Text.Mix(t.Background, .25), 8, 0)
			b.Children(func() { native.Text(c, label).FontSize(11).TextColor(t.Background).SingleLine() })
			if b.Clicked() || area.Shortcut(native.Cmd, native.KeyEnter) && ready {
				a.gitCommitChanges(w, all)
			}
			generateLabel := "生成"
			if w.gitGenerating {
				generateLabel = "停止生成"
			}
			if actionButton(c.Key("generate"), generateLabel).Height(30).Disabled(busy).Clicked() {
				a.gitGenerateMessage(w)
			}
			if staged > 0 {
				gitIconButton(c.Key("commit-more"), "更多提交操作", gitMoreIcon, busy).Menu(func(m *native.Menu) {
					if m.Item("全部暂存并提交").Disabled(!ready || len(gitEntries(w, "更改")) == 0).Chosen() {
						a.gitCommitChanges(w, true)
					}
				})
			}
		})
	})
}

func (a *App) gitDecisionView(c *native.Context, w *workspacePage) {
	decision := w.gitDecision
	native.Column(c).FillWidth().Margin(4, 12).Padding(10).Gap(7).Radius(8).
		Background(c.Theme().Background).Border(1, c.Theme().Border).Children(func() {
		native.Text(c, decision.title).FontSize(12).Bold().MaxLines(2)
		native.Text(c, decision.detail).FontSize(10).TextColor(c.Theme().TextMuted).MaxLines(6)
		native.Column(c).FillWidth().Gap(4).Children(func() {
			for i, label := range decision.labels {
				if actionButton(c.Key(label), label).FillWidth().Height(28).Clicked() {
					w.gitDecision = nil
					if decision.actions[i] != nil {
						decision.actions[i]()
					}
				}
			}
		})
	})
}

func (a *App) gitGroup(c *native.Context, w *workspacePage, group string, entries []gitview.Entry, busy bool) {
	native.Column(c).FillWidth().Gap(0).Children(func() {
		t := c.Theme()
		row := native.Row(c.Key("heading")).FillWidth().Height(26).Radius(5).AlignItems(native.Center)
		shown := row.Hovered() || row.FocusWithin()
		if shown {
			row.Background(t.SurfaceHover)
		}
		row.Children(func() {
			header := native.ButtonBase(c.Key("header")).Label("折叠 "+group).Grow(1).FillHeight().Padding(0, 6)
			header.Children(func() {
				native.Row(c).Fill().Gap(6).AlignItems(native.Center).Children(func() {
					g := native.Icon(c, chevron).Size(12, 12)
					if w.collapsed[group] {
						g.Rotate(-90)
					}
					native.Text(c, group).FontSize(12).Bold().SingleLine()
				})
			})
			if header.Clicked() {
				w.collapsed[group] = !w.collapsed[group]
			}
			native.Row(c.Key("group-actions")).Width(24).FillHeight().AlignItems(native.Center).Children(func() {
				if len(entries) > 0 && group != "冲突" && (shown || header.FocusVisible()) {
					label, glyph := "全部暂存", gitPlusIcon
					if group == "已暂存的更改" {
						label, glyph = "全部取消暂存", gitMinusIcon
					}
					if gitIconButton(c.Key("all"), label, glyph, busy).Clicked() {
						a.gitStage(w, entries, group == "已暂存的更改")
					}
				}
			})
			native.Box(c.Key("count")).MinWidth(22).Height(18).Margin(0, 5).Padding(0, 5).
				Radius(9).Background(t.TextMuted.Alpha(.1)).AlignItems(native.Center).Justify(native.Center).Children(func() {
				native.Text(c, fmt.Sprint(len(entries))).FontSize(10).TextColor(t.TextMuted).SingleLine()
			})
		})
		if w.collapsed[group] {
			return
		}
		var selected []gitview.Entry
		for _, entry := range entries {
			if w.gitSelection[gitKey(group, entry.Path)] {
				selected = append(selected, entry)
			}
		}
		if len(selected) > 1 && group != "冲突" {
			native.Row(c.Key("selection")).FillWidth().Height(28).Padding(0, 8).Gap(6).AlignItems(native.Center).Children(func() {
				native.Text(c, fmt.Sprintf("已选 %d 项", len(selected))).FontSize(10).TextColor(c.Theme().TextMuted).Grow(1)
				label := "暂存所选"
				if group == "已暂存的更改" {
					label = "取消暂存所选"
				}
				if actionButton(c, label).Height(24).Disabled(busy).Clicked() {
					a.gitStage(w, selected, group == "已暂存的更改")
				}
			})
		}
		if len(entries) == 0 {
			native.Text(c, "没有"+group).FontSize(11).TextColor(c.Theme().TextMuted).Padding(4, 24)
		}
		for _, entry := range entries {
			a.gitFileRow(c.Key(entry.Path), w, group, entry, busy)
		}
	})
}

func gitStatusLabel(e gitview.Entry, staged bool) string {
	if e.Conflict {
		return "冲突"
	}
	if e.XY == "??" {
		return "新增"
	}
	index := 1
	if staged {
		index = 0
	}
	if len(e.XY) <= index {
		return "修改"
	}
	switch e.XY[index] {
	case 'A':
		return "新增"
	case 'D':
		return "删除"
	case 'R':
		return "重命名"
	case 'C':
		return "复制"
	case 'T':
		return "类型"
	}
	return "修改"
}

func gitStatusCode(e gitview.Entry, staged bool) string {
	if e.Conflict || e.XY == "??" {
		return "U"
	}
	index := 1
	if staged {
		index = 0
	}
	if len(e.XY) > index && strings.ContainsRune("ADRCT", rune(e.XY[index])) {
		return e.XY[index : index+1]
	}
	return "M"
}

func (a *App) gitFileActive(w *workspacePage, group string, entry gitview.Entry) bool {
	d := a.currentFile()
	if d == nil || d.root != w.root {
		return false
	}
	if group == "冲突" {
		return d.path == filepath.Join(w.root, entry.Path) && d.gitConflict != nil
	}
	return d.kind == "diff" && d.gitCommit == "" && d.path == filepath.Join(w.root, entry.Path) &&
		(d.compare == "已暂存") == (group == "已暂存的更改") && d.compare != "外部更改"
}

func (a *App) gitFileRow(c *native.Context, w *workspacePage, group string, entry gitview.Entry, busy bool) {
	t := c.Theme()
	staged := group == "已暂存的更改"
	row := native.Row(c).FillWidth().Height(28).Radius(6).AlignItems(native.Center).Gap(0)
	shown := row.Hovered() || row.FocusWithin()
	if a.gitFileActive(w, group, entry) {
		row.Background(t.Accent.Alpha(.12))
	} else if w.gitSelection[gitKey(group, entry.Path)] {
		row.Background(t.SurfacePressed)
	} else if shown {
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		b := native.ButtonBase(c.Key("open")).Label("Git "+group+" "+entry.Path).Grow(1).FillHeight().MinWidth(0).Padding(0, 8)
		b.ContextMenu(func(m *native.Menu) {
			if group != "冲突" {
				label := "暂存"
				if staged {
					label = "取消暂存"
				}
				if m.Item(label).Disabled(busy).Chosen() {
					a.gitStage(w, a.gitChosen(w, group, entry), staged)
				}
				if !staged && m.Item("撤销改动…").Disabled(busy).Chosen() {
					a.gitDiscard(w, a.gitChosen(w, group, entry))
				}
			}
			if m.Item("打开文件").Disabled(entry.XY == ".D" || entry.XY == "D.").Chosen() {
				a.openFile(w.root, filepath.Join(w.root, entry.Path))
			}
			m.Separator()
			for _, intent := range []string{"解释这些改动", "检查问题", "继续修改"} {
				if m.Item(intent).Chosen() {
					a.gitToCodex(w, a.gitChosen(w, group, entry), staged, intent, nil)
				}
			}
		})
		b.Children(func() {
			native.Row(c).Fill().Gap(6).AlignItems(native.Center).Children(func() {
				native.Icon(c, fileIcon).Size(13, 13).TextColor(t.TextMuted)
				native.Row(c.Key("name")).Grow(1).MinWidth(0).Gap(5).AlignItems(native.Center).Children(func() {
					native.Text(c, filepath.Base(entry.Path)).FontSize(12).TextColor(t.Text).SingleLine().Shrink(1).MinWidth(0)
					if parent := filepath.Dir(entry.Path); parent != "." {
						native.Text(c, parent).FontSize(10).TextColor(t.TextMuted.Alpha(.8)).SingleLine().Shrink(3).MinWidth(0)
					}
					native.Spacer(c)
				})

			})
		})
		if b.Clicked() {
			if a.gitSelect(w, group, entry, b.ClickModifiers()) {
				if entry.Conflict {
					a.gitOpenConflict(w, entry)
				} else {
					a.openGitDiff(w, entry, staged)
				}
			}
		}
		// Reserve the action strip even before hover so the filename never moves.
		strip := native.Row(c.Key("actions")).Width(72).FillHeight().AlignItems(native.Center)
		strip.Children(func() {
			if shown || b.FocusVisible() {
				if gitIconButton(c.Key("open-file"), "打开文件 "+entry.Path, gitOpenFileIcon, gitStatusLabel(entry, staged) == "删除").Tooltip("打开文件").Clicked() {
					a.openFile(w.root, filepath.Join(w.root, entry.Path))
				}
				if group == "更改" {
					if gitIconButton(c.Key("discard"), "撤销改动 "+entry.Path, gitUndoIcon, busy).Tooltip("撤销改动").Clicked() {
						a.gitDiscard(w, a.gitChosen(w, group, entry))
					}
				} else {
					native.Box(c).Width(24)
				}
				label, glyph := "暂存 "+entry.Path, gitPlusIcon
				if staged {
					label, glyph = "取消暂存 "+entry.Path, gitMinusIcon
				}
				if entry.Conflict {
					label = "标记已解决 " + entry.Path
				}
				if gitIconButton(c, label, glyph, busy).Tooltip(strings.TrimSuffix(label, " "+entry.Path)).Clicked() {
					a.gitStage(w, a.gitChosen(w, group, entry), staged)
				}
			}
		})
		native.Row(c.Key("stats")).Gap(4).Padding(0, 4).Shrink(0).AlignItems(native.Center).Children(func() {
			if stat, ok := w.gitStats[gitKey(fmt.Sprint(staged), entry.Path)]; ok && !stat.Binary {
				if stat.Added > 0 {
					native.Text(c, fmt.Sprintf("+%d", stat.Added)).FontSize(9).TextColor(t.Success).SingleLine()
				}
				if stat.Deleted > 0 {
					native.Text(c, fmt.Sprintf("−%d", stat.Deleted)).FontSize(9).TextColor(t.Danger).SingleLine()
				}
			}
		})
		color := t.Warning
		switch gitStatusLabel(entry, staged) {
		case "冲突", "删除":
			color = t.Danger
		case "新增":
			color = t.Success
		case "重命名", "复制":
			color = t.Accent
		}
		native.Box(c.Key("status")).Width(20).FillHeight().AlignItems(native.Center).Justify(native.Center).
			Tooltip(gitStatusLabel(entry, staged)).Children(func() {
			native.Text(c, gitStatusCode(entry, staged)).FontSize(11).TextColor(color).SingleLine()
		})
	})
}

func (a *App) gitHistoryView(c *native.Context, w *workspacePage) {
	native.Column(c).FillWidth().Gap(0).Children(func() {
		header := treeButton(c.Key("header"), "切换最近提交").FillWidth().Height(26).Padding(0, 6)
		header.Children(func() {
			native.Row(c).Fill().AlignItems(native.Center).Gap(6).Children(func() {
				g := native.Icon(c, chevron).Size(12, 12)
				if !w.collapsed["最近提交已展开"] {
					g.Rotate(-90)
				}
				native.Text(c, "最近提交").FontSize(12).Bold()
			})
		})
		if header.Clicked() {
			w.collapsed["最近提交已展开"] = !w.collapsed["最近提交已展开"]
		}
		if !w.collapsed["最近提交已展开"] {
			return
		}
		if !w.gitHistoryLoaded && !w.gitHistoryLoading {
			a.gitLoadHistory(w, false)
		}
		for _, info := range w.gitHistory {
			b := treeButton(c.Key(info.Hash), "提交 "+info.Hash).FillWidth().Height(42).Padding(4, 8)
			b.Children(func() {
				native.Column(c).FillWidth().Gap(2).Children(func() {
					native.Text(c, info.Subject).FontSize(11).TextColor(c.Theme().Text).SingleLine()
					native.Text(c, info.Hash[:8]+" · "+info.Author+" · "+info.Time.Format("01-02 15:04")).FontSize(9).TextColor(c.Theme().TextMuted).SingleLine()
				})
			})
			if b.Clicked() {
				a.gitOpenCommit(w, info)
			}
		}
		if w.gitHistoryError != "" {
			native.Text(c, w.gitHistoryError).FontSize(10).TextColor(c.Theme().Danger).MaxLines(2)
		}
		if w.gitHistoryLoading {
			native.Text(c, "正在读取提交…").FontSize(10).TextColor(c.Theme().TextMuted).Padding(4, 8)
		} else if len(w.gitHistory) == 0 {
			native.Text(c, "尚无提交").FontSize(11).TextColor(c.Theme().TextMuted).Padding(4, 24)
		} else if w.gitHistoryMore {
			if actionButton(c, "加载更多提交").FillWidth().Height(28).Clicked() {
				a.gitLoadHistory(w, true)
			}
		}
	})
}
