package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/codex"
	"github.com/shenming0821/Fuyu/internal/editor"
	gitview "github.com/shenming0821/Fuyu/internal/git"
	"github.com/shenming0821/Fuyu/internal/project"
	"github.com/shenming0821/Fuyu/internal/storage"
	"github.com/shenming0821/Fuyu/internal/workspace"
)

func p4App(t *testing.T) (*App, chan func(), string) {
	t.Helper()
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "internal"), 0700)
	os.WriteFile(filepath.Join(root, "internal", "main.go"), []byte("package main\n\nfunc main() {\n\tprintln(\"你好 Fuyu\")\n}\n"), 0640)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("# Fuyu\n原生工作区编辑器\n"), 0640)
	a := New(stubBackend{}, "test", root, nil)
	queue := make(chan func(), 500)
	a.dispatch = func(fn func()) {
		select {
		case queue <- fn:
		case <-a.shutdown:
		}
	}
	t.Cleanup(func() { close(a.shutdown); a.closeWorkspaceResources() })
	return a, queue, root
}
func p4Wait(t *testing.T, tt *native.Tester, queue chan func(), condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case fn := <-queue:
			fn()
		default:
		}
		tt.Frame()
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("P4 state did not settle")
}
func TestP4BrowseEditSaveAndMixedTabs(t *testing.T) {
	a, queue, root := p4App(t)
	original := a.session
	original.State.Draft = "聊天草稿"
	original.State.Busy = true
	a.historyOpen = true
	a.rightTab = "files"
	tt := native.NewTester(a.view, 1280, 800)
	w := a.workspace(root)
	p4Wait(t, tt, queue, func() bool { return len(w.children[root]) > 0 })
	if err := tt.Click("文件树 " + filepath.Join(root, "internal")); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, queue, func() bool { return len(w.children[filepath.Join(root, "internal")]) > 0 })
	tt.Advance(250 * time.Millisecond)
	path := filepath.Join(root, "internal", "main.go")
	if err := tt.Click("文件树 " + path); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, queue, func() bool { d := a.currentFile(); return d != nil && d.loaded })
	d := a.currentFile()
	if tt.HasText("任务输入") || !original.State.Busy || original.State.Draft != "聊天草稿" {
		t.Fatal("file page leaked composer or changed running chat")
	}
	if err := tt.Click("文件内容 " + path); err != nil {
		t.Fatal(err)
	}
	tt.Key(native.Cmd, native.KeyA)
	tt.Type("package main\n// 修改\n")
	tt.Key(native.Cmd, native.KeyS)
	p4Wait(t, tt, queue, func() bool { raw, _ := os.ReadFile(path); return !d.saving && strings.Contains(string(raw), "修改") })
	if d.dirty() {
		t.Fatal("saved text remains dirty")
	}
	if err := tt.Click("会话标签 1"); err != nil {
		t.Fatal(err)
	}
	if a.currentFile() != nil || a.State.Draft != "聊天草稿" {
		t.Fatal("chat state not restored")
	}
	if err := tt.Click("文件标签 " + path); err != nil {
		t.Fatal(err)
	}
	if a.currentFile() != d {
		t.Fatal("file tab was duplicated")
	}
	if out := os.Getenv("FUYU_P4_PREVIEW"); out != "" {
		file, e := os.Create(out)
		if e != nil {
			t.Fatal(e)
		}
		e = png.Encode(file, tt.Image())
		file.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
}

