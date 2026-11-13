package ui

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	gitview "github.com/shenming0821/Fuyu/internal/git"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func gitWorkflowApp(t *testing.T) (*App, chan func(), string, func(...string) string) {
	t.Helper()
	a, queue, root := p4App(t)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return string(out)
	}
	git("init", "-b", "main")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.invalid")
	git("config", "core.hooksPath", "/dev/null")
	git("config", "commit.gpgsign", "false")
	git("add", ".")
	git("commit", "-m", "feat: baseline")
	a.historyOpen, a.rightTab = true, "git"
	return a, queue, root, git
}

func TestGitWorkflowStagesOnlyChosenFileAndCommitsIndex(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("selected\n"), 0600)
	os.WriteFile(filepath.Join(root, "internal/main.go"), []byte("package main\n// other change\n"), 0600)
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1280, 800)
	tt.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	p4Wait(t, tt, q, func() bool { return !w.gitLoading && len(w.git.Entries) == 2 })
	r, _ := tt.Find("Git 更改 README.md")
	tt.Move(r.X+10, r.Y+10)
	if err := tt.Click("暂存 README.md"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && len(gitEntries(w, "已暂存的更改")) == 1 })
	if err := tt.Click("Git 提交信息"); err != nil {
		t.Fatal(err)
	}
	tt.Type("fix: selected readme")
	if err := tt.Click("提交 1 个文件"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		return w.gitBusy == "" && w.gitMessage == "" && len(gitEntries(w, "已暂存的更改")) == 0
	})
	if got := git("show", "--format=", "--name-only", "HEAD"); strings.TrimSpace(got) != "README.md" {
		t.Fatal("commit included unselected files", got)
	}
	if len(gitEntries(w, "更改")) != 1 || !strings.Contains(git("diff", "--", "internal/main.go"), "other change") {
		t.Fatal("other workspace changes were lost")
	}
	if err := tt.Click("切换最近提交"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 2 })
	info := w.gitHistory[0]
	if err := tt.Click("提交 " + info.Hash); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		tree := w.gitCommitTrees[info.Hash]
		return tree != nil && tree.loaded
	})
	if err := tt.Click("提交文件 " + info.Hash + " README.md"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		d := a.currentFile()
		return d != nil && d.loaded && !d.loading && d.gitCommit == info.Hash
	})
	d := a.currentFile()
	if len(d.gitCommitFiles) != 1 || d.gitCommitFiles[0].Path != "README.md" || !strings.Contains(d.buffer.String(), "selected") {
		t.Fatal("commit content not shown", d.err)
	}
}

func TestGitHistoryCommitExpandCollapseAndOpenChosenFile(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("history readme\n"), 0600)
	os.WriteFile(filepath.Join(root, "internal/main.go"), []byte("package chosen\n"), 0600)
	git("commit", "-am", "two files")
	w := a.workspace(root)
	w.collapsed["最近提交已展开"] = true
	tt := native.NewTester(a.view, 1600, 900)
	tt.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	p4Wait(t, tt, q, func() bool { return w.gitHistoryLoaded && len(w.gitHistory) == 2 })
	info := w.gitHistory[0]
	tree := w.gitCommitTrees[info.Hash]
	if tree == nil || tree.expanded || tree.loaded || tree.loading {
		t.Fatal("collapsed commit should not read files")
	}
	fileLabel := "提交文件 " + info.Hash + " internal/main.go"
	tt.Click("提交 " + info.Hash)
	p4Wait(t, tt, q, func() bool { return tree.loaded })
	if len(tree.entries) != 2 || a.currentFile() != nil {
		t.Fatal("expansion should list files without opening a tab")
	}
	if _, ok := tt.Find(fileLabel); !ok {
		t.Fatal("expanded commit file missing")
	}
	tt.Click("提交 " + info.Hash)
	if _, ok := tt.Find(fileLabel); ok {
		t.Fatal("collapsed commit still shows files")
	}
	tt.Click("提交 " + info.Hash)
	if tree.loading || len(tree.entries) != 2 {
		t.Fatal("reopening should reuse immutable commit file list")
	}
	if err := tt.Click(fileLabel); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded && !a.currentFile().loading })
	d := a.currentFile()
	if d.gitCommitFiles[d.gitCommitFile].Path != "internal/main.go" ||
		!strings.Contains(d.buffer.String(), "package chosen") || strings.Contains(d.buffer.String(), "history readme") {
		t.Fatal("file click did not select the corresponding historical diff", d.err)
	}
	a.gitOpenCommit(w, info, "README.md")
	a.gitOpenCommit(w, info, "internal/main.go")
	p4Wait(t, tt, q, func() bool { return !d.loading })
	if d.gitCommitFiles[d.gitCommitFile].Path != "internal/main.go" || !strings.Contains(d.buffer.String(), "package chosen") {
		t.Fatal("late result replaced the last selected file")
	}
	other := a.workspace(t.TempDir())
	if len(other.gitCommitTrees) != 0 {
		t.Fatal("commit expansion leaked to another workspace")
	}
}

