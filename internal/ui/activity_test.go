package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/app"
	"github.com/shenming0821/Sasa/internal/codex"
)

func TestSubmissionFailuresSeparateRejectedCreationFromUncertainExecution(t *testing.T) {
	for _, tc := range []struct {
		name      string
		submitted bool
		err       error
		retry     bool
	}{
		{"creation rejected", false, &codex.RPCError{Code: -32600, Message: "cloud config timed out"}, true},
		{"creation response lost", false, context.DeadlineExceeded, false},
		{"turn response lost", true, context.DeadlineExceeded, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := New(stubBackend{}, "test", "/tmp", nil)
			a.State.Begin("task")
			a.applySubmissionFailure(a.State.Sequence, "task", tc.err, tc.submitted)
			if a.canSend() != tc.retry || a.State.Draft != "task" || a.State.Busy {
				t.Fatalf("retry=%v state=%+v", a.canSend(), a.State)
			}
			if !tc.retry && !strings.Contains(a.State.Error, "核验原生记录") {
				t.Fatal("uncertain execution has no recovery instruction")
			}
		})
	}
}

func TestApprovalButtonsKeepNativeDecisionAndDisableAfterSubmit(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	r := &app.Interaction{Status: "待处理", Request: codex.Interaction{
		Method: "item/commandExecution/requestApproval", Command: "go test ./...", Cwd: "/tmp",
		AvailableDecisions: []json.RawMessage{json.RawMessage(`"decline"`), json.RawMessage(`"accept"`)},
	}}
	count := 0
	a.respond = func(prompt *app.Interaction, value any) {
		result, err := prompt.Submit(value)
		if err != nil {
			return
		}
		raw, _ := json.Marshal(result)
		if string(raw) != `{"decision":"decline"}` {
			t.Fatal(string(raw))
		}
		count++
	}
	tt := native.NewTester(func(c *native.Context) { a.interactionCard(c, r) }, 640, 480)
	if _, ok := tt.Find("本会话允许"); ok {
		t.Fatal("invented decision")
	}
	if err := tt.Click("拒绝操作"); err != nil {
		t.Fatal(err)
	}
	tt.Click("拒绝操作")
	if count != 1 || r.Status != "提交中" {
		t.Fatalf("count=%d status=%s", count, r.Status)
	}
}

func TestQuestionFormMapsOptionsAndValidatesBeforeSending(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	r := &app.Interaction{Status: "待处理", Answers: make([]string, 1), Request: codex.Interaction{
		Method: "item/tool/requestUserInput", IsBlocking: false,
		Questions: []codex.Question{{ID: "design", Header: "方案", Question: "选择方案", Options: []codex.Option{{Label: "紧凑", Description: "更少留白"}, {Label: "宽松", Description: "更多留白"}}}},
	}}
	var sent any
	a.respond = func(prompt *app.Interaction, value any) {
		result, err := prompt.Submit(value)
		if err == nil {
			sent = result
		}
	}
	tt := native.NewTester(func(c *native.Context) { a.interactionCard(c, r) }, 640, 600)
	if err := tt.Click("提交回答"); err != nil {
		t.Fatal(err)
	}
	if sent != nil || r.Error == "" {
		t.Fatal("empty form sent")
	}
	if err := tt.Click("宽松"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("提交回答"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sent)
	if string(raw) != `{"answers":{"design":{"answers":["宽松"]}}}` {
		t.Fatal(string(raw))
	}
}

func TestPermissionsGrantOnlyRequestedProfile(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	r := &app.Interaction{Status: "待处理", Request: codex.Interaction{Method: "item/permissions/requestApproval", Permissions: json.RawMessage(`{"network":{"enabled":true}}`)}}
	var sent any
	a.respond = func(prompt *app.Interaction, value any) { sent, _ = prompt.Submit(value) }
	tt := native.NewTester(func(c *native.Context) { a.interactionCard(c, r) }, 640, 480)
	if err := tt.Click("允许本轮请求的权限"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sent)
	if string(raw) != `{"permissions":{"network":{"enabled":true}},"scope":"turn"}` {
		t.Fatal(string(raw))
	}
}

func TestLargeToolOutputKeepsComposerAndStopUsable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) {
			a := New(stubBackend{}, "test", "/tmp", nil)
			a.State.ThreadID, a.State.TurnID, a.State.Busy = "thread", "turn", true
			a.State.Messages = []*app.Message{{ID: "tool", Role: "tool", Tool: &app.Tool{
				Kind: "commandExecution", Command: "go test ./...", Status: "inProgress", Expanded: true, Output: strings.Repeat("long output with unicode 中文\n", 4000),
			}}}
			*a.commandGroupOpen("tool") = true
			tt := native.NewTester(a.view, 640, 480)
			tt.SetDark(dark)
			for _, label := range []string{"任务输入", "停止"} {
				b, ok := tt.Find(label)
				if !ok || b.Y < 0 || b.Y+b.H > 480 || b.X < 0 || b.X+b.W > 640 {
					t.Fatalf("%s outside minimum window: %+v", label, b)
				}
			}
			if err := tt.Click("任务输入"); err != nil {
				t.Fatal(err)
			}
			tt.Type("unsent draft")
			if a.State.Draft != "unsent draft" {
				t.Fatal("output blocked input")
			}
		})
	}
}

func TestOwnResponseContinuesWorkflowWhileUnrelatedResolutionPreservesReading(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	a.State.ThreadID = "thread"
	a.State.Messages = []*app.Message{{ID: "old", Role: "assistant", Done: true, Text: strings.Repeat("A garden paragraph.\n\n", 150)}}
	tt := native.NewTester(a.view, 640, 480)
	tt.Scroll(300, 200, 0, -400)
	if a.list.AtEnd() {
		t.Fatal("did not leave latest")
	}
	a.applyBatch([]codex.Event{{Method: "serverRequest/resolved", Params: json.RawMessage(`{"threadId":"thread","requestId":42}`)}})
	tt.Frame()
	if a.list.AtEnd() {
		t.Fatal("unrelated resolution interrupted reading")
	}
	a.followResponses = map[string]bool{"42": true}
	a.applyBatch([]codex.Event{{Method: "serverRequest/resolved", Params: json.RawMessage(`{"threadId":"thread","requestId":42}`)}})
	tt.Frame()
	if !a.list.AtEnd() || len(a.followResponses) != 0 {
		t.Fatal("own response did not position next workflow step")
	}
}