func TestP4BackgroundApprovalKeepsFileWorkspaceForSaveHistoryAndNewChat(t *testing.T) {
	a, q, rootA := p4App(t)
	owner := a.session
	owner.State.ThreadID = "task-A"
	owner.State.Begin("background task in A")
	owner.State.TurnID = "turn-A"
	originalA, err := os.ReadFile(filepath.Join(rootA, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootB := t.TempDir()
	path := filepath.Join(rootB, "README.md")
	if err := os.WriteFile(path, []byte("workspace B\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.nativeThreads = []codex.ThreadMeta{
		{ID: "history-A", Cwd: rootA, Preview: "history in A"},
		{ID: "history-B", Cwd: rootB, Preview: "history in B"},
	}
	a.historyOpen, a.rightTab = true, "history"
	tt := native.NewTester(a.view, 1100, 760)
	a.openFile(rootB, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	d := a.currentFile()
	params, err := json.Marshal(map[string]string{
		"threadId": "task-A", "turnId": "turn-A", "itemId": "command-A",
		"command": "pwd", "cwd": rootA,
	})
	if err != nil {
		t.Fatal(err)
	}
	a.applyBatch([]codex.Event{{ID: json.RawMessage(`42`), Method: "item/commandExecution/requestApproval", Params: params}})
	tt.Frame()
	if a.currentFile() != d || len(owner.State.PendingInteractions()) != 1 || !owner.State.Busy {
		t.Fatal("background approval stole the file or lost its running owner")
	}
	if _, ok := tt.Find("打开历史 history-B"); !ok {
		t.Fatal("file workspace history missing")
	}
	if _, ok := tt.Find("打开历史 history-A"); ok {
		t.Fatal("running task workspace leaked into file workspace history")
	}
	d.buffer.Set("saved only in B\n")
	d.observe()
	a.saveFile(d, false, false, nil)
	p4Wait(t, tt, q, func() bool {
		raw, _ := os.ReadFile(path)
		return !d.saving && string(raw) == "saved only in B\n"
	})
	nowA, err := os.ReadFile(filepath.Join(rootA, "README.md"))
	if err != nil || string(nowA) != string(originalA) {
		t.Fatal("file save modified running task workspace", err)
	}
	if err := tt.Click("使用 Codex 新建会话"); err != nil {
		t.Fatal(err)
	}
	if a.session == owner || a.State.Cwd != rootB || !owner.State.Busy {
		t.Fatal("new conversation did not inherit the file workspace")
	}
	if err := tt.Click("文件标签 " + path); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("返回待处理会话"); err != nil {
		t.Fatal(err)
	}
	if a.session != owner || a.currentFile() != nil || a.State.Cwd != rootA || len(a.State.PendingInteractions()) != 1 {
		t.Fatal("approval return did not restore its original conversation")
	}
}

func TestP4FileTitlesDistinguishSameNamedWorkspaces(t *testing.T) {
	a := New(stubBackend{}, "test", "/projects/one/repo", nil)
	a.tabs = []storage.Tab{
		{ID: "one", Kind: "file", Workspace: "/projects/one/repo", Path: "/projects/one/repo/src/main.go"},
		{ID: "two", Kind: "file", Workspace: "/projects/two/repo", Path: "/projects/two/repo/src/main.go"},
		{ID: "staged", Kind: "diff", Workspace: "/projects/one/repo", Path: "/projects/one/repo/src/main.go", Compare: "已暂存"},
	}
	first, second := a.tabTitle(a.tabs[0]), a.tabTitle(a.tabs[1])
	if first == second || !strings.Contains(first, "one/repo") || !strings.Contains(second, "two/repo") {
		t.Fatal("ambiguous file titles", first, second)
	}
	if staged := a.tabTitle(a.tabs[2]); staged != first+" · 已暂存" {
		t.Fatal("comparison title lost file identity", staged)
	}
}

func TestP4ClickingPartlyVisibleTabRevealsWholeTab(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	first := a.session
	a.newSession("/workspace")
	a.newSession("/workspace")
	tt := native.NewTester(func(c *native.Context) { a.sessionTabs(c, true, true) }, 450, 38)
	before, ok := tt.Find("会话标签 1")
	if !ok || before.W >= 100 || before.X+before.W <= 20 {
		t.Fatal("fixture does not contain a partly visible tab", before)
	}
	tt.ClickAt(20, 18)
	tt.Frame()
	after, ok := tt.Find("会话标签 1")
	if a.session != first || !ok || after.X < 0 || after.W < 100 {
		t.Fatal("clicked tab remained clipped", before, after, a.activeTab)
	}
}

func TestP4NarrowEditorKeepsSaveAndFilePanelSeparate(t *testing.T) {
	a, q, root := p4App(t)
	a.historyOpen, a.rightTab = true, "files"
	a.repositories = []project.Repository{{Path: root, Name: "fixture", Worktrees: []project.Worktree{{Path: root, Branch: "feature/p4"}}}}
	tt := native.NewTester(a.view, 1280, 800)
	path := filepath.Join(root, "internal", "main.go")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	for _, width := range []int{1280, 1000, 640} {
		tt.SetSize(width, 480)
		save, saveOK := tt.Find("保存")
		search, searchOK := tt.Find("查找文件")
		content, contentOK := tt.Find("文件内容 " + path)
		if !saveOK || !searchOK || !contentOK || save.X < 0 || save.X+save.W > search.X ||
			search.X+search.W > float32(width) || content.Y+content.H > 480 {
			t.Fatalf("editor controls overlap or leave the viewport at %d: save=%+v search=%+v content=%+v", width, save, search, content)
		}
		if !tt.HasText("feature/p4") {
			t.Fatal("file panel omitted the workspace branch")
		}
	}
	w := a.workspace(root)
	w.selected = path
	a.toggleFileDirectory(w, filepath.Dir(path))
	if w.selected != filepath.Dir(path) || w.expanded[filepath.Dir(path)] {
		t.Fatal("collapsed directory left its child selected")
	}
	if out := os.Getenv("FUYU_P4_NARROW_PREVIEW"); out != "" {
		tt.Frame()
		f, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
func TestP4ConflictAndSaveDuringTyping(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("mine\n")
	d.observe()
	os.WriteFile(path, []byte("external\n"), 0640)
	a.checkFile(d)
	p4Wait(t, tt, q, func() bool { return d.conflict })
	a.saveFile(d, false, false, nil)
	p4Wait(t, tt, q, func() bool { return !d.saving })
	raw, _ := os.ReadFile(path)
	if string(raw) != "external\n" || !d.dirty() {
		t.Fatal("conflict lost one version")
	}
	if !tt.HasText("查看磁盘版本") {
		t.Fatal("conflict interaction missing")
	}
}
func TestP4FileTabsPersistWithoutStartingCodex(t *testing.T) {
	a, q, root := p4App(t)
	store, e := storage.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	a.store = store
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	a.closeSession(a.session)
	if a.currentFile() == nil {
		t.Fatal("closing background chat stole file selection")
	}
	if e = a.persist(); e != nil {
		t.Fatal(e)
	}
	b := New(stubBackend{}, "test", "", nil)
	if e = b.AttachStore(store); e != nil {
		t.Fatal(e)
	}
	if b.currentFile() == nil || b.currentFile().path != path || b.currentFile().loaded || len(b.tabs) != 1 {
		t.Fatal("file restore did not remain lazy")
	}
	if b.session.open {
		t.Fatal("file restore created a chat tab")
	}
}

func TestP4EditorUndoFindAndCloseLastFile(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	d := a.currentFile()
	if e := tt.Click("文件内容 " + path); e != nil {
		t.Fatal(e)
	}
	tt.Key(native.Cmd, native.KeyA)
	tt.Type("第一行\n第二行\n")
	tt.Key(native.Cmd, native.KeyZ)
	if strings.Contains(d.buffer.String(), "第一行") {
		t.Fatal("undo did not restore document history")
	}
	tt.Key(native.Cmd|native.Shift, native.KeyZ)
	if !strings.Contains(d.buffer.String(), "第二行") {
		t.Fatal("redo lost document edit")
	}
	tt.Key(native.Cmd, native.KeyF)
	if e := tt.Click("文件内查找"); e != nil {
		t.Fatal(e)
	}
	tt.Type("第二行")
	tt.Key(0, native.KeyEnter)
	p4Wait(t, tt, q, func() bool { return d.findResult == "已定位" })
	if d.findResult != "已定位" {
		t.Fatal("find did not select result")
	}
	tt.Key(0, native.KeyEscape)
	if d.findOpen {
		t.Fatal("Escape did not close file find")
	}
	a.saveFile(d, false, false, nil)
	p4Wait(t, tt, q, func() bool { return !d.saving })
	a.closeSession(a.session)
	tt.Frame()
	tt.Key(native.Cmd, native.KeyW)
	tt.Frame()
	if len(a.tabs) != 0 || tt.HasText("任务输入") || a.currentFile() != nil {
		t.Fatal("last file close created replacement chat")
	}
}

func TestP4SaveQueueKeepsTypingAndLatestRequest(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("v1")
	d.observe()
	a.saveFile(d, false, false, nil)
	d.buffer.Set("v2")
	d.observe()
	a.saveFile(d, false, false, nil)
	p4Wait(t, tt, q, func() bool { raw, _ := os.ReadFile(path); return !d.saving && string(raw) == "v2" })
	if d.dirty() {
		t.Fatal("latest serialized save not acknowledged")
	}
}

func TestP4CloseDirtyCancelAndDiscard(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("unsaved")
	d.observe()
	a.quitPrompt = func(mygo.MessageOptions) (mygo.MessageResult, error) { return mygo.MessageResult{Button: 0}, nil }
	a.closeFile(d)
	p4Wait(t, tt, q, func() bool { return !a.quitDialog })
	if a.currentFile() != d || !d.dirty() {
		t.Fatal("cancel lost dirty file")
	}
	a.quitPrompt = func(mygo.MessageOptions) (mygo.MessageResult, error) { return mygo.MessageResult{Button: 1}, nil }
	a.closeFile(d)
	p4Wait(t, tt, q, func() bool { return !a.quitDialog })
	if a.files[d.id] != nil {
		t.Fatal("discard did not close")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) == "unsaved" {
		t.Fatal("discard saved file")
	}
}

func TestP4TreeKeyboardAndSearch(t *testing.T) {
	a, q, root := p4App(t)
	a.historyOpen = true
	a.rightTab = "files"
	tt := native.NewTester(a.view, 1100, 760)
	w := a.workspace(root)
	p4Wait(t, tt, q, func() bool { return len(w.children[root]) > 0 })
	dir := filepath.Join(root, "internal")
	if e := tt.Click("文件树 " + dir); e != nil {
		t.Fatal(e)
	}
	p4Wait(t, tt, q, func() bool { return len(w.children[dir]) > 0 })
	tt.Advance(250 * time.Millisecond)
	tt.Key(0, native.KeyRight)
	tt.Frame()
	if w.selected != filepath.Join(dir, "main.go") {
		t.Fatal("tree right did not enter directory", w.selected)
	}
	tt.Key(0, native.KeyEnter)
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	if e := tt.Click("查找文件"); e != nil {
		t.Fatal(e)
	}
	tt.Type("README")
	p4Wait(t, tt, q, func() bool { return !w.searchLoading && len(w.search.Entries) == 1 })
	tt.Key(0, native.KeyEnter)
	p4Wait(t, tt, q, func() bool {
		return a.currentFile() != nil && a.currentFile().path == filepath.Join(root, "README.md") && a.currentFile().loaded
	})
	if e := tt.Click("查找文件"); e != nil {
		t.Fatal(e)
	}
	tt.Key(0, native.KeyEscape)
	tt.Frame()
	if w.query != "" {
		t.Fatal("Escape did not clear search")
	}
}

func TestP4GitPanelDiffAndReadOnly(t *testing.T) {
	a, q, root := p4App(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(out))
		}
	}
	run("init", "-b", "main")
	run("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "--allow-empty", "-m", "fixture")
	path := filepath.Join(root, "example.go")
	os.WriteFile(path, []byte("package staged\n"), 0600)
	run("add", "--", "example.go")
	os.WriteFile(path, []byte("package disk\n"), 0600)
	a.historyOpen = true
	a.rightTab = "git"
	tt := native.NewTester(a.view, 1280, 800)
	w := a.workspace(root)
	p4Wait(t, tt, q, func() bool { return !w.gitLoading && len(w.git.Entries) > 0 })
	if e := tt.Click("Git 更改 example.go"); e != nil {
		t.Fatal(e)
	}
	p4Wait(t, tt, q, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	d := a.currentFile()
	if !strings.Contains(d.buffer.String(), "+package disk") || !strings.Contains(d.buffer.String(), "-package staged") {
		t.Fatal("wrong unstaged baseline")
	}
	before := d.buffer.String()
	if e := tt.Click("文件内容 " + path); e != nil {
		t.Fatal(e)
	}
	tt.Type("bad")
	if d.buffer.String() != before {
		t.Fatal("Diff is editable")
	}
	if e := tt.Click("Git 已暂存的更改 example.go"); e != nil {
		t.Fatal(e)
	}
	p4Wait(t, tt, q, func() bool { return a.currentFile() != d && a.currentFile().loaded })
	if !strings.Contains(a.currentFile().buffer.String(), "+package staged") {
		t.Fatal("wrong staged baseline")
	}
	tt.Advance(300 * time.Millisecond)
	if out := os.Getenv("FUYU_P4_GIT_PREVIEW"); out != "" {
		f, e := os.Create(out)
		if e != nil {
			t.Fatal(e)
		}
		e = png.Encode(f, tt.Image())
		f.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
}

func TestP4GitIndexWatcherRefreshesPanel(t *testing.T) {
	a, q, root := p4App(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	a.historyOpen, a.rightTab = true, "git"
	tt := native.NewTester(a.view, 1280, 800)
	w := a.workspace(root)
	metadata, err := filepath.EvalSymlinks(filepath.Join(root, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool {
		if w.watcher == nil || w.gitLoading || len(w.git.Entries) == 0 {
			return false
		}
		for _, dir := range w.watcher.WatchList() {
			if dir == metadata {
				return true
			}
		}
		return false
	})
	run("add", "--", "README.md")
	// No source-file write or manual refresh: only .git/index changes.
	p4Wait(t, tt, q, func() bool {
		for _, entry := range w.git.Entries {
			if entry.Path == "README.md" && entry.Staged() {
				return true
			}
		}
		return false
	})
	if _, ok := tt.Find("Git 已暂存的更改 README.md"); !ok {
		t.Fatal("metadata event did not update the rendered Git group")
	}
}

func TestP4GitBackgroundRefreshKeepsListSteady(t *testing.T) {
	a := New(nil, "test", "/repo", nil)
	a.historyOpen, a.rightTab = true, "git"
	w := a.workspace("/repo")
	w.gitLoading = true
	tt := native.NewTester(a.view, 1280, 800)
	if !tt.HasText("正在读取 Git…") {
		t.Fatal("initial Git load did not show progress")
	}
	w.git = gitview.Status{Branch: "main", Upstream: "origin/main"}
	for i := range 40 {
		w.git.Entries = append(w.git.Entries, gitview.Entry{Path: fmt.Sprintf("file-%02d.go", i), XY: ".M"})
	}
	w.gitTime = time.Now()
	w.gitFingerprint = fmt.Sprintf("%+v", w.git)
	w.collapsed["已暂存的更改"] = true
	w.gitLoading = false
	tt.Frame()
	header, ok := tt.Find("折叠 更改")
	if !ok {
		t.Fatal("missing Git group")
	}
	tt.Scroll(header.X+40, header.Y+80, 0, 180)
	if w.gitScroll.Y <= 0 {
		t.Fatal("Git list did not scroll")
	}
	row, ok := tt.Find("Git 更改 file-10.go")
	if !ok {
		t.Fatal("missing scrolled Git row")
	}
	scrollY := w.gitScroll.Y
	for i := range 6 {
		w.gitLoading = i%2 == 0
		tt.Frame()
		if tt.HasText("正在读取 Git…") {
			t.Fatal("cached background refresh inserted a loading row")
		}
		next, ok := tt.Find("Git 更改 file-10.go")
		if !ok || next != row || w.gitScroll.Y != scrollY {
			t.Fatalf("background refresh moved Git content: before=%+v after=%+v scroll=%v/%v", row, next, scrollY, w.gitScroll.Y)
		}
	}
	w.gitLoading = true
	tt.Frame()
	if !w.collapsed["已暂存的更改"] || tt.HasText("没有已暂存的更改") {
		t.Fatal("background refresh changed Git group folding")
	}
}

func TestP4ReloadDiscardsRecoveryAndUndo(t *testing.T) {
	a, q, root := p4App(t)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.store = store
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("discarded local edit")
	d.observe()
	r := editor.Recovery{ID: editor.RecoveryID(root, path), Workspace: root, Path: path, Text: d.buffer.String(), Base: d.file}
	if err := editor.WriteRecovery(a.recoveryDir(), r); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("latest disk\n"), 0640); err != nil {
		t.Fatal(err)
	}
	d.conflict = true
	a.reloadFile(d)
	p4Wait(t, tt, q, func() bool { return !d.loading })
	if d.buffer.String() != "latest disk\n" || d.dirty() || d.conflict || len(d.undo) != 0 || len(d.redo) != 0 {
		t.Fatal("reload retained discarded state")
	}
	rs, err := editor.Recoveries(a.recoveryDir())
	if err != nil || len(rs) != 0 {
		t.Fatal("discarded recovery could resurrect", rs, err)
	}
}

func TestP4RemovedExpandedDirectoryIsPruned(t *testing.T) {
	for _, refreshParent := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent=%t", refreshParent), func(t *testing.T) {
			a, q, root := p4App(t)
			w := a.workspace(root)
			gone := filepath.Join(root, "deleted-directory")
			child := filepath.Join(gone, "nested")
			// These are persisted expansions for a directory removed while closed.
			w.expanded[gone], w.expanded[child] = true, true
			w.directoryProgress = map[string]float32{gone: 1, child: 1}
			w.children[gone] = []workspace.Entry{{Path: child, Name: "nested", Dir: true}}
			w.selected = filepath.Join(child, "file.txt")
			tt := native.NewTester(a.view, 1100, 760)
			path := child
			if refreshParent {
				path = root
			}
			a.loadDirectory(w, path)
			p4Wait(t, tt, q, func() bool { return !w.loading[path] })
			if w.error != "" || w.expanded[gone] || w.expanded[child] || len(w.children[gone]) != 0 {
				t.Fatalf("deleted directory kept stale expansion or error: %q %+v", w.error, w.expanded)
			}
			if _, ok := w.directoryProgress[child]; ok || workspace.Contains(gone, w.selected) {
				t.Fatal("deleted directory retained animation or selection state")
			}
			var snapshot storage.Snapshot
			a.snapshotWorkspaceTabs(&snapshot)
			for _, path := range snapshot.ExpandedFiles[root] {
				if workspace.Contains(gone, path) {
					t.Fatal("deleted expansion persisted for the next restart")
				}
			}
		})
	}
}

func TestP4EmptyExpandedDirectoryDoesNotReloadEveryFrame(t *testing.T) {
	a, q, root := p4App(t)
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0700); err != nil {
		t.Fatal(err)
	}
	w := a.workspace(root)
	w.expanded[empty] = true
	tt := native.NewTester(a.view, 1100, 760)
	a.loadDirectory(w, root)
	a.loadDirectory(w, empty)
	p4Wait(t, tt, q, func() bool { return !w.loading[root] && !w.loading[empty] })
	a.historyOpen, a.rightTab = true, "files"
	for range 5 {
		tt.Frame()
		if w.loading[empty] {
			t.Fatal("an already loaded empty directory was requested again")
		}
	}
}

func TestP4MissingRootAndDirectoryPermissionsRemainErrors(t *testing.T) {
	for _, kind := range []string{"missing-root", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			a, q, root := p4App(t)
			path := filepath.Join(root, kind)
			w := a.workspace(path)
			if kind == "permissions" {
				if os.Geteuid() == 0 {
					t.Skip("root bypasses directory permissions")
				}
				if err := os.Mkdir(path, 0000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(path, 0700) })
				w = a.workspace(root)
				w.expanded[path] = true
			}
			tt := native.NewTester(a.view, 1100, 760)
			a.loadDirectory(w, path)
			p4Wait(t, tt, q, func() bool { return !w.loading[path] })
			if w.error == "" || !w.expanded[path] {
				t.Fatal("a missing workspace root or unreadable directory was silently discarded")
			}
		})
	}
}

func TestP4OldDiskCheckCannotReplaceSavedText(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded && !a.currentFile().highlighting })
	a.closeWorkspaceResources()
	d := a.currentFile()
	a.checkFile(d)
	var stale func()
	select {
	case stale = <-q:
	case <-time.After(5 * time.Second):
		t.Fatal("disk check callback missing")
	}
	d.buffer.Set("new saved text\n")
	d.observe()
	a.saveFile(d, false, false, nil)
	p4Wait(t, tt, q, func() bool { return !d.saving })
	stale()
	tt.Frame()
	if d.buffer.String() != "new saved text\n" || d.dirty() || d.conflict {
		t.Fatal("old disk check changed latest saved state", d.buffer.String())
	}
}