func TestGitHistoryCommitReadFailureRetryAndEmptyCommit(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	git("commit", "--allow-empty", "-m", "empty")
	w := a.workspace(root)
	w.collapsed["最近提交已展开"] = true
	info := gitview.CommitInfo{Hash: strings.Repeat("a", 40), Subject: "missing"}
	w.gitHistory, w.gitHistoryLoaded = []gitview.CommitInfo{info}, true
	tt := native.NewTester(a.view, 1600, 900)
	tt.Click("提交 " + info.Hash)
	tree := w.gitCommitTrees[info.Hash]
	p4Wait(t, tt, q, func() bool { return !tree.loading && tree.err != "" })
	if _, ok := tt.Find("重试读取文件"); !ok {
		t.Fatal("file read failure lacks retry")
	}
	tt.Click("重试读取文件")
	p4Wait(t, tt, q, func() bool { return !tree.loading && tree.err != "" })
	a.gitLoadHistory(w, false)
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading })
	if _, ok := w.gitCommitTrees[info.Hash]; ok {
		t.Fatal("removed history entry retained cached state")
	}
	info = w.gitHistory[0]
	tt.Click("提交 " + info.Hash)
	p4Wait(t, tt, q, func() bool { return w.gitCommitTrees[info.Hash].loaded })
	if !tt.HasText("此提交没有文件改动") {
		t.Fatal("empty commit lacks explicit result")
	}
}

func TestGitHistoryHeaderRefreshReloadsNewCommits(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	w := a.workspace(root)
	w.collapsed["最近提交已展开"] = true
	tt := native.NewTester(a.view, 1600, 900)
	p4Wait(t, tt, q, func() bool { return !w.gitLoading && w.gitHistoryLoaded && len(w.gitHistory) == 1 })
	git("commit", "--allow-empty", "-m", "newest")
	if err := tt.Click("刷新提交历史"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 2 })
	if w.gitHistory[0].Subject != "newest" {
		t.Fatal("refresh did not pick up the new commit", w.gitHistory[0].Subject)
	}
}

func TestGitSelectionAndDraftsAreSeparatedByWorkspaceAndIndexSide(t *testing.T) {
	a := New(stubBackend{}, "test", "/one", nil)
	w := a.workspace("/one")
	w.git.Entries = []gitview.Entry{{Path: "a", XY: "MM"}, {Path: "b", XY: ".M"}, {Path: "c", XY: ".M"}}
	a.gitSelect(w, "更改", w.git.Entries[0], 0)
	a.gitSelect(w, "更改", w.git.Entries[2], native.Shift)
	if len(a.gitChosen(w, "更改", w.git.Entries[0])) != 3 || len(a.gitChosen(w, "已暂存的更改", w.git.Entries[0])) != 1 {
		t.Fatal("selection crossed the index boundary", w.gitSelection)
	}
	a.gitSelect(w, "更改", w.git.Entries[1], native.Cmd)
	if len(a.gitChosen(w, "更改", w.git.Entries[0])) != 2 {
		t.Fatal("command click did not toggle selection")
	}
	w.gitMessage = "feat: one"
	a.workspace("/two").gitMessage = "fix: two"
	var snapshot storage.Snapshot
	a.snapshotWorkspaceTabs(&snapshot)
	b := New(stubBackend{}, "test", "/one", nil)
	if err := b.restoreWorkspaceTabs(snapshot, ""); err != nil {
		t.Fatal(err)
	}
	if b.workspace("/one").gitMessage != "feat: one" || b.workspace("/two").gitMessage != "fix: two" {
		t.Fatal("workspace commit drafts did not restore")
	}
}

