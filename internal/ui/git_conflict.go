package ui

import (
	"context"
	"path/filepath"
	"time"
	"unicode/utf8"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

type gitConflictState struct {
	sides                   gitview.ConflictSides
	base, current, incoming *native.TextBuffer
	scroll                  [3]native.ScrollState
	selected                int
	showSides               bool
}

func (a *App) gitOpenConflict(w *workspacePage, entry gitview.Entry) {
	a.openFile(w.root, filepath.Join(w.root, entry.Path))
	d := a.currentFile()
	if d == nil || d.gitConflict != nil {
		return
	}
	ctx, cancel := a.projectContext(5 * time.Second)
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		sides, err := gitview.ConflictText(ctx, w.root, entry.Path)
		a.update(func() {
			if a.files[d.id] != d {
				return
			}
			if err != nil {
				d.err = err.Error()
				return
			}
			d.gitConflict = &gitConflictState{
				sides: sides, showSides: true,
				base: native.NewTextBuffer(sides.Base), current: native.NewTextBuffer(sides.Current), incoming: native.NewTextBuffer(sides.Incoming),
			}
		})
	})
}

func (a *App) gitConflictToolbar(c *native.Context, d *filePage) {
	if d.buffer == nil {
		return
	}
	w := a.workspace(d.root)
	state := d.gitConflict
	blocks := gitview.ConflictBlocks(d.buffer.String())
	state.selected = min(max(0, state.selected), max(0, len(blocks)-1))
	native.Column(c).FillWidth().Padding(4, 12).Gap(4).Children(func() {
		native.Row(c).FillWidth().Gap(5).AlignItems(native.Center).Children(func() {
			native.Text(c, "解决冲突").FontSize(11).TextColor(c.Theme().Warning).Grow(1)
			if actionButton(c, "三方版本").Height(26).Clicked() {
				state.showSides = !state.showSides
			}
			if actionButton(c, "保存并标记已解决").Height(26).Disabled(w.gitBusy != "" || gitview.HasConflictMarkers(d.buffer.String())).Clicked() {
				a.saveFile(d, false, false, func(ok bool) {
					if !ok || d.dirty() {
						return
					}
					rel, _ := filepath.Rel(w.root, d.path)
					a.gitAction(w, "标记已解决", func(ctx context.Context) error { return gitview.MarkResolved(ctx, w.root, rel) }, func() {
						d.gitConflict = nil
					})
				})
			}
		})
		if state.showSides {
			native.Row(c).FillWidth().Height(140).Gap(1).Background(c.Theme().Border).Children(func() {
				for i, side := range []struct {
					name string
					text *native.TextBuffer
				}{{"共同祖先", state.base}, {"当前版本", state.current}, {"传入版本", state.incoming}} {
					native.Column(c.Key(side.name)).Grow(1).MinWidth(0).FillHeight().Background(c.Theme().Background).Children(func() {
						native.Text(c, side.name).FontSize(10).TextColor(c.Theme().TextMuted).Padding(4, 6)
						native.TextAreaBufferBase(c, side.text).Label("冲突 "+side.name).ReadOnly(true).Font(a.codeFontFamily()).
							FontSize(10).FixedLineHeight(16).Padding(4, 6).FillWidth().Grow(1).MinHeight(0).TrackScroll(&state.scroll[i])
					})
				}
			})
		}
		if len(blocks) > 0 {
			native.Row(c).FillWidth().Gap(4).Children(func() {
				if gitIconButton(c.Key("prev"), "上一个冲突", upIcon, false).Clicked() {
					state.selected = (state.selected + len(blocks) - 1) % len(blocks)
					a.gitRevealConflict(d, blocks[state.selected])
				}
				if gitIconButton(c.Key("next"), "下一个冲突", downIcon, false).Clicked() {
					state.selected = (state.selected + 1) % len(blocks)
					a.gitRevealConflict(d, blocks[state.selected])
				}
				for _, choice := range []string{"接受当前", "接受传入", "接受两者"} {
					if actionButton(c.Key(choice), choice).Height(26).Clicked() {
						block := blocks[state.selected]
						replacement := block.Current
						if choice == "接受传入" {
							replacement = block.Incoming
						} else if choice == "接受两者" {
							replacement += block.Incoming
						}
						text := d.buffer.String()
						d.buffer.Replace(utf8.RuneCountInString(text[:block.Start]), utf8.RuneCountInString(text[:block.End]), replacement)
						a.observeFile(d)
						d.resync = true
					}
				}
			})
		}
	})
}

func (a *App) gitRevealConflict(d *filePage, block gitview.ConflictBlock) {
	runeOffset := utf8.RuneCountInString(d.buffer.String()[:block.Start])
	line := d.buffer.Snapshot().LineAt(runeOffset)
	d.scroll.Y = float32(line) * 20
	d.pinY, d.pinFrame = d.scroll.Y, 2
}
