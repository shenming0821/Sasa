package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Sasa/internal/git"
	"github.com/shenming0821/Sasa/internal/project"
	"github.com/shenming0821/Sasa/internal/storage"
)

func worktreePickerFixture(count int) *App {
	a := New(stubBackend{}, "test", "/repo", nil)
	draft := &worktreeCreation{picker: true, errors: make(map[string]string)}
	for index := range count {
		name := fmt.Sprintf("feature/branch-%05d", index)
		draft.sources = append(draft.sources, gitview.WorktreeSource{Ref: "refs/heads/" + name, Label: name})
	}
	draft.base = draft.sources[0].Ref
	a.worktreeCreate = draft
	return a
}

func BenchmarkWorktreeSourcePicker(b *testing.B) {
	for _, count := range []int{100, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			a := worktreePickerFixture(count)
			tester := native.NewTester(a.worktreeCreateView, 1000, 760)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				tester.Frame()
			}
		})
	}
}

func TestWorktreeSourcePickerLargeRepositoryScrollSearchAndSelection(t *testing.T) {
	a := worktreePickerFixture(10000)
	draft := a.worktreeCreate
	tester := native.NewTester(a.worktreeCreateView, 1000, 760)
	if _, found := tester.Find("Worktree 来源 feature/branch-09999"); found {
		t.Fatal("offscreen branches were built eagerly")
	}
	rows := draft.sourceRows()
	if len(rows) != 10000 || &rows[0] != &draft.sourceRows()[0] {
		t.Fatal("unchanged search did not reuse its results")
	}
	draft.sourceList.ScrollTo(9999, native.End)
	tester.Frame()
	if _, found := tester.Find("Worktree 来源 feature/branch-09999"); !found {
		t.Fatal("cannot reach the last branch")
	}
	if err := tester.Click("搜索基础分支"); err != nil {
		t.Fatal(err)
	}
	tester.Type("branch-09998")
	tester.Frame()
	if err := tester.Click("Worktree 来源 feature/branch-09998"); err != nil {
		t.Fatal("search did not reveal distant branch", err)
	}
	if draft.base != "refs/heads/feature/branch-09998" || draft.picker {
		t.Fatal("selected the wrong branch", draft.base)
	}
}

func worktreeCreateFixture(t *testing.T) (*App, chan func(), project.Repository) {
	t.Helper()
	repo := projectFixture(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	a := New(stubBackend{}, "test", repo.Path, nil)
	a.store = store
	a.repositories = []project.Repository{repo}
	queue := make(chan func(), 50)
	a.dispatch = func(fn func()) { queue <- fn }
	t.Cleanup(func() { close(a.shutdown); a.closeWorkspaceResources() })
	return a, queue, repo
}

func TestWorktreeCreationMenuFormAndNavigation(t *testing.T) {
	a, queue, repo := worktreeCreateFixture(t)
	original := a.session
	original.State.Draft = "keep draft"
	tester := native.NewTester(a.view, 1000, 760)
	if err := tester.RightClick("展开仓库 " + repo.CommonDir); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tester.Menu(), []string{"新建 Worktree", "移除项目"}) {
		t.Fatal(tester.Menu())
	}
	if err := tester.ChooseMenuItem("新建 Worktree"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tester, queue, func() bool { return a.worktreeCreate != nil && !a.worktreeCreate.loading })
	draft := a.worktreeCreate
	if draft.base != "refs/heads/main" {
		t.Fatal("wrong default base", draft.base)
	}
	if err := tester.Click("Worktree 基础分支"); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("搜索基础分支"); err != nil {
		t.Fatal(err)
	}
	tester.Type("chat")
	tester.Frame()
	if _, found := tester.Find("Worktree 来源 main"); found {
		t.Fatal("base search did not filter nonmatching branches")
	}
	if err := tester.Click("Worktree 来源 feature/chat"); err != nil {
		t.Fatal(err)
	}
	if draft.base != "refs/heads/feature/chat" || draft.picker {
		t.Fatal("base selection did not commit")
	}
	typeField := func(label, text string) {
		t.Helper()
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
		tester.Key(native.Cmd, native.KeyA)
		tester.Type(text)
		tester.Frame()
	}
	typeField("Worktree 新分支名", "feature/login")
	if draft.folder != "feature-login" {
		t.Fatal("folder did not follow branch", draft.folder)
	}
	typeField("Worktree 文件夹名称", "自定义 空格")
	typeField("Worktree 新分支名", "feature/second")
	if draft.folder != "自定义 空格" {
		t.Fatal("custom folder was replaced", draft.folder)
	}
	if err := tester.Click("复制完整路径"); err != nil || tester.Clipboard() != draft.path() {
		t.Fatal("path copy failed", err, tester.Clipboard())
	}
	for _, dark := range []bool{false, true} {
		tester.SetDark(dark)
		for _, label := range []string{"Worktree 基础分支", "Worktree 新分支名", "Worktree 文件夹名称", "创建"} {
			bounds, ok := tester.Find(label)
			if !ok || bounds.W <= 0 || bounds.X < 0 || bounds.X+bounds.W > 1000 {
				t.Fatal("form control outside modal", dark, label, bounds)
			}
		}
	}
	if err := tester.Click("创建"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tester, queue, func() bool { return a.worktreeCreate == nil || !draft.busy })
	if a.worktreeCreate != nil {
		t.Fatal("creation failed", draft.errors)
	}
	if a.activeWorkspace != draft.target || a.session != original || len(a.sessions) != 1 || original.State.Draft != "keep draft" || !tester.HasText("选择面板内容") {
		t.Fatal("creation did not open the new worktree picker", a.activeWorkspace, len(a.sessions))
	}
	rediscovered, err := project.Discover(context.Background(), repo.Path)
	if err != nil || len(rediscovered.Worktrees) != len(repo.Worktrees)+1 {
		t.Fatal("new worktree not rediscovered", rediscovered, err)
	}
}