func TestGitUnsavedGuardAndWriteOwnership(t *testing.T) {
	a, q, root, _ := gitWorkflowApp(t)
	tt := native.NewTester(a.view, 1280, 800)
	a.openFile(root, filepath.Join(root, "README.md"))
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("unsaved editor\n")
	a.observeFile(d)
	w := a.workspace(root)
	entry := gitview.Entry{Path: "README.md", XY: ".M"}
	a.gitStage(w, []gitview.Entry{entry}, false)
	if w.gitDecision == nil || w.gitBusy != "" {
		t.Fatal("staging bypassed unsaved confirmation")
	}
	tt.Frame()
	if err := tt.Click("保存后继续"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		return !d.saving && !d.dirty() && w.gitBusy == "" && len(gitEntries(w, "已暂存的更改")) == 1
	})
	raw, _ := os.ReadFile(d.path)
	if string(raw) != "unsaved editor\n" {
		t.Fatal("stage ran before saving")
	}
	started, release := make(chan struct{}), make(chan struct{})
	a.gitAction(w, "controlled action", func(context.Context) error { close(started); <-release; return nil }, nil)
	<-started
	if !a.hasGitWrites() {
		t.Fatal("quit guard missed a running write")
	}
	called := false
	a.gitAction(w, "duplicate", func(context.Context) error { called = true; return nil }, nil)
	a.selectWorkspace("/other")
	if !a.hasGitWrites() || w.gitBusy == "" {
		t.Fatal("workspace switching canceled a started write")
	}
	close(release)
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" })
	if called || a.workspace("/other").gitNotice != "" {
		t.Fatal("write duplicated or callback leaked into the other workspace")
	}
}

func TestGitDiffHunkNavigationAndSplitLayout(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("line %02d\n", i))
	}
	path := filepath.Join(root, "hunks.txt")
	os.WriteFile(path, []byte(strings.Join(lines, "")), 0600)
	git("add", "hunks.txt")
	git("commit", "-m", "add hunk fixture")
	lines[2], lines[30] = "changed 02\n", "changed 30\n"
	os.WriteFile(path, []byte(strings.Join(lines, "")), 0600)
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1600, 800)
	p4Wait(t, tt, q, func() bool { return !w.gitLoading && len(w.git.Entries) == 1 })
	tt.Click("Git 更改 hunks.txt")
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	d := a.currentFile()
	if d.gitReview == nil || len(d.gitReview.Hunks) != 2 {
		t.Fatal("missing hunk review")
	}
	tt.Click("下一个修改")
	if d.gitHunk != 1 {
		t.Fatal("hunk navigation did not select the next block", d.gitHunk)
	}
	tt.Click("并排")
	if _, ok := tt.Find("差异 原版本"); !ok {
		t.Fatal("split view not rendered")
	}
}

type messageFixtureBackend struct {
	stubBackend
	prompt chan string
	result chan string
}

