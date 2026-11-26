package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

type gitBranchPicker struct {
	workspace *workspacePage
	query     string
	shown     string
	selected  int
	prompt    bool
	base      string
	errorText string
	catalog   *branchCatalog
	checked   time.Time
	current   string
	choices   []gitBranchChoice
}

type gitBranchChoice struct {
	branch    gitview.Branch
	prompt    bool
	create    string
	positions []int
	score     int
}

type gitBranchPalette struct {
	surface, border, text, muted, faint, accent, hover, selected native.Color
}

var gitBranchHover = native.ElementTransition{Duration: 120 * time.Millisecond, Colors: true}

func (a *App) gitBranchColors(t *native.Theme) gitBranchPalette {
	if themeByID(a.application().preferences.ThemeID).id != "" {
		return gitBranchPalette{t.Surface, t.Border, t.Text, t.TextMuted, t.TextMuted.Alpha(.8), t.Accent, t.SurfaceHover, t.Accent.Alpha(.16)}
	}
	if t.Dark {
		return gitBranchPalette{
			native.Hex("#202027"), native.Hex("#26262e"), native.Hex("#d6d6de"), native.Hex("#8c8c99"),
			native.Hex("#666672"), native.Hex("#8b7cf6"), native.RGBA(255, 255, 255, .045), native.RGBA(139, 124, 246, .16),
		}
	}
	return gitBranchPalette{
		native.Hex("#ffffff"), native.Hex("#e4e4ea"), native.Hex("#2a2a33"), native.Hex("#6c6c79"),
		native.Hex("#8e8e9a"), native.Hex("#6d5bd0"), native.RGBA(0, 0, 0, .04), native.RGBA(109, 91, 208, .12),
	}
}

func (a *App) openGitBranchPicker(w *workspacePage) {
	a.branchPicker = &gitBranchPicker{workspace: w}
	a.refreshBranchCatalog(w.root)
}

func (a *App) gitBranchPickerView(c *native.Context) {
	picker := a.branchPicker
	if picker == nil {
		return
	}
	w := picker.workspace
	catalog := a.branchCatalog(w.root)
	if picker.prompt {
		a.gitBranchPromptView(c, picker, catalog.branches)
		return
	}
	query := strings.TrimSpace(picker.query)
	loading := !catalog.loaded && catalog.err == nil
	picker.updateChoices(catalog, query)
	choices := picker.choices
	a.gitBranchOverlay(c.Key("git-branch-picker"), 560, "切换分支", func(box native.Element) {
		p := a.gitBranchColors(c.Theme())
		box.Clip().Children(func() {
			moved := false
			native.Row(c).Padding(10, 14).Gap(10).AlignItems(native.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
				native.Icon(c, gitIcon).Size(15, 15).TextColor(p.muted)
				native.Box(c).Grow(1).MinWidth(0).Children(func() {
					input := native.TextInputBase(c.Key("query"), &picker.query).FillWidth().MinWidth(0).FontSize(14).TextColor(p.text).AutoFocus().
						Label("搜索 Git 分支").Disabled(w.gitBusy != "")
					if picker.query == "" {
						native.Text(c, "切换到分支，或输入名称以创建分支").Absolute().Left(0).Right(0).Top(0).
							PassThrough().FontSize(14).TextColor(p.muted).SingleLine()
					}
					if input.Changed() {
						query = strings.TrimSpace(picker.query)
						picker.updateChoices(catalog, query)
						choices = picker.choices
						moved = true
					}
					if input.Shortcut(0, native.KeyDown) {
						picker.selected = min(picker.selected+1, max(0, len(choices)-1))
						moved = true
					}
					if input.Shortcut(0, native.KeyUp) {
						picker.selected = max(0, picker.selected-1)
						moved = true
					}
					if input.Submitted() && picker.selected < len(choices) {
						a.chooseGitBranch(picker, choices[picker.selected])
					}
				})
			})
			switch {
			case len(choices) > 0:
				_, height := c.Size()
				native.Scroll(c.Key("results")).MaxHeight(min(380, max(80, height-132))).Padding(6).Children(func() {
					group := ""
					for index, choice := range choices {
						if query == "" && choice.branch.Name != "" {
							nextGroup := "本地分支"
							if choice.branch.Remote {
								nextGroup = "远程分支"
							}
							if nextGroup != group {
								native.Text(c, nextGroup).FontSize(11.5).TextColor(p.faint).Padding(6, 10, 2, 10)
								group = nextGroup
							}
						}
						a.gitBranchRow(c, p, picker, choice, index == picker.selected, moved)
						if index == 0 && choice.branch.Name == "" && len(choices) > 1 {
							native.Box(c).Height(1).Background(p.border).Margin(4, 6)
						}
					}
				})
			case loading:
				native.Text(c, "正在读取分支…").FontSize(13).TextColor(p.muted).Padding(12, 16)
			case catalog.err != nil:
				native.Text(c, "读取分支失败："+catalog.err.Error()).FontSize(13).TextColor(c.Theme().Danger).Padding(12, 16).MaxLines(3)
			default:
				native.Text(c, "没有匹配的分支").FontSize(13).TextColor(p.muted).Padding(12, 16)
			}
			action := "切换"
			if picker.selected < len(choices) && choices[picker.selected].branch.Name == "" {
				action = "创建"
			}
			gitBranchFooter(c, p, action)
		})
	})
}