func TestP4OverwriteConfirmationRejectsLaterDiskChange(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	d := a.currentFile()
	d.buffer.Set("local draft\n")
	d.observe()
	d.conflict = true
	a.quitPrompt = func(mygo.MessageOptions) (mygo.MessageResult, error) {
		// The disk version changes after the confirmation captured its baseline.
		err := os.WriteFile(path, []byte("newer external write\n"), 0640)
		return mygo.MessageResult{Button: 1}, err
	}
	a.confirmOverwrite(d)
	p4Wait(t, tt, q, func() bool { return d.err != "" && !d.saving && !a.quitDialog })
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "newer external write\n" || d.buffer.String() != "local draft\n" || !d.dirty() || !d.conflict {
		t.Fatal("confirmation authorized an unseen disk version", string(raw), err, d.err)
	}
}

func TestP4SymlinkAndTargetKeepSeparateTabs(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	link := filepath.Join(root, "readme-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	a.openFile(root, link)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	linked := a.currentFile()
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	if a.currentFile() == linked || a.currentFile().file.Reason != "" || linked.file.Reason == "" {
		t.Fatal("link and writable target were merged")
	}
}

func TestP4FileSwitchKeepsReadingPosition(t *testing.T) {
	a, q, root := p4App(t)
	path := filepath.Join(root, "long.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("long document line\n", 1000)), 0600); err != nil {
		t.Fatal(err)
	}
	tt := native.NewTester(a.view, 1100, 760)
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded && !a.currentFile().highlighting })
	d := a.currentFile()
	bounds, ok := tt.Find("文件内容 " + path)
	if !ok {
		t.Fatal("editor not rendered")
	}
	tt.Scroll(bounds.X+80, bounds.Y+80, 0, 4000)
	tt.Advance(500 * time.Millisecond)
	position := d.scroll.Y
	if position < 3900 {
		t.Fatal("fixture did not scroll", position)
	}
	a.selectSession(a.session)
	tt.Frame()
	a.selectFile(d)
	tt.Frame()
	tt.Advance(50 * time.Millisecond)
	if d.scroll.Y < position-1 || d.scroll.Y > position+1 || d.caret != 0 {
		t.Fatal("tab switch moved reading position or caret", position, d.scroll.Y, d.caret)
	}
}

