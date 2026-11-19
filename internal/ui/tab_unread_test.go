package ui

import (
	"encoding/json"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/storage"
)

func TestUnreadReplyBelongsToBackgroundSession(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(kind.String(), func(t *testing.T) {
			a := New(stubBackend{}, "test", "/project", nil)
			first := a.session
			first.kind, first.State.Agent = kind, kind
			first.saved.CustomTitle = "后台任务"
			first.State.Begin("执行任务")
			first.State.ThreadID, first.State.TurnID = "thread", "turn"
			first.State.Phase = "正在执行"
			a.newSession("/project")
			second := a.session
			second.State.ThreadID, second.State.TurnID = "thread", "turn"
			a.windowFocused = func() bool { return true }
			tester := native.NewTester(a.view, 1200, 760)
			beforeTitle, _ := tester.Find("后台任务")
			beforeClose, _ := tester.Find("关闭会话标签 1")
			apply := func(method, params string) {
				t.Helper()
				a.applyBatch([]agent.Event{{
					Session: agent.SessionKey{Agent: kind}, Method: method, Params: json.RawMessage(params),
				}})
			}
			apply("item/agentMessage/delta", `{"threadId":"other","turnId":"turn","itemId":"reply","delta":"错误归属"}`)
			apply("item/agentMessage/delta", `{"threadId":"thread","turnId":"stale","itemId":"reply","delta":"过期回复"}`)
			apply("item/started", `{"threadId":"thread","turnId":"turn","item":{"id":"tool","type":"commandExecution","command":"pwd"}}`)
			if first.saved.Unread {
				t.Fatal("foreign, stale or tool events created an unread reply")
			}
			apply("item/agentMessage/delta", `{"threadId":"thread","turnId":"turn","itemId":"reply","delta":"任务结果"}`)
			tester.Frame()
			if !first.saved.Unread || second.saved.Unread || !tester.HasText("未读回复") {
				t.Fatal("background reply did not mark only its owning session")
			}
			if title, _ := tester.Find("后台任务"); title != beforeTitle {
				t.Fatal("unread indicator moved the title")
			}
			if close, _ := tester.Find("关闭会话标签 1"); close != beforeClose {
				t.Fatal("unread indicator moved the close button")
			}
			if got := a.tabTooltip(a.tabs[0]); got != "后台任务\n有未读回复" {
				t.Fatalf("tooltip lost unread status: %q", got)
			}
			apply("turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)
			tester.Frame()
			if !first.saved.Unread || !tester.HasText("未读回复") {
				t.Fatal("completion cleared an unseen reply")
			}
			if err := tester.Click("会话标签 1"); err != nil {
				t.Fatal(err)
			}
			tester.Frame()
			if first.saved.Unread || tester.HasText("未读回复") {
				t.Fatal("opening the session did not clear the unread indicator")
			}
			a.selectSession(second)
			apply("item/completed", `{"threadId":"thread","turnId":"turn","item":{"id":"reply","type":"agentMessage","text":"任务结果"}}`)
			if first.saved.Unread {
				t.Fatal("duplicate reply completion created another unread indicator")
			}
		})
	}
}

func TestUnreadReplyRespectsWindowFocus(t *testing.T) {
	a := New(stubBackend{}, "test", "/project", nil)
	a.State.Begin("执行任务")
	a.State.ThreadID, a.State.TurnID = "thread", "turn"
	focused := true
	a.windowFocused = func() bool { return focused }
	tester := native.NewTester(a.view, 1200, 760)
	apply := func(text string) {
		params, _ := json.Marshal(map[string]string{
			"threadId": "thread", "turnId": "turn", "itemId": "reply", "delta": text,
		})
		a.applyBatch([]agent.Event{{Method: "item/agentMessage/delta", Params: params}})
		tester.Frame()
	}
	apply("正在阅读")
	if a.saved.Unread {
		t.Fatal("visible foreground reply marked unread")
	}
	focused = false
	apply("后台收到")
	if !a.saved.Unread {
		t.Fatal("reply received while the window was inactive was marked read")
	}
	tester.Frame()
	if !a.saved.Unread {
		t.Fatal("background rendering cleared unread status")
	}
	focused = true
	a.settingsOpen = true
	tester.Frame()
	if !a.saved.Unread {
		t.Fatal("settings overlay cleared unread status")
	}
	a.settingsOpen = false
	tester.Frame()
	if a.saved.Unread {
		t.Fatal("returning to the conversation did not mark its reply read")
	}
}

func TestUnreadReplySurvivesStoreRestore(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := New(stubBackend{}, "test", "/project", nil)
	a.saved.CustomTitle = "后台任务"
	a.saved.Unread = true
	a.newSession("/project")
	if err := store.Save(a.snapshot()); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "/project", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	tester := native.NewTester(restored.view, 1200, 760)
	if !restored.sessions[0].saved.Unread || !tester.HasText("未读回复") {
		t.Fatal("restart lost the background unread indicator")
	}
	if err := tester.Click("会话标签 1"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := store.Save(restored.snapshot()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Sessions[0].Unread {
		t.Fatal("read status was not persisted")
	}
}
