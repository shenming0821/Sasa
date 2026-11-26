package ui

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
)

func catalogFixtureBranches(root, linked string) []gitview.Branch {
	return []gitview.Branch{
		{Name: "main", Ref: "refs/heads/main", Worktree: root},
		{Name: "feature/chat", Ref: "refs/heads/feature/chat", Worktree: linked},
		{Name: "origin/main", Ref: "refs/remotes/origin/main", Remote: true},
	}
}

func TestGitBranchCatalogSharedByPanelWorktreesAndCreateDialog(t *testing.T) {
	a, queue, repo := worktreeCreateFixture(t)
	main := a.workspace(repo.Path)
	linked := a.workspace(repo.Worktrees[1].Path)
	linked.git = gitview.Status{Branch: "feature/chat", Head: repo.Worktrees[1].Head}
	var calls atomic.Int32
	release := make(chan struct{})
	a.readBranches = func(ctx context.Context, root string) ([]gitview.Branch, error) {
		calls.Add(1)
		select {
		case <-release:
			return catalogFixtureBranches(repo.Path, linked.root), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	a.refreshGit(main)
	a.beginWorktreeCreate(repo)
	a.refreshBranchCatalog(linked.root)
	tester := native.NewTester(a.worktreeCreateView, 1000, 760)
	p4Wait(t, tester, queue, func() bool { return calls.Load() == 1 })
	close(release)
	p4Wait(t, tester, queue, func() bool { return !main.gitLoading && !a.worktreeCreate.loading })
	if calls.Load() != 1 || a.branchCatalog(main.root) != a.branchCatalog(linked.root) {
		t.Fatal("panel, dialog and worktrees did not share a single request", calls.Load())
	}
	catalog := a.branchCatalog(main.root)
	if len(catalog.branches) != 3 || len(a.worktreeCreate.sources) != 3 {
		t.Fatal("local and remote projections were mixed")
	}
	for _, state := range []*workspacePage{main, linked} {
		choices, selected := gitBranchChoices(catalog.branches, state.git.Branch, "", true)
		if selected != 1 || choices[selected].branch.Name != state.git.Branch {
			t.Fatal("current branch leaked between worktrees", state.root, state.git.Branch, choices)
		}
	}
	a.beginWorktreeCreate(repo)
	if a.worktreeCreate.loading || len(a.worktreeCreate.sources) != 3 {
		t.Fatal("warm dialog did not display cache immediately")
	}
	a.refreshGit(main)
	p4Wait(t, tester, queue, func() bool { return !main.gitLoading })
	if calls.Load() != 1 {
		t.Fatal("ordinary status refresh reloaded branch catalog", calls.Load())
	}
	if a.branchMetadataChanged(main.root, map[string]bool{filepath.Join(main.root, "README.md"): true, filepath.Join(repo.CommonDir, "index"): true}) {
		t.Fatal("file or staging change invalidated shared branches")
	}
	if !a.branchMetadataChanged(linked.root, map[string]bool{filepath.Join(repo.CommonDir, "refs", "heads", "main"): true}) {
		t.Fatal("reference change was missed")
	}
}

func TestGitBranchCatalogDiscardsReadInvalidatedDuringFlight(t *testing.T) {
	a, queue, repo := worktreeCreateFixture(t)
	var calls atomic.Int32
	releases := []chan struct{}{make(chan struct{}), make(chan struct{})}
	a.readBranches = func(ctx context.Context, root string) ([]gitview.Branch, error) {
		index := calls.Add(1) - 1
		select {
		case <-releases[index]:
			return []gitview.Branch{{Name: "new", Ref: "refs/heads/new"}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	a.beginWorktreeCreate(repo)
	tester := native.NewTester(a.worktreeCreateView, 1000, 760)
	p4Wait(t, tester, queue, func() bool { return calls.Load() == 1 })
	a.invalidateBranchCatalog(repo.Path)
	a.refreshBranchCatalog(repo.Path)
	close(releases[0])
	p4Wait(t, tester, queue, func() bool { return calls.Load() == 2 })
	if !a.worktreeCreate.loading || len(a.worktreeCreate.sources) != 0 {
		t.Fatal("invalidated in-flight result was published")
	}
	close(releases[1])
	p4Wait(t, tester, queue, func() bool { return !a.worktreeCreate.loading })
	if calls.Load() != 2 || a.worktreeCreate.sources[0].Ref != "refs/heads/new" {
		t.Fatal("new branch snapshot was not published")
	}
}

func TestGitBranchCatalogFailureKeepsCacheAndDraft(t *testing.T) {
	a, queue, repo := worktreeCreateFixture(t)
	var calls atomic.Int32
	a.readBranches = func(ctx context.Context, root string) ([]gitview.Branch, error) {
		if calls.Add(1) == 2 {
			return nil, errors.New("controlled read failure")
		}
		return catalogFixtureBranches(repo.Path, repo.Worktrees[1].Path), nil
	}
	a.beginWorktreeCreate(repo)
	tester := native.NewTester(a.worktreeCreateView, 1000, 760)
	p4Wait(t, tester, queue, func() bool { return !a.worktreeCreate.loading })
	draft := a.worktreeCreate
	draft.base, draft.branch, draft.folder, draft.query = "refs/remotes/origin/main", "keep-branch", "keep-folder", "origin"
	draft.sourceRows()
	a.invalidateBranchCatalog(repo.Path)
	a.refreshBranchCatalog(repo.Path)
	p4Wait(t, tester, queue, func() bool { return !a.branchCatalog(repo.Path).loading })
	if draft.loading || len(draft.sources) != 3 || draft.errors["base"] == "" || draft.base != "refs/remotes/origin/main" || draft.branch != "keep-branch" || draft.folder != "keep-folder" || draft.query != "origin" {
		t.Fatal("refresh failure cleared usable data or draft", draft)
	}
	if err := tester.Click("重试读取分支"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tester, queue, func() bool { return !a.branchCatalog(repo.Path).loading })
	if draft.errors["base"] != "" || len(draft.sourceRows()) != 1 {
		t.Fatal("retry did not clear error or refresh filtered snapshot", draft.errors)
	}
}

func TestGitBranchCatalogInitialFailureCanReopenAndRetry(t *testing.T) {
	a, queue, repo := worktreeCreateFixture(t)
	a.readBranches = func(context.Context, string) ([]gitview.Branch, error) { return nil, errors.New("unavailable") }
	a.beginWorktreeCreate(repo)
	tester := native.NewTester(a.worktreeCreateView, 1000, 760)
	p4Wait(t, tester, queue, func() bool { return !a.worktreeCreate.loading })
	a.beginWorktreeCreate(repo)
	if a.worktreeCreate.loading || a.worktreeCreate.errors["base"] == "" {
		t.Fatal("cached failure left reopened dialog permanently loading")
	}
}

func TestGitBranchCatalogPromotesPreDiscoveryCacheAndPreservesSearch(t *testing.T) {
	a, queue, repo := worktreeCreateFixture(t)
	a.repositories = nil
	var calls atomic.Int32
	a.readBranches = func(context.Context, string) ([]gitview.Branch, error) {
		calls.Add(1)
		return catalogFixtureBranches(repo.Path, repo.Worktrees[1].Path), nil
	}
	a.refreshBranchCatalog(repo.Path)
	tester := native.NewTester(a.worktreeCreateView, 1000, 760)
	p4Wait(t, tester, queue, func() bool { return a.branchCatalog(repo.Path).loaded })
	previous := a.branchCatalog(repo.Path)
	a.repositories = append(a.repositories, repo)
	a.beginWorktreeCreate(repo)
	draft := a.worktreeCreate
	if a.branchCatalog(repo.Path) != previous || draft.loading || calls.Load() != 1 {
		t.Fatal("repository discovery dropped the Git panel cache")
	}
	draft.query = "origin"
	filtered := draft.sourceRows()
	a.refreshBranchCatalog(repo.Path)
	if &filtered[0] != &draft.sourceRows()[0] {
		t.Fatal("cache reuse reset the active search")
	}
	previous.checked = time.Now().Add(-31 * time.Second)
	a.refreshBranchCatalog(repo.Path)
	if draft.loading || draft.query != "origin" {
		t.Fatal("background refresh blocked or changed the dialog")
	}
	p4Wait(t, tester, queue, func() bool { return !previous.loading })
	if calls.Load() != 2 || &filtered[0] != &draft.sourceRows()[0] {
		t.Fatal("unchanged background result reset the active search", calls.Load())
	}
}

func TestGitBranchCatalogRefreshesAfterGitPanelBranchCreation(t *testing.T) {
	a, queue, repo := worktreeCreateFixture(t)
	state := a.workspace(repo.Path)
	var calls atomic.Int32
	a.readBranches = func(ctx context.Context, root string) ([]gitview.Branch, error) {
		calls.Add(1)
		return gitview.RepositoryBranches(ctx, root)
	}
	a.refreshGit(state)
	tester := native.NewTester(a.worktreeCreateView, 1000, 760)
	p4Wait(t, tester, queue, func() bool { return !state.gitLoading && a.branchCatalog(repo.Path).loaded })
	a.gitAction(state, "新建分支", func(ctx context.Context) error {
		return gitview.CreateBranch(ctx, state.root, "from-panel", "main")
	}, nil)
	p4Wait(t, tester, queue, func() bool {
		return state.gitBusy == "" && !state.gitLoading && state.git.Branch == "from-panel" && len(a.branchCatalog(state.root).branches) == 3
	})
	a.beginWorktreeCreate(repo)
	if a.worktreeCreate.loading || a.worktreeCreate.base != "refs/heads/from-panel" || len(a.worktreeCreate.sources) != 3 || calls.Load() != 2 {
		t.Fatal("Git panel branch operation did not refresh shared snapshot", a.worktreeCreate, calls.Load())
	}
}
