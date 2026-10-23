package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/codex"
)

type inputCompletion struct {
	key, dismissed  string
	selected        int
	skills          []codex.Skill
	cwd             string
	loading, loaded bool
	error           string
	chosen          map[string]codex.Skill
	focus           bool
	caret           int
	reveal          bool
}

type completionQuery struct {
	kind       byte
	query      string
	start, end int
}
type completionItem struct {
	name, description string
	skill             *codex.Skill
}

var slashCommands = []completionItem{
	{name: "/new", description: "在当前目录开始新对话"},
	{name: "/resume", description: "打开历史会话"},
	{name: "/status", description: "查看当前模型、权限与运行状态"},
	{name: "/skills", description: "选择当前目录可用的技能"},
}

func queryCompletion(text string, anchor, caret int) (completionQuery, bool) {
	r := []rune(text)
	if anchor != caret || caret < 0 || caret > len(r) {
		return completionQuery{}, false
	}
	start := caret
	for start > 0 && !unicode.IsSpace(r[start-1]) {
		start--
	}
	if start == caret {
		return completionQuery{}, false
	}
	token := r[start:caret]
	if token[0] == '/' && start == 0 && caret == len(r) && !strings.ContainsAny(string(token[1:]), "/$") {
		return completionQuery{'/', string(token[1:]), start, caret}, true
	}
	if token[0] == '$' && !strings.ContainsAny(string(token[1:]), "/$") {
		return completionQuery{'$', string(token[1:]), start, caret}, true
	}
	return completionQuery{}, false
}

func completionKey(q completionQuery) string {
	return fmt.Sprintf("%c:%d:%d:%s", q.kind, q.start, q.end, q.query)
}

func (a *App) completionItems(q completionQuery) []completionItem {
	var items []completionItem
	query := strings.ToLower(q.query)
	if q.kind == '/' {
		for _, item := range slashCommands {
			if strings.Contains(item.name, query) {
				items = append(items, item)
			}
		}
	} else {
		for i := range a.completion.skills {
			skill := &a.completion.skills[i]
			if !skill.Enabled || skill.Name == "" || skill.Path == "" {
				continue
			}
			name, description := skill.Name, skill.ShortDescription
			if description == "" {
				description = skill.Description
			}
			if skill.Interface != nil {
				if skill.Interface.DisplayName != "" {
					name = skill.Interface.DisplayName
				}
				if skill.Interface.ShortDescription != "" {
					description = skill.Interface.ShortDescription
				}
			}
			if strings.Contains(strings.ToLower(name+" "+skill.Name+" "+description), query) {
				items = append(items, completionItem{name, description, skill})
			}
		}
		sort.SliceStable(items, func(i, j int) bool { return strings.ToLower(items[i].name) < strings.ToLower(items[j].name) })
	}
	return items
}

func (a *App) loadCompletionSkills() {
	s := a.session
	if s.completion.cwd == s.State.Cwd && (s.completion.loaded || s.completion.loading) {
		return
	}
	backend, ok := a.backend.(interface {
		ListSkills(context.Context, string) ([]codex.SkillsEntry, error)
	})
	s.completion.cwd = s.State.Cwd
	s.completion.skills, s.completion.error = nil, ""
	s.completion.loaded = false
	if !ok {
		s.completion.loaded = true
		s.completion.error = "当前连接不支持读取技能"
		return
	}
	s.completion.loading = true
	cwd := s.State.Cwd
	bound := a.forSession(s)
	go func() {
		ctx, cancel := codex.RequestContext()
		defer cancel()
		entries, err := backend.ListSkills(ctx, cwd)
		bound.update(func() {
			if s.State.Cwd != cwd || s.completion.cwd != cwd {
				return
			}
			s.completion.loading = false
			s.completion.loaded = true
			if err != nil {
				s.completion.error = "读取技能失败：" + err.Error()
				return
			}
			for _, entry := range entries {
				if entry.Cwd != cwd {
					continue
				}
				s.completion.skills = append(s.completion.skills, entry.Skills...)
				if len(entry.Errors) > 0 {
					s.completion.error = "技能加载异常：" + entry.Errors[0].Message
				}
			}
		})
	}()
}

