package ui

import (
	"context"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/project"
	"github.com/shenming0821/Sasa/internal/storage"
)

func projectFixture(t *testing.T) project.Repository {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "repository")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, raw)
		}
	}
	run("init", "-b", "main")
	run("-c", "core.hooksPath=/dev/null", "commit", "--allow-empty", "-m", "fixture")
	run("worktree", "add", "-b", "feature/chat", filepath.Join(base, "chat-worktree"))
	repo, err := project.Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestRepositoryImportGroupsBranchesAndSessions(t *testing.T) {
	repo := projectFixture(t)
	a := New(stubBackend{}, "test", repo.Path, nil)
	first := a.session
	first.State.Draft = "main draft"
	queue := make(chan func(), 10)
	a.dispatch = func(fn func()) { queue <- fn }
	a.addProject(repo.Worktrees[1].Path)
	pumpUpdate(t, queue)
	if len(a.repositories) != 1 || len(a.sessions) != 1 {
		t.Fatal("import did not discover repository or created all sessions eagerly")
	}
	a.addProject(repo.Path)
	pumpUpdate(t, queue)
	if len(a.repositories) != 1 {
		t.Fatal("duplicate repository imported through linked worktree")
	}
	tt := native.NewTester(a.view, 1000, 760)
	if !tt.HasText("main") || !tt.HasText("feature/chat") || !tt.HasText("主要") {
		t.Fatal("repository branch hierarchy missing")
	}
	if err := tt.Click("打开工作区 " + repo.Worktrees[1].Path); err != nil {
		t.Fatal(err)
	}
	if a.session != first || a.activeWorkspace != repo.Worktrees[1].Path || !tt.HasText("选择面板内容") {
		t.Fatal("worktree click did not show the picker")
	}
	if err := tt.Click("Codex"); err != nil {
		t.Fatal(err)
	}
	second := a.session
	if second == first || a.State.Cwd != repo.Worktrees[1].Path {
		t.Fatal("worktree selection did not bind session cwd")
	}
	second.State.Draft = "feature draft"
	if err := tt.Click("打开工作区 " + repo.Path); err != nil {
		t.Fatal(err)
	}
	if a.session != first || a.State.Draft != "main draft" {
		t.Fatal("switching worktree lost independent draft")
	}
	if err := tt.Click("打开工作区 " + repo.Worktrees[1].Path); err != nil {
		t.Fatal(err)
	}
	a.newSession(repo.Worktrees[1].Path)
	tt.Frame()
	if a.session == second || a.State.Cwd != second.State.Cwd || second.State.Draft != "feature draft" {
		t.Fatal("worktree cannot own multiple conversations")
	}
	if len(a.projectGroups()) != 0 {
		t.Fatal("repository sessions duplicated in directory groups")
	}
	for _, w := range repo.Worktrees {
		if !tt.HasText("打开工作区 " + w.Path) {
			t.Fatal("worktree omitted from project tree", w.Path)
		}
	}
}

func TestProjectRefreshPersistenceAndScanFailure(t *testing.T) {
	repo := projectFixture(t)
	a := New(nil, "test", repo.Path, nil)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	queue := make(chan func(), 10)
	a.dispatch = func(fn func()) { queue <- fn }
	a.refreshProjects()
	pumpUpdate(t, queue)
	if len(a.repositories) != 1 {
		t.Fatal("existing directory session did not discover repository")
	}
	b := New(nil, "test", "", nil)
	if err := b.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if len(b.repositories) != 1 || len(b.repositories[0].Worktrees) != 2 {
		t.Fatal("repository catalog not restored")
	}
	linked := repo.Worktrees[1].Path
	cmd := exec.Command("git", "-C", repo.Path, "worktree", "remove", linked)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture remove: %v %s", err, raw)
	}
	a.refreshProjects()
	pumpUpdate(t, queue)
	if len(a.repositories[0].Worktrees) != 1 {
		t.Fatal("refresh did not reflect external worktree removal")
	}
	cmd = exec.Command("git", "-C", repo.Path, "worktree", "add", "-b", "feature/external", linked)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture add: %v %s", err, raw)
	}
	a.refreshProjects()
	pumpUpdate(t, queue)
	if len(a.repositories[0].Worktrees) != 2 || a.repositories[0].Worktrees[1].Branch != "feature/external" {
		t.Fatal("refresh did not discover external worktree creation")
	}
	cmd = exec.Command("git", "-C", repo.Path, "worktree", "remove", linked)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture remove: %v %s", err, raw)
	}
	a.refreshProjects()
	pumpUpdate(t, queue)
	if err := os.Rename(repo.Path, repo.Path+"-moved"); err != nil {
		t.Fatal(err)
	}
	a.refreshProjects()
	pumpUpdate(t, queue)
	if len(a.repositories) != 1 || len(a.repositories[0].Worktrees) != 1 || a.projectErrors[repo.CommonDir] == "" {
		t.Fatal("failed scan erased cached repository instead of showing error")
	}
	a.addProject(filepath.Join(t.TempDir(), "missing"))
	pumpUpdate(t, queue)
	if a.projectsError == "" || len(a.repositories) != 1 {
		t.Fatal("failed import lost catalog or hid error")
	}
}

