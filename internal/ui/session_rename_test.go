package ui

import (
	"slices"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
)

func TestSessionRenameMenuSaveCancelAndRestart(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	store := imageStore(t, a)
	target := a.session
	target.saved.Title = "原始标题"
	target.State.Draft = "保留草稿"
	tt := native.NewTester(a.view, 1400, 800)
	openRename := func() {
		t.Helper()
		if err := tt.RightClick("会话标签 1"); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(tt.Menu(), []string{"重命名", "删除会话"}) {
			t.Fatal("missing session actions", tt.Menu())
		}
		if err := tt.ChooseMenuItem("重命名"); err != nil {
			t.Fatal(err)
		}
		if !a.sessionRenameOpen || a.sessionRenameDraft != target.title() {
			t.Fatal("title was not prefilled")
		}
	}
	typeName := func(name string) {
		t.Helper()
		if err := tt.Click("会话标题"); err != nil {
			t.Fatal(err)
		}
		tt.Key(native.Cmd, native.KeyA)
		tt.Type(name)
	}
	for _, cancel := range []string{"button", "escape", "close", "outside"} {
		openRename()
		typeName("取消的标题")
		switch cancel {
		case "button":
			if err := tt.Click("取消重命名"); err != nil {
				t.Fatal(err)
			}
		case "escape":
			tt.Key(0, native.KeyEscape)
		case "close":
			tt.Key(native.Cmd, native.KeyW)
		case "outside":
			tt.ClickAt(10, 10)
		}
		if a.sessionRenameOpen || target.title() != "原始标题" || !target.open {
			t.Fatal("cancel changed title or closed session", cancel)
		}
	}
	openRename()
	typeName("  自定义会话标题  ")
	tt.Key(0, native.KeyEnter)
	tt.Frame()
	if a.sessionRenameOpen || target.title() != "自定义会话标题" || !tt.HasText("自定义会话标题") {
		t.Fatal("Enter did not update visible title", a.sessionRenameError)
	}
	if a.session != target || target.State.Draft != "保留草稿" || target.State.Cwd != "/project" || len(a.sessions) != 1 {
		t.Fatal("rename changed session identity or draft")
	}
	restored := New(stubBackend{}, "test", "/project", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.title() != target.title() || restored.State.Draft != "保留草稿" {
		t.Fatal("restart lost title or draft")
	}
	if err := restored.saveSessionName(restored.session, "新会话"); err != nil || restored.title() != "新会话" {
		t.Fatal("explicit default title was ignored", err)
	}
}

func TestSessionRenameHistorySearchAndIdentity(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			a.kind, a.runtimeKey = kind, "runtime-a"
			a.historyCacheKey = agent.CacheKey{Agent: kind, Runtime: "runtime-a", Cwd: "/project"}
			store := imageStore(t, a)
			original := a.session
			a.nativeThreads = []agent.ThreadMeta{{ID: "history", Cwd: "/project", Preview: "原历史标题"}}
			a.historyOpen = true
			a.rightTab = "history"
			tt := native.NewTester(a.view, 1400, 800)
			if err := tt.RightClick("打开历史 history"); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem("重命名"); err != nil {
				t.Fatal(err)
			}
			a.sessionRenameDraft = "重命名历史"
			if err := tt.Click("保存标题"); err != nil {
				t.Fatal(err)
			}
			target := a.historySession(a.nativeThreads[0])
			if a.sessionRenameOpen || target.id == 0 || target.open || a.session != original || len(a.sessions) != 2 {
				t.Fatal("rename opened or duplicated history", a.sessionRenameError)
			}
			a.nativeThreads[0].Preview = "刷新后的原生标题"
			a.historyQuery = "重命名历史"
			tt.Frame()
			if !tt.HasText("重命名历史") {
				t.Fatal("refresh or search lost custom title")
			}
			a.runtimeKey = "runtime-b"
			if a.historySession(a.nativeThreads[0]).saved.CustomTitle != "" {
				t.Fatal("title crossed runtime identity")
			}
			a.runtimeKey = "runtime-a"
			if kind == agent.Codex {
				a.kind = agent.Claude
			} else {
				a.kind = agent.Codex
			}
			if a.historySession(a.nativeThreads[0]).saved.CustomTitle != "" {
				t.Fatal("title crossed agent identity")
			}
			a.kind = kind
			restored := New(stubBackend{}, "test", "/project", nil)
			if err := restored.AttachStore(store); err != nil {
				t.Fatal(err)
			}
			restored.kind, restored.runtimeKey = kind, "runtime-a"
			if restored.historySession(a.nativeThreads[0]).title() != "重命名历史" {
				t.Fatal("restart lost unopened history title")
			}
			if err := tt.Click("打开历史 history"); err != nil {
				t.Fatal(err)
			}
			if a.session != target || !target.open || len(a.sessions) != 2 {
				t.Fatal("opening renamed history created a duplicate")
			}
		})
	}
}

func TestSessionRenameValidationAndPersistenceFailure(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	store := imageStore(t, a)
	target := a.session
	target.saved.Title = "原始标题"
	for _, name := range []string{"", "  ", "多行\n标题", "控制\x00字符"} {
		if err := a.saveSessionName(target, name); err == nil || target.title() != "原始标题" {
			t.Fatal("invalid title accepted", name)
		}
	}
	target.State.Busy = true
	if err := a.saveSessionName(target, "运行中改名"); err != nil || !target.State.Busy {
		t.Fatal("rename changed active task", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	a.beginSessionRename(target)
	a.sessionRenameDraft = "无法保存"
	tt := native.NewTester(a.view, 1400, 800)
	if err := tt.Click("保存标题"); err != nil {
		t.Fatal(err)
	}
	if !a.sessionRenameOpen || a.sessionRenameDraft != "无法保存" || a.sessionRenameError == "" || target.title() != "运行中改名" {
		t.Fatal("failure did not retain original title and edit")
	}
	history := a.historySession(agent.ThreadMeta{ID: "unopened", Cwd: "/project", Preview: "未打开历史"})
	previousID := a.nextSession
	if err := a.saveSessionName(history, "导入失败"); err == nil || history.id != 0 || len(a.sessions) != 1 || a.nextSession != previousID {
		t.Fatal("failed rename left a new history record")
	}
	a.store = nil
	a.sessions = nil
	if err := a.saveSessionName(target, "已删除"); err == nil {
		t.Fatal("removed session was renamed")
	}
}