func (picker *gitBranchPicker) updateChoices(catalog *branchCatalog, query string) {
	current := picker.workspace.git.Branch
	if picker.catalog == catalog && picker.checked == catalog.checked && picker.shown == query && picker.current == current {
		return
	}
	picker.catalog, picker.checked, picker.shown, picker.current = catalog, catalog.checked, query, current
	picker.choices, picker.selected = gitBranchChoices(catalog.branches, current, query, catalog.loaded && catalog.err == nil)
}

func (a *App) gitBranchOverlay(c *native.Context, width float32, label string, build func(native.Element)) {
	native.Overlay(c, func() {
		back := native.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).AlignItems(native.Center).Modal()
		back.Children(func() {
			t := c.Theme()
			p := a.gitBranchColors(t)
			box := native.Column(c).Width(width).MaxWidthPercent(92).Margin(46, 0, 0, 0).Radius(10).
				Background(p.surface).Border(1, p.border).Role(native.RoleDialog).Label(label)
			if a.application().preferences.ThemeID == "claude" {
				box.Border(1, p.accent.Alpha(.8))
			}
			if t.Dark {
				box.Shadow(0, 16, 40, 0, native.RGBA(0, 0, 0, .40))
			} else {
				box.Shadow(0, 12, 32, 0, native.RGBA(20, 20, 40, .16))
			}
			progress := box.Keyframes("show", []native.Keyframe{{Value: 0}, {At: 150 * time.Millisecond, Value: 1, Ease: native.EaseOut}})
			scale := .98 + .02*progress
			build(box.Opacity(progress).TransformOrigin(.5, 0).Scale(scale, scale))
			if box.PressedOutside() || back.OverlayShortcut(0, native.KeyEscape) {
				a.branchPicker = nil
			}
		})
	})
}

func (a *App) gitBranchRow(c *native.Context, p gitBranchPalette, picker *gitBranchPicker, choice gitBranchChoice, selected, moved bool) {
	branch := choice.branch
	key, label := "branch:"+branch.Name, "切换分支 "+branch.Name
	if choice.prompt {
		key, label = "create", "创建分支…"
	} else if choice.create != "" {
		key, label = "create", "创建分支 "+choice.create
	}
	row := native.ButtonBase(c.Key(key)).Padding(5, 10).Gap(10).Radius(6).Justify(native.Start).
		AlignItems(native.Center).Label(label).Disabled(picker.workspace.gitBusy != "")
	if selected {
		row.Background(p.selected)
		if moved {
			row.ScrollIntoView()
		}
	} else {
		row.Transition(gitBranchHover)
		if row.Hovered() {
			row.Background(p.hover)
		}
	}
	if row.Clicked() {
		a.chooseGitBranch(picker, choice)
	}
	row.Children(func() {
		switch {
		case branch.Name == "":
			native.Icon(c, gitPlusIcon).Size(14, 14).TextColor(p.accent)
			if choice.prompt {
				native.Text(c, label).FontSize(13).TextColor(p.text).SingleLine().Shrink(1)
			} else {
				native.RichText(c, native.Span{Text: "创建分支 "}, native.Span{Text: choice.create, Weight: 650}).FontSize(13).TextColor(p.text).SingleLine().Shrink(1)
			}
			native.Spacer(c)
			native.Text(c, "基于 "+gitBranchSource(picker.workspace)).FontSize(12).TextColor(p.faint).SingleLine().Shrink(1)
		default:
			glyph, color := gitIcon, p.muted
			switch {
			case branch.Remote:
				glyph = gitFetchIcon
			case branch.Name == picker.workspace.git.Branch:
				glyph, color = checkIcon, p.accent
			}
			native.Icon(c, glyph).Size(14, 14).TextColor(color)
			native.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				native.RichText(c, gitBranchSpans(branch.Name, choice.positions, p)...).FontSize(13).SingleLine()
				info := []string{branch.Author, gitBranchAge(branch.CommitTime), branch.Subject}
				info = slices.DeleteFunc(info, func(value string) bool { return value == "" })
				native.Text(c, strings.Join(info, " • ")).FontSize(12).TextColor(p.faint).SingleLine()
			})
		}
	})
}

