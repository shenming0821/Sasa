package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
)

func actionButton(c *native.Context, label string) native.Element {
	b := outlinedButton(c, label).AlignSelf(native.Start).Padding(7, 10)
	b.Children(func() { native.Text(c, label).FontSize(12) })
	return b
}

func detailText(c *native.Context, label, value string) {
	if value == "" || value == "null" {
		return
	}
	t := c.Theme()
	native.Text(c, label).FontSize(11).TextColor(t.TextMuted)
	native.Text(c, value).Selectable().FontSize(12).FillWidth()
}

// statusPill renders a tool's status as a tinted pill: green on success, red on
// failure, neutral while running or otherwise.
func statusPill(c *native.Context, t *app.Tool) {
	theme := c.Theme()
	fg, bg := theme.TextMuted, theme.SurfacePressed
	switch {
	case t.Status == "failed" || t.ExitCode != nil && *t.ExitCode != 0 || t.Status == "declined":
		fg = theme.Danger
		bg = theme.Danger.Alpha(.10)
	case t.Status == "completed":
		fg = theme.Success
		bg = theme.Success.Alpha(.10)
	case t.Status == "inProgress":
		fg = theme.Accent
		bg = theme.Accent.Alpha(.10)
	}
	native.Box(c).Label("工具状态 "+t.StatusLabel()).Radius(99).Padding(1, 8).Background(bg).Children(func() {
		native.Text(c, t.StatusLabel()).FontSize(10.5).TextColor(fg).SingleLine()
	})
}

func pretty(raw json.RawMessage) string {
	var out bytes.Buffer
	if json.Indent(&out, raw, "", "  ") == nil {
		return out.String()
	}
	return string(raw)
}

func (a *App) toolCard(c *native.Context, t *app.Tool) {
	if t.Kind == "mcpToolCall" || t.Kind == "dynamicToolCall" || t.Kind == "webSearch" {
		a.toolDisclosure(c, t)
		return
	}
	theme := c.Theme()
	native.Column(c).FillWidth().Gap(8).Padding(12, 14).Radius(12).
		Border(1, theme.Border.Alpha(.6)).Background(theme.Surface).Clip().Children(func() {
		native.Row(c).FillWidth().Gap(8).Children(func() {
			native.Text(c, t.Title()).FontSize(12).Grow(1)
			statusPill(c, t)
		})
		if t.Command != "" {
			native.Text(c, t.Command).Label("命令预览").Selectable().Font(a.codeFontFamily()).FontSize(12).FillWidth().MaxLines(4)
		}
		if t.Cwd != "" {
			detailText(c, "工作目录", t.Cwd)
		}
		for _, change := range t.Changes {
			native.Text(c, change.Summary()).Selectable().FontSize(12)
		}
		var metrics []string
		if t.ExitCode != nil {
			metrics = append(metrics, fmt.Sprintf("退出码 %d", *t.ExitCode))
		}
		if t.DurationMs != nil {
			metrics = append(metrics, fmt.Sprintf("耗时 %.2f 秒", float64(*t.DurationMs)/1000))
		}
		if t.ProcessID != "" {
			metrics = append(metrics, "进程 "+t.ProcessID)
		}
		if len(metrics) > 0 {
			native.Text(c, strings.Join(metrics, " · ")).FontSize(11).TextColor(theme.TextMuted)
		}
		hasDetails := t.Command != "" || t.Output != "" || t.Input != ""
		for _, change := range t.Changes {
			hasDetails = hasDetails || change.Diff != ""
		}
		if hasDetails {
			noun := "输出"
			if t.Command != "" {
				noun = "详情"
			}
			label := "展开" + noun
			if t.Expanded {
				label = "收起" + noun
			}
			if actionButton(c, label).Clicked() {
				t.Expanded = !t.Expanded
			}
			collapsibleSection(c, "tool-card-details", t.Expanded, func() {
				native.Scroll(c).Label("工具详情").FillWidth().MaxHeight(180).Children(func() {
					native.Column(c).FillWidth().Gap(6).Children(func() {
						if t.Command != "" {
							native.Text(c, "完整命令").FontSize(11).TextColor(theme.TextMuted)
							native.Text(c, t.Command).Label("完整命令内容").Selectable().Font(a.codeFontFamily()).FontSize(12).FillWidth()
						}
						if t.Input != "" {
							detailText(c, "输入", t.Input)
						}
						if t.Output != "" {
							native.Text(c, t.Output).Selectable().Font(a.codeFontFamily()).FontSize(12).FillWidth()
						}
						for _, change := range t.Changes {
							if change.Diff != "" {
								detailText(c, change.Path, change.Diff)
							}
						}
					})
				})
			})
		}
		if t.Truncated {
			native.Text(c, "输出显示已截断（上限 256 KiB），请核验完整运行记录。").FontSize(11).TextColor(theme.TextMuted)
		}
	})
}

