package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shenming0821/Fuyu/internal/codex"
)

func activeState() State {
	s := New("/tmp")
	s.ThreadID = "thread"
	s.Begin("task")
	apply(&s, "turn/started", `{"threadId":"thread","turn":{"id":"turn"}}`)
	return s
}

func TestToolLifecycleOutputAndTurnRemainIndependent(t *testing.T) {
	s := activeState()
	apply(&s, "item/started", `{"threadId":"thread","turnId":"turn","item":{"id":"cmd","type":"commandExecution","command":"go test ./...","cwd":"/tmp","status":"inProgress"}}`)
	apply(&s, "item/commandExecution/outputDelta", `{"threadId":"thread","turnId":"turn","itemId":"cmd","delta":"running\n"}`)
	apply(&s, "item/completed", `{"threadId":"thread","turnId":"turn","item":{"id":"files","type":"fileChange","status":"completed","changes":[{"path":"a.go","kind":{"type":"update"},"diff":"@@\n-old\n+new\n+second"}]}}`)
	apply(&s, "turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)
	if s.Messages[1].Done || s.Messages[1].Tool.Status != "inProgress" {
		t.Fatal("turn completion invented a tool terminal state")
	}
	if s.Messages[2].Tool.Changes[0].Summary() != "修改 a.go  +2 −1" {
		t.Fatal(s.Messages[2].Tool)
	}
	apply(&s, "item/completed", `{"threadId":"thread","turnId":"turn","item":{"id":"cmd","type":"commandExecution","status":"completed","command":"go test ./...","aggregatedOutput":"late final","exitCode":0}}`)
	if !s.Messages[1].Done || s.Messages[1].Tool.AwaitingFinal || s.Messages[1].Tool.Output != "late final" || s.Phase != "已中断" || s.Error != "" {
		t.Fatal("late tool result was lost or changed the turn terminal state")
	}
	// A separate task covers a real item failure and final output replacement.
	s = activeState()
	apply(&s, "item/commandExecution/outputDelta", `{"threadId":"thread","turnId":"turn","itemId":"cmd","delta":"partial"}`)
	apply(&s, "item/completed", `{"threadId":"thread","turnId":"turn","item":{"id":"cmd","type":"commandExecution","command":"exit 7","status":"failed","aggregatedOutput":"final","exitCode":7,"durationMs":32}}`)
	tool := s.Messages[1].Tool
	if !s.Messages[1].Done || tool.Output != "final" || *tool.ExitCode != 7 || tool.StatusLabel() != "执行失败" {
		t.Fatal(tool)
	}
}

func request(s *State, id, method, extra string) *Interaction {
	s.Apply(codex.Event{ID: json.RawMessage(id), Method: method, Params: json.RawMessage(`{"threadId":"thread","turnId":"turn","itemId":"tool"` + extra + `}`)})
	return s.Messages[len(s.Messages)-1].Interaction
}

func TestQuestionResponseValidationSecretsAndExpiry(t *testing.T) {
	s := activeState()
	r := request(&s, `"q-1"`, "item/tool/requestUserInput", `,"isBlocking":false,"questions":[{"id":"choice","header":"方案","question":"请选择","isOther":false,"options":[{"label":"A","description":"方案A"}]},{"id":"secret","header":"口令","question":"请输入","isSecret":true}]`)
	if _, err := r.Submit(nil); err == nil || r.Status != "待处理" {
		t.Fatal("empty answer accepted")
	}
	r.Answers[0], r.Answers[1] = "invalid", "sensitive-test"
	if _, err := r.Submit(nil); err == nil {
		t.Fatal("out of range option accepted")
	}
	r.Answers[0] = "A"
	answer, err := r.Submit(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(answer)
	if string(raw) != `{"answers":{"choice":{"answers":["A"]},"secret":{"answers":["sensitive-test"]}}}` {
		t.Fatal(string(raw))
	}
	if r.Answers[1] != "" || r.Status != "提交中" {
		t.Fatal("secret retained after submission")
	}
	if _, err := r.Submit(answer); err == nil {
		t.Fatal("duplicate submission")
	}
	apply(&s, "serverRequest/resolved", `{"threadId":"thread","requestId":"q-1"}`)
	if r.Status != "已解决" {
		t.Fatal(r.Status)
	}
	r2 := request(&s, `2`, "item/fileChange/requestApproval", "")
	s.CancelInteractions()
	if r2.Status != "已取消" {
		t.Fatal(r2.Status)
	}
	r3 := request(&s, `3`, "item/permissions/requestApproval", `,"permissions":{"network":{"enabled":true}}`)
	s.Apply(codex.Event{Method: "fuyu/disconnected"})
	if r3.Status != "已失效" || len(s.PendingInteractions()) != 0 {
		t.Fatal(r3.Status)
	}
}

func TestNativeResolutionCannotBeRevivedByDuplicateRequest(t *testing.T) {
	s := activeState()
	r := request(&s, `7`, "item/fileChange/requestApproval", "")
	apply(&s, "serverRequest/resolved", `{"threadId":"thread","requestId":7}`)
	r2 := request(&s, `7`, "item/fileChange/requestApproval", "")
	if r != r2 || r.Status != "已解决" {
		t.Fatal("resolved interaction recreated")
	}
	apply(&s, "turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)
	r = request(&s, `8`, "item/fileChange/requestApproval", "")
	if r.Active() {
		t.Fatal("late request activated after turn completion")
	}
}

func TestLateApprovalStaysInvalidWhileStopIsUnconfirmed(t *testing.T) {
	s := activeState()
	s.BeginStop()
	s.Phase = "停止待确认"
	apply(&s, "error", `{"threadId":"thread","turnId":"turn","error":{"message":"retrying"},"willRetry":true}`)
	r := request(&s, `3`, "item/fileChange/requestApproval", "")
	if r.Active() || r.Status != "已失效" || len(s.PendingInteractions()) != 0 || !s.Stopping() {
		t.Fatal("late request revived controls during an unconfirmed stop")
	}
	apply(&s, "turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)
	s.Begin("next task")
	if s.Stopping() {
		t.Fatal("stop intent leaked into next task")
	}
}

func TestOutputAndTotalDisplayLimits(t *testing.T) {
	s := activeState()
	large := strings.Repeat("中", MaxToolBytes/3+100)
	p, _ := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "tool", "delta": large})
	s.Apply(codex.Event{Method: "item/commandExecution/outputDelta", Params: p})
	tool := s.Messages[1].Tool
	if len(tool.Output) > MaxToolBytes || !utf8.ValidString(tool.Output) || !tool.Truncated {
		t.Fatal("tool output not bounded")
	}
	for i := 0; i < 10; i++ {
		p, _ := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"id": fmt.Sprint(i), "type": "agentMessage", "text": strings.Repeat("x", MaxMessageBytes)}})
		s.Apply(codex.Event{Method: "item/completed", Params: p})
	}
	total := 0
	for _, m := range s.Messages {
		total += messageBytes(m)
	}
	if total > MaxDisplayBytes || s.Notice == "" {
		t.Fatalf("display grew to %d", total)
	}
	s = activeState()
	for i := 0; i < MaxMessages+5; i++ {
		p, _ := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]string{
			"id": fmt.Sprint(i), "type": "agentMessage", "text": "small message",
		}})
		s.Apply(codex.Event{Method: "item/completed", Params: p})
	}
	if len(s.Messages) != MaxMessages || s.Notice == "" {
		t.Fatalf("message count grew to %d without eviction notice", len(s.Messages))
	}
}