func gitBranchFooter(c *native.Context, p gitBranchPalette, action string) {
	native.Row(c).Height(30).Padding(0, 14).Gap(14).AlignItems(native.Center).Background(p.hover).
		BorderWidth(1, 0, 0, 0).BorderColor(p.border).Children(func() {
		hint := func(label string, keys ...string) {
			native.Row(c).Gap(6).AlignItems(native.Center).Shrink(0).Children(func() {
				native.Row(c).Gap(3).Children(func() {
					for _, key := range keys {
						native.Text(c, key).FontSize(11).TextColor(p.muted).TextAlign(native.Center).
							MinWidth(18).Height(18).Padding(1, 4, 0).Radius(4).Background(p.hover).Border(1, p.border)
					}
				})
				native.Text(c, label).FontSize(11.5).TextColor(p.muted).SingleLine()
			})
		}
		hint("导航", "↑", "↓")
		hint(action, "⏎")
		hint("关闭", "esc")
		native.Spacer(c)
		native.Text(c, "输入新名称以创建分支").FontSize(11.5).TextColor(p.faint).SingleLine().Shrink(1).MinWidth(0)
	})
}

func (a *App) chooseGitBranch(picker *gitBranchPicker, choice gitBranchChoice) {
	w := picker.workspace
	if a.branchPicker != picker || w.gitBusy != "" || w.gitPreparing || w.gitOperation != "" || a.gitQuitPending || a.quitStopping {
		return
	}
	if choice.prompt {
		picker.prompt, picker.query, picker.base = true, "", "HEAD"
		return
	}
	a.branchPicker = nil
	if choice.create != "" {
		a.gitChangeBranch(w, choice.create, true, picker.base)
		return
	}
	name := choice.branch.Name
	if choice.branch.Remote {
		_, name, _ = strings.Cut(name, "/")
	}
	if name != w.git.Branch {
		a.gitSwitchBranch(w, name, false)
	}
}

func (a *App) gitBranchPromptView(c *native.Context, picker *gitBranchPicker, branches []gitview.Branch) {
	a.gitBranchOverlay(c.Key("git-branch-prompt"), 420, "创建分支…", func(box native.Element) {
		t := c.Theme()
		p := a.gitBranchColors(t)
		box.Padding(14, 16).Gap(10).Children(func() {
			native.Row(c).Gap(8).AlignItems(native.Center).Children(func() {
				native.Text(c, "创建分支…").FontSize(13).FontWeight(600).TextColor(p.text).SingleLine().Shrink(0)
			})
			gitBranchBaseSelector(c, picker, branches)
			input := native.TextInput(c.Key("name"), &picker.query).AutoFocus().FontSize(13).Label("新分支名称")
			if input.Changed() {
				picker.errorText = ""
			}
			if picker.errorText != "" {
				input.Error(picker.errorText)
				native.Text(c, picker.errorText).FontSize(12).TextColor(t.Danger)
			}
			submit := input.Submitted()
			name := strings.TrimSpace(picker.query)
			native.Row(c).Gap(8).Justify(native.End).Children(func() {
				if native.Button(c, "取消").Clicked() {
					a.branchPicker = nil
				}
				submit = native.PrimaryButton(c, "创建分支").Disabled(name == "" || picker.workspace.gitBusy != "").Clicked() || submit
			})
			if submit && name != "" {
				switch {
				case !validGitBranchName(name):
					picker.errorText = "请输入有效的 Git 分支名称。"
				case slices.ContainsFunc(branches, func(branch gitview.Branch) bool { return !branch.Remote && branch.Name == name }):
					picker.errorText = "已存在同名分支。"
				default:
					a.chooseGitBranch(picker, gitBranchChoice{create: name})
				}
			}
		})
	})
}