func (b *messageFixtureBackend) GenerateCommitMessage(ctx context.Context, prompt string) (string, error) {
	b.prompt <- prompt
	select {
	case message := <-b.result:
		return message, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestGitMessageGenerationAndCodexDraftPreserveUserInput(t *testing.T) {
	a, q, root, _ := gitWorkflowApp(t)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("meaningful change\n"), 0600)
	backend := &messageFixtureBackend{prompt: make(chan string, 1), result: make(chan string, 1)}
	a.backend = backend
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1280, 800)
	p4Wait(t, tt, q, func() bool { return !w.gitLoading && len(w.git.Entries) == 1 })
	a.gitGenerateMessage(w)
	select {
	case prompt := <-backend.prompt:
		if !strings.Contains(prompt, "feat: baseline") || !strings.Contains(prompt, "+meaningful change") {
			t.Fatal("generation omitted patch or recent convention")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("generation did not start")
	}
	w.gitMessage = "my own message"
	backend.result <- "feat: generated"
	p4Wait(t, tt, q, func() bool { return !w.gitGenerating })
	if w.gitMessage != "my own message" {
		t.Fatal("late generation replaced typed message")
	}
	a.session.State.Draft = "existing draft"
	a.gitToCodex(w, w.git.Entries, false, "检查问题", nil)
	p4Wait(t, tt, q, func() bool { return strings.Contains(a.session.State.Draft, "meaningful change") })
	if !strings.HasPrefix(a.session.State.Draft, "existing draft\n\n") || a.State.Busy {
		t.Fatal("Codex handoff lost draft or submitted without user action")
	}
}

func TestGitConflictAcceptSaveMarkAndContinue(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	path := filepath.Join(root, "README.md")
	git("switch", "-c", "incoming")
	os.WriteFile(path, []byte("incoming\n"), 0600)
	git("commit", "-am", "incoming")
	git("switch", "main")
	os.WriteFile(path, []byte("current\n"), 0600)
	git("commit", "-am", "current")
	if err := exec.Command("git", "-C", root, "merge", "incoming").Run(); err == nil {
		t.Fatal("fixture did not conflict")
	}
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1600, 900)
	p4Wait(t, tt, q, func() bool { return len(gitEntries(w, "冲突")) == 1 })
	if err := tt.Click("Git 冲突 README.md"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		d := a.currentFile()
		return d != nil && d.loaded && d.gitConflict != nil
	})
	d := a.currentFile()
	for _, label := range []string{"冲突 共同祖先", "冲突 当前版本", "冲突 传入版本"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("missing three-way pane", label)
		}
	}
	if err := tt.Click("接受两者"); err != nil {
		t.Fatal(err)
	}
	if d.buffer.String() != "current\nincoming\n" || !d.dirty() {
		t.Fatal("resolution dropped a side", d.buffer.String())
	}
	if err := tt.Click("保存并标记已解决"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !d.dirty() && d.gitConflict == nil && len(gitEntries(w, "冲突")) == 0 })
	if git("show", ":README.md") != "current\nincoming\n" {
		t.Fatal("resolved result not staged")
	}
	if err := tt.Click("继续"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitOperation == "" && w.gitBusy == "" })
	if git("show", "HEAD:README.md") != "current\nincoming\n" {
		t.Fatal("continued merge lost result")
	}
}

func TestGitDiscardRechecksConfirmationAndProtectsUnsavedEditor(t *testing.T) {
	a, q, root, _ := gitWorkflowApp(t)
	path := filepath.Join(root, "README.md")
	os.WriteFile(path, []byte("first disk change\n"), 0600)
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1280, 800)
	p4Wait(t, tt, q, func() bool { return len(gitEntries(w, "更改")) == 1 })
	entry := gitEntries(w, "更改")[0]
	a.gitDiscard(w, []gitview.Entry{entry})
	p4Wait(t, tt, q, func() bool { return w.gitDecision != nil })
	os.WriteFile(path, []byte("newer disk change\n"), 0600)
	if err := tt.Click("撤销改动"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && w.gitActionError != "" })
	if data, _ := os.ReadFile(path); string(data) != "newer disk change\n" {
		t.Fatal("stale discard overwrote latest change")
	}
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("unsaved draft\n")
	a.observeFile(d)
	a.gitDiscard(w, []gitview.Entry{entry})
	if !strings.Contains(w.gitActionError, "未保存编辑") || !d.dirty() || w.gitDecision != nil {
		t.Fatal("discard did not protect dirty editor", w.gitActionError)
	}
}

