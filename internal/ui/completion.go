package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
)

type inputCompletion struct {
	commands        []agent.Command
	key, dismissed  string
	selected        int
	skills          []agent.Skill
	cwd             string
	checked         time.Time
	cacheKey        agent.CacheKey
	loading, loaded bool
	error           string
	chosen          map[string]agent.Skill
	files           []completionFile
	filesCwd        string
	filesChecked    time.Time
	filesLoading    bool
	filesError      string
	filesTruncated  bool
	filesVersion    uint64
	fileFiltered    []completionItem
	fileFilterKey   string
	fileScroll      native.ScrollState
	fileShown       int
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
	skill             *agent.Skill
	path              string
	dir               bool
}

var slashCommands = []completionItem{
	{name: "/compact", description: "压缩上下文"},
	{name: "/new", description: "在当前目录开始新对话"},
	{name: "/resume", description: "打开历史会话"},
	{name: "/status", description: "查看当前模型、权限与运行状态"},
	{name: "/skills", description: "选择当前目录可用的技能"},
	{name: "/plan", description: "切换计划模式，先讨论方案再执行"},
}

func queryCompletion(text string, anchor, caret int, kind agent.AgentKind) (completionQuery, bool) {
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
	leading := start == 0 && caret == len(r)
	if token[0] == '/' && leading && !strings.ContainsAny(string(token[1:]), "/$") {
		return completionQuery{'/', string(token[1:]), start, caret}, true
	}
	// Codex $ references attach to any token; other runtimes only invoke
	// skills as a leading /command, so their $ trigger shares that shape.
	if token[0] == '$' && (kind.Normalized() == agent.Codex || leading) && !strings.ContainsAny(string(token[1:]), "/$") {
		return completionQuery{'$', string(token[1:]), start, caret}, true
	}
	// A path picked with the caret inside one replaces all of it, so the
	// query's end reaches the token's end; the query filters by the prefix.
	end := caret
	for end < len(r) && !unicode.IsSpace(r[end]) {
		end++
	}
	// @/absolute paths come from file drops; candidates are workspace-relative.
	if token[0] == '@' && !strings.HasPrefix(string(r[start+1:end]), "/") && !strings.ContainsAny(string(token[1:]), "@$") {
		return completionQuery{'@', string(token[1:]), start, end}, true
	}
	return completionQuery{}, false
}

func completionKey(q completionQuery) string {
	return fmt.Sprintf("%c:%d:%d:%s", q.kind, q.start, q.end, q.query)
}

func (a *App) completionItems(q completionQuery) []completionItem {
	if q.kind == '@' {
		return a.completionFileItems(q)
	}
	var items []completionItem
	query := strings.ToLower(q.query)
	if q.kind == '/' {
		if a.kind.Normalized() != agent.Codex {
			for _, command := range a.completion.commands {
				if command.Name == "plan" || command.Name == "compact" {
					continue
				}
				if a.kind.Normalized() == agent.Claude && !command.IsSkill {
					continue
				}
				name := "/" + command.Name
				if strings.Contains(strings.ToLower(name), query) {
					items = append(items, completionItem{name: name, description: command.Description})
				}
			}
			if strings.Contains("/clear", query) {
				items = append(items, completionItem{name: "/clear", description: "清空上下文，开始新对话；保留旧历史"})
			}
			if strings.Contains("/plan", query) {
				items = append(items, completionItem{name: "/plan", description: "切换计划模式，先讨论方案再执行"})
			}
			if strings.Contains("/compact", query) {
				items = append(items, completionItem{name: "/compact", description: "压缩上下文，可附压缩指引"})
			}
			return items
		}
		for _, item := range slashCommands {
			if strings.Contains(item.name, query) {
				items = append(items, item)
			}
		}
	} else if a.kind.Normalized() != agent.Codex {
		for _, command := range a.completion.commands {
			if !command.IsSkill {
				continue
			}
			name := "/" + command.Name
			if strings.Contains(strings.ToLower(name), query) {
				items = append(items, completionItem{name: name, description: command.Description})
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
				items = append(items, completionItem{name: name, description: description, skill: skill})
			}
		}
		sort.SliceStable(items, func(i, j int) bool { return strings.ToLower(items[i].name) < strings.ToLower(items[j].name) })
	}
	return items
}