func gitBranchBaseSelector(c *native.Context, picker *gitBranchPicker, branches []gitview.Branch) {
	t := c.Theme()
	current := gitBranchSource(picker.workspace)
	currentLabel := current + "（当前）"
	label := currentLabel
	if picker.base != "HEAD" {
		label = strings.TrimPrefix(strings.TrimPrefix(picker.base, "refs/heads/"), "refs/remotes/")
	}
	native.Row(c).FillWidth().Gap(8).AlignItems(native.Center).Children(func() {
		native.Text(c, "基于分支").FontSize(12).TextColor(t.TextMuted).Shrink(0)
		selection := native.SelectBase(c.Key("source"), &picker.base)
		trigger := selection.Trigger.Label("新分支基于分支").Grow(1).MinWidth(0).Height(28).
			Padding(0, 8).Gap(6).AlignItems(native.Center).Border(1, t.Border).Radius(6)
		buttonFace(c, trigger, t.Surface, t.SurfaceHover, t.SurfacePressed, 6, 0)
		trigger.Children(func() {
			native.Text(c, label).FontSize(12).SingleLine().Grow(1).MinWidth(0)
			native.Icon(c, chevron).Size(10, 10).Shrink(0).TextColor(t.TextMuted)
		})
		selection.Popup(func(panel native.Element) {
			panel.Width(trigger.Bounds().W).Padding(4).Radius(8).Background(t.Surface).Border(1, t.Border)
			native.Scroll(c.Key("sources")).FillWidth().MaxHeight(196).Children(func() {
				item := func(ref, label string) {
					row := selection.Item(ref).Label("来源分支 "+label).FillWidth().Height(28).Padding(0, 6).Radius(4).AlignItems(native.Center)
					if row.Highlighted() {
						row.Background(t.SurfaceHover)
					}
					row.Children(func() { native.Text(c, label).FontSize(12).SingleLine().Grow(1).MinWidth(0) })
				}
				item("HEAD", currentLabel)
				for _, branch := range branches {
					if branch.Remote || branch.Name != current {
						item(branch.Ref, branch.Name)
					}
				}
			})
		})
	})
}

func gitBranchSource(w *workspacePage) string {
	if w.git.Branch == "(detached)" {
		return "HEAD"
	}
	return w.git.Branch
}