func TestGitGroupDiscardAllRestoresEveryUnstagedChange(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("changed readme\n"), 0600)
	os.WriteFile(filepath.Join(root, "internal/main.go"), []byte("package main\n// changed\n"), 0600)
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1280, 800)
	p4Wait(t, tt, q, func() bool { return len(gitEntries(w, "更改")) == 2 })
	header, ok := tt.Find("折叠 更改")
	if !ok {
		t.Fatal("missing 更改 group header")
	}
	tt.Move(header.X+10, header.Y+10)
	if err := tt.Click("撤销全部改动"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitArmed != nil && len(w.gitArmed.reviews) == 2 })
	if err := tt.Click("确认撤销 2 个文件"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && len(gitEntries(w, "更改")) == 0 })
	if out := strings.TrimSpace(git("status", "--porcelain")); out != "" {
		t.Fatal("group discard left workspace changes", out)
	}
}

func TestGitRowDiscardArmsThenConfirmsAndExpires(t *testing.T) {
	old := gitDiscardArmDelay
	gitDiscardArmDelay = 2 * time.Second
	defer func() { gitDiscardArmDelay = old }()
	a, q, root, git := gitWorkflowApp(t)
	readme := filepath.Join(root, "README.md")
	os.WriteFile(readme, []byte("changed readme\n"), 0600)
	os.WriteFile(filepath.Join(root, "internal/main.go"), []byte("package main\n// changed\n"), 0600)
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1280, 800)
	p4Wait(t, tt, q, func() bool { return len(gitEntries(w, "更改")) == 2 })
	r, ok := tt.Find("Git 更改 README.md")
	if !ok {
		t.Fatal("missing README.md row")
	}
	tt.Move(r.X+10, r.Y+10)
	if err := tt.Click("撤销改动 README.md"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitArmed != nil && len(w.gitArmed.reviews) == 1 })
	if data, _ := os.ReadFile(readme); string(data) != "changed readme\n" {
		t.Fatal("arming discard changed the file")
	}
	p4Wait(t, tt, q, func() bool { return w.gitArmed == nil })
	if _, ok := tt.Find("撤销改动 README.md"); !ok {
		t.Fatal("discard button did not revert after the armed window")
	}
	gitDiscardArmDelay = time.Minute
	if err := tt.Click("撤销改动 README.md"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitArmed != nil && len(w.gitArmed.reviews) == 1 })
	if err := tt.Click("确认撤销改动 README.md"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && len(gitEntries(w, "更改")) == 1 })
	if data, _ := os.ReadFile(readme); string(data) != "# Fuyu\n原生工作区编辑器\n" {
		t.Fatal("confirmed discard did not restore the file", string(data))
	}
	if out := strings.TrimSpace(git("status", "--porcelain")); !strings.Contains(out, "internal/main.go") || strings.Contains(out, "README") {
		t.Fatal("discard touched other entries", out)
	}
}

func TestGitSaveThenBranchSwitchAndQuitPreparationGate(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	a.openFile(root, filepath.Join(root, "README.md"))
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1280, 800)
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("edited before switch\n")
	a.observeFile(d)
	a.gitSwitchBranch(w, "new-branch", true)
	tt.Frame()
	if err := tt.Click("保存后切换"); err != nil {
		t.Fatal(err)
	}
	if !a.hasGitWrites() {
		t.Fatal("save preparation not included in quit gate")
	}
	a.gitQuitPending = true
	p4Wait(t, tt, q, func() bool { return !w.gitPreparing && w.gitBusy == "" && !d.dirty() })
	if strings.TrimSpace(git("branch", "--show-current")) != "new-branch" || d.buffer.String() != "edited before switch\n" {
		t.Fatal("pending quit dropped already requested switch", w.gitActionError)
	}
	called := false
	a.gitAction(w, "new write while quitting", func(context.Context) error { called = true; return nil }, nil)
	if called || w.gitBusy != "" {
		t.Fatal("quit allowed a new write")
	}
	exited := false
	a.quitApp = func() { exited = true }
	w.gitPreparing = true
	a.requestFileExit()
	if exited || !a.fileExitRequested {
		t.Fatal("file exit did not wait for Git preparation")
	}
	w.gitPreparing = false
	a.advanceFileExit()
	if !exited {
		t.Fatal("file exit did not finish after Git preparation")
	}
}

