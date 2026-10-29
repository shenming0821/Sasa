package ui

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	native "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/workspace"
)

func (a *App) messageMarkdown(c *native.Context, message *app.Message) {
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
			a.application().openFile(root, path)
		}
	}
	if message.Done {
		markdown.View(c, message.Text, &a.md)
	} else {
		markdown.Stream(c, message.Text, &a.md)
	}
}

func messageFileLink(cwd, destination string) (string, string, error) {
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
	path := parsed.Path
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
