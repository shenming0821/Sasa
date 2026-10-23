package ui

import (
	"context"
	"errors"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/codex"
)

var lockIcon = icon(`<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V6a4 4 0 0 1 8 0v4"/>`)

func (a *App) occupiedCard(c *native.Context) {
	t := c.Theme()
	native.Column(c).FillWidth().Padding(18).Gap(12).Radius(20).
		Border(1, t.Border.Alpha(.7)).Background(t.Surface).Children(func() {
		native.Row(c).FillWidth().Gap(12).AlignItems(native.Center).Children(func() {
			native.Icon(c, lockIcon).Size(20, 20).TextColor(t.TextMuted)
			native.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
				native.Text(c, "已在另一个应用中打开").FontSize(14).Bold()
				native.Text(c, "请先在那边关闭会话，才能在这里继续。").FontSize(12).TextColor(t.TextMuted)
			})
		})
		if a.State.Error != "" {
			native.Text(c, a.State.Error).FontSize(12).TextColor(t.Danger)
		}
		native.Row(c).FillWidth().Justify(native.End).Gap(8).Children(func() {
			retry := "重试"
			if a.recovering {
				retry = "正在重试…"
			}
			disabled := a.recovering || a.forking || a.State.Disconnected || a.application().quitStopping
			if actionButton(c, retry).Disabled(disabled).Clicked() {
				a.recoverSession(a.session)
			}
			label := "创建聊天分叉"
			if a.forking {
				label = "正在创建…"
			}
			if actionButton(c, label).Disabled(disabled || a.forkUncertain).Clicked() {
				a.forkSession()
			}
		})
	})
}

func (a *App) forkSession() {
	backend, ok := a.backend.(interface {
		ForkThread(context.Context, string) (codex.ResumedThread, error)
	})
	s, root := a.session, a.application()
	if !ok || !s.writerOccupied || s.recovering || s.forking || s.forkUncertain ||
		s.State.Disconnected || root.quitStopping {
		return
	}
	bound := a.forSession(s)
	threadID, draft, title := s.State.ThreadID, s.State.Draft, s.title()
	s.forking, s.State.Error = true, ""
	go func() {
		ctx, cancel := codex.RequestContext()
		defer cancel()
		result, err := backend.ForkThread(ctx, threadID)
		bound.update(func() {
			s.forking = false
			if err != nil {
				var rpc *codex.RPCError
				s.forkUncertain = !errors.As(err, &rpc)
				s.State.Error = "创建聊天分叉失败：" + err.Error()
				if s.forkUncertain {
					s.State.Error += " 创建结果尚未确认，请在历史中检查新会话后继续。"
				}
				return
			}
			if result.ThreadMeta.ID == "" || result.ThreadMeta.ID == threadID || result.Cwd == "" {
				s.forkUncertain = true
				s.State.Error = "分叉返回的信息不完整，请在历史中核验新会话。"
				return
			}
			// Register and persist the new native identity before fetching history.
			// A late result may add a tab, but never replace the user's selection.
			root.nextSession++
			branch := &session{id: root.nextSession, State: app.New(result.Cwd), recoveryBlocked: true}
			branch.State.ThreadID, branch.State.Draft = result.ThreadMeta.ID, draft
			branch.saved.Title = title + "（分叉）"
			branch.saved.AtEnd = true
			branch.State.Disconnected = s.State.Disconnected
			root.sessions = append(root.sessions, branch)
			if root.session == s {
				root.session = branch
			}
			root.persist()
			root.loadSessionHistory(branch, &result)
		})
	}()
}
