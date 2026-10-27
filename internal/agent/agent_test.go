package agent

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestKindAndSessionNamespaces(t *testing.T) {
	if AgentKind("").Normalized() != Codex || AgentKind("other").Validate() == nil {
		t.Fatal("旧索引必须默认为 Codex，未知 Agent 不能回落")
	}
	keys := map[SessionKey]bool{
		{Agent: Codex, Runtime: "/one", SessionID: "same"}:  true,
		{Agent: Claude, Runtime: "/one", SessionID: "same"}: true,
		{Agent: Claude, Runtime: "/two", SessionID: "same"}: true,
	}
	if len(keys) != 3 {
		t.Fatal("跨 Agent 或 Runtime 的会话串线")
	}
}

func TestTurnIdentityBeforeSendAndLateCompletion(t *testing.T) {
	var turns TurnMap
	key := SessionKey{Agent: Claude, Runtime: "/claude", SessionID: "session"}
	first, err := turns.Begin(key, 1)
	if err != nil || first.ID == "" || first.UserUUID == "" || string(first.ID) == first.UserUUID {
		t.Fatal("发送前必须拥有独立的任务及用户 UUID", first, err)
	}
	if _, err := turns.Begin(key, 2); err == nil {
		t.Fatal("进程重建不能绕过旧任务核验")
	}
	first.NativeID, first.MessageID, first.TranscriptUUID = "native", "message", "checkpoint"
	if err := turns.Bind(first); err != nil {
		t.Fatal(err)
	}
	if mapped, ok := turns.Lookup(key, 1, first.ID); !ok || mapped.NativeID != "native" ||
		mapped.MessageID != "message" || mapped.TranscriptUUID != "checkpoint" {
		t.Fatal("独立原生标识未保留", mapped, ok)
	}
	changed := first
	changed.NativeID = "different"
	if turns.Bind(changed) == nil || turns.Complete(key, 2, first.ID) {
		t.Fatal("重绑定或旧进程代次被接受")
	}
	if !turns.Complete(key, 1, first.ID) || turns.Complete(key, 1, first.ID) {
		t.Fatal("终态必须只投影一次")
	}
	second, err := turns.Begin(key, 2)
	if err != nil || second.ID == first.ID {
		t.Fatal("新轮标识无效", second, err)
	}
	if turns.Complete(key, 1, first.ID) || turns.Bind(first) == nil {
		t.Fatal("迟到旧轮不能覆盖新轮")
	}
	other := key
	other.Agent = Codex
	if _, err := turns.Begin(other, 1); err != nil {
		t.Fatal("另一 Agent 不应被占用", err)
	}
}

func TestConcurrentCompletionOnlyOnce(t *testing.T) {
	var turns TurnMap
	key := SessionKey{SessionID: "legacy"}
	turn, err := turns.Begin(key, 1)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan bool, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- turns.Complete(key, 1, turn.ID)
		}()
	}
	wg.Wait()
	close(results)
	completed := 0
	for ok := range results {
		if ok {
			completed++
		}
	}
	if completed != 1 {
		t.Fatal("并发终态重复", completed)
	}
}

func TestApprovalIntentRequiresVerifiedSessionRule(t *testing.T) {
	base := ApprovalIntent{
		Request: RequestKey{
			Session: SessionKey{Agent: Claude, SessionID: "session"},
			Generation: 1, RequestID: "request",
		},
		Decision: Allow,
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"turn", "userSettings", "projectSettings", "session"} {
		intent := base
		intent.Rule = &RuleSuggestion{ID: "rule", Destination: destination, Rule: json.RawMessage(`{"type":"addRules","rules":[{"toolName":"Read"}]}`)}
		if intent.Validate() == nil {
			t.Fatal("未验证或永久规则不能自动启用", destination)
		}
	}
	base.Rule = &RuleSuggestion{
		ID: "rule", Destination: "session", Verified: true,
		Rule: json.RawMessage(`{"type":"addRules","rules":[{"toolName":"Read"}]}`),
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	mode := base
	mode.Rule = &RuleSuggestion{
		ID: "mode", Destination: "session", Verified: true,
		Rule: json.RawMessage(`{"type":"setMode","mode":"bypassPermissions"}`),
	}
	if mode.Validate() == nil {
		t.Fatal("单次审批不能携带权限模式切换")
	}
	base.Decision = Deny
	if base.Validate() == nil {
		t.Fatal("拒绝不能扩大授权")
	}
	base.Rule = nil
	base.Feedback, base.Stop = "请保留原文件", true
	if err := base.Validate(); err != nil {
		t.Fatal("拒绝应保留反馈和停止意图", err)
	}
	base.Request.Generation = 0
	if base.Validate() == nil {
		t.Fatal("不能响应未知进程的审批")
	}
}

func TestRequestCorrelationOutOfOrderAndTimeout(t *testing.T) {
	var table RequestTable[int]
	first, _ := table.Register("first")
	second, _ := table.Register("second")
	if _, ok := table.Register("first"); ok {
		t.Fatal("重复请求不能覆盖旧等待者")
	}
	if !table.Route("second", 2) || !table.Route("first", 1) || table.Route("first", 3) {
		t.Fatal("响应路由不应堵塞或覆盖未消费响应")
	}
	if <-first != 1 || <-second != 2 {
		t.Fatal("乱序响应串线")
	}
	table.Remove("first")
	if table.Route("first", 3) || table.Route("unknown", 4) {
		t.Fatal("超时或未知响应必须忽略")
	}
}
