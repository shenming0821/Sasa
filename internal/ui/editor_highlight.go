package ui

import (
	"context"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/editor"
	gitview "github.com/shenming0821/Fuyu/internal/git"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func (a *App) requestHighlight(d *filePage) {
	if d.buffer == nil || d.highlighting || !a.canAsync() {
		return
	}
	family := a.codeFontFamily()
	path := d.path
	buffer := d.buffer
	snapshot := buffer.Snapshot()
	review := d.gitReview
	decorate := d.kind == "file" && d.file.Reason == "" || d.kind == "diff" && len(d.gitRows) > 0
	var leftText, rightText string
	if review != nil {
		leftText, rightText = diffPairText(d.gitPairs)
	}
	first := max(0, int(d.scroll.Y/20)-50)
	last := min(snapshot.LineCount()-1, first+200)
	lo, _ := snapshot.LineRange(first)
	_, hi := snapshot.LineRange(last)
	ctx, cancel := context.WithCancel(context.Background())
	d.highlightCancel = cancel
	d.highlighting = true
	a.runHighlightTask(ctx, func() {
		var text string
		var ranges []syntaxToken
		var leftRanges, rightRanges []syntaxToken
		var width float32
		var leftWidth, rightWidth float32
		defer func() {
			valid := ctx.Err() == nil
			cancel()
			select {
			case <-a.shutdown:
				return
			default:
			}
			a.update(func() {
				d.highlighting = false
				d.highlightCancel = nil
				if !valid || a.files[d.id] != d || d.buffer != buffer || d.path != path || family != a.codeFontFamily() {
					return
				}
				if d.buffer.Version() == snapshot.Version() {
					d.highlights = ranges
					d.highlightVersion = snapshot.Version()
					d.contentWidth = width
					d.gitLeftWidth, d.gitRightWidth = leftWidth, rightWidth
					d.gitLeftHighlights, d.gitRightHighlights = leftRanges, rightRanges
					d.modified = d.kind == "file" && text != d.savedText
				}
			})
		}()
		if ctx.Err() != nil {
			return
		}
		lexer := editorLexer(path)
		if decorate && lexer != nil && snapshot.ByteLen() <= editor.EditBytes {
			preview := highlightText(ctx, lexer, snapshot.Slice(lo, hi), lo)
			if ctx.Err() != nil {
				return
			}
			a.update(func() {
				if ctx.Err() == nil && a.files[d.id] == d && d.path == path && d.buffer == buffer && buffer.Version() == snapshot.Version() && family == a.codeFontFamily() {
					d.highlights = preview
					d.highlightVersion = snapshot.Version()
				}
			})
		}
		text = snapshot.String()
		width = editorTextWidthForFont(ctx, text, family, 13, 90)
		if review != nil {
			leftWidth = editorTextWidthForFont(ctx, leftText, family, 12, 76)
			rightWidth = editorTextWidthForFont(ctx, rightText, family, 12, 76)
		}
		if decorate && len(text) <= editor.EditBytes && lexer != nil {
			ranges = highlightText(ctx, lexer, text, 0)
			if review != nil {
				leftRanges = highlightText(ctx, lexer, leftText, 0)
				rightRanges = highlightText(ctx, lexer, rightText, 0)
			}
		}
	})
}

func editorTextWidth(ctx context.Context, text string, size, padding float32) float32 {
	return editorTextWidthForFont(ctx, text, codeFont, size, padding)
}

func editorTextWidthForFont(ctx context.Context, text, family string, size, padding float32) float32 {
	var width float32
	for line := range strings.SplitSeq(text, "\n") {
		if ctx.Err() != nil {
			return 0
		}
		// Use the editor's shaping engine, including fallback fonts, tabs
		// and trailing whitespace; rune counts do not describe glyph width.
		w, _ := native.ShapeText(line, native.Font{Family: family, Size: size}, 0).Size()
		width = max(width, w)
	}
	return width + padding
}

func highlightText(ctx context.Context, lexer chroma.Lexer, text string, offset int) []syntaxToken {
	iter, err := lexer.Tokenise(nil, text)
	if err != nil {
		return nil
	}
	var ranges []syntaxToken
	for token := iter(); token != chroma.EOF; token = iter() {
		if ctx.Err() != nil {
			return nil
		}
		end := offset + utf8.RuneCountInString(token.Value)
		if end > offset && token.Type != chroma.Text && token.Type != chroma.TextWhitespace {
			ranges = append(ranges, syntaxToken{Start: offset, End: end, Type: token.Type})
		}
		offset = end
		if len(ranges) > 100000 {
			return nil
		}
	}
	return ranges
}
func (a *App) showDiskComparison(source *filePage) {
	if source.buffer == nil {
		return
	}
	id := "conflict-" + editor.RecoveryID(source.root, source.path)
	if d := a.files[id]; d != nil {
		a.selectFile(d)
		if !d.loaded || d.err != "" {
			a.loadDiskComparison(source, d)
		}
		return
	}
	d := &filePage{id: id, root: source.root, path: source.path, kind: "diff", compare: "外部更改", loading: true}
	a.files[id] = d
	a.tabs = append(a.tabs, storage.Tab{ID: id, Kind: "diff", Workspace: d.root, Path: d.path, Compare: d.compare})
	a.selectFile(d)
	a.loadDiskComparison(source, d)
}
func (a *App) loadDiskComparison(source, d *filePage) {
	if source.buffer == nil {
		return
	}
	local := source.buffer.Snapshot()
	a.cancelFileRead(d)
	d.loading = true
	ctx, cancel := a.projectContext(5 * time.Second)
	d.readContext, d.readCancel = ctx, cancel
	a.runGitTaskContext(ctx, func() {
		defer cancel()
		var disk editor.File
		err := ctx.Err()
		if err == nil {
			disk, err = editor.Read(source.path)
		}
		text := ""
		if err == nil {
			text, err = gitview.CompareText(ctx, local.String(), disk.Text)
			if text == "" && err == nil {
				text = "本地编辑与磁盘版本相同"
			}
		}
		a.update(func() {
			if a.files[d.id] != d || d.readContext != ctx {
				return
			}
			d.readContext, d.readCancel = nil, nil
			d.loading = false
			if err != nil {
				d.err = err.Error()
				return
			}
			review := gitview.ParsePatch(text)
			review.BeforeText, review.BeforeKnown = local.String(), true
			a.gitSetDiffText(d, text, &review)
			d.file.Reason = "只读比较 · 本地编辑 ↔ 磁盘版本"
			d.comparisonSource, d.comparisonVersion = source.id, local.Version()
			d.comparisonDisk = disk
			d.comparisonStale = source.buffer == nil || source.buffer.Version() != local.Version()
		})
	})
}
func (a *App) tabTitle(tab storage.Tab) string {
	if tab.Kind == "empty" {
		return "新标签页"
	}
	if tab.Kind == "terminal" {
		return a.terminalTitle(tab)
	}
	if tab.Kind == "session" {
		for _, s := range a.sessions {
			if s.id == tab.SessionID {
				return s.title()
			}
		}
		return "会话"
	}
	name := filepath.Base(tab.Path)
	rel, _ := filepath.Rel(tab.Workspace, tab.Path)
	parts := strings.Split(rel, string(filepath.Separator))
	needed := 1
	for _, other := range a.tabs {
		if other.ID == tab.ID || other.Kind == "session" || other.Kind == "terminal" || other.Kind == "empty" || other.Path == tab.Path && other.Workspace == tab.Workspace {
			continue
		}
		otherRel, _ := filepath.Rel(other.Workspace, other.Path)
		otherParts := strings.Split(otherRel, string(filepath.Separator))
		for n := 1; n <= len(parts) && n <= len(otherParts); n++ {
			if strings.Join(parts[len(parts)-n:], "/") != strings.Join(otherParts[len(otherParts)-n:], "/") {
				break
			}
			needed = max(needed, min(n+1, len(parts)))
			if n == len(parts) && n == len(otherParts) {
				var roots []string
				for _, candidate := range a.tabs {
					candidateRel, _ := filepath.Rel(candidate.Workspace, candidate.Path)
					if candidate.Kind != "session" && candidateRel == rel {
						roots = append(roots, candidate.Workspace)
					}
				}
				name = rel + " · " + workspaceTabLabel(tab.Workspace, roots)
			}
		}
	}
	if name == filepath.Base(tab.Path) && needed > 1 {
		name = strings.Join(parts[len(parts)-needed:], "/")
	}
	if tab.Kind == "diff" {
		name += " · " + tab.Compare
	}
	return name
}

func workspaceTabLabel(root string, roots []string) string {
	parts := strings.Split(filepath.Clean(root), string(filepath.Separator))
	for n := 1; n <= len(parts); n++ {
		label := strings.Join(parts[len(parts)-n:], string(filepath.Separator))
		unique := true
		for _, other := range roots {
			if filepath.Clean(other) == filepath.Clean(root) {
				continue
			}
			otherParts := strings.Split(filepath.Clean(other), string(filepath.Separator))
			if len(otherParts) >= n && label == strings.Join(otherParts[len(otherParts)-n:], string(filepath.Separator)) {
				unique = false
				break
			}
		}
		if unique {
			return label
		}
	}
	return root
}
