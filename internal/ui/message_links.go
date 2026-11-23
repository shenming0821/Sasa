package ui

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
	"github.com/shenming0821/Sasa/internal/app"
	"github.com/shenming0821/Sasa/internal/attachment"
	"github.com/shenming0821/Sasa/internal/workspace"
)

var messageFileLocationSuffix = regexp.MustCompile(`:([0-9]+)(?::([0-9]+))?$`)
var messageFileLocationFragment = regexp.MustCompile(`^L([0-9]+)(?:C([0-9]+))?(?:-L[0-9]+)?$`)

type fileLocation struct {
	line, column int
}

func (a *App) messageMarkdown(c *native.Context, message *app.Message) {
	theme := c.Theme()
	palette := a.activePalette(theme)
	search := palette.searchFor(theme.Accent, .55)
	a.md.Theme.Strong = theme.Danger
	a.md.Theme.Mark = search.match
	a.md.Theme.CodeColors = palette.editor.codeColors
	text := message.Text
	owner := a.session
	cwd := owner.State.Cwd
	a.md.LoadImage = markdown.FileImages(cwd)
	a.md.OnLink = func(destination string) {
		if path, ok := messageImageDestination(cwd, destination); ok {
			root := a.application()
			root.viewerImage = attachment.Image{Path: path}
			root.viewerOpen, root.viewerZoom = true, 1
			root.viewerScroll = native.ScrollState{}
			return
		}
		if native.SafeToOpen(destination) {
			c.OpenURLThen(destination, func(err error) {
				if err != nil {
					a.forSession(owner).update(func() { owner.State.Error = "打开链接失败：" + err.Error() })
				}
			})
			return
		}
		root, path, err := messageFileLink(cwd, destination)
		if err != nil {
			owner.State.Error = "打开文件链接失败：" + err.Error()
			return
		}
		if path != "" {
			application := a.application()
			application.openFile(root, path)
			if file := application.currentFile(); file != nil {
				file.pendingLocation = messageLinkLocation(destination)
				file.pinFrame = 0
			}
		}
	}
	opts := &a.md
	var o *markdown.Options
	var fm *findMarks
	if a.find.open {
		cur := [2]int{-1, -1}
		if c := a.sessionFindCurrent(message); c != nil {
			cur = *c
		}
		fm = sessionFindMarks(message.Text, a.find.query, cur)
		text = applyMarkEdits(message.Text, fm.edits)
		o = findMessageOptions(&a.md)
		o.CodeBlock = func(cc *native.Context, b *markdown.CodeBlockView) {
			a.findCodeBlock(cc, message, b)
		}
		opts = o
	}
	// Stream's reveal lives on the element; a row that unmounts and
	// rebuilds would type what it already showed. Only the first mounted
	// view of a message streams; later views show it as it is.
	box := native.Column(c.Key("stream:" + message.ID)).FillWidth()
	armed := native.Local(box, streamArmedKey{}, func() bool { return false })
	box.Children(func() {
		if owner.streamReveal(message, armed) {
			markdown.Stream(c, text, opts)
		} else {
			// The current match brightens through a transparent overlay
			// painting only its mark, laid over the block holding it.
			if fm != nil && fm.curSrc != "" {
				ov := findOverlayOptions(opts, search.overlay())
				cb, src := fm.curBlock, fm.curSrc
				o.Block = func(cc *native.Context, d *parser.Document, i int) bool {
					if i != cb {
						return false
					}
					if k := d.Block(i).Kind; k != parser.Paragraph && k != parser.Heading {
						return false
					}
					markdown.View(cc, src, ov).Absolute().Top(0).Left(0).Right(0).PassThrough()
					return false
				}
			}
			markdown.View(c, text, opts)
		}
	})
}

type streamArmedKey struct{}

// streamReveal reports whether message paces its text on this element:
// the first mounted view of an unfinished message streams and keeps
// doing so; any later view shows the text as it is.
func (s *session) streamReveal(m *app.Message, armed *bool) bool {
	if m.Done {
		return false
	}
	if *armed || !s.streamedMessages[m.ID] {
		*armed = true
		if s.streamedMessages == nil {
			s.streamedMessages = map[string]bool{}
		}
		s.streamedMessages[m.ID] = true
		return true
	}
	return false
}

func messageLinkLocation(destination string) *fileLocation {
	path, fragment, _ := strings.Cut(destination, "#")
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	match := messageFileLocationSuffix.FindStringSubmatch(path)
	if match == nil {
		if decoded, err := url.PathUnescape(fragment); err == nil {
			fragment = decoded
		}
		match = messageFileLocationFragment.FindStringSubmatch(fragment)
	}
	if match == nil {
		return nil
	}
	line, err := strconv.Atoi(match[1])
	if err != nil || line < 1 {
		return nil
	}
	column := 1
	if match[2] != "" {
		column, err = strconv.Atoi(match[2])
		if err != nil || column < 1 {
			return nil
		}
	}
	return &fileLocation{line: line, column: column}
}

func (d *filePage) resolveFileLocation(viewportHeight float32) {
	if d.pendingLocation == nil || d.buffer == nil || viewportHeight <= 0 {
		return
	}
	snapshot := d.buffer.Snapshot()
	start, end := snapshot.LineRange(d.pendingLocation.line - 1)
	caret := start + min(d.pendingLocation.column-1, end-start)
	d.pendingSelection = &[2]int{caret, caret}
	line := snapshot.LineAt(caret)
	maxScroll := max(0, float32(snapshot.LineCount())*20+16-viewportHeight)
	d.scroll.Y = min(maxScroll, max(0, 8+float32(line)*20+10-viewportHeight/2))
	d.pinFrame = 0
	d.pendingLocation = nil
}

func messageFileLink(cwd, destination string) (string, string, error) {
	if candidate := messageFileLocationSuffix.ReplaceAllString(destination, ""); candidate != destination {
		if local, err := url.Parse(candidate); err == nil && (local.Scheme == "" || strings.EqualFold(local.Scheme, "file")) {
			destination = candidate
		}
	}
	parsed, err := url.Parse(destination)
	if err != nil {
		return "", "", err
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "" && scheme != "file" {
		return "", "", nil
	}
	if parsed.Host != "" && parsed.Host != "localhost" || parsed.Opaque != "" || parsed.User != nil {
		return "", "", fmt.Errorf("不支持的本地文件地址：%s", destination)
	}
	path := messageFileLocationSuffix.ReplaceAllString(parsed.Path, "")
	if path == "" {
		return "", "", nil
	}
	if strings.ContainsAny(path, "\x00\r\n") {
		return "", "", fmt.Errorf("文件路径包含无效字符")
	}
	if !filepath.IsAbs(cwd) {
		return "", "", fmt.Errorf("会话工作目录无效：%s", cwd)
	}
	root := filepath.Clean(cwd)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	if !workspace.Contains(root, path) {
		root = filepath.Dir(path)
	}
	return root, path, nil
}

// messageImageDestination resolves a clicked image destination to a local
// path for the in-app viewer; other schemes and non-image links return false.
func messageImageDestination(cwd, destination string) (string, bool) {
	path, _, _ := strings.Cut(destination, "#")
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "" && scheme != "file" {
		return "", false
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		return "", false
	}
	local := parsed.Path
	if local == "" {
		local = parsed.Opaque
	}
	if local == "" || strings.ContainsAny(local, "\x00\r\n") || !isImagePath(local) {
		return "", false
	}
	if !filepath.IsAbs(local) {
		if !filepath.IsAbs(cwd) {
			return "", false
		}
		local = filepath.Join(cwd, local)
	}
	return filepath.Clean(local), true
}