func TestWorktreeCreationFolderResetValidationAndBusyClose(t *testing.T) {
	a, queue, repo := worktreeCreateFixture(t)
	a.openWorktree(repo.Worktrees[1])
	a.beginWorktreeCreate(repo)
	tester := native.NewTester(a.view, 1000, 760)
	p4Wait(t, tester, queue, func() bool { return !a.worktreeCreate.loading })
	draft := a.worktreeCreate
	if draft.base != "refs/heads/feature/chat" {
		t.Fatal("active linked worktree was not used", draft.base)
	}
	draft.branch, draft.folder, draft.customFolder = "feature/new", "custom", true
	if err := tester.Click("Worktree 文件夹名称"); err != nil {
		t.Fatal(err)
	}
	tester.Key(native.Cmd, native.KeyA)
	tester.Key(0, native.KeyBackspace)
	tester.Frame()
	if draft.folder != "feature-new" || draft.customFolder {
		t.Fatal("empty folder did not restore follow", draft.folder)
	}
	draft.folder, draft.customFolder = "../escape", true
	a.createWorktree(draft)
	if draft.errors["folder"] == "" || draft.busy {
		t.Fatal("invalid folder accepted", draft.errors)
	}
	draft.folder = "existing"
	if err := os.MkdirAll(draft.path(), 0700); err != nil {
		t.Fatal(err)
	}
	a.createWorktree(draft)
	p4Wait(t, tester, queue, func() bool { return !draft.busy })
	if draft.errors["folder"] == "" || draft.branch != "feature/new" || draft.folder != "existing" {
		t.Fatal("conflict lost form input", draft.errors)
	}
	draft.busy = true
	a.closeCurrentPage()
	if a.worktreeCreate != draft || !a.hasGitWrites() {
		t.Fatal("busy operation lost exit protection")
	}
	draft.busy = false
	a.closeCurrentPage()
	if a.worktreeCreate != nil {
		t.Fatal("idle modal did not close")
	}
}

func TestWorktreeCreationRefreshFailureRetriesOnlyDiscovery(t *testing.T) {
	a, queue, repo := worktreeCreateFixture(t)
	draft := &worktreeCreation{repo: repo, root: repo.Path, dataDir: a.store.Directory(), branch: "refresh-test", folder: "refresh-test", base: "refs/heads/main", errors: make(map[string]string)}
	draft.target = draft.path()
	result, err := gitview.CreateWorktree(context.Background(), repo.Path, draft.base, draft.branch, draft.target)
	if err != nil || !result.Created {
		t.Fatal(result, err)
	}
	draft.created = true
	a.worktreeCreate = draft
	gitFile := filepath.Join(draft.target, ".git")
	if err := os.Rename(gitFile, gitFile+".held"); err != nil {
		t.Fatal(err)
	}
	a.finishWorktreeCreate(draft)
	tester := native.NewTester(a.view, 1000, 760)
	p4Wait(t, tester, queue, func() bool { return !draft.busy })
	if !strings.Contains(draft.errors["action"], "已创建，列表刷新失败") {
		t.Fatal(draft.errors)
	}
	if err := os.Rename(gitFile+".held", gitFile); err != nil {
		t.Fatal(err)
	}
	if err := tester.Click("重试刷新"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tester, queue, func() bool { return a.worktreeCreate == nil })
	if a.activeWorkspace != draft.target || len(a.sessions) != 1 || !tester.HasText("选择面板内容") {
		t.Fatal("retry did not open the created worktree picker")
	}
}