func TestGitHistoryPerFileNavigationAndPagination(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	for i := range 21 {
		git("commit", "--allow-empty", "-m", fmt.Sprintf("history %02d", i))
	}
	os.WriteFile(filepath.Join(root, "README.md"), []byte("history readme\n"), 0600)
	os.WriteFile(filepath.Join(root, "internal/main.go"), []byte("package historical\n"), 0600)
	git("commit", "-am", "two files")
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1600, 900)
	a.gitLoadHistory(w, false)
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 20 })
	a.gitLoadHistory(w, true)
	p4Wait(t, tt, q, func() bool { return !w.gitHistoryLoading && len(w.gitHistory) == 23 })
	a.gitOpenCommit(w, w.gitHistory[0])
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	d := a.currentFile()
	if len(d.gitCommitFiles) != 2 || !strings.Contains(d.buffer.String(), "history readme") {
		t.Fatal("historical first file missing", d.err)
	}
	a.gitStepFile(d, 1)
	p4Wait(t, tt, q, func() bool { return !d.loading })
	if !strings.Contains(d.buffer.String(), "package historical") || strings.Contains(d.buffer.String(), "history readme") {
		t.Fatal("historical file selection showed wrong file")
	}
	if err := tt.Click("打开文件"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return a.currentFile() != d && a.currentFile().loaded })
	if a.currentFile().path != filepath.Join(root, "internal/main.go") || a.currentFile().err != "" {
		t.Fatal("historical open used a synthetic commit path", a.currentFile().path)
	}
}