func TestP4DiskComparisonPreservesSnapshotUntilRefresh(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded })
	source := a.currentFile()
	source.buffer.Set("local version\n")
	source.observe()
	os.WriteFile(path, []byte("disk version\n"), 0640)
	a.showDiskComparison(source)
	p4Wait(t, tt, q, func() bool { return a.currentFile() != source && a.currentFile().loaded })
	d := a.currentFile()
	before := d.buffer.String()
	if !strings.Contains(before, "-local version") || !strings.Contains(before, "+disk version") || d.file.Reason == "" {
		t.Fatal("comparison is not read-only unified diff", before)
	}
	source.buffer.Set("local version two\n")
	source.observe()
	tt.Frame()
	if !d.comparisonStale || d.buffer.String() != before {
		t.Fatal("comparison silently replaced its snapshot")
	}
	if err := tt.Click("刷新比较"); err != nil {
		t.Fatal(err)
	}
	p4Wait(t, tt, q, func() bool { return !d.loading })
	if d.comparisonStale || !strings.Contains(d.buffer.String(), "-local version two") {
		t.Fatal("explicit comparison refresh failed", d.err)
	}
	// Restart restores the comparison tab identity, but not its immutable text.
	d.loaded, d.buffer = false, nil
	d.err = "比较快照未保存，请从原文件重新打开外部更改比较"
	a.selectFile(source)
	a.showDiskComparison(source)
	p4Wait(t, tt, q, func() bool { return d.loaded && !d.loading })
	if a.currentFile() != d || d.err != "" || !strings.Contains(d.buffer.String(), "-local version two") {
		t.Fatal("reopening from the source left the restored comparison unusable", d.err)
	}
}

