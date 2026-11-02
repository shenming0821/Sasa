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
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/workspace"
)

var messageFileLocationSuffix = regexp.MustCompile(`:([0-9]+)(?::([0-9]+))?$`)
var messageFileLocationFragment = regexp.MustCompile(`^L([0-9]+)(?:C([0-9]+))?(?:-L[0-9]+)?$`)

type fileLocation struct {
	line, column int
}

func (a *App) messageMarkdown(c *native.Context, message *app.Message) {
	a.md.Theme.Strong = c.Theme().Danger
	owner := a.session
	cwd := owner.State.Cwd
	a.md.OnLink = func(destination string) {
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
	if message.Done {
		markdown.View(c, message.Text, &a.md)
	} else {
		markdown.Stream(c, message.Text, &a.md)
	}
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
