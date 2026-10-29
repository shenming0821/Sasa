package ui

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/storage"
)

func TestDeleteLastSessionKeepsActiveUnsavedFile(t *testing.T) {
	a, queue, root := p4App(t)
	target := a.session
	tt := native.NewTester(a.view, 1280, 800)
	a.openFile(root, filepath.Join(root, "internal", "main.go"))
	p4Wait(t, tt, queue, func() bool { return a.currentFile() != nil && a.currentFile().loaded })
	file := a.currentFile()
	file.modified = true
	a.deleteSession(target)
	if a.currentFile() != file || !file.modified || slices.Contains(a.sessions, target) {
		t.Fatal("deleting the last chat changed the active file")
	}
}

func TestDeleteClaudeSessionKeepsOtherAgentAndRuntimeHistory(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	codex := a.session
	codex.State.ThreadID = "shared"
	a.newSession("/project")
	claude := a.session
	claude.kind, claude.runtimeKey, claude.State.ThreadID = agent.Claude, "claude-home", "shared"
	a.deleteSession(claude)
	if !slices.Contains(a.sessions, codex) || slices.Contains(a.sessions, claude) || !a.threadDeleted(agent.Claude, "claude-home", "shared") || a.threadDeleted(agent.Codex, "", "shared") || a.threadDeleted(agent.Claude, "other-home", "shared") {
		t.Fatal("Claude deletion crossed history boundaries")
	}
}

func TestDeleteSessionRemovesTabsAndSurvivesRestart(t *testing.T) {
	for _, scenario := range []string{"selected", "background", "closed", "last"} {
		t.Run(scenario, func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			a := New(stubBackend{}, "test", "/project", nil)
			a.store = store
			target := a.session
			target.State.ThreadID, target.State.Draft = "deleted-thread", "deleted-draft"
			target.saved.Title = "删除目标"
			if scenario != "last" {
				a.newSession("/project")
				a.State.Draft = "保留草稿"
				if scenario == "selected" {
					a.selectSession(target)
				}
				if scenario == "closed" {
					a.closeSession(target)
				}
			}
			if err := a.persist(); err != nil {
				t.Fatal(err)
			}
			a.deleteSession(target)
			if slices.Contains(a.sessions, target) || a.session == target || a.sessionDeleteError != "" {
				t.Fatal("deleted session remained selected or indexed", a.sessionDeleteError)
			}
			for _, tab := range a.tabs {
				if tab.Kind == "session" && tab.SessionID == target.id {
					t.Fatal("deleted tab remained open")
				}
			}
			if scenario != "last" && a.State.Draft != "保留草稿" {
				t.Fatal("other session draft changed")
			}
			restarted := New(stubBackend{}, "test", "", nil)
			if err := restarted.AttachStore(store); err != nil {
				t.Fatal(err)
			}
			for _, remaining := range restarted.sessions {
				if remaining.State.ThreadID == "deleted-thread" || remaining.State.Draft == "deleted-draft" {
					t.Fatal("deleted data returned on restart")
				}
			}
			if !restarted.threadDeleted(agent.Codex, "", "deleted-thread") || restarted.threadDeleted(agent.Claude, "", "deleted-thread") || restarted.threadDeleted(agent.Codex, "other", "deleted-thread") {
				t.Fatal("history filter lost or crossed agent/runtime boundary")
			}
		})
	}
}

func TestDeleteSessionConfirmationAndRecheck(t *testing.T) {
	for _, scenario := range []string{"cancel", "confirm", "became-busy", "identity-changed", "dialog-error"} {
		t.Run(scenario, func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			target := a.session
			target.State.ThreadID = "original"
			queue := make(chan func(), 1)
			a.dispatch = func(fn func()) { queue <- fn }
			a.quitPrompt = func(options mygo.MessageOptions) (mygo.MessageResult, error) {
				if options.DefaultButton != 0 || options.CancelButton != 1 || !slices.Equal(options.Buttons, []string{"删除会话", "取消"}) {
					return mygo.MessageResult{}, errors.New("unsafe confirmation defaults")
				}
				if scenario == "dialog-error" {
					return mygo.MessageResult{}, errors.New("dialog unavailable")
				}
				button := 0
				if scenario == "cancel" {
					button = 1
				}
				return mygo.MessageResult{Button: button}, nil
			}
			a.confirmDeleteSession(target)
			if scenario == "became-busy" {
				target.State.Busy = true
			}
			if scenario == "identity-changed" {
				target.State.ThreadID = "changed"
			}
			pumpUpdate(t, queue)
			if slices.Contains(a.sessions, target) != (scenario != "confirm") || a.quitDialog {
				t.Fatal("confirmation did not respect result/current state")
			}
			if (scenario == "dialog-error" || scenario == "identity-changed" || scenario == "became-busy") && a.sessionDeleteError == "" {
				t.Fatal("failure was not visible")
			}
		})
	}
}

func TestDeleteSessionProtectsPendingWork(t *testing.T) {
	for _, state := range []string{"busy", "recovering", "uncertain", "forking", "saved-active", "images", "blocked"} {
		t.Run(state, func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			target := a.session
			switch state {
			case "busy":
				target.State.Busy = true
			case "recovering":
				target.recovering = true
			case "uncertain":
				target.reviewRequired = true
			case "forking":
				target.forking = true
			case "saved-active":
				target.saved.Active = true
			case "images":
				target.imagesImporting = true
			case "blocked":
				target.recoveryBlocked = true
			}
			a.deleteSession(target)
			if !slices.Contains(a.sessions, target) {
				t.Fatal("pending work was deleted")
			}
		})
	}
}

func TestDeleteSessionFailureKeepsOriginalState(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(stubBackend{}, "test", "/project", nil)
	a.store = store
	target := a.session
	target.State.ThreadID = "original"
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	store.Close()
	a.deleteSession(target)
	if a.session != target || !target.open || !slices.Contains(a.sessions, target) || len(a.deletedThreads) != 0 || a.sessionDeleteError == "" {
		t.Fatal("failed deletion altered original state")
	}
}

func TestDeleteHistoryMenusFilterAndLateOpen(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	a.nativeThreads = []agent.ThreadMeta{{ID: "delete", Cwd: "/project", Preview: "删除历史"}, {ID: "keep", Cwd: "/project", Preview: "保留历史"}}
	a.historyOpen = true
	tt := native.NewTester(a.view, 1400, 800)
	if tt.HasText("历史会话操作 delete") || tt.HasText("历史会话操作 keep") {
		t.Fatal("history rows should expose actions through the context menu")
	}
	if err := tt.RightClick("打开历史 delete"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tt.Menu(), []string{"重命名", "删除会话"}) {
		t.Fatal("history delete menu missing", tt.Menu())
	}
	tt.Key(0, native.KeyEscape)
	target := a.historySession(a.nativeThreads[0])
	a.newSession("/project")
	opened := a.session
	opened.State.ThreadID = "delete"
	opened.State.Busy = true
	a.deleteSession(target)
	if !slices.Contains(a.sessions, opened) {
		t.Fatal("late opened running session was deleted")
	}
	opened.State.Busy = false
	a.deleteSession(target)
	tt.Frame()
	if tt.HasText("打开历史 delete") || !tt.HasText("打开历史 keep") || slices.Contains(a.sessions, opened) {
		t.Fatal("deleted history returned or unrelated history disappeared")
	}
	if err := tt.RightClick("会话标签 1"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tt.Menu(), []string{"重命名", "删除会话"}) {
		t.Fatal("tab delete menu missing")
	}
}
