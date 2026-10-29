package ui

import (
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"unicode"

	native "github.com/egoist/mygo/ui"
)

type worktreeRenameState struct {
	worktreeNames       map[string]string
	worktreeRenameOpen  bool
	worktreeRenamePath  string
	worktreeRenameDraft string
	worktreeRenameError string
}

func (a *App) worktreeName(path string) string {
	if name := a.worktreeNames[path]; name != "" {
		return name
	}
	return filepath.Base(path)
}

func (a *App) beginWorktreeRename(path string) {
	a.worktreeRenamePath = path
	a.worktreeRenameDraft = a.worktreeName(path)
	a.worktreeRenameError = ""
	a.worktreeRenameOpen = true
}

func (a *App) saveWorktreeName(path, name string) error {
	worktree := a.worktreeFor(path)
	if worktree == nil || worktree.Path != path || worktree.Missing || worktree.Prunable {
		return fmt.Errorf("工作区已不在项目列表中，请刷新后重试")
	}
	name = strings.TrimSpace(name)
	if strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("名称请使用单行文字")
	}
	previous := a.worktreeNames
	a.worktreeNames = maps.Clone(previous)
	if a.worktreeNames == nil {
		a.worktreeNames = make(map[string]string)
	}
	if name == "" || name == filepath.Base(path) {
		delete(a.worktreeNames, path)
	} else {
		a.worktreeNames[path] = name
	}
	if err := a.persist(); err != nil {
		a.worktreeNames = previous
		return fmt.Errorf("保存名称失败：%w", err)
	}
	return nil
}

func (a *App) worktreeRenameView(c *native.Context) {
	native.Modal(c.Key("worktree-rename"), &a.worktreeRenameOpen, func() {
		if c.Shortcut(native.Cmd, native.KeyW) {
			a.worktreeRenameOpen = false
		}
		width, _ := c.Size()
		native.Column(c).Width(min(width-64, 400)).Gap(12).Children(func() {
			native.Text(c, "重命名工作区").FontSize(16).Bold()
			native.Text(c, a.worktreeRenamePath).FontSize(11).TextColor(c.Theme().TextMuted).FillWidth().MaxLines(3)
			input := native.TextInput(c, &a.worktreeRenameDraft).Label("工作区名称").Placeholder(filepath.Base(a.worktreeRenamePath)).FillWidth().AutoFocus()
			native.Text(c, "留空恢复文件夹名称").FontSize(11).TextColor(c.Theme().TextMuted)
			if a.worktreeRenameError != "" {
				native.Text(c, a.worktreeRenameError).FontSize(11).TextColor(c.Theme().Danger).FillWidth()
			}
			native.Row(c).FillWidth().Gap(8).Justify(native.End).Children(func() {
				if actionButton(c, "取消重命名").Clicked() {
					a.worktreeRenameOpen = false
				}
				if actionButton(c, "保存名称").Clicked() || input.Submitted() {
					if err := a.saveWorktreeName(a.worktreeRenamePath, a.worktreeRenameDraft); err != nil {
						a.worktreeRenameError = err.Error()
					} else {
						a.worktreeRenameOpen = false
					}
				}
			})
		})
	})
}