func (a *App) loadCompletionSkills() {
	a.loadCompletionCatalog(false)
}

func (a *App) isClearCommand(text string) bool {
	parts := strings.Fields(text)
	return a.kind.Normalized() != agent.Codex && len(parts) > 0 && parts[0] == "/clear"
}

func (a *App) runSlashCommand(text string) bool {
	if strings.TrimSpace(text) == "/plan" {
		if a.setPlanMode(a.selectedMode != agent.ModePlan) {
			a.State.Draft = ""
			a.completion.focus, a.completion.caret = true, 0
		}
		return true
	}
	if a.isClearCommand(text) {
		if strings.TrimSpace(text) != "/clear" {
			a.State.Error = "/clear 不接受参数，请单独发送该命令"
			return true
		}
		a.clearConversation()
		return true
	}
	if a.kind.Normalized() != agent.Codex {
		return false
	}
	switch text {
	case "/new":
		a.newConversation()
	case "/resume":
		a.State.Draft = ""
		root := a.application()
		root.historyOpen = true
		root.openThreadList()
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
	if q.kind == '/' && item.name == "/compact" {
		a.State.Draft = item.name + " "
		a.completion.caret = len([]rune(a.State.Draft))
		a.completion.focus = true
	} else if q.kind == '/' && item.name == "/plan" {
		a.runSlashCommand(item.name)
	} else if a.kind.Normalized() != agent.Codex && q.kind != '@' {
		a.State.Draft = item.name + " "
		a.completion.caret = len([]rune(a.State.Draft))
		a.completion.focus = true
	} else if q.kind == '/' {
		a.runSlashCommand(item.name)
	} else if q.kind == '@' {
		r := []rune(a.State.Draft)
		// Directories keep the token open so typing continues into their children.
		token, caret := "@"+item.path+" ", 0
		if item.dir {
			token = "@" + item.path + "/"
		} else if q.end < len(r) && unicode.IsSpace(r[q.end]) {
			token, caret = "@"+item.path, 1
		}
		a.State.Draft = string(r[:q.start]) + token + string(r[q.end:])
		a.completion.caret = q.start + len([]rune(token)) + caret
		a.completion.focus = true
	} else {
		r := []rune(a.State.Draft)
		token := "$" + item.skill.Name + " "
		a.State.Draft = string(r[:q.start]) + token + string(r[q.end:])
		if a.completion.chosen == nil {
			a.completion.chosen = make(map[string]agent.Skill)
		}
		a.completion.chosen[item.skill.Name] = *item.skill
		a.completion.caret = q.start + len([]rune(token))
		a.completion.focus = true
	}
}

func (a *session) draftSkills(text string) []agent.Skill {
	if a.completion.cwd != a.State.Cwd {
		return nil
	}
	return referencedSkills(text, a.kind, a.completion.chosen)
}

func referencedSkills(text string, kind agent.AgentKind, chosen map[string]agent.Skill) []agent.Skill {
	var skills []agent.Skill
	seen := make(map[string]bool)
	r := []rune(text)
	for end := 1; end <= len(r); end++ {
		if end < len(r) && !unicode.IsSpace(r[end]) {
			continue
		}
		q, ok := queryCompletion(text, end, end, kind)
		if !ok || q.kind != '$' || seen[q.query] {
			continue
		}
		skill, ok := chosen[q.query]
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
		runtimeSkills := q.kind == '/' && a.kind.Normalized() != agent.Codex
		title := "命令"
		switch {
		case q.kind == '@':
			title = "路径"
		case runtimeSkills:
			title = "技能与命令"
		case q.kind == '$':
			title = "技能"
		}
		native.Row(c).FillWidth().Children(func() {
			native.Text(c, title).FontSize(11).TextColor(t.TextMuted)
			native.Spacer(c)
			native.Text(c, "↑↓ 选择 · Enter / Tab 确认 · Esc 关闭").FontSize(10).TextColor(t.TextMuted)
			if q.kind == '$' || runtimeSkills {
				if quietButton(c, "刷新技能").KeepFocus().Disabled(a.completion.loading).Children(func() { native.Text(c, "刷新").FontSize(11) }).Clicked() {
					a.loadCompletionCatalog(true)
				}
			}
			if q.kind == '@' {
				if quietButton(c, "刷新文件候选").KeepFocus().Disabled(a.completion.filesLoading).Children(func() { native.Text(c, "刷新").FontSize(11) }).Clicked() {
					a.loadCompletionFiles(true)
				}
			}
		})
		if (q.kind == '$' || runtimeSkills) && a.completion.error != "" {
			native.Row(c).FillWidth().Gap(8).Children(func() {
				native.Text(c, a.completion.error).FontSize(12).Grow(1).TextColor(t.TextMuted)
				if quietButton(c, "重新读取").KeepFocus().Children(func() { native.Text(c, "重试").FontSize(12) }).Clicked() {
					a.loadCompletionCatalog(true)
				}
			})
		}
		if q.kind == '@' && a.completion.filesError != "" {
			native.Row(c).FillWidth().Gap(8).Children(func() {
				native.Text(c, a.completion.filesError).FontSize(12).Grow(1).TextColor(t.TextMuted)
				if quietButton(c, "重新读取文件").KeepFocus().Children(func() { native.Text(c, "重试").FontSize(12) }).Clicked() {
					a.loadCompletionFiles(true)
				}
			})
		}
		if len(items) == 0 && !(a.completion.loading || q.kind == '@' && a.completion.filesLoading) {
			native.Text(c, "没有匹配项").FontSize(12).TextColor(t.TextMuted)
		}
		if q.kind == '@' && a.completion.filesLoading && len(items) == 0 {
			native.Text(c, "正在读取工作区文件…").FontSize(12).TextColor(t.TextMuted)
		}
		// File candidates page in as the user scrolls; keyboard selection past
		// the rendered window grows it just enough to stay visible.
		shown := items
		if q.kind == '@' {
			limit := max(a.completion.fileShown, a.completion.selected+1)
			if limit < len(items) {
				shown = items[:limit]
			}
		}
		scroll := native.Scroll(c).FillWidth().MaxHeight(156)
		if q.kind == '@' {
			scroll = scroll.TrackScroll(&a.completion.fileScroll)
			if len(shown) < len(items) && a.completion.fileScroll.MaxY > 0 && a.completion.fileScroll.Y >= a.completion.fileScroll.MaxY-24 {
				a.completion.fileShown += completionFilePageSize
				c.Invalidate()
			}
		}
		scroll.Children(func() {
			native.Column(c).FillWidth().Gap(2).Children(func() {
				for i, item := range shown {
					row := native.ButtonBase(c.Key(item.name + item.path + fmt.Sprint(i))).Label("候选 " + item.name).FillWidth().Radius(6).KeepFocus()
					if i == a.completion.selected || row.Hovered() {
						row.Background(t.SurfaceHover)
					}
					if q.kind == '@' {
						row.Padding(6, 8)
						row.Children(func() {
							native.Row(c).FillWidth().AlignItems(native.Center).Gap(8).Children(func() {
								glyph := fileIcon
								if item.dir {
									glyph = folderIcon
								}
								native.Icon(c, glyph).Size(13, 13).Shrink(0).TextColor(t.TextMuted)
								native.Text(c, item.name).FontSize(13).SingleLine().Shrink(0)
								native.Text(c, item.description).FontSize(11).TextColor(t.TextMuted).SingleLine().Grow(1).MinWidth(0)
							})
						})
					} else {
						row.Padding(7, 8)
						row.Children(func() {
							native.Column(c).FillWidth().Gap(3).Children(func() {
								native.Text(c, item.name).FontSize(13).SingleLine()
								native.Text(c, strings.Join(strings.Fields(item.description), " ")).FontSize(11).TextColor(t.TextMuted).SingleLine()
							})
						})
					}
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
		if q.kind == '@' && a.completion.filesTruncated {
			native.Text(c, "工作区过大，候选仅覆盖部分内容").FontSize(10).TextColor(t.TextMuted)
		}
	})
	a.completion.reveal = false
}
