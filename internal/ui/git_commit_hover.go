// Portions adapted from Mayoi, Copyright (c) 2026 chencheng.
// Licensed under MIT; see THIRD_PARTY_NOTICES/Mayoi-LICENSE.
package ui

import (
	"fmt"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

type gitCommitStatRead struct {
	stat    gitview.CommitStat
	loading bool
	err     string
}

func (a *App) gitWantCommitStat(w *workspacePage, cm gitview.CommitInfo) *gitCommitStatRead {
	if read := w.gitCommitStats[cm.Hash]; read != nil {
		return read
	}
	read := &gitCommitStatRead{loading: true}
	if !a.canAsync() {
		read.loading = false
		read.err = "暂不可用"
		return read
	}
	if w.gitCommitStats == nil {
		w.gitCommitStats = map[string]*gitCommitStatRead{}
	}
	if len(w.gitCommitStats) >= 100 {
		for key, entry := range w.gitCommitStats {
			if !entry.loading {
				delete(w.gitCommitStats, key)
				break
			}
		}
	}
	if len(w.gitCommitStats) >= 100 {
		read.loading = false
		read.err = "暂不可用"
		return read
	}
	w.gitCommitStats[cm.Hash] = read
	ctx, cancel := a.projectContext(5 * time.Second)
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		stat, err := gitview.CommitStatistics(ctx, w.root, cm)
		a.update(func() {
			if w.gitCommitStats[cm.Hash] != read {
				return
			}
			read.loading = false
			read.stat = stat
			if err != nil {
				read.err = err.Error()
			}
		})
	})
	return read
}
func (a *App) gitCommitHoverCard(c *native.Context, w *workspacePage, row native.Element, cm gitview.CommitInfo) {
	t := c.Theme()
	native.TooltipBase(c, row, func(tip native.Element) {
		stat := a.gitWantCommitStat(w, cm)
		tip.AttachTo(row, native.AnchorRight, native.AnchorLeft).Margin(0, 0, 0, 8).Width(320).MaxWidthPercent(90).Radius(10).Background(t.Surface).Border(1, t.Border).TextColor(t.Text).Padding(12, 14).Gap(8)
		tip.Children(func() {
			native.Row(c).Gap(10).AlignItems(native.Center).Children(func() {
				gitCommitAvatar(c, cm, 28, t.Surface)
				native.Text(c, cm.Author).FontSize(13).Bold().SingleLine()
			})
			if len(cm.CoAuthors) > 0 {
				var names []string
				for _, person := range cm.CoAuthors {
					names = append(names, person.Name)
				}
				native.Text(c, "共同作者："+strings.Join(names, ", ")).FontSize(11.5).TextColor(t.TextMuted).MaxLines(2)
			}
			native.Text(c, cm.Subject).FontSize(12).MaxLines(3)
			native.Box(c).FillWidth().Height(1).Background(t.Border)
			field := func(name, value string) {
				native.Row(c).FillWidth().Gap(12).Children(func() {
					native.Text(c, name).FontSize(11.5).TextColor(t.TextMuted).Shrink(0)
					native.Spacer(c)
					native.Text(c, value).FontSize(11.5).SingleLine().Shrink(1).Tooltip(value)
				})
			}
			field("提交", cm.Short())
			switch {
			case stat.loading:
				field("改动", "正在读取…")
			case stat.err != "":
				field("改动", "—")
			default:
				field("改动", fmt.Sprintf("%d 个文件  +%d −%d", stat.stat.Files, stat.stat.Added, stat.stat.Deleted))
			}
			field("提交时间", cm.Time.Local().Format("2006-01-02 15:04:05"))
			if len(cm.Refs) > 0 {
				field("分支 / 标签", refNames(cm.Refs))
			}
			if w.gitUnpushed[cm.Hash] {
				native.Text(c, "尚未推送").FontSize(11.5).TextColor(t.TextMuted)
			}
			if len(cm.Parents) > 1 {
				native.Text(c, "合并提交").FontSize(11.5).TextColor(t.TextMuted)
			}
		})
	})
}