func TestGapReconcilesFromAuthoritativeItems(t *testing.T) {
	s := activeState()
	s.Apply(codex.Event{Method: "fuyu/eventGap"})
	apply(&s, "item/commandExecution/outputDelta", `{"threadId":"thread","turnId":"turn","itemId":"cmd","delta":"incomplete"}`)
	apply(&s, "turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)
	if !s.NeedsSnapshot || !s.Incomplete {
		t.Fatal("gap hidden")
	}
	err := s.Reconcile(json.RawMessage(`{"thread":{"id":"thread","turns":[{"id":"turn","status":"completed","items":[{"id":"cmd","type":"commandExecution","status":"completed","command":"echo ok","aggregatedOutput":"ok\n","exitCode":0}]}]}}`))
	if err != nil || s.Incomplete || s.Messages[1].Tool.Output != "ok\n" {
		t.Fatalf("%v %+v", err, s)
	}
	if err := s.Reconcile(json.RawMessage(`{"thread":{"id":"other"}}`)); err == nil {
		t.Fatal("accepted foreign snapshot")
	}
	if err := s.Reconcile(json.RawMessage(`{"thread":{"id":"thread","turns":[{"id":"turn","status":"failed","error":{"message":"authoritative failure"},"items":[]}]}}`)); err != nil || s.Phase != "执行失败" || s.Error != "authoritative failure" {
		t.Fatalf("snapshot did not reconcile terminal status: %v %+v", err, s)
	}
}

func TestUnprojectedCompatibilityNoticeDoesNotBecomeParseFailure(t *testing.T) {
	s := activeState()
	apply(&s, "codex/event/patch_apply_end", `{"error":"legacy event has a string error","success":true}`)
	if s.Error != "" || !s.Busy {
		t.Fatal(s.Error)
	}
	add := FileChange{Path: "proof.txt", Kind: json.RawMessage(`{"type":"add"}`), Diff: "fuyu-p2-proof\n"}
	if add.Summary() != "新增 proof.txt  +1 −0" {
		t.Fatal(add.Summary())
	}
}
