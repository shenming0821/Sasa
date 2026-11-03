package ui

import (
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/app"
	"github.com/shenming0821/Fuyu/internal/attachment"
)

func TestTaskFailureCardDisclosureAndRetry(t *testing.T) {
	for _, width := range []int{440, 800} {
		a := New(stubBackend{}, "test", "/tmp", nil)
		a.State.Phase = "执行失败"
		a.State.Error = "Claude 任务失败：success，terminal_reason=api_error，"
		a.State.Messages = []*app.Message{{ID: "user", Role: "user", Text: "original task"}}
		sent := 0
		a.submit = func() {
			sent++
			a.State.Busy = true
		}
		tester := native.NewTester(func(c *native.Context) {
			native.Column(c).FillWidth().Children(func() {
				a.taskFailureCard(c)
				a.composer(c)
			})
		}, width, 500)
		tester.SetDark(width == 440)
		if !tester.HasText("任务未完成") || tester.HasText(a.State.Error) {
			t.Fatal("failure card must show summary with collapsed raw error")
		}
		card, _ := tester.Find("任务未完成")
		input, _ := tester.Find("任务输入")
		if input.Y <= card.Y || input.X < 0 || input.X+input.W > float32(width)+1 {
			t.Fatal("failure card or composer exceeds reading column")
		}
		if err := tester.Click("错误详情"); err != nil {
			t.Fatal(err)
		}
		tester.Advance(250 * time.Millisecond)
		if !tester.HasText(a.State.Error) {
			t.Fatalf("raw error missing after disclosure: expanded=%v texts=%v", a.errorExpanded, tester.Texts())
		}
		if err := tester.Click("重试本次任务"); err != nil {
			t.Fatal(err)
		}
		a.retryFailedTask()
		if sent != 1 || a.State.Draft != "original task" {
			t.Fatalf("retry submission count=%d draft=%q", sent, a.State.Draft)
		}
	}
}

func TestRetryFailedTaskPreservesDraftAndRecoveryGates(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	a.State.Phase = "执行失败"
	a.State.Messages = []*app.Message{{Role: "user", Text: "original", Images: []attachment.Image{{Name: "old"}}}}
	sent := 0
	a.submit = func() { sent++ }
	for _, block := range []func(){
		func() { a.State.Draft = "new draft" },
		func() { a.State.DraftImages = []attachment.Image{{Name: "new"}} },
		func() { a.recoveryBlocked = true },
		func() { a.State.Incomplete = true },
		func() { a.State.Disconnected = true },
	} {
		a.State.Draft, a.State.DraftImages = "", nil
		a.recoveryBlocked, a.State.Incomplete, a.State.Disconnected = false, false, false
		block()
		draft, images := a.State.Draft, len(a.State.DraftImages)
		a.retryFailedTask()
		if sent != 0 || a.State.Draft != draft || len(a.State.DraftImages) != images {
			t.Fatal("blocked retry altered draft or submitted task")
		}
	}
	a.State.Draft, a.State.DraftImages = "", nil
	a.recoveryBlocked, a.State.Incomplete, a.State.Disconnected = false, false, false
	a.retryFailedTask()
	if sent != 1 || a.State.Draft != "original" || len(a.State.DraftImages) != 1 {
		t.Fatal("retry did not restore original text and image")
	}
	a.State.DraftImages[0].Name = "changed"
	if a.State.Messages[0].Images[0].Name != "old" {
		t.Fatal("retry draft shares image slice with history")
	}
}