func (a *App) interactionCard(c *native.Context, r *app.Interaction) {
	t := c.Theme()
	request := r.Request
	title := "需要批准命令"
	switch request.Method {
	case "item/fileChange/requestApproval":
		title = "需要批准文件修改"
	case "item/permissions/requestApproval":
		title = "需要额外权限"
	case "item/tool/requestUserInput":
		title = a.kind.String() + " 向你提问"
	}
	native.Column(c).FillWidth().Gap(10).Padding(14).Radius(12).
		Border(1, t.Border.Alpha(.6)).Background(t.Surface).Clip().DrawOver(func(painter *native.Painter, bounds native.Rect) {
			bar := native.Color{}
			switch {
			case r.Active():
				bar = t.Warning
			case request.Method == "item/tool/requestUserInput":
				bar = t.Accent
			}
			if bar != (native.Color{}) {
				painter.Fill(native.Rect{X: bounds.X, Y: bounds.Y, W: 3, H: bounds.H}, bar, 0)
			}
		}).Children(func() {
		native.Row(c).FillWidth().Gap(8).Children(func() {
			native.Text(c, title).FontSize(14).Grow(1)
			native.Text(c, r.Status).FontSize(11).TextColor(t.TextMuted)
		})
		if !r.Active() && r.Status != "发送待核验" {
			// Completed forms never retain secret values or editable fields.
			detailText(c, "操作", request.Command)
			detailText(c, "处理结果", r.Outcome)
			if r.Outcome == "" {
				native.Text(c, "此请求已结束。").FontSize(12).TextColor(t.TextMuted)
			}
			return
		}
		disabled := r.Status != "待处理"
		detailText(c, "原因", request.Reason)
		detailText(c, "命令", request.Command)
		detailText(c, "工作目录", request.Cwd)
		if request.Kind == "writeStdin" {
			detailText(c, "操作", "向现有终端写入输入")
		}
		detailText(c, "请求写入范围", request.GrantRoot)
		if request.Method == "item/fileChange/requestApproval" {
			for _, m := range a.State.Messages {
				if m.ID == request.ItemID && m.Tool != nil {
					for _, change := range m.Tool.Changes {
						native.Text(c, change.Summary()).FontSize(12).Selectable()
					}
				}
			}
		}
		detailText(c, "请求权限", pretty(request.Permissions))
		detailText(c, "额外权限", pretty(request.AdditionalPermissions))
		detailText(c, "网络目标", pretty(request.NetworkApprovalContext))
		if request.Method == "item/tool/requestUserInput" {
			label := "需要回答后继续"
			if !request.IsBlocking {
				label = "可稍后回答，任务可以继续"
			}
			native.Text(c, label).FontSize(11).TextColor(t.TextMuted)
			for i, q := range request.Questions {
				native.Column(c.Key(q.ID)).FillWidth().Gap(6).Children(func() {
					native.Text(c, q.Header).FontSize(12).TextColor(t.TextMuted)
					native.Text(c, q.Question).FontSize(14)
					for index, option := range q.Options {
						button := selectableActionButton(c, option.Label, r.AnswerSelected(q, option.Label)).
							AlignSelf(native.Start).Padding(7, 10).FillWidth().Disabled(disabled)
						button.Children(func() { native.Text(c, option.Label).FontSize(12) })
						if r.FocusInvalid && r.InvalidQuestion == q.ID && !q.IsOther && index == 0 {
							button.Focus()
							r.FocusInvalid = false
						}
						if button.Clicked() {
							r.ToggleAnswer(i, option.Label)
						}
						native.Text(c, option.Description).FontSize(11).TextColor(t.TextMuted)
					}
					if len(q.Options) == 0 || q.IsOther {
						input := native.TextInputBase(c, &r.Answers[i]).Label(q.Header+"的回答").
							Placeholder("输入回答…").FillWidth().FontSize(14).Padding(8).Radius(6).
							Border(1, t.Border).Background(t.Background).Disabled(disabled)
						if q.IsSecret {
							input.Password()
						}
						if r.FocusInvalid && r.InvalidQuestion == q.ID {
							input.Focus()
							r.FocusInvalid = false
						}
						if input.Changed() && r.InvalidQuestion == q.ID {
							r.Error, r.InvalidQuestion = "", ""
						}
					}
					if r.InvalidQuestion == q.ID && r.Error != "" {
						native.Text(c, r.Error).FontSize(12).TextColor(t.Danger)
					}
				})
			}
			if actionButton(c, "提交回答").Disabled(disabled).Clicked() {
				a.respond(r, nil)
			}
			if actionButton(c, "取消回答").Disabled(disabled).Clicked() {
				a.respond(r, map[string]any{"answers": map[string]any{}})
			}
		} else if request.Method == "item/permissions/requestApproval" {
			for _, scope := range []struct{ label, value string }{
				{"允许本轮请求的权限", "turn"}, {"在本会话允许请求的权限", "session"},
			} {
				if actionButton(c, scope.label).Disabled(disabled).Clicked() {
					a.respond(r, map[string]any{"permissions": request.Permissions, "scope": scope.value})
				}
			}
			if actionButton(c, "拒绝额外权限").Disabled(disabled).Clicked() {
				a.respond(r, map[string]any{"permissions": map[string]any{}, "scope": "turn"})
			}
		} else {
			if a.kind.Normalized() == agent.Claude {
				native.TextInputBase(c, &r.Feedback).Label("拒绝反馈").Placeholder("拒绝原因（可选）").FillWidth().FontSize(12).Padding(8).Radius(6).Border(1, t.Border).Disabled(disabled)
			}
			for _, decision := range request.Decisions() {
				native.Column(c).FillWidth().Gap(2).Children(func() {
					if actionButton(c, decision.Label).Disabled(disabled).Clicked() {
						response := map[string]any{"decision": decision.Value}
						if a.kind.Normalized() == agent.Claude && r.Feedback != "" {
							response["feedback"] = r.Feedback
						}
						a.respond(r, response)
					}
					native.Text(c, decision.Detail).FontSize(11).TextColor(t.TextMuted)
				})
			}
		}
		if r.Error != "" && r.InvalidQuestion == "" {
			native.Text(c, r.Error).FontSize(12).TextColor(t.Danger)
		}
	})
}
