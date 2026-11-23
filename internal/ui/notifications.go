package ui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/shenming0821/Sasa/internal/agent"
)

func (a *App) notifyCompletion(e agent.Event) {
	root := a.application()
	if !root.preferences.CompletionNotification || root.quitting || root.quitStopping {
		return
	}
	var p struct {
		Turn struct {
			Status string `json:"status"`
		} `json:"turn"`
	}
	if json.Unmarshal(e.Params, &p) != nil || p.Turn.Status != "completed" {
		return
	}
	focused := false
	if root.windowFocused != nil {
		focused = root.windowFocused()
	} else if root.win != nil {
		focused = root.win.IsFocused()
	}
	if focused && root.activeTab == sessionTabID(a.id) && !root.settingsOpen {
		return
	}
	sender := root.notificationSender
	if sender == nil {
		if root.win == nil {
			return
		}
		sender = func(options mygo.NotificationOptions) error {
			if !mygo.NotificationsSupported() {
				return fmt.Errorf("当前环境不支持系统通知")
			}
			return mygo.NewNotification(options).Show()
		}
	}
	options := mygo.NotificationOptions{ID: fmt.Sprintf("sasa-session-%d", a.id), Title: "任务已完成", Body: a.title(), Group: "sasa-tasks", Silent: !root.preferences.NotificationSound}
	go func() {
		err := sender(options)
		select {
		case <-root.shutdown:
			return
		default:
		}
		root.update(func() {
			defer root.redrawSettings()
			if err != nil {
				root.notifyError = "无法显示完成通知，请检查系统通知权限：" + err.Error()
			} else {
				root.notifyError = ""
			}
		})
	}()
}

func (a *App) installNotificationActions() {
	mygo.App.OnNotificationClick(func(id string) {
		sessionID, err := strconv.Atoi(strings.TrimPrefix(id, "sasa-session-"))
		if err != nil {
			return
		}
		a.win.Update(func() {
			for _, s := range a.sessions {
				if s.id == sessionID {
					a.settingsOpen = false
					a.selectSession(s)
					break
				}
			}
		})
		a.win.Show()
	})
}