func TestP4FindCaseAndExpiredResult(t *testing.T) {
	a, q, root := p4App(t)
	tt := native.NewTester(a.view, 1100, 760)
	path := filepath.Join(root, "README.md")
	a.openFile(root, path)
	p4Wait(t, tt, q, func() bool { return a.currentFile().loaded && !a.currentFile().highlighting })
	a.closeWorkspaceResources()
	d := a.currentFile()
	d.buffer.Set("你好 FUYU\n你好 fuyu\n")
	d.observe()
	d.findOpen, d.query = true, "fuyu"
	a.findInFile(d, false)
	p4Wait(t, tt, q, func() bool { return d.findResult == "已定位" })
	if d.anchor != 3 || d.caret != 7 {
		t.Fatal("case-insensitive Unicode selection", d.anchor, d.caret)
	}
	d.findCase, d.anchor, d.caret = true, 0, 0
	a.findInFile(d, false)
	p4Wait(t, tt, q, func() bool { return d.findResult == "已定位" })
	if d.anchor != 11 || d.caret != 15 {
		t.Fatal("case-sensitive selection", d.anchor, d.caret)
	}
	// Prevent the old worker result from selecting text after another edit.
	d.pendingSelection = nil
	a.findInFile(d, false)
	var result func()
	select {
	case result = <-q:
	case <-time.After(5 * time.Second):
		t.Fatal("find callback missing")
	}
	d.buffer.Set("replacement")
	d.observe()
	result()
	if d.pendingSelection != nil {
		t.Fatal("stale find result selected replaced text")
	}
}

