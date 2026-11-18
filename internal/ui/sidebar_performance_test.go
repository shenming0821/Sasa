package ui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
	"github.com/shenming0821/Sasa/internal/project"
	"github.com/shenming0821/Sasa/internal/storage"
)

func sidebarLoadFixture(worktrees, sessions int) *App {
	a := New(nil, "test", "/project-0", nil)
	a.sessions = nil
	a.collapsedProjects = make(map[string]bool)
	for r := 0; r < 7; r++ {
		root := fmt.Sprintf("/project-%d", r)
		repo := project.Repository{Path: root, CommonDir: root + "/.git", Name: fmt.Sprintf("project-%d", r)}
		for w := 0; w < worktrees; w++ {
			path := fmt.Sprintf("%s/worktree-%d", root, w)
			repo.Worktrees = append(repo.Worktrees, project.Worktree{Path: path, Branch: fmt.Sprintf("feature/%d", w)})
		}
		a.repositories = append(a.repositories, repo)
		a.collapsedProjects[repo.CommonDir] = r != 0
	}
	for i := 0; i < sessions; i++ {
		w := a.repositories[i%len(a.repositories)].Worktrees[(i/len(a.repositories))%worktrees]
		a.sessions = append(a.sessions, &session{
			id: i + 1, kind: agent.Codex,
			State: app.State{Cwd: w.Path + "/src"},
			saved: storage.Session{Title: fmt.Sprintf("历史任务 %d", i), ThreadID: fmt.Sprintf("thread-%d", i)},
		})
	}
	return a
}

func BenchmarkSidebarDisclosure(b *testing.B) {
	for _, count := range []int{10, 100} {
		b.Run(fmt.Sprintf("Worktrees%d", count), func(b *testing.B) {
			a := sidebarLoadFixture(count, 258)
			tt := native.NewTester(a.sidebar, 260, 760)
			frame := 0
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if frame%24 == 0 {
					key := a.repositories[0].CommonDir
					a.collapsedProjects[key] = !a.collapsedProjects[key]
					tt.Frame()
				}
				tt.Advance(time.Second / 60)
				frame++
			}
		})
	}
}

func TestProjectFrameMatchesUncachedOwnership(t *testing.T) {
	a := New(nil, "test", "", nil)
	a.repositories = []project.Repository{
		{Worktrees: []project.Worktree{{Path: "/repo"}, {Path: "/repo/nested"}, {Path: "/other"}}},
		{Worktrees: []project.Worktree{{Path: "/repo"}}},
	}
	paths := []string{"", "/repo", "/repo/src/a", "/repo/nested/src", "/repo/nested/../src", "/repo-other/src", "/other", "/missing", "repo"}
	want := make([]*project.Worktree, len(paths))
	for i, path := range paths {
		want[i] = a.worktreeFor(path)
	}
	end := a.beginProjectFrame()
	for repeat := 0; repeat < 2; repeat++ {
		for i, path := range paths {
			if got := a.worktreeFor(path); got != want[i] {
				t.Fatalf("owner of %q = %v, want %v", path, got, want[i])
			}
		}
	}
	end()
	if a.projectFrame != nil {
		t.Fatal("sidebar index escaped its frame")
	}
	a.repositories[0].Worktrees = append(a.repositories[0].Worktrees, project.Worktree{Path: "/"})
	defer a.beginProjectFrame()()
	if owner := a.worktreeFor("/missing"); owner == nil || owner.Path != "/" {
		t.Fatal("next frame did not pick up a repository change")
	}
}

func TestProjectFramePicksUpSessionChanges(t *testing.T) {
	a := sidebarLoadFixture(10, 258)
	w := a.repositories[0].Worktrees[0]
	want := a.worktreeSessions(w)
	end := a.beginProjectFrame()
	if got := a.worktreeSessions(w); !slices.Equal(got, want) {
		t.Fatal("indexed sessions changed newest-first order or ownership")
	}
	end()
	moved := want[0]
	moved.State.Cwd = "/untracked"
	latest := &session{id: 1000, State: app.State{Cwd: w.Path}}
	a.sessions = append(a.sessions, latest)
	defer a.beginProjectFrame()()
	got := a.worktreeSessions(w)
	if len(got) == 0 || got[0] != latest || slices.Contains(got, moved) {
		t.Fatal("next frame did not pick up a moved or newly created session")
	}
}
