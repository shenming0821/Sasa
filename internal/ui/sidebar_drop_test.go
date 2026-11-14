package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/project"
)

func sidebarDropTester(t *testing.T, a *App) (*native.Tester, chan func()) {
	t.Helper()
	queue := make(chan func(), 10)
	a.dispatch = func(fn func()) { queue <- fn }
	t.Cleanup(func() {
		for a.projectsLoading {
			pumpUpdate(t, queue)
		}
		close(a.shutdown)
		a.closeWorkspaceResources()
	})
	return native.NewTester(a.view, 1000, 760), queue
}

func dropOnSidebar(t *testing.T, tt *native.Tester, label string, paths ...string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok || r.W <= 0 || r.H <= 0 {
		t.Fatalf("missing drop target %q: %+v", label, r)
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	if label == "项目列表" {
		// The bottom of this viewport is empty, below the fixture's rows.
		y = r.Y + r.H - 8
	}
	if !tt.DropFiles(x, y, paths...) {
		t.Fatalf("file drop not accepted on %q", label)
	}
}

func TestSidebarDropGitProject(t *testing.T) {
	repo := projectFixture(t)
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		for _, label := range []string{"项目列表", "打开工作区 /repo", "展开项目 /alpha"} {
			t.Run(string(kind)+"/"+label, func(t *testing.T) {
				a := projectOrderFixture()
				if kind == agent.Claude {
					a.newSessionForAgent("/beta", kind, "")
				}
				selected, workspace, tab := a.session, a.activeWorkspace, a.activeTab
				selected.State.Draft = "保留当前草稿"
				order := slices.Clone(a.orderedProjectIDs())
				sessions := len(a.sessions)
				tt, queue := sidebarDropTester(t, a)
				dropOnSidebar(t, tt, label, repo.Path)
				if !a.projectsLoading || len(a.repositories) != 1 {
					t.Fatal("drop did not reserve import before dispatching the result")
				}
				pumpUpdate(t, queue)
				tt.Frame()
				if a.projectsLoading || a.projectsError != "" || len(a.repositories) != 2 ||
					a.repositories[1].CommonDir != repo.CommonDir {
					t.Fatalf("Git project not imported: %v, %s", a.repositories, a.projectsError)
				}
				if a.session != selected || a.activeWorkspace != workspace || a.activeTab != tab ||
					len(a.sessions) != sessions || selected.State.Draft != "保留当前草稿" {
					t.Fatal("external drop activated the underlying row or changed the selected session")
				}
				assertProjectOrder(t, a, append(order, repositoryProjectID(repo.CommonDir))...)
				if len(a.collapsedProjects) != 0 || a.paneTabDragging {
					t.Fatal("external file drop triggered an internal drag or disclosure")
				}
			})
		}
	}
}

func TestSidebarDropPlainDirectory(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/initial", nil)
			a.defaultAgent = kind
			if kind == agent.Claude {
				a.newSessionForAgent("/initial", kind, "")
			}
			folder := project.Canonical(t.TempDir())
			tt, queue := sidebarDropTester(t, a)
			dropOnSidebar(t, tt, "项目列表", folder)
			pumpUpdate(t, queue)
			tt.Frame()
			if a.projectsLoading || a.projectsError != "" || len(a.repositories) != 0 ||
				a.State.Cwd != folder || a.session.kind != kind {
				t.Fatalf("plain directory did not use the existing folder import: %s, %s", a.State.Cwd, a.projectsError)
			}
			selected, count := a.session, len(a.sessions)
			selected.State.Draft = "目录草稿"
			dropOnSidebar(t, tt, "项目列表", folder)
			pumpUpdate(t, queue)
			if a.session != selected || len(a.sessions) != count || a.State.Draft != "目录草稿" {
				t.Fatal("duplicate folder drop lost the existing session or its draft")
			}
		})
	}
}

func TestSidebarDropFilesWithoutDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][]string{{file}, {file, file + ".missing"}} {
		t.Run(strings.Join(paths, ","), func(t *testing.T) {
			a := projectOrderFixture()
			selected, workspace := a.session, a.activeWorkspace
			order := slices.Clone(a.orderedProjectIDs())
			tt, queue := sidebarDropTester(t, a)
			dropOnSidebar(t, tt, "打开工作区 /repo", paths...)
			pumpUpdate(t, queue)
			tt.Frame()
			if a.projectsLoading || !strings.Contains(a.projectsError, "没有可添加的文件夹") ||
				!tt.HasText(a.projectsError) {
				t.Fatalf("missing visible invalid-drop feedback: %q", a.projectsError)
			}
			if a.session != selected || a.activeWorkspace != workspace || len(a.repositories) != 1 {
				t.Fatal("invalid drop changed selection or repositories")
			}
			assertProjectOrder(t, a, order...)
		})
	}
}

func TestSidebarDropFirstDirectoryOnly(t *testing.T) {
	first := projectFixture(t)
	second := projectFixture(t)
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, plainFirst := range []bool{false, true} {
		name := "git"
		firstPath := first.Path
		if plainFirst {
			name, firstPath = "plain", project.Canonical(t.TempDir())
		}
		t.Run(name, func(t *testing.T) {
			a := New(stubBackend{}, "test", "/initial", nil)
			tt, queue := sidebarDropTester(t, a)
			dropOnSidebar(t, tt, "项目列表", file+".missing", file, firstPath, second.Path)
			pumpUpdate(t, queue)
			if a.projectsLoading || a.projectsError != "" {
				t.Fatalf("directory selection failed: %s", a.projectsError)
			}
			if plainFirst {
				if len(a.repositories) != 0 || a.State.Cwd != firstPath {
					t.Fatal("plain first directory was skipped or second directory was imported")
				}
			} else if len(a.repositories) != 1 || a.repositories[0].CommonDir != first.CommonDir {
				t.Fatal("drop did not import exactly the first directory")
			}
		})
	}
}

func TestSidebarDropWhileProjectImportBusy(t *testing.T) {
	first := projectFixture(t)
	second := projectFixture(t)
	a := New(stubBackend{}, "test", "/initial", nil)
	selected := a.session
	tt, queue := sidebarDropTester(t, a)
	a.addProject(first.Path)
	a.projectsError = "保留已有提示"
	dropOnSidebar(t, tt, "项目列表", second.Path)
	if !a.projectsLoading || a.projectsError != "保留已有提示" {
		t.Fatal("busy drop reset the in-flight import state")
	}
	pumpUpdate(t, queue)
	if a.projectsLoading || len(a.repositories) != 1 || a.repositories[0].CommonDir != first.CommonDir ||
		a.session != selected || len(queue) != 0 {
		t.Fatal("busy drop replaced or duplicated the original import")
	}
	// A later drop must work after the first import completes.
	dropOnSidebar(t, tt, "项目列表", second.Path)
	pumpUpdate(t, queue)
	if a.projectsLoading || a.projectsError != "" || len(a.repositories) != 2 {
		t.Fatal("sidebar did not accept another project after becoming idle")
	}
}

func TestSidebarDropPreservesGitErrors(t *testing.T) {
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, ".git"), []byte("invalid gitfile\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := New(stubBackend{}, "test", "/initial", nil)
	selected, count := a.session, len(a.sessions)
	tt, queue := sidebarDropTester(t, a)
	dropOnSidebar(t, tt, "项目列表", broken, t.TempDir())
	pumpUpdate(t, queue)
	if a.projectsLoading || a.projectsError == "" || len(a.repositories) != 0 ||
		a.session != selected || len(a.sessions) != count {
		t.Fatal("broken Git directory was downgraded to a folder or skipped")
	}
}