func TestP4TreeAnimationAndReducedMotion(t *testing.T) {
	root := "/workspace"
	a := New(nil, "test", root, nil)
	a.historyOpen, a.rightTab = true, "files"
	w := a.workspace(root)
	dir, file := root+"/dir", root+"/dir/file.go"
	w.children[root] = []workspace.Entry{{Path: dir, Name: "dir", Dir: true}}
	w.children[dir] = []workspace.Entry{{Path: file, Name: "file.go"}}
	w.expanded[dir] = true
	tt := native.NewTester(a.view, 1100, 760)
	if err := tt.Click("文件树 " + dir); err != nil {
		t.Fatal(err)
	}
	tt.Advance(60 * time.Millisecond)
	if progress := w.directoryProgress[dir]; progress <= 0 || progress >= 1 {
		t.Fatal("collapse skipped intermediate frame", progress)
	}
	tt.Advance(250 * time.Millisecond)
	if w.directoryProgress[dir] != 0 || tt.HasText("file.go") {
		t.Fatal("collapse left children visible")
	}
	if err := tt.Click("文件树 " + dir); err != nil {
		t.Fatal(err)
	}
	tt.Advance(60 * time.Millisecond)
	if progress := w.directoryProgress[dir]; progress <= 0 || progress >= 1 {
		t.Fatal("expand skipped intermediate frame", progress)
	}
	tt.Advance(250 * time.Millisecond)
	if !tt.HasText("file.go") {
		t.Fatal("expand did not reveal children")
	}
	tt.SetPreferences(native.Preferences{ReduceMotion: true, TextScale: 1})
	if err := tt.Click("文件树 " + dir); err != nil {
		t.Fatal(err)
	}
	if w.directoryProgress[dir] != 0 || tt.HasText("file.go") {
		t.Fatal("reduced motion was not immediate")
	}
}

