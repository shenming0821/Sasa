package ui

import (
	"fmt"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
)

var terminalIcon = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 8h18M7 12h6M7 16h10"/>`)

type conversationRow struct {
	message  *app.Message
	commands []*app.Message
	changes  []*app.Message
}

func (r conversationRow) processRecord() bool {
	if len(r.commands) > 0 {
		return true
	}
	if len(r.changes) > 0 || r.message == nil || r.message.Interaction != nil || r.message.Tool == nil {
		return false
	}
	kind := r.message.Tool.Kind
	return kind == "mcpToolCall" || kind == "dynamicToolCall" || kind == "webSearch"
}

func conversationSpacing(rows []conversationRow, i int) float32 {
	if i == 0 {
		return 0
	}
	if i < len(rows) && rows[i-1].processRecord() && rows[i].processRecord() {
		return 4
	}
	if rows[i-1].message != nil && rows[i-1].message.Role == "user" ||
		i < len(rows) && rows[i].message != nil && rows[i].message.Role == "user" {
		return 24
	}
	return 12
}

// Project groups once for both virtual-list indices and saved reading anchors.
func (s *session) conversationRows() []conversationRow {
	var rows []conversationRow
	var changes []*app.Message
	turn := ""
	flush := func() {
		if len(changes) > 0 {
			rows = append(rows, conversationRow{message: changes[0], changes: changes})
			changes = nil
		}
	}
	for _, m := range s.State.Messages {
		if m.Role == "user" || (m.TurnID != "" && turn != "" && m.TurnID != turn) {
			flush()
			turn = ""
		}
		if m.TurnID != "" {
			turn = m.TurnID
		}
		if m.Role == "assistant" && m.Text == "" {
			continue
		}
		if m.Role == "tool" && m.Interaction == nil && m.Tool != nil && m.Tool.Kind == "fileChange" && m.Tool.Status == "completed" && len(m.Tool.Changes) > 0 {
			changes = append(changes, m)
			continue
		}
		command := m.Role == "tool" && m.Interaction == nil && m.Tool != nil && m.Tool.Kind == "commandExecution"
		if command && len(rows) > 0 {
			last := &rows[len(rows)-1]
			if len(last.commands) > 0 && last.message.TurnID == m.TurnID && last.message.Tool.ParentToolID == m.Tool.ParentToolID {
				last.commands = append(last.commands, m)
				continue
			}
		}
		row := conversationRow{message: m}
		if command {
			row.commands = []*app.Message{m}
		}
		rows = append(rows, row)
	}
	flush()
	return rows
}

func (s *session) commandGroupOpen(id string) *bool {
	key := "command-group:" + id
	if s.commandGroups == nil {
		s.commandGroups = make(map[string]*bool)
	}
	if s.commandGroups[key] == nil {
		open := s.saved.Expanded[key]
		s.commandGroups[key] = &open
	}
	return s.commandGroups[key]
}

func commandFailed(t *app.Tool) bool {
	return t.Status == "failed" || (t.ExitCode != nil && *t.ExitCode != 0)
}

func commandSummary(commands []*app.Message) (string, bool) {
	failed := false
	declined := 0
	var active *app.Tool
	for _, m := range commands {
		failed = failed || commandFailed(m.Tool)
		if m.Tool.Status == "declined" {
			declined++
		}
		if m.Tool.Status == "inProgress" || m.Tool.AwaitingFinal {
			active = m.Tool
		}
	}
	if active != nil {
		if active.AwaitingFinal {
			return "等待确认 " + strings.Join(strings.Fields(active.Command), " "), failed
		}
		return "正在运行 " + strings.Join(strings.Fields(active.Command), " "), failed
	}
	if failed {
		return "命令运行失败", true
	}
	if declined == len(commands) {
		return "命令已拒绝", false
	}
	return "运行了命令", false
}

