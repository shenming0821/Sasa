package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/project"
)

func repositoryProjectID(common string) string { return "repo:" + common }
func directoryProjectID(path string) string    { return "dir:" + path }

type sidebarProject struct {
	id    string
	repo  *project.Repository
	group projectGroup
}

type projectDrag struct{ ID string }

func (a *App) projectItems() []sidebarProject {
	var items []sidebarProject
	for i := range a.repositories {
		repo := &a.repositories[i]
		id := repositoryProjectID(repo.CommonDir)
		if !a.hiddenProjects[id] {
			items = append(items, sidebarProject{id: id, repo: repo})
		}
	}
	for _, group := range a.projectGroups() {
		id := directoryProjectID(group.path)
		if !a.hiddenProjects[id] {
			items = append(items, sidebarProject{id: id, group: group})
		}
	}
	rank := make(map[string]int, len(a.projectOrder))
	for i, id := range a.projectOrder {
		rank[id] = i + 1
	}
	slices.SortStableFunc(items, func(a, b sidebarProject) int {
		x, y := rank[a.id], rank[b.id]
		if x == 0 {
			x = len(rank) + 1
		}
		if y == 0 {
			y = len(rank) + 1
		}
		return x - y
	})
	return items
}

func (a *App) orderedProjectIDs() []string {
	var ids []string
	for _, item := range a.projectItems() {
		ids = append(ids, item.id)
	}
	return ids
}

func (a *App) moveProject(source, target string, after bool) {
	ids := a.orderedProjectIDs()
	from, to := slices.Index(ids, source), slices.Index(ids, target)
	if source == target || from < 0 || to < 0 {
		return
	}
	ids = slices.Delete(ids, from, from+1)
	to = slices.Index(ids, target)
	if after {
		to++
	}
	a.projectOrder = slices.Insert(ids, to, source)
	a.persist()
}

func (a *App) removeProject(id string) {
	if !slices.Contains(a.orderedProjectIDs(), id) || id == directoryProjectID("") {
		return
	}
	if a.hiddenProjects == nil {
		a.hiddenProjects = map[string]bool{}
	}
	a.hiddenProjects[id] = true
	a.projectOrder = slices.DeleteFunc(a.projectOrder, func(key string) bool { return key == id })
	// Keep catalog ownership so retained sessions do not reappear as plain folders.
	a.persist()
}

func (a *App) restoreProject(id string) {
	ids := a.orderedProjectIDs()
	if slices.Contains(ids, id) {
		return
	}
	delete(a.hiddenProjects, id)
	a.projectOrder = append(ids, id)
}

// Carry folder visibility and placement across background Git discovery. Read the
// current state on the UI thread so a removal during a scan is also respected.
func (a *App) promoteDirectoryProject(path string, repo project.Repository) {
	dirID, repoID := directoryProjectID(path), repositoryProjectID(repo.CommonDir)
	for _, existing := range a.repositories {
		if existing.CommonDir == repo.CommonDir {
			return
		}
	}
	if a.hiddenProjects[dirID] {
		a.hiddenProjects[repoID] = true
	}
	if i := slices.Index(a.projectOrder, dirID); i >= 0 {
		if !slices.Contains(a.projectOrder, repoID) {
			a.projectOrder[i] = repoID
		} else {
			a.projectOrder = slices.Delete(a.projectOrder, i, i+1)
		}
	}
}

func (a *App) projectHeadingActions(c *native.Context, row, header native.Element, id string) {
	header.Drag(projectDrag{ID: id})
	_, y, _ := row.PointerPosition()
	after := y >= row.Bounds().H/2
	if source, ok := native.DragOver[projectDrag](row); ok && source.ID != id {
		row.DrawOver(func(p *native.Painter, r native.Rect) {
			y := r.Y
			if after {
				y += r.H - 2
			}
			p.Fill(native.Rect{X: r.X, Y: y, W: r.W, H: 2}, c.Theme().Accent, 0)
		})
	}
	if source, ok := native.Drop[projectDrag](row); ok {
		a.moveProject(source.ID, id, after)
	}
	menu := func(m *native.Menu) {
		if path, plain := strings.CutPrefix(id, "dir:"); plain {
			a.projectFinderMenu(m, path)
		} else if common, ok := strings.CutPrefix(id, "repo:"); ok {
			if m.Item("新建 Worktree").Chosen() {
				for _, repo := range a.repositories {
					if repo.CommonDir == common {
						a.beginWorktreeCreate(repo)
						break
					}
				}
			}
		}
		if m.Item("移除项目").Chosen() {
			a.removeProject(id)
		}
	}
	header.ContextMenu(menu)
}

func (a *App) projectFinderMenu(menu *native.Menu, path string) {
	if menu.Item("在Finder中打开").Chosen() {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			err = fmt.Errorf("路径不是文件夹：%s", path)
		}
		if err == nil {
			err = mygo.Shell.OpenPath(path)
		}
		if err != nil {
			a.beginFileAction(path, path, "错误")
			a.fileAction.err = "在Finder中打开失败：" + err.Error()
		}
	}
}
