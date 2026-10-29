package ui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
)

type sessionRenameState struct {
	sessionRenameOpen   bool
	sessionRenameTarget *session
	sessionRenameDraft  string
	sessionRenameError  string
}

func (a *App) beginSessionRename(target *session) {
	if target == nil || a.quitting || a.quitDialog {
		return
	}
	a.sessionRenameTarget = target
	a.sessionRenameDraft = target.title()
	a.sessionRenameError = ""
	a.sessionRenameOpen = true
}

func (a *App) saveSessionName(target *session, name string) error {
	a = a.application()
	if target == nil || a.quitting || a.quitDialog {
		return fmt.Errorf("会话暂时无法重命名，请稍后重试")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("请输入会话标题")
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("标题请使用单行文字")
	}
	if target.id == 0 {
		for _, existing := range a.sessions {
			if existing.kind.Normalized() == target.kind.Normalized() && existing.runtimeKey == target.runtimeKey && existing.State.ThreadID == target.State.ThreadID {
				target = existing
				break
			}
		}
	}
	if (target.id != 0 && !slices.Contains(a.sessions, target)) ||
		(target.id == 0 && (target.State.ThreadID == "" || a.threadDeleted(target.kind, target.runtimeKey, target.State.ThreadID))) {
		return fmt.Errorf("会话已移除，请刷新后重试")
	}
	previous := target.saved.CustomTitle
	imported := target.id == 0
	if imported {
		a.nextSession++
		target.id = a.nextSession
		target.historyCacheKey = agent.CacheKey{Agent: target.kind.Normalized(), Runtime: target.runtimeKey, Cwd: target.State.Cwd}
		target.recoveryBlocked = true
		target.saved.Active = target.State.Busy
		a.sessions = append(a.sessions, target)
	}
	target.saved.CustomTitle = name
	if err := a.persist(); err != nil {
		target.saved.CustomTitle = previous
		if imported {
			a.sessions = slices.DeleteFunc(a.sessions, func(item *session) bool { return item == target })
			target.id = 0
			a.nextSession--
		}
		return fmt.Errorf("保存标题失败：%w", err)
	}
	return nil
}

func (a *App) sessionRenameView(c *native.Context) {
	native.Modal(c.Key("session-rename"), &a.sessionRenameOpen, func() {
		if c.Shortcut(native.Cmd, native.KeyW) {
			a.sessionRenameOpen = false
		}
		width, _ := c.Size()
		native.Column(c).Width(min(width-64, 400)).Gap(12).Children(func() {
			native.Text(c, "重命名会话").FontSize(16).Bold()
			input := native.TextInput(c, &a.sessionRenameDraft).Label("会话标题").FillWidth().AutoFocus()
			if a.sessionRenameError != "" {
				native.Text(c, a.sessionRenameError).FontSize(11).TextColor(c.Theme().Danger).FillWidth()
			}
			native.Row(c).FillWidth().Gap(8).Justify(native.End).Children(func() {
				if actionButton(c, "取消重命名").Clicked() {
					a.sessionRenameOpen = false
				}
				if actionButton(c, "保存标题").Clicked() || input.Submitted() {
					if err := a.saveSessionName(a.sessionRenameTarget, a.sessionRenameDraft); err != nil {
						a.sessionRenameError = err.Error()
					} else {
						a.sessionRenameOpen = false
					}
				}
			})
		})
	})
}
