package app

import (
	"encoding/json"
	"github.com/shenming0821/Sasa/internal/codex"
	"strings"
	"testing"
	"unicode/utf8"
)

func apply(s *State, method, params string) {
	s.Apply(codex.Event{Method: method, Params: json.RawMessage(params)})
}
func TestStreamSnapshotAndNextTurn(t *testing.T) {
	s := New("/tmp")
	s.ThreadID = "thread"
	s.Begin("first")
	apply(&s, "turn/started", `{"threadId":"thread","turn":{"id":"one"}}`)
	apply(&s, "item/agentMessage/delta", `{"threadId":"thread","turnId":"one","itemId":"a","delta":"你好"}`)
	apply(&s, "item/completed", `{"threadId":"thread","turnId":"one","item":{"id":"a","type":"agentMessage","text":"你好，世界"}}`)
	apply(&s, "turn/completed", `{"threadId":"thread","turn":{"id":"one","status":"completed"}}`)
	if s.Busy || s.Phase != "本轮已完成" || len(s.Messages) != 2 || s.Messages[1].Text != "你好，世界" {
		t.Fatalf("%+v", s)
	}
	s.Begin("second")
	apply(&s, "turn/completed", `{"threadId":"thread","turn":{"id":"one","status":"completed"}}`)
	if !s.Busy {
		t.Fatal("old completion ended new submission")
	}
	apply(&s, "turn/started", `{"threadId":"thread","turn":{"id":"two"}}`)
	apply(&s, "turn/completed", `{"threadId":"other","turn":{"id":"two","status":"completed"}}`)
	if !s.Busy {
		t.Fatal("other thread changed state")
	}
	apply(&s, "error", `{"threadId":"thread","turnId":"two","willRetry":true,"error":{"message":"retry"}}`)
	if !s.Busy || s.Phase != "Codex 正在重试" {
		t.Fatal(s)
	}
	apply(&s, "turn/completed", `{"threadId":"thread","turn":{"id":"two","status":"interrupted"}}`)
	if s.Busy || s.Phase != "已中断" {
		t.Fatal(s)
	}
}
func TestDisconnectAndFailure(t *testing.T) {
	s := New("/tmp")
	s.ThreadID = "thread"
	s.Begin("task")
	apply(&s, "turn/completed", `{"threadId":"thread","turn":{"id":"one","status":"failed","error":{"message":"failed test"}}}`)
	if s.Phase != "执行失败" || s.Error != "failed test" {
		t.Fatal(s)
	}
	s.Apply(codex.Event{Method: "sasa/disconnected"})
	if !s.Disconnected || s.Busy || s.Phase != "连接已断开" {
		t.Fatal(s)
	}
	apply(&s, "turn/started", `{"threadId":"thread","turn":{"id":"late"}}`)
	if s.Busy {
		t.Fatal("late event revived disconnected task")
	}
}
func TestTruncatePreservesUTF8(t *testing.T) {
	text := bounded(strings.Repeat("中", MaxMessageBytes/3+10))
	if !utf8.ValidString(text) || !strings.HasSuffix(text, "[显示已截断]") {
		t.Fatal("invalid truncation")
	}
}

func TestRetryNoticeClearsWhenOutputResumes(t *testing.T) {
	s := New("/tmp")
	s.ThreadID = "thread"
	s.Begin("task")
	apply(&s, "turn/started", `{"threadId":"thread","turn":{"id":"turn"}}`)
	apply(&s, "error", `{"threadId":"thread","turnId":"turn","willRetry":true,"error":{"message":"Reconnecting..."}}`)
	apply(&s, "item/agentMessage/delta", `{"threadId":"thread","turnId":"turn","itemId":"reply","delta":"resumed"}`)
	if s.Error != "" || s.Phase != "正在执行" || !s.Busy {
		t.Fatalf("retry notice remains after resumed output: %+v", s)
	}
	apply(&s, "error", `{"threadId":"thread","turnId":"turn","willRetry":true,"error":{"message":"retry again"}}`)
	s.Error = "a separate warning"
	apply(&s, "turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)
	if s.Error != "a separate warning" {
		t.Fatal("completion erased an unrelated warning")
	}
}
