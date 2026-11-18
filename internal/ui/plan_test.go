package ui

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
	"github.com/shenming0821/Sasa/internal/storage"
)

func TestPlanSlashBadgeHoverAndExitKeepDraft(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dark=%v", kind, dark), func(t *testing.T) {
				a := completionTestApp()
				a.kind = kind
				sent := 0
				a.submit = func() { sent++ }
				tt := native.NewTester(a.view, 800, 500)
				tt.SetDark(dark)
				tt.Click("任务输入")
				tt.Type("/pla")
				if _, ok := tt.Find("候选 /plan"); !ok {
					t.Fatal("plan completion missing")
				}
				tt.Key(0, native.KeyEnter)
				tt.Frame()
				if sent != 0 || a.selectedMode != agent.ModePlan || a.State.Draft != "" || len(a.State.Messages) != 0 {
					t.Fatal("local command submitted or did not enter plan mode")
				}
				badge, ok := tt.Find("退出计划模式")
				if !ok || !tt.HasText("计划") {
					t.Fatal("plan badge missing")
				}
				savePlanFrame(t, tt, fmt.Sprintf("%s-%v-idle", kind, dark))
				before := tt.Image().RGBAAt(int(badge.X+badge.W/2), int(badge.Y+badge.H/2))
				tt.Move(badge.X+badge.W/2, badge.Y+badge.H/2)
				tt.Frame()
				tt.Advance(150 * time.Millisecond)
				savePlanFrame(t, tt, fmt.Sprintf("%s-%v-hover", kind, dark))
				after := tt.Image().RGBAAt(int(badge.X+badge.W/2), int(badge.Y+badge.H/2))
				hover, _ := tt.Find("退出计划模式")
				if before == after || hover != badge {
					t.Fatal("hover did not change the glyph or changed badge geometry", before, after)
				}
				tt.Click("任务输入")
				tt.Type("保留的草稿")
				if err := tt.Click("退出计划模式"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if a.selectedMode != agent.ModeDefault || a.State.Draft != "保留的草稿" || sent != 0 {
					t.Fatal("exit lost draft or sent a task")
				}
				if _, visible := tt.Find("退出计划模式"); visible {
					t.Fatal("badge stayed after leaving plan mode")
				}
			})
		}
	}
}

