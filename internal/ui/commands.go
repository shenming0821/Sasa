package ui

import (
	"fmt"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
)

var terminalIcon = icon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 8h18M7 12h6M7 16h10"/>`)

type conversationRow struct {
	message   *app.Message
	commands  []*app.Message
	changes   []*app.Message
	processID string
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
	if i < len(rows) && rows[i].message != nil && rows[i].message.Role == "user" {
		return 24
	}
	if previous := rows[i-1].message; previous != nil && previous.Role == "user" {
		if previous.Timing != nil {
			return 12
		}
		return 24
	}
	return 12
}

// Project groups once for both virtual-list indices and saved reading anchors.
func (s *session) conversationRows() []conversationRow {
	if s.conversationUnchanged() {
		return s.projectedRows
	}
	var rows []conversationRow
	var changes []*app.Message
	turn := ""
	flush := func() {
		if len(changes) > 0 {
			rows = append(rows, conversationRow{message: changes[0], changes: changes})
			changes = nil
		}
	}
	for _, m := range s.visibleConversationMessages() {
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
		command := groupableTool(m)
		if command {
			flush()
		}
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
	question, result := "", -1
	for index := range rows {
		message := rows[index].message
		if message.Role == "user" {
			if result >= 0 {
				rows[result].processID = ""
			}
			question, result = "", -1
			if message.Timing != nil && !message.Timing.Finished.IsZero() {
				question = message.ID
			}
			continue
		}
		rows[index].processID = question
		if question != "" && message.Role == "assistant" {
			result = index
		}
	}
	if result >= 0 {
		rows[result].processID = ""
	}
	s.projectedRows = rows
	s.navigationEntries = conversationEntries(rows)
	s.projectedMessages = make([]*app.Message, len(rows))
	for index, row := range rows {
		s.projectedMessages[index] = row.message
	}
	return rows
}

func groupableTool(message *app.Message) bool {
	if message.Role != "tool" || message.Interaction != nil || message.Tool == nil {
		return false
	}
	switch message.Tool.Kind {
	case "commandExecution", "mcpToolCall", "dynamicToolCall", "webSearch":
		return true
	}
	return false
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
	return t.Status == "failed" || (t.ExitCode != nil && *t.ExitCode != 0) || toolResultFailed(t)
}

// continuing marks the trailing group of a turn that has not reached a final
// state yet, so the header reads as running even between individual commands.
func commandSummary(commands []*app.Message, continuing bool) (text string, failed, running bool) {
	declined, failures := 0, 0
	var active *app.Tool
	for _, m := range commands {
		if commandFailed(m.Tool) {
			failures++
		}
		if m.Tool.Status == "declined" {
			declined++
		}
		if m.Tool.Status == "inProgress" || m.Tool.AwaitingFinal {
			active = m.Tool
		}
	}
	failed = failures > 0
	if active != nil && active.AwaitingFinal {
		return "等待确认 " + toolCaption(active), failed, false
	}
	if active != nil || continuing {
		if failed {
			return fmt.Sprintf("正在执行命令 · %d失败", failures), true, true
		}
		return "正在执行命令", false, true
	}
	if failed {
		if failures == len(commands) {
			return "命令执行失败", true, false
		}
		return fmt.Sprintf("执行了命令 · %d失败", failures), true, false
	}
	if declined == len(commands) {
		return "命令已拒绝", false, false
	}
	return "执行了命令", false, false
}

// Process records share the same hierarchy, hover affordance and keyboard disclosure.
func (a *App) processDisclosure(c *native.Context, label, text string, open *bool, glyph *native.SVG, live bool, suffix, content func()) {
	theme := c.Theme()
	caption := func() native.Element {
		if live {
			return shimmerText(c, text, 13)
		}
		return native.Text(c, text).FontSize(13).TextColor(theme.TextMuted)
	}
	if content == nil {
		native.Row(c).Label(label).FillWidth().MinWidth(0).Height(30).Gap(8).AlignItems(native.Center).Children(func() {
			if glyph != nil {
				native.Icon(c, glyph).Size(16, 16).Shrink(0).TextColor(theme.TextMuted)
			}
			caption().SingleLine().MinWidth(0).Shrink(1)
			if suffix != nil {
				native.Spacer(c)
				suffix()
			}
		})
		return
	}
	native.Column(c).FillWidth().MinWidth(0).TextColor(theme.TextMuted).Children(func() {
		parts := native.CollapsibleBase(c, open)
		trigger := parts.Trigger.Label(label).FillWidth().MinWidth(0).Height(30).Gap(8).AlignItems(native.Center).TextColor(theme.TextMuted)
		hover := trigger.Hovered() || trigger.FocusVisible()
		trigger.Children(func() {
			if glyph != nil {
				native.Icon(c, glyph).Size(16, 16).Shrink(0).TextColor(theme.TextMuted)
			}
			caption().SingleLine().MinWidth(0).Shrink(1)
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
		target := float32(0)
		if *open {
			target = 1
		}
		if trigger.Clicked() || parts.Progress() != target {
			a.disclosureMoving = true
			a.processScrollHold = true
			a.list.FollowEnd = false
		}
		parts.Panel(func() {
			native.Column(c).FillWidth().MinWidth(0).TextColor(theme.TextMuted).Opacity(parts.Progress()).Padding(4, 0, 4, 24).Children(content)
		})
	})
}

func (a *App) commandDisclosure(c *native.Context, label, text string, open *bool, group, live bool, content func()) {
	var glyph *native.SVG
	if group {
		glyph = terminalIcon
	}
	a.processDisclosure(c, label, text, open, glyph, live, nil, content)
}

func (a *App) commandGroup(c *native.Context, commands []*app.Message, continuing bool) {
	text, _, running := commandSummary(commands, continuing)
	if commands[0].Tool.ParentToolID != "" {
		text = "子任务 · " + text
	}
	a.commandDisclosure(c, "命令组 "+commands[0].ID, text, a.commandGroupOpen(commands[0].ID), true, running, func() {
		for _, m := range commands {
			tool := m.Tool
			ctx := c.Key(m.ID)
			if tool.Kind != "commandExecution" {
				a.toolDisclosureWithIcon(ctx, tool, nil)
				continue
			}
			label := ""
			switch {
			case tool.AwaitingFinal:
				label = "等待确认 "
			case tool.Status == "inProgress":
				label = "正在执行 "
			case commandFailed(tool):
				label = "执行失败 "
			case tool.Status == "declined":
				label = "已拒绝 "
			}
			a.commandDisclosure(ctx, "命令详情 "+m.ID, label+toolOperationCaption(tool), &tool.Expanded, false, false, func() {
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
