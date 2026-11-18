package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/project"
	"github.com/shenming0821/Sasa/internal/storage"
)

func projectOrderFixture() *App {
	a := New(stubBackend{}, "test", "/repo", nil)
	a.repositories = []project.Repository{{CommonDir: "/repo/.git", Path: "/repo", Name: "repo", Worktrees: []project.Worktree{{Path: "/repo", Branch: "main"}}}}
	a.newSession("/alpha")
	a.newSession("/beta")
	a.projectOrder = []string{"repo:/repo/.git", "dir:/alpha", "dir:/beta"}
	return a
}

func assertProjectOrder(t *testing.T, a *App, want ...string) {
	t.Helper()
	if got := a.orderedProjectIDs(); !slices.Equal(got, want) {
		t.Fatalf("project order = %v, want %v", got, want)
	}
}

func TestProjectHeadingDragAndCancel(t *testing.T) {
	a := projectOrderFixture()
	tt := native.NewTester(a.view, 1000, 760)
	drag := func(source, target string, after, cancel bool) {
		t.Helper()
		from, ok := tt.Find(source)
		if !ok {
			t.Fatal("missing source", source)
		}
		to, ok := tt.Find(target)
		if !ok {
			t.Fatal("missing target", target)
		}
		x, y := from.X+40, from.Y+from.H/2
		destY := to.Y + 2
		if after {
			destY = to.Y + to.H - 2
		}
		tt.Press(x, y)
		tt.Move(x+12, y)
		tt.Move(to.X+40, destY)
		if cancel {
			tt.Key(0, native.KeyEscape)
		}
		tt.Release(to.X+40, destY)
	}
	drag("展开项目 /beta", "展开仓库 /repo/.git", false, false)
	assertProjectOrder(t, a, "dir:/beta", "repo:/repo/.git", "dir:/alpha")
	drag("展开仓库 /repo/.git", "展开项目 /alpha", true, false)
	assertProjectOrder(t, a, "dir:/beta", "dir:/alpha", "repo:/repo/.git")
	drag("展开项目 /beta", "展开仓库 /repo/.git", true, true)
	assertProjectOrder(t, a, "dir:/beta", "dir:/alpha", "repo:/repo/.git")
	if len(a.collapsedProjects) != 0 {
		t.Fatal("drag toggled disclosure")
	}
	if err := tt.Click("展开项目 /alpha"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if !a.collapsedProjects["/alpha"] {
		t.Fatal("normal header click no longer collapses")
	}
}

func TestProjectMenuAndOrder(t *testing.T) {
	a := projectOrderFixture()
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.RightClick("展开项目 /alpha"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Menu(); !slices.Equal(got, []string{"在Finder中打开", "移除项目"}) {
		t.Fatalf("project context menu = %v", got)
	}
	tt.Key(0, native.KeyEscape)
	for _, label := range []string{"展开项目 /alpha", "展开项目 /beta", "展开仓库 /repo/.git"} {
		if !tt.HasText(label) {
			t.Fatal("project list omitted", label)
		}
	}
	a.moveProject("dir:/beta", "repo:/repo/.git", false)
	tt.Frame()
	assertProjectOrder(t, a, "dir:/beta", "repo:/repo/.git", "dir:/alpha")
	if err := tt.RightClick("展开仓库 /repo/.git"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("移除项目"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("展开仓库 /repo/.git") || tt.HasText("打开工作区 /repo") {
		t.Fatal("removed repository still rendered")
	}
	if len(a.sessions) != 3 || len(a.repositories) != 1 {
		t.Fatal("removal deleted catalog or sessions")
	}
}

func TestProjectFinderReportsMissingClickedDirectory(t *testing.T) {
	for _, target := range []struct {
		label string
		path  string
	}{
		{"展开项目 /alpha", "/alpha"},
		{"打开工作区 /repo", "/repo"},
		{"打开工作区 /feature", "/feature"},
	} {
		t.Run(target.path, func(t *testing.T) {
			desktop := projectOrderFixture()
			desktop.repositories[0].Worktrees = append(desktop.repositories[0].Worktrees, project.Worktree{Path: "/feature", Branch: "feature"})
			tester := native.NewTester(desktop.view, 1000, 760)
			if err := tester.RightClick(target.label); err != nil {
				t.Fatal(err)
			}
			if err := tester.ChooseMenuItem("在Finder中打开"); err != nil {
				t.Fatal(err)
			}
			if !desktop.fileAction.open || desktop.fileAction.path != target.path || !strings.Contains(desktop.fileAction.err, "在Finder中打开失败") {
				t.Fatalf("missing directory error = %+v", desktop.fileAction)
			}
			assertProjectOrder(t, desktop, "repo:/repo/.git", "dir:/alpha", "dir:/beta")
		})
	}
}

func TestProjectRemovalRefreshRestartAndReadd(t *testing.T) {
	repo := projectFixture(t)
	folder := project.Canonical(t.TempDir())
	marker := filepath.Join(folder, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	a := New(nil, "test", repo.Path, nil)
	a.repositories = []project.Repository{repo}
	repoSession := a.session
	repoSession.State.Draft = "repository draft"
	a.newSession(folder)
	folderSession := a.session
	folderSession.State.Draft = "folder draft"
	queue := make(chan func(), 10)
	a.dispatch = func(fn func()) { queue <- fn }
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Save a populated snapshot, then attach through the normal restoration path.
	if err := store.Save(a.snapshot()); err != nil {
		t.Fatal(err)
	}
	a.store = store
	a.moveProject(directoryProjectID(folder), repositoryProjectID(repo.CommonDir), false)
	a.refreshProjects()
	a.removeProject(repositoryProjectID(repo.CommonDir))
	pumpUpdate(t, queue)
	assertProjectOrder(t, a, directoryProjectID(folder))
	a.removeProject(directoryProjectID(folder))
	a.refreshProjects()
	pumpUpdate(t, queue)
	assertProjectOrder(t, a)
	if a.session != folderSession || folderSession.State.Draft != "folder draft" || repoSession.State.Draft != "repository draft" || !folderSession.open || !repoSession.open {
		t.Fatal("removal changed active session, tabs or drafts")
	}
	for _, path := range []string{repo.Path, repo.Worktrees[1].Path, marker} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("removal changed disk", err)
		}
	}
	b := New(nil, "test", "", nil)
	if err := b.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	assertProjectOrder(t, b)
	b.dispatch = func(fn func()) { queue <- fn }
	b.refreshProjects()
	pumpUpdate(t, queue)
	assertProjectOrder(t, b)
	b.addProject(repo.Worktrees[1].Path)
	pumpUpdate(t, queue)
	b.addProject(folder)
	pumpUpdate(t, queue)
	assertProjectOrder(t, b, repositoryProjectID(repo.CommonDir), directoryProjectID(folder))
	if len(b.sessions) != 2 || b.State.Draft != "folder draft" {
		t.Fatal("explicit re-add did not reuse retained sessions")
	}
	c := New(nil, "test", "", nil)
	if err := c.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	assertProjectOrder(t, c, repositoryProjectID(repo.CommonDir), directoryProjectID(folder))
}

func TestRemovedFolderPromotionDuringRefresh(t *testing.T) {
	repo := projectFixture(t)
	path := repo.Worktrees[1].Path
	a := New(nil, "test", path, nil)
	a.newSession(project.Canonical(t.TempDir()))
	other := directoryProjectID(a.State.Cwd)
	a.projectOrder = []string{directoryProjectID(path), other}
	queue := make(chan func(), 10)
	a.dispatch = func(fn func()) { queue <- fn }
	a.refreshProjects()
	a.removeProject(directoryProjectID(path))
	pumpUpdate(t, queue)
	assertProjectOrder(t, a, other)
	if !a.hiddenProjects[repositoryProjectID(repo.CommonDir)] {
		t.Fatal("in-flight folder discovery resurrected removed project")
	}
	a.addProject(path)
	pumpUpdate(t, queue)
	assertProjectOrder(t, a, other, repositoryProjectID(repo.CommonDir))
}

func TestDirectoryPromotionPreservesOrder(t *testing.T) {
	repo := projectFixture(t)
	a := New(nil, "test", repo.Path, nil)
	a.newSession(project.Canonical(t.TempDir()))
	other := directoryProjectID(a.State.Cwd)
	a.projectOrder = []string{directoryProjectID(repo.Path), other}
	queue := make(chan func(), 10)
	a.dispatch = func(fn func()) { queue <- fn }
	a.refreshProjects()
	pumpUpdate(t, queue)
	assertProjectOrder(t, a, repositoryProjectID(repo.CommonDir), other)
}

func TestProjectHeadingUsesOneHoverBackground(t *testing.T) {
	for _, pair := range [][2]string{{"展开仓库 /repo/.git", "项目操作 repo:/repo/.git"}, {"展开项目 /alpha", "项目操作 dir:/alpha"}} {
		t.Run(pair[0], func(t *testing.T) {
			a := projectOrderFixture()
			tt := native.NewTester(a.view, 1000, 760)
			title, ok := tt.Find(pair[0])
			if !ok {
				t.Fatal("missing heading")
			}
			if _, ok := tt.Find(pair[1]); ok {
				t.Fatal("project options button still rendered")
			}
			sampleTitleX, sampleMenuX := title.X+5, title.X+title.W-3
			y := title.Y + title.H/2
			before := tt.Image().RGBAAt(int(sampleMenuX), int(y))
			for _, x := range []float32{title.X + 40, title.X + title.W - 8} {
				tt.Move(x, y)
				image := tt.Image()
				left, right := image.RGBAAt(int(sampleTitleX), int(y)), image.RGBAAt(int(sampleMenuX), int(y))
				if left != right || right == before {
					t.Fatalf("heading does not share hover across its width: %v / %v, idle %v", left, right, before)
				}
				tt.Press(x, y)
				if got := tt.Image().RGBAAt(int(sampleMenuX), int(y)); got != right {
					t.Fatal("press split the shared background")
				}
				tt.Key(0, native.KeyEscape)
				tt.Release(x, y)
			}
			if err := tt.RightClick(pair[0]); err != nil {
				t.Fatal(err)
			}
			want := []string{"新建 Worktree", "移除项目"}
			if pair[0] == "展开项目 /alpha" {
				want = []string{"在Finder中打开", "移除项目"}
			}
			if got := tt.Menu(); !slices.Equal(got, want) {
				t.Fatalf("project context menu = %v", got)
			}
		})
	}
}