func savePlanFrame(t *testing.T, tt *native.Tester, name string) {
	t.Helper()
	dir := os.Getenv("SASA_PLAN_UI_ARTIFACTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestPlanModePersistsPerSessionAndBlocksUnsafeSwitch(t *testing.T) {
	a := New(stubBackend{}, "test", "/one", nil)
	store := imageStore(t, a)
	owner := a.session
	if !a.setPlanMode(true) {
		t.Fatal("could not enter plan mode")
	}
	a.newSession("/two")
	if a.selectedMode != "" {
		t.Fatal("mode leaked into new session")
	}
	a.selectSession(owner)
	a.State.Draft = "草稿"
	if err := a.persist(); err != nil {
		t.Fatal(err)
	}
	reopened := New(stubBackend{}, "test", "/one", nil)
	if err := reopened.AttachStore(store); err != nil || reopened.selectedMode != agent.ModePlan || reopened.State.Draft != "草稿" {
		t.Fatal("mode or draft lost on restart", err)
	}
	reopened.State.Busy = true
	reopened.State.Draft = "/plan"
	reopened.send()
	if reopened.selectedMode != agent.ModePlan || len(reopened.pendingInputs) != 0 {
		t.Fatal("busy /plan toggled mode or queued a command")
	}
	reopened.State.Busy = false
	reopened.pendingInputs = []storage.PendingInput{{ID: "queued", Status: "paused", Options: agent.TurnOptions{Mode: agent.ModePlan}}}
	if reopened.setPlanMode(false) || reopened.selectedMode != agent.ModePlan {
		t.Fatal("mode switch changed pending-message semantics")
	}
}

func TestExecuteLatestPlanUsesDefaultModeAndSubmitsOnce(t *testing.T) {
	backend := &modelCaptureBackend{request: make(chan modelRequest, 4)}
	a := New(backend, "test", "/workspace", nil)
	a.selectedMode = agent.ModePlan
	a.State.ThreadID, a.State.TurnID, a.State.Phase = "thread", "turn", "本轮已完成"
	m := &app.Message{ID: "plan", Role: "assistant", Text: "实施方案", TurnID: "turn", Plan: true, Done: true}
	a.State.Messages = []*app.Message{m}
	updates := make(chan func(), 8)
	a.dispatch = func(fn func()) { updates <- fn }
	a.State.Draft = "尚未发送的意见"
	a.executePlan(m)
	select {
	case <-backend.request:
		t.Fatal("draft was ignored while executing")
	default:
	}
	a.State.Draft = ""
	a.executePlan(m)
	select {
	case request := <-backend.request:
		if request.options.Mode != agent.ModeDefault || a.selectedMode != agent.ModeDefault {
			t.Fatal("execution stayed in plan mode")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("execution was not submitted")
	}
	select {
	case update := <-updates:
		update()
	case <-time.After(3 * time.Second):
		t.Fatal("submission was not completed")
	}
	a.executePlan(m)
	select {
	case <-backend.request:
		t.Fatal("duplicate execution")
	default:
	}
	a.State.Busy = false
	a.selectedMode = agent.ModePlan
	if a.canExecutePlan(m) {
		t.Fatal("already executed plan became executable")
	}
	a.executedPlanID = ""
	a.State.Messages = append(a.State.Messages, &app.Message{ID: "later", Role: "user", Text: "新的要求"})
	if a.canExecutePlan(m) {
		t.Fatal("old plan stayed executable after a new request")
	}
}

func TestPlanExecuteIconSharesCopyToolbar(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dark=%v", kind, dark), func(t *testing.T) {
				a := New(stubBackend{}, "test", "/workspace", nil)
				if kind == agent.Claude {
					if err := a.RegisterBackend(kind, "", &stubBackend{}); err != nil {
						t.Fatal(err)
					}
				}
				a.kind = kind
				a.selectedMode = agent.ModePlan
				a.State.ThreadID, a.State.TurnID, a.State.Phase = "thread", "turn", "本轮已完成"
				m := &app.Message{ID: "plan", Role: "assistant", Text: "实施方案", TurnID: "turn", Plan: true, Done: true}
				a.State.Messages = []*app.Message{m}
				tt := native.NewTester(func(c *native.Context) { a.message(c, m) }, 640, 240)
				tt.SetDark(dark)
				execute, found := tt.Find("开始执行")
				copy, copyFound := tt.Find("复制回复")
				if !found || !copyFound || execute.Y != copy.Y || execute.W != copy.W || execute.H != copy.H || execute.X+execute.W+8 != copy.X {
					t.Fatal("execute and copy must share one toolbar with equal targets and an 8 DIP gap", execute, copy)
				}
				if _, found := tt.Find("继续完善"); found {
					t.Fatal("redundant refine action is still visible")
				}
				savePlanFrame(t, tt, fmt.Sprintf("execute-%s-%v", kind, dark))
				if err := tt.Click("复制回复"); err != nil || tt.Clipboard() != m.Text {
					t.Fatal("copy action changed", err)
				}
				a.State.Draft = "补充意见"
				if err := tt.Click("开始执行"); err != nil {
					t.Fatal(err)
				}
				if a.State.Draft != "补充意见" || a.selectedMode != agent.ModePlan || a.executedPlanID != "" || a.State.Error == "" {
					t.Fatal("icon bypassed the unsent draft guard")
				}
				a.State.Messages = append(a.State.Messages, &app.Message{ID: "next", Role: "user", Text: "补充意见"})
				tt.Frame()
				if _, visible := tt.Find("开始执行"); visible {
					t.Fatal("old plan still exposes execution after a new request")
				}
			})
		}
	}
}

func TestPlanQueueCannotSteerAcrossModes(t *testing.T) {
	backend := &followupBackend{calls: make(chan followupCall, 1), reply: make(chan error, 1)}
	a := activeFollowupApp(backend)
	a.activeMode = agent.ModePlan
	a.pendingInputs = []storage.PendingInput{{ID: "next", Text: "实施", Status: "queued", Options: agent.TurnOptions{Mode: agent.ModeDefault}}}
	a.drainInputs()
	select {
	case call := <-backend.calls:
		t.Fatal("execution steered into a planning turn", call)
	default:
	}
}

func TestPlanApprovalShowsPlanAndKeepsExpiredActionsInactive(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	a.State.Busy, a.State.ThreadID, a.State.TurnID = true, "thread", "turn"
	request := &app.Interaction{
		Status: "待处理",
		Request: agent.Interaction{
			ID: json.RawMessage(`"approval"`), Method: "item/commandExecution/requestApproval",
			Kind: "ExitPlanMode", Plan: "# 待确认方案\n\n修改输入区并验证。",
		},
	}
	tt := native.NewTester(func(c *native.Context) { a.interactionCard(c, request) }, 640, 400)
	if !tt.HasText("待确认方案") {
		t.Fatal("plan content missing")
	}
	for _, label := range []string{"开始执行", "继续完善"} {
		if _, visible := tt.Find(label); !visible {
			t.Fatal("plan action missing", label)
		}
	}
	if tt.HasText("需要批准命令") {
		t.Fatal("plan treated as a generic command")
	}
	request.Status = "已失效"
	tt.Frame()
	if _, visible := tt.Find("开始执行"); visible {
		t.Fatal("expired plan approval can still be accepted")
	}
}
