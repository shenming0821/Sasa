package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/codex"
)

func TestThinkingFollowsCurrentRequest(t *testing.T) {
	a := New(stubBackend{}, "test", "/tmp", nil)
	a.State.ThreadID = "thread"
	a.State.Begin("first")
	tt := native.NewTester(a.view, 640, 480)
	check := func(want bool) {
		t.Helper()
		tt.Frame()
		if tt.HasText("正在思考") != want {
			t.Fatalf("thinking visibility: want %v, phase %q", want, a.State.Phase)
		}
	}
	event := func(method, params string) {
		a.State.Apply(codex.Event{Method: method, Params: json.RawMessage(params)})
	}
	check(true)
	event("item/started", `{"threadId":"thread","item":{"id":"reply","type":"agentMessage"}}`)
	check(true)
	event("item/agentMessage/delta", `{"threadId":"thread","itemId":"reply","delta":"你好"}`)
	check(false)
	event("turn/completed", `{"threadId":"thread","turn":{"id":"one","status":"completed"}}`)
	check(false)
	a.State.Begin("second")
	check(true)
	event("item/started", `{"threadId":"thread","item":{"id":"tool","type":"commandExecution","command":"pwd"}}`)
	check(false)
	event("turn/completed", `{"threadId":"thread","turn":{"id":"two","status":"completed"}}`)
	a.State.Begin("third")
	check(true)
	a.State.Phase = "正在停止"
	check(false)
	a.State.Phase = "正在执行"
	a.State.Error = "请求失败"
	check(false)
	event("fuyu/disconnected", `{}`)
	check(false)
}

func TestThinkingShimmerAndReducedMotion(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, reduce := range []bool{false, true} {
			t.Run(fmt.Sprintf("dark=%v/reduce=%v", dark, reduce), func(t *testing.T) {
				tt := native.NewTester(func(c *native.Context) {
					native.Column(c).Fill().Background(c.Theme().Background).Children(func() {
						thinkingIndicator(c)
					})
				}, 200, 60)
				tt.SetDark(dark)
				tt.SetPreferences(native.Preferences{ReduceMotion: reduce})
				before := append([]byte(nil), tt.Image().Pix...)
				tt.Advance(650 * time.Millisecond)
				same := bytes.Equal(before, tt.Image().Pix)
				if same != reduce {
					t.Fatalf("shimmer unchanged=%v with reduced motion=%v", same, reduce)
				}
			})
		}
	}
}