func TestGitPanelLightDarkNarrowWorkflowLayout(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{260, 360} {
			t.Run(fmt.Sprintf("dark=%v-width=%d", dark, width), func(t *testing.T) {
				a, _, root := p4App(t)
				w := a.workspace(root)
				w.gitTime, w.gitFingerprint, w.gitStatsFingerprint = time.Now(), "fixture", "fixture"
				w.git = gitview.Status{Branch: "feature/long-branch-name", Upstream: "origin/main", Ahead: 2, Behind: 1,
					Entries: []gitview.Entry{{Path: "internal/ui/long-filename.go", XY: ".M"}, {Path: "README.md", XY: ".M"}, {Path: "staged.go", XY: "M."}}}
				w.gitRemotes = []string{"origin"}
				w.gitStats = map[string]gitview.Review{
					gitKey("false", "internal/ui/long-filename.go"): {Added: 12, Deleted: 3},
					gitKey("false", "README.md"):                    {Added: 4},
				}
				w.gitMessage = "fix: 调整面板布局"
				w.gitSelection = map[string]bool{gitKey("更改", "internal/ui/long-filename.go"): true, gitKey("更改", "README.md"): true}
				tt := native.NewTester(func(c *native.Context) {
					native.Column(c).Fill().Background(c.Theme().Surface).Children(func() { a.gitPanel(c, w) })
				}, width, 650)
				tt.SetDark(dark)
				tt.Advance(150 * time.Millisecond)
				for _, label := range []string{"Git 提交信息", "提交 1 个文件", "暂存所选", "获取远程更新", "Git 更改 internal/ui/long-filename.go"} {
					r, ok := tt.Find(label)
					if !ok || r.W <= 0 || r.X < 0 || r.X+r.W > float32(width) {
						t.Fatal("workflow control not within panel", label, r)
					}
				}
				if dir := os.Getenv("FUYU_UI_EVIDENCE"); dir != "" {
					os.MkdirAll(dir, 0700)
					f, err := os.Create(filepath.Join(dir, fmt.Sprintf("git-dark-%v-width-%d.png", dark, width)))
					if err != nil {
						t.Fatal(err)
					}
					if err = png.Encode(f, tt.Image()); err != nil {
						t.Fatal(err)
					}
					f.Close()
					r, _ := tt.Find("Git 更改 internal/ui/long-filename.go")
					tt.Move(r.X+10, r.Y+10)
					f, err = os.Create(filepath.Join(dir, fmt.Sprintf("git-hover-dark-%v-width-%d.png", dark, width)))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestGitBatchCommitAndRemoteWorkflowThroughPanel(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "--initial-branch=main", remote).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	git("remote", "add", "origin", remote)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("batch readme\n"), 0600)
	os.WriteFile(filepath.Join(root, "internal/main.go"), []byte("package batch\n"), 0600)
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1600, 900)
	p4Wait(t, tt, q, func() bool { return len(gitEntries(w, "更改")) == 2 && len(w.gitRemotes) == 1 })
	tt.ClickWith(native.Cmd, "Git 更改 README.md")
	tt.ClickWith(native.Cmd, "Git 更改 internal/main.go")
	if err := tt.Click("暂存所选"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return len(gitEntries(w, "已暂存的更改")) == 2 && w.gitBusy == "" })
	tt.Click("Git 提交信息")
	tt.Type("feat: batch")
	tt.Click("提交 2 个文件")
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && w.gitMessage == "" && len(w.git.Entries) == 0 })
	if err := tt.Click("发布分支"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && w.git.Upstream == "origin/main" })
	os.WriteFile(filepath.Join(root, "README.md"), []byte("next\n"), 0600)
	a.refreshGit(w)
	p4Wait(t, tt, q, func() bool { return len(gitEntries(w, "更改")) == 1 })
	tt.Click("Git 提交信息")
	tt.Type("fix: next")
	if err := tt.Click("全部暂存并提交"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && w.git.Ahead == 1 })
	if err := tt.Click("推送 1 个提交"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && w.git.Ahead == 0 })
	out, err := exec.Command("git", "--git-dir", remote, "rev-parse", "main").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != strings.TrimSpace(git("rev-parse", "HEAD")) {
		t.Fatal("panel push did not update isolated bare remote", err, string(out))
	}
}

func TestGitSplitScrollAndSelectionFollowEitherPane(t *testing.T) {
	a := New(stubBackend{}, "test", "/one", nil)
	var patch strings.Builder
	patch.WriteString("diff --git a/file b/file\n--- a/file\n+++ b/file\n")
	for i := range 8 {
		fmt.Fprintf(&patch, "@@ -%d,4 +%d,4 @@\n context a\n-old\n+new\n context b\n context c\n", i*20+1, i*20+1)
	}
	review := gitview.ParsePatch(patch.String())
	d := &filePage{root: "/one", gitDiffState: gitDiffState{gitReview: &review, gitSplit: true}}
	tt := native.NewTester(func(c *native.Context) {
		native.Column(c).Fill().Children(func() { a.gitSplitView(c, d, 1000) })
	}, 1000, 220)
	r, ok := tt.Find("差异 原版本")
	if !ok {
		t.Fatal("missing left pane")
	}
	tt.Scroll(r.X+100, r.Y+80, 0, 170)
	tt.Frame()
	if d.gitLeftScroll.Y <= 0 || d.gitLeftScroll.Y != d.gitRightScroll.Y {
		t.Fatal("scrolling left did not sync right", d.gitLeftScroll.Y, d.gitRightScroll.Y)
	}
	right, _ := tt.Find("差异 新版本")
	tt.ClickAt(right.X+100, right.Y+80)
	tt.Frame()
	if d.gitHunk <= 0 {
		t.Fatal("clicking a later block did not select it")
	}
}