func TestP4SearchPreservesTreeScrollAndRejectsOldRows(t *testing.T) {
	a, _, root := p4App(t)
	a.historyOpen, a.rightTab = true, "files"
	w := a.workspace(root)
	for i := range 100 {
		name := fmt.Sprintf("file%03d.go", i)
		w.children[root] = append(w.children[root], workspace.Entry{Path: filepath.Join(root, name), Name: name})
	}
	tt := native.NewTester(a.view, 1100, 760)
	w.list.ScrollTo(70, native.Start)
	tt.Frame()
	first, _ := w.list.Visible()
	if first < 60 {
		t.Fatal("tree fixture did not scroll", first)
	}
	w.query, w.searchQuery = "first-query", "first-query"
	w.search.Entries = []workspace.Entry{{Path: filepath.Join(root, "README.md"), Name: "README.md"}}
	tt.Frame()
	w.query = "different-query"
	a.searchFiles(w)
	if len(w.search.Entries) != 0 || !w.searchLoading {
		t.Fatal("old search result remains actionable")
	}
	if err := tt.Click("查找文件"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, native.KeyEnter)
	if a.currentFile() != nil {
		t.Fatal("Enter opened a stale search row")
	}
	w.query = ""
	a.searchFiles(w)
	tt.Frame()
	after, _ := w.list.Visible()
	if after != first {
		t.Fatal("cancel search lost tree position", first, after)
	}
}

