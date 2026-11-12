package ui

import (
	"slices"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
)

type taskErrorState struct {
	errorSession  *session
	errorMessage  string
	errorExpanded bool
}

var taskErrorIcon = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 7v6m0 4h.01"/>`)

func taskFailureSummary(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "terminal_reason=api_error"):
		return "模型服务返回错误，可以重试本次任务。"
	case strings.Contains(lower, "rate_limit"), strings.Contains(lower, "429"):
		return "请求过于频繁，请稍后重试。"
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "timed out"):
		return "请求超时，请稍后重试。"
	default:
		return "本次任务未能完成，可以查看详情后重试。"
	}
}

func (a *App) failedTaskMessage() *app.Message {
	for index := len(a.State.Messages) - 1; index >= 0; index-- {
		if a.State.Messages[index].Role == "user" {
			return a.State.Messages[index]
		}
	}
	return nil
}

func (a *App) canRetryTask() bool {
	return a.edit == nil && a.State.Phase == "执行失败" && !a.State.Incomplete && !a.State.Busy &&
		strings.TrimSpace(a.State.Draft) == "" && len(a.State.DraftImages) == 0 &&
		a.failedTaskMessage() != nil && a.canSubmitTask()
}

func (a *App) retryFailedTask() {
	if !a.canRetryTask() {
		return
	}
	message := a.failedTaskMessage()
	a.State.Draft = message.Text
	a.State.DraftImages = slices.Clone(message.Images)
	a.submit()
}

func (a *App) taskFailureCard(c *native.Context) {
	if a.errorSession != a.session || a.errorMessage != a.State.Error {
		a.errorSession, a.errorMessage, a.errorExpanded = a.session, a.State.Error, false
	}
	theme := c.Theme()
	native.Column(c.Key("task-failure")).FillWidth().MinWidth(0).Padding(0, 0, 10, 0).Children(func() {
		native.Column(c).FillWidth().MinWidth(0).Padding(12).Gap(8).Radius(12).
			Background(theme.Surface).Border(1, theme.Border.Alpha(.6)).Children(func() {
			native.Row(c).FillWidth().Gap(8).AlignItems(native.Center).Children(func() {
				native.Icon(c, taskErrorIcon).Size(16, 16).Shrink(0).TextColor(theme.Danger)
				native.Text(c, "任务未完成").FontSize(13).Bold().Grow(1)
				if actionButton(c, "重试本次任务").Disabled(!a.canRetryTask()).
					Tooltip("重新发送上次任务；输入框已有草稿时，请先发送或清空草稿").Clicked() {
					a.retryFailedTask()
				}
			})
			native.Text(c, taskFailureSummary(a.State.Error)).FontSize(12).TextColor(theme.TextMuted).FillWidth()
			parts := native.CollapsibleBase(c.Key(a.State.Error), &a.errorExpanded)
			trigger := parts.Trigger.Label("错误详情").AlignSelf(native.Start).Padding(4, 0).Gap(4).AlignItems(native.Center)
			trigger.Children(func() {
				native.Icon(c, chevron).Size(12, 12).Rotate(-90 * (1 - parts.Progress())).TextColor(theme.TextMuted)
				native.Text(c, "错误详情").FontSize(11).TextColor(theme.TextMuted)
			})
			parts.Panel(func() {
				native.Scroll(c).FillWidth().MaxHeight(100).Children(func() {
					native.Text(c, a.State.Error).Selectable().FontSize(12).FillWidth().TextColor(theme.TextMuted)
				})
			})
		})
	})
}
