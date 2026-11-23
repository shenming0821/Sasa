package ui

import (
	"context"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/editor"
	gitview "github.com/shenming0821/Sasa/internal/git"
	"github.com/shenming0821/Sasa/internal/storage"
)

func (a *App) requestHighlight(d *filePage) {
	a.requestHighlightAt(d, 0)
}

func (a *App) requestHighlightAt(d *filePage, height float32) {
	if d.buffer == nil || d.highlighting || !a.canAsync() {
		return
	}
	buffer := d.buffer
	snapshot := buffer.Snapshot()
	refreshDocument := d.contentWidth == 0 || d.highlightVersion != snapshot.Version()
	window := d.highlightWindow
	if !refreshDocument && !window.active {
		return
	}
	lineCount := snapshot.LineCount()
	visibleFirst := min(lineCount-1, max(0, int(d.scroll.Y/20)))
	visibleRows := editorHighlightViewRows
	if height > 0 {
		visibleRows = int(height/20) + 2
	}
	visibleLast := min(lineCount-1, visibleFirst+visibleRows-1)
	if !refreshDocument && window.covers(visibleFirst, visibleLast, lineCount) {
		return
	}

	family := a.codeFontFamily()
	path := d.path
	review := d.gitReview
	isFile := d.kind == "file"
	largeReadOnly := isFile && d.file.Reason == editor.LargeFileReadOnly
	decorate := isFile && (d.file.Reason == "" || largeReadOnly) || d.kind == "diff" && len(d.gitRows) > 0
	useWindow := isFile && (!refreshDocument || largeReadOnly || lineCount > editorHighlightWholeLines || snapshot.ByteLen() > editor.EditBytes)
	var leftText, rightText string
	if refreshDocument && review != nil {
		leftText, rightText = diffPairText(d.gitPairs)
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.highlightCancel = cancel
	d.highlighting = true
	a.runHighlightTask(ctx, func() {
		current := func() bool {
			return a.files[d.id] == d && d.buffer == buffer && buffer.Version() == snapshot.Version() && d.path == path && family == a.codeFontFamily()
		}
		publishPreview := func(preview []syntaxToken, previewWindow editorHighlightWindow) {
			a.update(func() {
				if ctx.Err() == nil && current() {
					d.highlights = preview
					d.highlightVersion = snapshot.Version()
					d.highlightWindow = previewWindow
				}
			})
		}
		var text string
		var ranges []syntaxToken
		var leftRanges, rightRanges []syntaxToken
		var width float32
		var leftWidth, rightWidth float32
		var resultWindow editorHighlightWindow
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
				if !valid || !current() {
					return
				}
				d.highlights = ranges
				d.highlightVersion = snapshot.Version()
				d.highlightWindow = resultWindow
				if refreshDocument {
					d.contentWidth = width
					d.gitLeftWidth, d.gitRightWidth = leftWidth, rightWidth
					d.gitLeftHighlights, d.gitRightHighlights = leftRanges, rightRanges
					d.modified = isFile && text != d.savedText
				}
			})
		}()
		if ctx.Err() != nil {
			return
		}
		lexer := editorLexer(path)
		whole := decorate && lexer != nil && !useWindow && snapshot.ByteLen() <= editor.EditBytes
		if whole {
			first := max(0, visibleFirst-50)
			last := min(lineCount-1, first+max(200, visibleRows+50))
			lo, _ := snapshot.LineRange(first)
			_, hi := snapshot.LineRange(last)
			publishPreview(highlightText(ctx, lexer, snapshot.Slice(lo, hi), lo), editorHighlightWindow{})
		}
		if refreshDocument && ctx.Err() == nil {
			text = snapshot.String()
		}
		if whole && ctx.Err() == nil {
			var overflow bool
			ranges, overflow = collectHighlightText(ctx, lexer, text, 0, editorHighlightTokenLimit)
			if overflow {
				ranges = nil
				useWindow = isFile
			}
		}
		if decorate && lexer != nil && useWindow && ctx.Err() == nil {
			sections := window.vueSections
			if refreshDocument {
				sections = nil
				if strings.EqualFold(filepath.Ext(path), ".vue") {
					sections = indexVueHighlightSections(ctx, text)
				}
			}
			ranges, resultWindow = highlightViewport(ctx, lexer, snapshot, sections, visibleFirst, visibleLast)
			resultWindow.startTokens, resultWindow.endTokens = window.startTokens, window.endTokens
			if refreshDocument {
				resultWindow.cacheDocumentEnds(ctx, lexer, snapshot)
				publishPreview(ranges, resultWindow)
			}
		}
		if refreshDocument {
			width = editorTextWidthForFont(ctx, text, family, 13, 90)
			if review != nil {
				leftWidth = editorTextWidthForFont(ctx, leftText, family, 12, 76)
				rightWidth = editorTextWidthForFont(ctx, rightText, family, 12, 76)
				if whole {
					leftRanges = highlightText(ctx, lexer, leftText, 0)
					rightRanges = highlightText(ctx, lexer, rightText, 0)
				}
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
	ranges, overflow := collectHighlightText(ctx, lexer, text, offset, editorHighlightTokenLimit)
	if overflow {
		return nil
	}
	return ranges
}

const editorHighlightTokenLimit = 100_000

func collectHighlightText(ctx context.Context, lexer chroma.Lexer, text string, offset, limit int) ([]syntaxToken, bool) {
	if ctx.Err() != nil || text == "" {
		return nil, false
	}
	iter, err := lexer.Tokenise(nil, text)
	if err != nil {
		return nil, false
	}
	var ranges []syntaxToken
	for token := iter(); token != chroma.EOF; token = iter() {
		if ctx.Err() != nil {
			return nil, false
		}
		end := offset + utf8.RuneCountInString(token.Value)
		if end > offset && token.Type != chroma.Text && token.Type != chroma.TextWhitespace {
			if len(ranges) == limit {
				return ranges, true
			}
			ranges = append(ranges, syntaxToken{Start: offset, End: end, Type: token.Type})
		}
		offset = end
	}
	return ranges, false
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
	if tab.Kind == "empty" || tab.Kind == "terminal" {
		return "新标签页"
	}
	if tab.Kind == "session" {
		for _, s := range a.sessions {
			if s.id == tab.SessionID {
				title := s.title()
				if title == "新会话" && s.saved.CustomTitle == "" {
					for _, message := range s.State.Messages {
						if message.Role == "user" {
							return title
						}
					}
					return "新标签页"
				}
				return title
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

func (a *App) tabTooltip(tab storage.Tab) string {
	if tab.Kind == "terminal" {
		return "终端 · " + tab.Workspace
	}
	if tab.Kind == "session" {
		for _, s := range a.sessions {
			if s.id == tab.SessionID {
				title := a.tabTitle(tab)
				if s.saved.Unread {
					title += "\n有未读回复"
				}
				return title
			}
		}
	}
	return a.tabTitle(tab)
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
