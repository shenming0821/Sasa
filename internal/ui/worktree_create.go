package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
	"github.com/shenming0821/Fuyu/internal/project"
)

type worktreeCreation struct {
	repo                                               project.Repository
	root, dataDir, base, branch, folder, query, target string
	customFolder, loading, busy, picker, created       bool
	sources                                            []gitview.WorktreeSource
	errors                                             map[string]string
	sourceList                                         native.ListState
	filteredSources                                    []gitview.WorktreeSource
	filteredQuery                                      string
	filteredCount                                      int
	headLabel                                          string
	noCommit                                           bool
	catalog                                            *branchCatalog
	catalogVersion                                     uint64
}

func (draft *worktreeCreation) sourceRows() []gitview.WorktreeSource {
	if draft.filteredSources != nil && draft.filteredQuery == draft.query && draft.filteredCount == len(draft.sources) {
		return draft.filteredSources
	}
	draft.filteredSources = make([]gitview.WorktreeSource, 0, len(draft.sources))
	for _, source := range draft.sources {
		if matchesQuery(draft.query, source.Label) {
			draft.filteredSources = append(draft.filteredSources, source)
		}
	}
	draft.filteredQuery, draft.filteredCount = draft.query, len(draft.sources)
	draft.sourceList.ScrollTo(0, native.Start)
	return draft.filteredSources
}

func (a *App) beginWorktreeCreate(repo project.Repository) {
	if a.worktreeCreate != nil && a.worktreeCreate.busy {
		return
	}
	draft := &worktreeCreation{repo: repo, root: repo.Path, loading: true, errors: make(map[string]string)}
	for _, tree := range repo.Worktrees {
		if tree.Available() && a.workspaceRoot(a.activeWorkspace) == tree.Path {
			draft.root = tree.Path
			break
		}
	}
	if a.store != nil {
		draft.dataDir = a.store.Directory()
	}
	for _, tree := range repo.Worktrees {
		if tree.Path != draft.root {
			continue
		}
		draft.base = "refs/heads/" + tree.Branch
		if tree.Detached {
			draft.base = "HEAD"
			draft.headLabel = "当前提交 " + tree.Head[:min(8, len(tree.Head))]
		}
		if tree.Head == "" || strings.Trim(tree.Head, "0") == "" {
			draft.noCommit = true
			draft.errors["base"] = "请先完成首次提交"
		}
	}
	if state := a.workspaces[draft.root]; state != nil && state.git.Head != "" {
		draft.noCommit = state.git.Head == "(initial)"
		if state.git.Branch == "(detached)" {
			draft.base = "HEAD"
			draft.headLabel = "当前提交 " + state.git.Head[:min(8, len(state.git.Head))]
		} else if state.git.Branch != "" {
			draft.base = "refs/heads/" + state.git.Branch
		}
	}
	a.worktreeCreate = draft
	a.refreshBranchCatalog(draft.root)
}

func (draft *worktreeCreation) syncFolder() {
	if !draft.customFolder {
		draft.folder = gitview.WorktreeFolder(draft.branch)
	}
}

func (draft *worktreeCreation) path() string {
	if draft.folder == "" || gitview.ValidateWorktreeFolder(draft.folder) != nil {
		return ""
	}
	return gitview.WorktreeDirectory(draft.dataDir, draft.repo.CommonDir, draft.folder)
}

func (draft *worktreeCreation) setError(err error) {
	var field *gitview.WorktreeFieldError
	if errors.As(err, &field) {
		draft.errors[field.Field] = field.Message
	} else {
		draft.errors["action"] = err.Error()
	}
}

func (a *App) createWorktree(draft *worktreeCreation) {
	if draft.busy || draft.loading || draft.created || a.gitQuitPending || a.quitStopping {
		return
	}
	draft.errors = make(map[string]string)
	if draft.dataDir == "" {
		draft.errors["action"] = "应用数据目录不可用"
		return
	}
	draft.branch = strings.TrimSpace(draft.branch)
	if draft.branch == "" {
		draft.errors["branch"] = "请输入新分支名"
		return
	}
	draft.syncFolder()
	if err := gitview.ValidateWorktreeFolder(draft.folder); err != nil {
		draft.setError(err)
		return
	}
	draft.target = draft.path()
	draft.busy = true
	base, branch, target := draft.base, draft.branch, draft.target
	go func() {
		ctx, cancel := a.projectContext(2 * time.Minute)
		defer cancel()
		result, err := gitview.CreateWorktree(ctx, draft.root, base, branch, target)
		a.update(func() {
			draft.busy = false
			a.invalidateBranchCatalog(draft.root)
			a.refreshBranchCatalog(draft.root)
			if err != nil {
				draft.setError(err)
				return
			}
			draft.created = result.Created
			a.finishWorktreeCreate(draft)
		})
	}()
}