// Process records share the same hierarchy, hover affordance and keyboard disclosure.
func processDisclosure(c *native.Context, label, text string, open *bool, glyph *native.SVG, suffix, content func()) {
	theme := c.Theme()
	native.Column(c).FillWidth().MinWidth(0).TextColor(theme.TextMuted).Children(func() {
		parts := native.CollapsibleBase(c, open)
		trigger := parts.Trigger.Label(label).FillWidth().MinWidth(0).Height(30).Gap(8).AlignItems(native.Center).TextColor(theme.TextMuted)
		hover := trigger.Hovered() || trigger.FocusVisible()
		trigger.Children(func() {
			if glyph != nil {
				native.Icon(c, glyph).Size(16, 16).Shrink(0).TextColor(theme.TextMuted)
			}
			native.Text(c, text).FontSize(13).SingleLine().MinWidth(0).Shrink(1).TextColor(theme.TextMuted)
			opacity := float32(0)
			if hover {
				opacity = 1
			}
			native.Icon(c, chevron).Label(label+"箭头").Size(16, 16).Shrink(0).TextColor(theme.TextMuted).
				Rotate(-90 * (1 - parts.Progress())).Opacity(opacity)
			if suffix != nil {
				native.Spacer(c)
				suffix()
			}
		})
		parts.Panel(func() {
			native.Column(c).FillWidth().MinWidth(0).TextColor(theme.TextMuted).Opacity(parts.Progress()).Padding(4, 0, 4, 24).Children(content)
		})
	})
}

func commandDisclosure(c *native.Context, label, text string, open *bool, group bool, content func()) {
	var glyph *native.SVG
	if group {
		glyph = terminalIcon
	}
	processDisclosure(c, label, text, open, glyph, nil, content)
}

func (a *App) commandGroup(c *native.Context, commands []*app.Message) {
	text, _ := commandSummary(commands)
	if commands[0].Tool.ParentToolID != "" {
		text = "子任务 · " + text
	}
	commandDisclosure(c, "命令组 "+commands[0].ID, text, a.commandGroupOpen(commands[0].ID), true, func() {
		for _, m := range commands {
			tool := m.Tool
			ctx := c.Key(m.ID)
			label := "已运行 "
			switch {
			case tool.AwaitingFinal:
				label = "等待确认 "
			case tool.Status == "inProgress":
				label = "正在运行 "
			case commandFailed(tool):
				label = "运行失败 "
			case tool.Status == "declined":
				label = "已拒绝 "
			}
			commandDisclosure(ctx, "命令详情 "+m.ID, label+strings.Join(strings.Fields(tool.Command), " "), &tool.Expanded, false, func() {
				native.Scroll(ctx).Label("命令输出 " + m.ID).FillWidth().MaxHeight(180).Children(func() {
					native.Column(ctx).FillWidth().Gap(6).Padding(8).Background(ctx.Theme().Surface).Radius(8).Clip().Children(func() {
						detailText(ctx, "工作目录", tool.Cwd)
						detailText(ctx, "进程", tool.ProcessID)
						native.Text(ctx, "完整命令").FontSize(11).TextColor(ctx.Theme().TextMuted)
						native.Text(ctx, tool.Command).Label("完整命令内容").Selectable().Font(a.codeFontFamily()).FontSize(12).FillWidth()
						copy := quietButton(ctx, "复制命令").AlignSelf(native.Start)
						copy.Children(func() { native.Icon(ctx, copyIcon).Size(14, 14) })
						if copy.Clicked() {
							ctx.WriteClipboard(tool.Command)
						}
						if tool.Input != "" {
							detailText(ctx, "输入", tool.Input)
						}
						if tool.Output != "" {
							native.Text(ctx, tool.Output).Selectable().Font(a.codeFontFamily()).FontSize(12).FillWidth()
						}
						if tool.ExitCode != nil {
							native.Text(ctx, fmt.Sprintf("退出码 %d", *tool.ExitCode)).FontSize(11)
						}
						if tool.DurationMs != nil {
							native.Text(ctx, fmt.Sprintf("耗时 %.2f 秒", float64(*tool.DurationMs)/1000)).FontSize(11)
						}
						if tool.Truncated {
							native.Text(ctx, "输出显示已截断（上限 256 KiB），请核验完整运行记录。").FontSize(11)
						}
					})
				})
			})
		}
	})
}
