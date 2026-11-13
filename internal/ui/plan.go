package ui

import (
	"encoding/json"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
)

// Tabler Icons bulb, using the composer's existing stroke and theme color.
var planBulbIcon = icon(`<path d="M3 12h1m8 -9v1m8 8h1m-15.4 -6.4l.7 .7m12.1 -.7l-.7 .7"/><path d="M9 16a5 5 0 1 1 6 0a3.5 3.5 0 0 0 -1 3a2 2 0 0 1 -4 0a3.5 3.5 0 0 0 -1 -3"/><path d="M9.7 17h4.6"/>`)
var planExitIcon = icon(`<path d="m6 6 12 12M18 6 6 18"/>`)

func (a *App) canChangePlanMode() bool {
	return !a.State.Busy && a.submitCancel == nil && len(a.pendingInputs) == 0 &&
		!a.recovering && !a.recoveryBlocked && !a.writerOccupied && !a.editPreparing && !a.editSending
}

func (a *App) setPlanMode(plan bool) bool {
	if !a.canChangePlanMode() {
		a.State.Error = "请在当前任务结束并处理待发送消息后切换计划模式"
		return false
	}
	mode := agent.ModeDefault
	if plan {
		mode = agent.ModePlan
	}
	previous := a.selectedMode
	a.selectedMode = mode
	if err := a.persist(); err != nil {
		a.selectedMode = previous
		a.State.Error = "计划模式未能保存：" + err.Error()
		return false
	}
	a.composerFocus = true
	return true
}

func (a *App) planModeBadge(c *native.Context) {
	if a.selectedMode != agent.ModePlan {
		return
	}
	t := c.Theme()
	native.Box(c).Width(1).Height(16).Background(t.Border.Alpha(.6))
	chip := native.Row(c.Key("plan-mode")).AlignItems(native.Center).Gap(4).Padding(3, 8).Radius(99)
	hovered := chip.Hovered()
	if hovered {
		chip.Background(t.SurfaceHover)
	}
	chip.Children(func() {
		b := native.ButtonBase(c.Key("exit-plan")).Label("退出计划模式").Tooltip("退出计划模式").
			Size(20, 20).Padding(0).Radius(99).AlignItems(native.Center).Justify(native.Center).
			Disabled(!a.canChangePlanMode()).TextColor(t.TextMuted)
		b.Children(func() {
			if hovered || b.FocusVisible() {
				native.Box(c).Size(16, 16).Radius(99).Background(t.TextMuted).AlignItems(native.Center).Justify(native.Center).Children(func() {
					native.Icon(c, planExitIcon).Size(10, 10).TextColor(t.Background)
				})
			} else {
				native.Icon(c, planBulbIcon).Size(18, 18)
			}
		})
		native.Text(c, "计划").FontSize(12).TextColor(t.TextMuted).SingleLine()
		if b.Clicked() {
			a.setPlanMode(false)
		}
	})
}

func (a *App) latestPlan() *app.Message {
	for i := len(a.State.Messages) - 1; i >= 0; i-- {
		m := a.State.Messages[i]
		if m.Role == "user" {
			return nil
		}
		if m.Plan && m.Done && strings.TrimSpace(m.Text) != "" {
			return m
		}
	}
	return nil
}

func (a *App) canExecutePlan(m *app.Message) bool {
	return a.selectedMode == agent.ModePlan && m == a.latestPlan() && m.ID != a.executedPlanID &&
		!a.State.Incomplete && !a.State.Disconnected && a.State.Phase == "本轮已完成" &&
		a.canSubmitTask() && len(a.pendingInputs) == 0 && a.edit == nil
}

func (a *App) planActions(c *native.Context, m *app.Message) {
	if !m.Plan || !a.canExecutePlan(m) {
		return
	}
	native.Row(c).Gap(8).Children(func() {
		if actionButton(c, "开始执行").Clicked() {
			a.executePlan(m)
		}
		if actionButton(c, "继续完善").Clicked() {
			a.composerFocus = true
		}
	})
}

func (a *App) executePlan(m *app.Message) {
	if !a.canExecutePlan(m) {
		return
	}
	if strings.TrimSpace(a.State.Draft) != "" || len(a.State.DraftImages) > 0 {
		a.State.Error = "输入框中有未发送内容，请先发送或清空，再开始执行"
		a.composerFocus = true
		return
	}
	previous := a.executedPlanID
	a.executedPlanID = m.ID
	if !a.setPlanMode(false) {
		a.executedPlanID = previous
		return
	}
	a.sendInput("请按照上面确认的计划开始实施。", nil, false)
}

func (a *App) planApprovalCard(c *native.Context, r *app.Interaction) {
	exit := r.Request.Kind == "ExitPlanMode"
	title, accept, decline := "进入计划模式", "进入计划", "继续执行"
	if exit {
		title, accept, decline = "计划", "开始执行", "继续完善"
	}
	native.Column(c).FillWidth().Gap(10).Padding(14).Radius(12).
		Border(1, c.Theme().Border.Alpha(.6)).Children(func() {
		native.Text(c, title).FontSize(12).TextColor(c.Theme().TextMuted)
		if r.Request.Plan != "" {
			a.messageMarkdown(c, &app.Message{
				ID: "plan-" + string(r.Request.ID), Role: "assistant", Text: r.Request.Plan, Done: true,
			})
		} else if exit {
			native.Text(c, "确认上方方案后开始执行，或继续完善。").FontSize(12).TextColor(c.Theme().TextMuted)
		}
		if r.Error != "" {
			native.Text(c, r.Error).FontSize(12).TextColor(c.Theme().Danger)
		}
		if !r.Active() {
			native.Text(c, r.Status).FontSize(11).TextColor(c.Theme().TextMuted)
			return
		}
		disabled := r.Status != "待处理" || a.State.Disconnected || a.State.Stopping()
		native.Row(c).Gap(8).Children(func() {
			if actionButton(c, accept).Disabled(disabled).Clicked() {
				a.answer(r, map[string]any{"decision": json.RawMessage(`"accept"`)})
			}
			if actionButton(c, decline).Disabled(disabled).Clicked() {
				feedback := "请继续执行，不进入计划模式。"
				if exit {
					feedback = "请继续完善计划，暂不实施；等待我补充意见。"
					a.composerFocus = true
				}
				a.answer(r, map[string]any{"decision": json.RawMessage(`"decline"`), "feedback": feedback})
			}
		})
	})
}
