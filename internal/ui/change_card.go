package ui

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/editor"
	gitview "github.com/shenming0821/Fuyu/internal/git"
	"github.com/shenming0821/Fuyu/internal/storage"
)

type cachedChangeSummary struct {
	signature uint64
	summary   app.ChangeSummary
}
type turnReview struct {
	files    []app.ChangedFile
	selected int
}

var changeIcon = icon(`<rect x="5" y="3" width="14" height="18" rx="3"/><path d="M9 9h6M12 6v6M9 16h6"/>`)

func (s *session) summarizeChanges(messages []*app.Message) app.ChangeSummary {
	h := fnv.New64a()
	for _, m := range messages {
		fmt.Fprintf(h, "%s\x00%s\x00%t\x00", m.ID, m.Tool.Status, m.Tool.Truncated)
		for _, f := range m.Tool.Changes {
			fmt.Fprintf(h, "%s\x00%s\x00%s\x00%t\x00", f.Path, f.Kind, f.Diff, f.Truncated)
		}
	}
	if s.changeSummaries == nil {
		s.changeSummaries = map[string]cachedChangeSummary{}
	}
	id := messages[0].ID
	cached, ok := s.changeSummaries[id]
	if !ok || cached.signature != h.Sum64() {
		cached = cachedChangeSummary{h.Sum64(), app.SummarizeChanges(s.State.Cwd, messages)}
		// The native history can page indefinitely; retain only a bounded display cache.
		if len(s.changeSummaries) >= 64 {
			clear(s.changeSummaries)
		}
		s.changeSummaries[id] = cached
	}
	result := cached.summary
	turn := messages[0].TurnID
	missing := s.State.Incomplete && turn == s.State.TurnID
	missing = missing || (s.State.DisplayTrimmed && len(s.State.Messages) > 0 && s.State.Messages[0].TurnID == turn && s.State.Messages[0].Role != "user")
	if missing {
		result.Exact = false
		result.Files = append([]app.ChangedFile(nil), result.Files...)
		for i := range result.Files {
			result.Files[i].Exact = false
		}
	}
	return result
}

func changeCounts(c *native.Context, added, deleted int, exact bool) {
	if !exact {
		native.Text(c, "统计不完整").FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
		return
	}
	if added > 0 {
		native.Text(c, fmt.Sprintf("+%d", added)).FontSize(12).TextColor(c.Theme().Success).SingleLine()
	}
	if deleted > 0 {
		native.Text(c, fmt.Sprintf("−%d", deleted)).FontSize(12).TextColor(c.Theme().Danger).SingleLine()
	}
	if added == 0 && deleted == 0 {
		native.Text(c, "无净增减").FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
	}
}

func (a *App) changeCard(c *native.Context, messages []*app.Message) {
	summary := a.session.summarizeChanges(messages)
	id := messages[0].ID
	expanded := a.commandGroupOpen("change-files:" + id)
	theme := c.Theme()
	native.Column(c).Label("文件变更汇总 "+id).FillWidth().MinWidth(0).Radius(12).
		Border(1, theme.Border.Alpha(.7)).Background(theme.Background).Clip().Children(func() {
		native.Row(c.Key("header")).FillWidth().Padding(14).Gap(10).AlignItems(native.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(theme.Border.Alpha(.6)).Children(func() {
			native.Box(c).Size(36, 36).Shrink(0).Radius(10).Background(theme.Surface).AlignItems(native.Center).Justify(native.Center).Children(func() { native.Icon(c, changeIcon).Size(20, 20).TextColor(theme.TextMuted) })
			native.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				native.Text(c, fmt.Sprintf("已编辑 %d 个文件", len(summary.Files))).FontSize(14).FontWeight(600).SingleLine()
				native.Row(c).Gap(5).Children(func() { changeCounts(c, summary.Added, summary.Deleted, summary.Exact) })
			})
		})
		limit := min(3, len(summary.Files))
		if *expanded {
			limit = len(summary.Files)
		}
		for i, file := range summary.Files[:limit] {
			row := treeButton(c.Key(file.Path), "查看本轮差异 "+file.Path).FillWidth().MinWidth(0).Padding(10, 14).Gap(8).Radius(0)
			row.Children(func() {
				path := a.relativeChangePath(file.Path)
				name := native.Row(c).Grow(1).MinWidth(0).AlignItems(native.Center)
				name.Children(func() {
					prefix := changeDirectory(c, path, name.Bounds().W)
					native.RichText(c, native.Span{Text: prefix, Color: theme.TextMuted}, native.Span{Text: filepath.Base(path), Color: theme.Text}).FontSize(13).SingleLine().MinWidth(0).Shrink(1)
				})
				if name.Bounds().W == 0 {
					c.AnimationFrame()
				}
				native.Row(c).Gap(5).Shrink(0).AlignItems(native.Center).Children(func() { changeCounts(c, file.Added, file.Deleted, file.Exact) })
			})
			if row.Clicked() {
				a.openTurnReview(messages, summary, i)
			}
		}
		if len(summary.Files) > 3 {
			label := fmt.Sprintf("再显示 %d 个文件", len(summary.Files)-3)
			if *expanded {
				label = "收起文件列表"
			}
			b := treeButton(c.Key("show-more"), label).FillWidth().Padding(10, 14).Gap(8).Radius(0).Justify(native.Start)
			b.Children(func() {
				native.Text(c, label).FontSize(12).TextColor(theme.Text)
				arrow := native.Icon(c, chevron).Size(12, 12)
				if *expanded {
					arrow.Rotate(180)
				}
			})
			if b.Clicked() {
				*expanded = !*expanded
			}
		}
	})
}