func (a *App) runSlashCommand(text string) bool {
	switch text {
	case "/new":
		cwd := a.State.Cwd
		a.State.Draft = ""
		a.application().newSession(cwd)
	case "/resume":
		a.State.Draft = ""
		root := a.application()
		root.historyOpen = true
		root.loadThreadList()
	case "/status":
		a.State.Draft = ""
		a.detailsOpen = true
	case "/skills":
		a.State.Draft = "$"
		a.completion.dismissed = ""
		a.completion.focus = true
		a.completion.caret = 1
	default:
		return false
	}
	return true
}

func (a *App) acceptCompletion(q completionQuery, item completionItem) {
	a.completion.dismissed = ""
	if q.kind == '/' {
		a.runSlashCommand(item.name)
	} else {
		r := []rune(a.State.Draft)
		token := "$" + item.skill.Name + " "
		a.State.Draft = string(r[:q.start]) + token + string(r[q.end:])
		if a.completion.chosen == nil {
			a.completion.chosen = make(map[string]codex.Skill)
		}
		a.completion.chosen[item.skill.Name] = *item.skill
		a.completion.caret = q.start + len([]rune(token))
		a.completion.focus = true
	}
}

func (a *session) draftSkills(text string) []codex.Skill {
	if a.completion.cwd != a.State.Cwd {
		return nil
	}
	var skills []codex.Skill
	seen := make(map[string]bool)
	r := []rune(text)
	for end := 1; end <= len(r); end++ {
		if end < len(r) && !unicode.IsSpace(r[end]) {
			continue
		}
		q, ok := queryCompletion(text, end, end)
		if !ok || q.kind != '$' || seen[q.query] {
			continue
		}
		skill, ok := a.completion.chosen[q.query]
		if ok {
			skills = append(skills, skill)
			seen[q.query] = true
		}
	}
	return skills
}

func (a *App) completionPanel(c *native.Context, editor native.Element, q completionQuery) {
	items := a.completionItems(q)
	t := c.Theme()
	panel := native.Column(c).Label("输入候选").AttachTo(editor, native.AnchorTopStart, native.AnchorBottomStart).
		Width(editor.Bounds().W).MinWidth(200).Gap(4).Padding(8).Margin(8, 0).Radius(12).
		Background(t.Background).Border(1, t.Border.Alpha(.7)).Shadow(0, 2, 10, 0, native.RGBA(0, 0, 0, .08))
	if panel.PressedOutside() {
		a.completion.dismissed = completionKey(q)
		return
	}
	panel.Children(func() {
		title := "命令"
		if q.kind == '$' {
			title = "技能"
		}
		native.Row(c).FillWidth().Children(func() {
			native.Text(c, title).FontSize(11).TextColor(t.TextMuted)
			native.Spacer(c)
			native.Text(c, "↑↓ 选择 · Enter / Tab 确认 · Esc 关闭").FontSize(10).TextColor(t.TextMuted)
		})
		if q.kind == '$' && a.completion.loading {
			native.Text(c, "正在读取技能…").FontSize(12)
		}
		if q.kind == '$' && a.completion.error != "" {
			native.Row(c).FillWidth().Gap(8).Children(func() {
				native.Text(c, a.completion.error).FontSize(12).Grow(1).TextColor(t.TextMuted)
				if quietButton(c, "重新读取技能").Children(func() { native.Text(c, "重试").FontSize(12) }).Clicked() {
					a.completion.loaded = false
					a.loadCompletionSkills()
				}
			})
		}
		if len(items) == 0 && !a.completion.loading {
			native.Text(c, "没有匹配项").FontSize(12).TextColor(t.TextMuted)
		}
		native.Scroll(c).FillWidth().MaxHeight(156).Children(func() {
			native.Column(c).FillWidth().Gap(2).Children(func() {
				for i, item := range items {
					row := native.ButtonBase(c.Key(item.name+fmt.Sprint(i))).Label("候选 "+item.name).FillWidth().Padding(7, 8).Radius(6).KeepFocus()
					if i == a.completion.selected || row.Hovered() {
						row.Background(t.SurfaceHover)
					}
					row.Children(func() {
						native.Column(c).FillWidth().Gap(3).Children(func() {
							native.Text(c, item.name).FontSize(13).SingleLine()
							native.Text(c, strings.Join(strings.Fields(item.description), " ")).FontSize(11).TextColor(t.TextMuted).SingleLine()
						})
					})
					if a.completion.reveal && i == a.completion.selected {
						row.ScrollIntoView()
					}
					if row.Clicked() {
						a.acceptCompletion(q, item)
						editor.Focus()
					}
				}
			})
		})
	})
	a.completion.reveal = false
}