func TestGitMessageRejectsSameStatusWithChangedBytes(t *testing.T) {
	a, q, root, _ := gitWorkflowApp(t)
	path := filepath.Join(root, "README.md")
	os.WriteFile(path, []byte("first\n"), 0600)
	backend := &messageFixtureBackend{prompt: make(chan string, 1), result: make(chan string, 1)}
	a.backend = backend
	w := a.workspace(root)
	tt := native.NewTester(a.view, 1280, 800)
	p4Wait(t, tt, q, func() bool { return len(gitEntries(w, "更改")) == 1 })
	a.gitGenerateMessage(w)
	select {
	case <-backend.prompt:
	case <-time.After(5 * time.Second):
		t.Fatal("generation not started")
	}
	os.WriteFile(path, []byte("second\n"), 0600)
	backend.result <- "fix: outdated first change"
	p4Wait(t, tt, q, func() bool { return !w.gitGenerating })
	if w.gitMessage != "" || !strings.Contains(w.gitActionError, "已变化") {
		t.Fatal("generation accepted stale bytes with same porcelain status", w.gitMessage, w.gitActionError)
	}
}

func TestGitOccupiedBranchReportsSwitchFailureAndPreservesWorkspace(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	other := filepath.Join(t.TempDir(), "other worktree")
	git("worktree", "add", "-b", "other", other)
	other, err := filepath.EvalSymlinks(other)
	if err != nil {
		t.Fatal(err)
	}
	w := a.workspace(root)
	original := a.session
	original.State.Draft = "保留原工作区的输入"
	w.gitMessage = "fix: original workspace"
	tt := native.NewTester(a.view, 1600, 900)
	p4Wait(t, tt, q, func() bool { return !w.gitLoading && len(w.gitBranches) == 2 })
	if err := tt.Click("选择 Git 分支"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(200 * time.Millisecond)
	if tt.HasText("other · 打开工作区") {
		t.Fatal("branch row shows a workspace action")
	}
	if err := tt.Click("切换分支 other"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && w.gitActionError != "" })
	if a.State.Cwd != root || a.activeWorkspace != root || original.State.Draft != "保留原工作区的输入" ||
		w.gitMessage != "fix: original workspace" || strings.TrimSpace(git("branch", "--show-current")) != "main" {
		t.Fatal("occupied branch changed original worktree or lost its draft", a.State.Cwd, a.activeWorkspace)
	}
	if !strings.Contains(w.gitActionError, "other") || !w.gitBranchPicker {
		t.Fatal("switch failure did not retain the picker and report the occupied branch", w.gitActionError)
	}
	git("branch", "available")
	a.refreshGit(w)
	p4Wait(t, tt, q, func() bool { return !w.gitLoading && len(w.gitBranches) == 3 })
	if err := tt.Click("切换分支 available"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && w.git.Branch == "available" })
	if a.State.Cwd != root || a.activeWorkspace != root || w.gitBranchPicker || strings.TrimSpace(git("branch", "--show-current")) != "available" {
		t.Fatal("branch click did not switch the original workspace")
	}
}

func TestGitCommitFailureKeepsMessageAndIndexForRetry(t *testing.T) {
	a, q, root, git := gitWorkflowApp(t)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("retry change\n"), 0600)
	git("add", "README.md")
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	git("config", "core.hooksPath", hooks)
	head := git("rev-parse", "HEAD")
	w := a.workspace(root)
	w.gitMessage = "fix: retry after hook failure"
	tt := native.NewTester(a.view, 1280, 800)
	p4Wait(t, tt, q, func() bool { return len(gitEntries(w, "已暂存的更改")) == 1 })
	if err := tt.Click("提交 1 个文件"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && w.gitActionError != "" })
	if w.gitMessage != "fix: retry after hook failure" || git("rev-parse", "HEAD") != head || git("show", ":README.md") != "retry change\n" {
		t.Fatal("failed commit lost the retry state")
	}
	git("config", "core.hooksPath", "/dev/null")
	tt.Frame()
	if err := tt.Click("提交 1 个文件"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return w.gitBusy == "" && w.gitMessage == "" })
	if git("rev-parse", "HEAD") == head || !strings.Contains(w.gitNotice, "retry after hook failure") {
		t.Fatal("explicit retry did not commit or show its summary")
	}
}