func changeDirectory(c *native.Context, path string, available float32) string {
	base := filepath.Base(path)
	width, _ := c.MeasureText(0, native.Span{Text: base, Size: 13})
	room := available - width
	dir := filepath.Dir(path)
	if dir == "." || room < 24 {
		return ""
	}
	prefix := dir + "/"
	for {
		w, _ := c.MeasureText(0, native.Span{Text: prefix, Size: 13})
		if w <= room {
			return prefix
		}
		at := strings.Index(dir, "/")
		if at < 0 {
			return ""
		}
		dir = dir[at+1:]
		prefix = "…/" + dir + "/"
	}
}

func (a *App) relativeChangePath(path string) string {
	if !filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	root := a.workspaceRoot(a.State.Cwd)
	if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return path
}

func (a *App) openTurnReview(messages []*app.Message, summary app.ChangeSummary, selected int) {
	if selected < 0 || selected >= len(summary.Files) {
		return
	}
	root := a.workspaceRoot(a.State.Cwd)
	if !filepath.IsAbs(root) {
		root = filepath.Dir(summary.Files[selected].Path)
	}
	if !filepath.IsAbs(root) {
		a.State.Error = "此历史记录缺少工作目录，无法打开差异"
		return
	}
	id := "turn-diff-" + editor.RecoveryID(root, fmt.Sprintf("%d:%s", a.session.id, messages[0].ID))
	if a.files == nil {
		a.files = map[string]*filePage{}
	}
	d := a.files[id]
	if d == nil {
		d = &filePage{id: id, root: root, kind: "diff", compare: "本轮修改（历史记录）"}
		a.files[id] = d
		a.tabs = append(a.tabs, storage.Tab{ID: id, Kind: "diff", Workspace: root, Path: root, Compare: d.compare})
	}
	d.turnReview = &turnReview{files: summary.Files, selected: selected}
	a.setTurnReviewFile(d)
	a.selectFile(d)
}

func (a *App) setTurnReviewFile(d *filePage) {
	f := d.turnReview.files[d.turnReview.selected]
	d.path = f.Path
	if !filepath.IsAbs(d.path) {
		d.path = filepath.Join(d.root, d.path)
	}
	text := f.Patch
	d.gitNote = ""
	if !f.Exact {
		d.gitNote = "记录不完整，以下按操作顺序显示已保存的差异，未合并为净变更。"
	}
	if text == "" {
		text = "本轮无净内容增减"
	}
	review := gitview.ParsePatch(text)
	review.Entry = gitview.Entry{Path: f.Path}
	if f.DeletedFile {
		review.Entry.XY = ".D"
	}
	a.gitSetDiffText(d, text, &review)
	d.file.Path, d.file.Reason = d.path, "本轮历史差异 · 只读"
	d.gitHunk, d.caret, d.anchor = 0, 0, 0
	d.scroll, d.hscroll = native.ScrollState{}, native.ScrollState{}
	d.gitLeftScroll, d.gitRightScroll = native.ScrollState{}, native.ScrollState{}
	d.gitLeftHScroll, d.gitRightHScroll = native.ScrollState{}, native.ScrollState{}
	for i := range a.tabs {
		if a.tabs[i].ID == d.id {
			a.tabs[i].Path = d.path
		}
	}
}