func TestRepositoryNavigationLayoutAndCollapse(t *testing.T) {
	for _, width := range []int{640, 1000, 1400} {
		a := New(stubBackend{}, "test", "/workspace/sasa", nil)
		a.repositories = []project.Repository{{CommonDir: "/workspace/sasa/.git", Path: "/workspace/sasa", Name: "Sasa", Worktrees: []project.Worktree{
			{Path: "/workspace/sasa", Branch: "main", Main: true},
			{Path: "/workspace/feature-chat", Branch: "feature/chat-navigation"},
		}}}
		a.saved.Title = "优化项目导航"
		a.newSession("/workspace/feature-chat")
		a.saved.Title = "实现独立工作区会话"
		tt := native.NewTester(a.view, width, 760)
		if width == 640 {
			if err := tt.Click("切换左侧边栏"); err != nil {
				t.Fatal(err)
			}
		}
		branch, ok := tt.Find("打开工作区 /workspace/feature-chat")
		if !ok || branch.W <= 0 || tt.HasText("新建工作区会话 /workspace/feature-chat") {
			t.Fatalf("worktree row missing or retains removed action at %d: %+v", width, branch)
		}
		if preview := os.Getenv("SASA_PROJECT_PREVIEW"); preview != "" && width == 1000 {
			file, err := os.Create(preview)
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, tt.Image())
			file.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := tt.Click("展开仓库 /workspace/sasa/.git"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(300 * time.Millisecond)
		if tt.HasText("打开工作区 /workspace/feature-chat") {
			t.Fatal("collapsed repository retained worktree rows")
		}
	}
}

func TestNestedRepositoryOwnershipAndUnavailableWorktrees(t *testing.T) {
	a := New(nil, "test", "/repo/nested/src", nil)
	a.repositories = []project.Repository{
		{CommonDir: "/repo/.git", Path: "/repo", Worktrees: []project.Worktree{{Path: "/repo"}}},
		{CommonDir: "/repo/nested/.git", Path: "/repo/nested", Worktrees: []project.Worktree{{Path: "/repo/nested"}}},
	}
	if owner := a.worktreeFor(a.State.Cwd); owner == nil || owner.Path != "/repo/nested" {
		t.Fatal("parent repository captured nested repository session")
	}
	if a.worktreeFor("/repository-other") != nil {
		t.Fatal("directory-prefix collision assigned unrelated session")
	}
	for _, tree := range []project.Worktree{{Path: "/bare", Bare: true}, {Path: "/missing", Missing: true}, {Path: "/pruned", Prunable: true}} {
		a.openWorktree(tree)
		if len(a.sessions) != 1 {
			t.Fatal("unavailable worktree created session")
		}
	}
}

// Opt-in verification of user-retained local worktrees. Git is only read here;
// optional app data must point to a dedicated test directory.
func TestLiveProjectWorktreesReadOnly(t *testing.T) {
	path := os.Getenv("SASA_PROJECT_TEST_REPO")
	if path == "" {
		t.Skip("set SASA_PROJECT_TEST_REPO to a retained test repository")
	}
	repo, err := project.Discover(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.Worktrees) < 5 {
		t.Fatalf("expected at least five real worktrees, got %d", len(repo.Worktrees))
	}
	data := os.Getenv("SASA_PROJECT_TEST_DATA")
	if data == "" {
		data = t.TempDir()
	}
	store, err := storage.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := New(nil, "worktree-test", repo.Path, nil)
	if err := a.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	queue := make(chan func(), 8)
	a.dispatch = func(fn func()) { queue <- fn }
	a.addProject(repo.Worktrees[len(repo.Worktrees)-1].Path)
	pumpUpdate(t, queue)
	a.addProject(repo.Path)
	pumpUpdate(t, queue)
	if len(a.repositories) != 1 {
		t.Fatal("real worktree import duplicated repository")
	}
	tt := native.NewTester(a.view, 1100, 900)
	drafts := make(map[string]string)
	for _, w := range repo.Worktrees {
		if !w.Available() {
			t.Fatalf("retained fixture worktree unavailable: %+v", w)
		}
		if err := tt.Click("打开工作区 " + w.Path); err != nil {
			t.Fatal(err)
		}
		a.newSession(w.Path)
		if a.State.Cwd != w.Path {
			t.Fatalf("wrong cwd: %s want %s", a.State.Cwd, w.Path)
		}
		drafts[w.Path] = "draft: " + w.Label()
		a.State.Draft = drafts[w.Path]
		t.Logf("worktree=%s branch=%s main=%v locked=%v detached=%v session=%d", w.Path, w.Label(), w.Main, w.Locked, w.Detached, a.session.id)
	}
	for _, w := range repo.Worktrees {
		if err := tt.Click("打开工作区 " + w.Path); err != nil {
			t.Fatal(err)
		}
		if a.State.Draft != drafts[w.Path] {
			t.Fatal("draft leaked between real worktree sessions")
		}
	}
	a.refreshProjects()
	pumpUpdate(t, queue)
	if len(a.repositories[0].Worktrees) != len(repo.Worktrees) {
		t.Fatal("refresh changed real worktree count")
	}
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	b := New(nil, "worktree-test", "", nil)
	if err := b.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if len(b.snapshot().OpenTabs) != len(repo.Worktrees) || len(b.repositories) != 1 {
		t.Fatal("restart lost real worktree tabs/catalog")
	}
	for _, s := range b.sessions {
		if s.State.Draft != drafts[s.State.Cwd] || s.State.ThreadID != "" {
			t.Fatal("restart lost draft or created model task")
		}
	}
	if preview := os.Getenv("SASA_PROJECT_PREVIEW"); preview != "" {
		tt.Frame()
		file, err := os.Create(preview)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, tt.Image())
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorktreeHidesBlankSessionRowsUntilConversationStarts(t *testing.T) {
	a := New(nil, "test", "/repo", nil)
	first := a.session
	a.repositories = []project.Repository{{CommonDir: "/repo/.git", Path: "/repo", Name: "repo", Worktrees: []project.Worktree{
		{Path: "/repo", Main: true, Branch: "main"}, {Path: "/feature", Branch: "feature/chat"},
	}}}
	a.newSession("/feature")
	draft := a.session
	draft.State.Draft = "unsent draft"
	tt := native.NewTester(a.view, 1000, 760)
	if tt.HasText("切换会话 1") || tt.HasText("切换会话 2") {
		t.Fatal("blank or draft-only conversations created placeholder sidebar rows")
	}
	if !tt.HasText("打开工作区 /repo") || !tt.HasText("打开工作区 /feature") || !tt.HasText("会话标签 2") {
		t.Fatal("hiding placeholder removed worktree navigation or draft tab")
	}
	if err := tt.Click("打开工作区 /repo"); err != nil {
		t.Fatal(err)
	}
	if a.session != first || len(a.sessions) != 2 {
		t.Fatal("opening worktree did not reuse hidden blank conversation")
	}
	if err := tt.Click("打开工作区 /feature"); err != nil {
		t.Fatal(err)
	}
	if a.session != draft || a.State.Draft != "unsent draft" {
		t.Fatal("hidden draft became unreachable")
	}
	draft.State.Begin("实现聊天交互")
	tt.Frame()
	if !tt.HasText("打开工作区 /feature") || tt.HasText("切换会话 1") {
		t.Fatal("submitted task did not become a sidebar conversation")
	}
	a.newSession("/feature")
	tt.Frame()
	if tt.HasText("切换会话 3") || !tt.HasText("打开工作区 /feature") {
		t.Fatal("new blank session duplicated placeholder beside real task")
	}
	fresh := a.session
	if err := tt.Click("打开工作区 /feature"); err != nil {
		t.Fatal(err)
	}
	if a.session != fresh || !draft.open || !draft.State.Busy {
		t.Fatal("folder click changed selected tab or background task")
	}
	first.State.ThreadID, first.loaded = "saved-history", false
	tt.Frame()
	if !tt.HasText("打开工作区 /repo") {
		t.Fatal("unloaded native history was hidden as blank")
	}
}

func TestWorktreeCardShowsOnlyLatestConversation(t *testing.T) {
	a := New(stubBackend{}, "test", "/repo", nil)
	a.repositories = []project.Repository{{CommonDir: "/repo/.git", Path: "/repo", Name: "repo", Worktrees: []project.Worktree{{Path: "/repo", Branch: "main"}}}}
	a.saved.Title = "older conversation"
	a.newSession("/repo")
	latest := a.session
	latest.saved.Title = "latest conversation"
	a.newSession("/repo")
	fresh := a.session
	tt := native.NewTester(a.view, 1000, 760)
	if tt.HasText("切换会话 1") || tt.HasText("切换会话 2") || tt.HasText("切换会话 3") {
		t.Fatal("worktree card must contain only its latest conversation")
	}
	if err := tt.Click("打开工作区 /repo"); err != nil {
		t.Fatal(err)
	}
	if a.session != fresh || !latest.open {
		t.Fatal("worktree card did not retain its selected tab")
	}
}

func TestWorktreeCardHoverAndClickCoverFullWidth(t *testing.T) {
	a := New(stubBackend{}, "test", "/repo", nil)
	a.saved.Title = "审阅 Sasa 设计文档 v2.0"
	a.repositories = []project.Repository{{CommonDir: "/repo/.git", Path: "/repo", Name: "Sasa", Worktrees: []project.Worktree{{Path: "/repo", Branch: "main", Main: true}, {Path: "/other", Branch: "feature/chat"}}}}
	a.newSession("/other")
	tt := native.NewTester(a.view, 1000, 760)
	body, ok := tt.Find("打开工作区 /repo")
	if !ok || tt.HasText("新建工作区会话 /repo") {
		t.Fatal("missing card controls")
	}
	// Both edges belong to the same full-width navigation button.
	x1, y := int(body.X+2), int(body.Y+body.H/2)
	before := tt.Image().RGBAAt(x1, y)
	tt.Move(body.X+body.W/2, body.Y+body.H/2)
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	hover := tt.Image()
	// The Mayoi-style focus shows as a translucent accent fill and a
	// pill on the left edge; the pixels change once the animations
	// advance, so the image must differ where it was.
	if hover.RGBAAt(x1, y) == before {
		t.Fatal("hovering the card produced no visual change")
	}
	tt.Move(body.X+body.W-2, body.Y+body.H/2)
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	if tt.Image().RGBAAt(x1, y) != hover.RGBAAt(x1, y) {
		t.Fatal("moving to the right edge split the card background")
	}
	if preview := os.Getenv("SASA_CARD_PREVIEW"); preview != "" {
		file, err := os.Create(preview)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, tt.Image())
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	count := len(a.sessions)
	tt.ClickAt(body.X+body.W-2, body.Y+body.H/2)
	if len(a.sessions) != count || a.State.Cwd != "/repo" {
		t.Fatal("right edge did not navigate to the existing conversation")
	}
}

func TestAddProjectSupportsPlainFoldersAndPreservesGitErrors(t *testing.T) {
	a := New(stubBackend{}, "test", "/initial", nil)
	queue := make(chan func(), 10)
	a.dispatch = func(fn func()) { queue <- fn }
	folder := project.Canonical(t.TempDir())
	selected, sessions := a.session, len(a.sessions)
	a.addProject(folder)
	pumpUpdate(t, queue)
	if a.projectsError != "" || len(a.repositories) != 0 || len(a.sessions) != sessions {
		t.Fatal("ordinary folder was not added as a project")
	}
	if a.session != selected || a.workspaceRoot(a.activeWorkspace) != a.workspaceRoot(folder) {
		t.Fatal("ordinary folder did not switch workspace without binding an agent")
	}
	emptyTabs := 0
	for _, tab := range a.tabs {
		if tab.Kind == "empty" && a.workspaceRoot(tab.Workspace) == a.workspaceRoot(folder) {
			emptyTabs++
		}
	}
	if emptyTabs != 1 {
		t.Fatalf("expected one empty tab for the dropped folder, got %d", emptyTabs)
	}
	a.addProject(folder)
	pumpUpdate(t, queue)
	if a.session != selected || len(a.sessions) != sessions {
		t.Fatal("adding the same folder duplicated a session or lost the empty tab")
	}
	emptyTabs = 0
	for _, tab := range a.tabs {
		if tab.Kind == "empty" && a.workspaceRoot(tab.Workspace) == a.workspaceRoot(folder) {
			emptyTabs++
		}
	}
	if emptyTabs != 1 {
		t.Fatalf("adding the same folder created a second empty tab: %d", emptyTabs)
	}
	tt := native.NewTester(a.view, 1000, 760)
	if !tt.HasText("添加项目") || tt.HasText("导入项目") || tt.HasText("打开项目") {
		t.Fatal("sidebar must expose a single add-project action")
	}
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, ".git"), []byte("invalid gitfile\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.addProject(broken)
	pumpUpdate(t, queue)
	if a.projectsError == "" || len(a.sessions) != sessions || a.session != selected {
		t.Fatal("broken Git repository was silently treated as an ordinary folder")
	}
	if err := os.WriteFile(filepath.Join(broken, ".git"), []byte("gitdir: /missing-sasa-test-git-dir\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.addProject(broken)
	pumpUpdate(t, queue)
	if a.projectsError == "" || len(a.sessions) != sessions {
		t.Fatal("missing linked Git directory was treated as a plain folder")
	}
}

func TestProjectDisclosureAnimatesHeightAndHonorsReducedMotion(t *testing.T) {
	a := New(nil, "test", "/repo", nil)
	tt := native.NewTester(func(c *native.Context) {
		native.Column(c).FillWidth().Children(func() {
			a.projectDisclosure(c, "/repo", "toggle", func() {
				native.Text(c, "repo")
				native.Spacer(c)
			}, func() { native.Button(c, "inside").Height(100) })
			native.Button(c, "after")
		})
	}, 300, 300)
	position := func() float32 {
		t.Helper()
		r, ok := tt.Find("after")
		if !ok {
			t.Fatal("missing following row")
		}
		return r.Y
	}
	expanded := position()
	if err := tt.Click("toggle"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(60 * time.Millisecond)
	closing := position()
	if closing <= 0 || closing >= expanded || !tt.HasText("inside") {
		t.Fatal("closing skipped intermediate height or removed content too early")
	}
	tt.Advance(300 * time.Millisecond)
	collapsed := position()
	if collapsed >= closing || tt.HasText("inside") {
		t.Fatal("collapse did not finish")
	}
	if err := tt.Click("toggle"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(60 * time.Millisecond)
	opening := position()
	if opening <= collapsed || opening >= expanded {
		t.Fatal("opening skipped intermediate height")
	}
	// Reverse while opening; the transition must finish in the requested state.
	if err := tt.Click("toggle"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if tt.HasText("inside") || position() != collapsed {
		t.Fatal("reversing transition did not settle closed")
	}
	tt.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	if err := tt.Click("toggle"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("inside") || position() != expanded {
		t.Fatal("reduced motion did not expand immediately")
	}
}

func TestTreeMouseDownKeepsHoverBackground(t *testing.T) {
	for _, label := range []string{"展开仓库 /repo/.git", "打开工作区 /repo", "展开项目 /plain", "切换会话 2"} {
		t.Run(label, func(t *testing.T) {
			a := New(stubBackend{}, "test", "/repo", nil)
			a.repositories = []project.Repository{{CommonDir: "/repo/.git", Path: "/repo", Name: "repo", Worktrees: []project.Worktree{{Path: "/repo", Branch: "main"}}}}
			a.newSession("/plain")
			a.newSession("/other")
			tt := native.NewTester(a.view, 1000, 760)
			r, ok := tt.Find(label)
			if !ok {
				t.Fatal("missing tree row")
			}
			x, y := r.X+2, r.Y+r.H/2
			tt.Move(x, y)
			hover := tt.Image().RGBAAt(int(x), int(y))
			tt.Press(x, y)
			if tt.Image().RGBAAt(int(x), int(y)) != hover {
				t.Fatal("mouse-down darkened the tree row")
			}
			tt.Release(x, y)
		})
	}
}

func TestRepositoryAndWorktreeHoverDoNotShowTooltips(t *testing.T) {
	for _, label := range []string{"展开仓库 /repo/.git", "打开工作区 /repo"} {
		t.Run(label, func(t *testing.T) {
			a := New(stubBackend{}, "test", "/repo", nil)
			a.repositories = []project.Repository{{CommonDir: "/repo/.git", Path: "/repo", Name: "repo", Worktrees: []project.Worktree{{Path: "/repo", Branch: "main"}}}}
			tt := native.NewTester(a.view, 1000, 760)
			before := tt.Texts()
			r, ok := tt.Find(label)
			if !ok {
				t.Fatal("missing directory row")
			}
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Advance(time.Second)
			if !slices.Equal(before, tt.Texts()) {
				t.Fatal("directory hover added tooltip text")
			}
		})
	}
}

func TestProjectTreeHidesMissingAndPrunableWorktrees(t *testing.T) {
	a := New(stubBackend{}, "test", "/repo", nil)
	a.repositories = []project.Repository{{CommonDir: "/repo/.git", Path: "/repo", Name: "repo", Worktrees: []project.Worktree{
		{Path: "/repo", Branch: "main"}, {Path: "/missing", Branch: "stale/missing", Missing: true},
		{Path: "/prunable", Branch: "stale/prunable", Prunable: true}, {Path: "/locked", Branch: "feature/locked", Locked: true},
		{Path: "/detached", Detached: true, Head: "12345678"},
	}}}
	tt := native.NewTester(a.view, 1000, 760)
	for _, path := range []string{"/missing", "/prunable"} {
		if tt.HasText("打开工作区 " + path) {
			t.Fatal("invalid worktree remains visible")
		}
	}
	for _, path := range []string{"/repo", "/locked", "/detached"} {
		if !tt.HasText("打开工作区 " + path) {
			t.Fatal("valid worktree was hidden")
		}
	}
	if len(a.repositories[0].Worktrees) != 5 {
		t.Fatal("filter modified repository metadata")
	}
}

func TestWorktreeCardKeepsMutedPillWhileFolderHasTabs(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/repo", nil)
			a.repositories = []project.Repository{{CommonDir: "/repo/.git", Path: "/repo", Name: "repo", Worktrees: []project.Worktree{
				{Path: "/repo", Branch: "main", Main: true}, {Path: "/other", Branch: "feature/chat"},
				{Path: "/idle", Branch: "feature/idle"},
			}}}
			a.newSession("/other")
			a.newSession("/repo")
			tt := native.NewTester(a.view, 1000, 760)
			tt.SetDark(dark)
			pillAt := func(label string) (color.RGBA, color.RGBA) {
				t.Helper()
				r, ok := tt.Find(label)
				if !ok {
					t.Fatalf("missing card %s", label)
				}
				// The pill is 3pt wide starting 2pt inside the left
				// edge, vertically centered; sample its middle and a
				// face pixel further right for contrast.
				img := tt.Image()
				y := int(r.Y + r.H/2)
				return img.RGBAAt(int(r.X+3), y), img.RGBAAt(int(r.X+40), y)
			}
			tt.Advance(300 * time.Millisecond)
			tt.Frame()
			onPill, _ := pillAt("打开工作区 /repo")
			tabsPill, tabsFace := pillAt("打开工作区 /other")
			idlePill, idleFace := pillAt("打开工作区 /idle")
			if tabsPill == tabsFace {
				t.Fatalf("open-tab folder lost the muted pill: pill=%#v face=%#v", tabsPill, tabsFace)
			}
			if tabsPill == onPill {
				t.Fatalf("open-tab pill did not differ from focused pill: %#v", tabsPill)
			}
			if idlePill != idleFace {
				t.Fatalf("folder without tabs gained a pill: pill=%#v face=%#v", idlePill, idleFace)
			}
		})
	}
}