func (a *App) finishWorktreeCreate(draft *worktreeCreation) {
	if draft.busy {
		return
	}
	draft.busy = true
	go func() {
		ctx, cancel := a.projectContext(10 * time.Second)
		defer cancel()
		repo, err := project.Discover(ctx, draft.target)
		a.update(func() {
			draft.busy = false
			if err != nil {
				draft.errors["action"] = fmt.Sprintf("已创建，列表刷新失败：%v\n路径：%s", err, draft.target)
				return
			}
			a.repositories = mergeRepository(a.repositories, repo)
			a.restoreProject(repositoryProjectID(repo.CommonDir))
			delete(a.collapsedProjects, repo.CommonDir)
			delete(a.projectErrors, repo.CommonDir)
			a.worktreeCreate = nil
			a.refreshFolderSelection()
			a.openWorkspacePicker(draft.target)
			a.persist()
		})
	}()
}

func (a *App) worktreeCreateView(c *native.Context) {
	draft := a.worktreeCreate
	if draft == nil {
		return
	}
	open := true
	native.Modal(c.Key("worktree-create"), &open, func() {
		width, _ := c.Size()
		native.Column(c).Width(min(width-64, 440)).Gap(10).Children(func() {
			native.Text(c, "新建 Worktree").FontSize(16).Bold()
			native.Text(c, draft.repo.Name).FontSize(12).TextColor(c.Theme().TextMuted)
			errorText := func(field string) {
				if text := draft.errors[field]; text != "" {
					native.Text(c, text).FontSize(11).TextColor(c.Theme().Danger).FillWidth().MaxLines(5)
				}
			}
			disabled := draft.busy || draft.created
			native.Text(c, "基础分支").FontSize(12)
			label := "请选择基础分支"
			if draft.loading {
				label = "正在读取分支…"
			}
			for _, source := range draft.sources {
				if source.Ref == draft.base {
					label = source.Label
					break
				}
			}
			if actionButton(c, label).Label("Worktree 基础分支").FillWidth().Disabled(disabled || draft.loading).Clicked() {
				draft.picker = !draft.picker
				draft.query = ""
				draft.sourceList.ScrollTo(0, native.Start)
			}
			if draft.picker && !disabled {
				native.TextInput(c, &draft.query).Label("搜索基础分支").Placeholder("搜索分支").FillWidth().AutoFocus()
				rows := draft.sourceRows()
				draft.sourceList.Key = func(index int) any { return rows[index].Ref }
				native.List(c.Key("worktree-sources"), &draft.sourceList, len(rows), func(index int) {
					source := rows[index]
					if actionButton(c.Key(source.Ref), source.Label).Label("Worktree 来源 " + source.Label).FillWidth().Height(28).Clicked() {
						draft.base, draft.picker = source.Ref, false
						delete(draft.errors, "base")
					}
				}).FillWidth().Height(130)
			}
			errorText("base")
			if draft.errors["base"] != "" && !draft.noCommit && !draft.busy && !draft.loading {
				if actionButton(c, "重试读取分支").Clicked() {
					a.invalidateBranchCatalog(draft.root)
					a.refreshBranchCatalog(draft.root)
				}
			}
			native.Text(c, "新分支名").FontSize(12)
			branch := native.TextInput(c, &draft.branch).Label("Worktree 新分支名").FillWidth().Disabled(disabled).AutoFocus()
			if branch.Changed() {
				draft.syncFolder()
				delete(draft.errors, "branch")
			}
			errorText("branch")
			native.Text(c, "文件夹名称").FontSize(12)
			folder := native.TextInput(c, &draft.folder).Label("Worktree 文件夹名称").FillWidth().Disabled(disabled)
			if folder.Changed() {
				draft.customFolder = draft.folder != ""
				draft.syncFolder()
				delete(draft.errors, "folder")
			}
			errorText("folder")
			path := draft.path()
			if draft.created {
				path = draft.target
			}
			if path == "" {
				path = "填写有效名称后显示存放位置"
			}
			native.Text(c, "存放位置："+path).Label("Worktree 存放位置").FontSize(11).TextColor(c.Theme().TextMuted).FillWidth().MaxLines(3)
			if actionButton(c, "复制完整路径").Disabled(draft.path() == "").Clicked() {
				c.WriteClipboard(path)
			}
			errorText("action")
			native.Row(c).FillWidth().Gap(8).Justify(native.End).Children(func() {
				if actionButton(c, "取消").Disabled(draft.busy).Clicked() {
					open = false
				}
				label := "创建"
				if draft.busy {
					label = "创建中…"
				} else if draft.created {
					label = "重试刷新"
				}
				button := actionButton(c, label).Disabled(draft.busy || draft.loading)
				if button.Clicked() || (branch.Submitted() || folder.Submitted()) && !draft.busy && !draft.loading {
					if draft.created {
						a.finishWorktreeCreate(draft)
					} else {
						a.createWorktree(draft)
					}
				}
			})
		})
	})
	if !open && !draft.busy {
		a.worktreeCreate = nil
	}
}