func TestP4WorkspaceSwitchCancelsQueuedDirectoryAndSearch(t *testing.T) {
	a, q, root := p4App(t)
	a.selectWorkspace(root)
	a.fileWorkers = make(chan struct{}, 2)
	a.fileWorkers <- struct{}{}
	a.fileWorkers <- struct{}{}
	w := a.workspace(root)
	a.loadDirectory(w, root)
	w.query = "README"
	a.searchFiles(w)
	a.selectWorkspace(t.TempDir())
	for range 2 {
		select {
		case fn := <-q:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("canceled reads waited for occupied workers")
		}
	}
	if w.loading[root] || w.searchLoading || len(w.children[root]) != 0 || len(w.search.Entries) != 0 || w.error != "" {
		t.Fatal("obsolete workspace read changed visible state")
	}
	<-a.fileWorkers
	<-a.fileWorkers
	a.selectWorkspace(root)
	a.loadDirectory(w, root)
	select {
	case fn := <-q:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("returning workspace could not load its directory")
	}
	if w.loading[root] || len(w.children[root]) == 0 || w.error != "" {
		t.Fatal("fresh directory result was lost after cancellation", w.error)
	}
}

func TestP4WorkspaceSwitchCancelsQueuedFileAndGitReads(t *testing.T) {
	a, q, root := p4App(t)
	a.selectWorkspace(root)
	a.fileWorkers, a.gitWorkers = make(chan struct{}, 2), make(chan struct{}, 2)
	for range 2 {
		a.fileWorkers <- struct{}{}
		a.gitWorkers <- struct{}{}
	}
	path := filepath.Join(root, "README.md")
	d := &filePage{id: "queued-file", root: root, path: path, kind: "file"}
	diff := &filePage{id: "queued-diff", root: root, path: path, kind: "diff"}
	a.files = map[string]*filePage{d.id: d, diff.id: diff}
	w := a.workspace(root)
	a.loadFile(d)
	a.refreshGit(w)
	a.loadGitDiff(diff, gitview.Entry{Path: "README.md", XY: "??"}, false)
	a.selectWorkspace(t.TempDir())
	for range 3 {
		select {
		case fn := <-q:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("canceled reads waited for occupied workers")
		}
	}
	if d.loading || d.loaded || d.err != "" || diff.loading || diff.loaded || diff.err != "" ||
		w.gitLoading || w.gitError != "" || !w.gitTime.IsZero() {
		t.Fatal("canceled reads published data/errors or left loading state")
	}
	for range 2 {
		<-a.fileWorkers
		<-a.gitWorkers
	}
	a.selectWorkspace(root)
	a.loadFile(d)
	select {
	case fn := <-q:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("returning file could not be loaded")
	}
	if !d.loaded || d.loading || d.buffer.String() != "# Fuyu\n原生工作区编辑器\n" {
		t.Fatal("fresh file result was lost after cancellation", d.err)
	}
}

func TestP4CanceledGitResultDoesNotClearNewRequest(t *testing.T) {
	a, q, root := p4App(t)
	w := a.workspace(root)
	a.gitWorkers = make(chan struct{}, 2)
	a.gitWorkers <- struct{}{}
	a.gitWorkers <- struct{}{}
	a.refreshGit(w)
	old := w.gitContext
	a.cancelGitRefresh(w)
	a.refreshGit(w)
	current := w.gitContext
	if current == old || current == nil {
		t.Fatal("fresh request did not receive its own identity")
	}
	select {
	case fn := <-q:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("old canceled request did not leave queue")
	}
	if !w.gitLoading || w.gitContext != current || w.gitError != "" {
		t.Fatal("obsolete callback cleared replacement request")
	}
	a.cancelGitRefresh(w)
	select {
	case fn := <-q:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("replacement request could not be canceled")
	}
	<-a.gitWorkers
	<-a.gitWorkers
}

func TestP4FileOpenKeepsSlotWhileBackgroundQueueIsBusy(t *testing.T) {
	a, q, root := p4App(t)
	a.selectWorkspace(root)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	a.runBackgroundFileTaskContext(context.Background(), func() {
		close(started)
		<-release
		close(finished)
	})
	defer func() {
		close(release)
		<-finished
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("background task did not start")
	}
	w := a.workspace(root)
	a.loadDirectory(w, root)
	d := &filePage{id: "priority-file", root: root, path: filepath.Join(root, "README.md"), kind: "file"}
	a.files = map[string]*filePage{d.id: d}
	a.loadFile(d)
	select {
	case fn := <-q:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("explicit file open waited behind background directory request")
	}
	if !d.loaded || d.loading || !w.loading[root] || len(w.children[root]) != 0 {
		t.Fatal("file open did not get the reserved slot", d.err)
	}
	a.selectWorkspace(t.TempDir())
}