func validGitBranchName(name string) bool {
	if name == "" || name == "HEAD" || strings.HasPrefix(name, "-") || strings.HasSuffix(name, ".") ||
		strings.ContainsAny(name, " ~^:?*[\\") || strings.Contains(name, "..") || strings.Contains(name, "@{") {
		return false
	}
	if strings.ContainsFunc(name, func(char rune) bool { return char < 32 || char == 127 }) {
		return false
	}
	for part := range strings.SplitSeq(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

func gitBranchChoices(branches []gitview.Branch, current, query string, ready bool) ([]gitBranchChoice, int) {
	local := make(map[string]bool, len(branches))
	for _, branch := range branches {
		if !branch.Remote {
			local[branch.Name] = true
		}
	}
	rank := func(branch gitview.Branch) int {
		if branch.Remote {
			return 2
		}
		if branch.Name == current {
			return 0
		}
		return 1
	}
	var matches []gitBranchChoice
	exists := false
	pattern := []rune(strings.ToLower(strings.ReplaceAll(query, " ", "")))
	for _, branch := range branches {
		_, short, _ := strings.Cut(branch.Name, "/")
		if branch.Remote && (local[short] || short == "HEAD") {
			continue
		}
		exists = exists || branch.Name == query || branch.Remote && short == query
		positions, score := gitBranchMatch(branch.Name, pattern)
		if len(positions) == len(pattern) {
			matches = append(matches, gitBranchChoice{branch: branch, positions: positions, score: score})
		}
	}
	slices.SortStableFunc(matches, func(left, right gitBranchChoice) int {
		if result := cmp.Compare(right.score, left.score); result != 0 {
			return result
		}
		if len(pattern) > 0 {
			if result := cmp.Compare(len(left.branch.Name), len(right.branch.Name)); result != 0 {
				return result
			}
		}
		if result := cmp.Compare(rank(left.branch), rank(right.branch)); result != 0 {
			return result
		}
		return right.branch.CommitTime.Compare(left.branch.CommitTime)
	})
	if len(pattern) > 0 {
		matches = matches[:min(200, len(matches))]
	}
	if ready && (query == "" || validGitBranchName(query) && !exists) {
		selected := 0
		if len(matches) > 0 {
			selected = 1
		}
		return slices.Insert(matches, 0, gitBranchChoice{prompt: query == "", create: query}), selected
	}
	return matches, 0
}

func gitBranchMatch(name string, pattern []rune) ([]int, int) {
	if len(pattern) == 0 {
		return nil, 0
	}
	positions := make([]int, len(pattern))
	index := len(pattern) - 1
	for end := len(name); end > 0 && index >= 0; {
		char, size := utf8.DecodeLastRuneInString(name[:end])
		end -= size
		if unicode.ToLower(char) == pattern[index] {
			positions[index] = end
			index--
		}
	}
	if index >= 0 {
		return nil, 0
	}
	base := strings.LastIndexByte(name, '/') + 1
	score := gitBranchMatchScore(name, pattern, positions, base)
	pulled := slices.Clone(positions)
	for index := 1; index < len(pulled); index++ {
		next := pulled[index-1] + utf8.RuneLen(pattern[index-1])
		if next < pulled[index] {
			if char, _ := utf8.DecodeRuneInString(name[next:]); unicode.ToLower(char) == pattern[index] {
				pulled[index] = next
			}
		}
	}
	if pulledScore := gitBranchMatchScore(name, pattern, pulled, base); pulledScore > score {
		return pulled, pulledScore
	}
	return positions, score
}

func gitBranchMatchScore(name string, pattern []rune, positions []int, base int) int {
	score := -(len(name)-base)/4 - strings.Count(name, "/")
	for index, position := range positions {
		score += 10
		if position >= base {
			score += 8
		}
		previous, _ := utf8.DecodeLastRuneInString(name[:position])
		current, _ := utf8.DecodeRuneInString(name[position:])
		if position == 0 || strings.ContainsRune("/_-. ", previous) || unicode.IsLower(previous) && unicode.IsUpper(current) {
			score += 12
		}
		if index > 0 && positions[index-1]+utf8.RuneLen(pattern[index-1]) == position {
			score += 15
		}
		if index == 0 && position == base {
			score += 10
		}
	}
	return score
}

func gitBranchSpans(name string, positions []int, p gitBranchPalette) []native.Span {
	var spans []native.Span
	start, matched := 0, false
	appendSpan := func(end int) {
		if end > start {
			span := native.Span{Text: name[start:end], Color: p.text}
			if matched {
				span.Color, span.Weight = p.accent, 650
			}
			spans = append(spans, span)
		}
	}
	for offset := range name {
		hit := len(positions) > 0 && positions[0] == offset
		if hit {
			positions = positions[1:]
		}
		if hit != matched {
			appendSpan(offset)
			start, matched = offset, hit
		}
	}
	appendSpan(len(name))
	return spans
}

func gitBranchAge(stamp time.Time) string {
	if stamp.IsZero() {
		return ""
	}
	age := time.Since(stamp)
	const day = 24 * time.Hour
	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm", age/time.Minute)
	case age < day:
		return fmt.Sprintf("%dh", age/time.Hour)
	case age < 7*day:
		return fmt.Sprintf("%dd", age/day)
	case age < 30*day:
		return fmt.Sprintf("%dw", age/(7*day))
	case age < 365*day:
		return fmt.Sprintf("%dmo", age/(30*day))
	default:
		return fmt.Sprintf("%dy", age/(365*day))
	}
}
