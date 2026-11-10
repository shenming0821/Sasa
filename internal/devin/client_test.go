package devin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

// Test helper: when FUYU_DEVIN_HELPER is set in the environment, the spawned
// copy of this test binary runs a scripted ACP server instead of the suite.
func init() {
	if os.Getenv("FUYU_DEVIN_HELPER") == "1" {
		runFakeACP()
		os.Exit(0)
	}
}

var fakeOptions = []any{
	map[string]any{"id": "model", "name": "Model", "type": "select", "currentValue": "m1", "options": []any{
		map[string]any{"value": "m1", "name": "Model One"},
		map[string]any{"value": "m2", "name": "Model Two"},
	}},
	map[string]any{"id": "thought_level", "name": "Thinking", "type": "select", "currentValue": "low", "options": []any{
		map[string]any{"value": "low", "name": "Low"},
		map[string]any{"value": "high", "name": "High"},
	}},
	map[string]any{"id": "speed", "name": "Speed", "type": "select", "currentValue": "standard", "options": []any{
		map[string]any{"value": "standard", "name": "Standard"},
		map[string]any{"value": "fast", "name": "Fast"},
	}},
}

func fakeConfigOptions() []any {
	raw, _ := json.Marshal(fakeOptions)
	var out []any
	_ = json.Unmarshal(raw, &out)
	return out
}

func runFakeACP() {
	in := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	write := func(v any) { _ = out.Encode(v) }
	update := func(sessionID string, payload map[string]any) {
		write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": sessionID, "update": payload}})
	}
	var pendingPrompt json.RawMessage
	pendingPermission := 0
	currentOptions := fakeConfigOptions()
	for in.Scan() {
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(in.Bytes(), &frame) != nil {
			continue
		}
		if frame.Method == "" {
			if pendingPermission != 0 && string(frame.ID) == strconv.Itoa(pendingPermission) {
				pendingPermission = 0
				write(map[string]any{"jsonrpc": "2.0", "id": pendingPrompt, "result": map[string]any{"stopReason": "end_turn"}})
				pendingPrompt = nil
			}
			continue
		}
		respond := func(result any) {
			write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": result})
		}
		respondErr := func(message string) {
			write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "error": map[string]any{"code": -32602, "message": message}})
		}
		var params struct {
			SessionID  string          `json:"sessionId"`
			Cwd        string          `json:"cwd"`
			McpServers json.RawMessage `json:"mcpServers"`
			Prompt     []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"prompt"`
			ConfigID string `json:"configId"`
			Value    string `json:"value"`
		}
		_ = json.Unmarshal(frame.Params, &params)
		switch frame.Method {
		case "initialize":
			respond(map[string]any{"protocolVersion": 1, "agentInfo": map[string]any{"name": "fake-devin", "version": "0.0.1"}})
		case "authenticate":
			var auth struct {
				Meta struct {
					Key string `json:"api_key"`
				} `json:"_meta"`
			}
			if json.Unmarshal(frame.Params, &auth) != nil || auth.Meta.Key != "test-key" {
				respondErr("invalid api key")
				continue
			}
			respond(map[string]any{})
		case "session/new":
			respond(map[string]any{"sessionId": "acp-session-1", "modes": map[string]any{"currentModeId": "default"}, "configOptions": currentOptions})
		case "session/set_config_option":
			for _, raw := range currentOptions {
				option := raw.(map[string]any)
				if option["id"] == params.ConfigID {
					option["currentValue"] = params.Value
				}
			}
			update(params.SessionID, map[string]any{"sessionUpdate": "config_option_update", "configOptions": currentOptions})
			respond(map[string]any{"configOptions": currentOptions})
		case "session/prompt":
			text := ""
			for _, block := range params.Prompt {
				text += block.Text
			}
			switch {
			case strings.Contains(text, "perm"):
				pendingPrompt, pendingPermission = frame.ID, 900
				write(map[string]any{"jsonrpc": "2.0", "id": 900, "method": "session/request_permission", "params": map[string]any{
					"sessionId": params.SessionID,
					"toolCall":  map[string]any{"toolCallId": "call-1", "title": "Run a privileged command", "rawInput": map[string]any{"command": "sudo make"}},
					"options": []any{
						map[string]any{"optionId": "perm-allow", "name": "允许一次", "kind": "allow_once"},
						map[string]any{"optionId": "perm-always", "name": "总是允许", "kind": "allow_always"},
						map[string]any{"optionId": "perm-deny", "name": "拒绝", "kind": "reject_once"},
					},
				}})
			case strings.Contains(text, "cancel"):
				pendingPrompt = frame.ID
			default:
				update(params.SessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "Hello "}})
				update(params.SessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "world"}})
				update(params.SessionID, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "Ran echo", "kind": "execute", "status": "in_progress", "rawInput": map[string]any{"command": "echo hi"}, "_meta": map[string]any{"cognition.ai/cwd": "/tmp"}})
				update(params.SessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed", "content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "hi\n"}}}})
				update(params.SessionID, map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": "planning"}})
				respond(map[string]any{"stopReason": "end_turn"})
			}
		case "session/cancel":
			if pendingPrompt != nil {
				write(map[string]any{"jsonrpc": "2.0", "id": pendingPrompt, "result": map[string]any{"stopReason": "cancelled"}})
				pendingPrompt = nil
			}
		case "session/list":
			respond(map[string]any{"sessions": []any{
				map[string]any{"sessionId": "old-1", "cwd": "/tmp", "title": "Earlier session", "updatedAt": "2026-01-01T00:00:00Z"},
				map[string]any{"sessionId": "locked-1", "cwd": "/tmp", "title": "Busy session", "updatedAt": "2026-01-01T00:00:00Z"},
			}})
		case "session/load":
			if params.SessionID == "locked-1" {
				write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "error": map[string]any{
					"code": -32015, "message": "Session 'locked-1' is already open in another process (PID 4242). Close the other instance before opening it here.",
					"data": map[string]any{"cognition.ai/errorKind": "session_locked", "cognition.ai/lockHolderPid": 4242, "cognition.ai/retryable": true},
				}})
				continue
			}
			if params.SessionID != "old-1" {
				respondErr("session not found")
				continue
			}
			if params.Cwd != "/tmp" || params.McpServers == nil {
				respondErr("missing field `cwd` or `mcpServers`")
				continue
			}
			update(params.SessionID, map[string]any{"sessionUpdate": "session_info_update", "title": "Earlier session"})
			update(params.SessionID, map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]any{"type": "text", "text": "previous question"}})
			update(params.SessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "previous "}})
			update(params.SessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "answer"}})
			respond(map[string]any{"modes": map[string]any{"currentModeId": "default"}, "configOptions": fakeConfigOptions()})
		case "session/close", "session/delete":
			respond(map[string]any{})
		case "debug/exit":
			os.Exit(0)
		default:
			respondErr("unknown method")
		}
	}
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	t.Setenv("FUYU_DEVIN_HELPER", "1")
	c := &Client{executable: os.Args[0], apiKey: "test-key", home: t.TempDir(), stateDir: t.TempDir(), sessions: make(map[string]*acpSession)}
	c.queue.start()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.connect(ctx); err != nil {
		c.queue.close()
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func collectUntil(t *testing.T, c *Client, method string) []agent.Event {
	t.Helper()
	var events []agent.Event
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, ok := <-c.Events():
			if !ok {
				t.Fatal("事件通道已关闭")
			}
			events = append(events, event)
			if event.Method == method {
				return events
			}
		case <-deadline:
			t.Fatalf("等待 %s 超时，已收到 %d 个事件", method, len(events))
		}
	}
}

func eventText(t *testing.T, events []agent.Event, method string) string {
	t.Helper()
	var text strings.Builder
	for _, event := range events {
		if event.Method != method {
			continue
		}
		var p struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(event.Params, &p) == nil {
			text.WriteString(p.Delta)
		}
	}
	return text.String()
}

func TestClientHandshakeAndPrompt(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, err := c.StartThread(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	resolvedTmp, _ := filepath.EvalSymlinks("/tmp")
	if thread.Thread.ID != "acp-session-1" || thread.Cwd != resolvedTmp || thread.Model != "m1" {
		t.Fatalf("unexpected thread: %+v", thread)
	}
	collectUntil(t, c, "fuyu/runtime")
	turn, err := c.Send(ctx, thread.Thread.ID, "hello")
	if err != nil || turn.Status != "inProgress" || turn.ID == "" {
		t.Fatalf("send: turn=%+v err=%v", turn, err)
	}
	events := collectUntil(t, c, "turn/completed")
	if text := eventText(t, events, "item/agentMessage/delta"); text != "Hello world" {
		t.Fatalf("assistant text %q", text)
	}
	var started, completed bool
	var toolItem map[string]any
	for _, event := range events {
		switch event.Method {
		case "turn/started", "item/completed":
			var p struct {
				Item struct {
					Type    string  `json:"type"`
					Status  string  `json:"status"`
					Command string  `json:"command"`
					Output  *string `json:"aggregatedOutput"`
				} `json:"item"`
			}
			if event.Method == "turn/started" {
				started = true
				continue
			}
			if json.Unmarshal(event.Params, &p) != nil {
				continue
			}
			if p.Item.Type == "commandExecution" {
				raw := map[string]any{}
				_ = json.Unmarshal(event.Params, &raw)
				toolItem = raw["item"].(map[string]any)
				completed = true
			}
		}
	}
	if !started || !completed {
		t.Fatal("turn or tool lifecycle missing")
	}
	if toolItem["command"] != "echo hi" || toolItem["status"] != "completed" || toolItem["cwd"] != "/tmp" || toolItem["aggregatedOutput"] != "hi\n" {
		t.Fatalf("tool item %v", toolItem)
	}
	var done struct {
		Turn struct {
			Status string `json:"status"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(events[len(events)-1].Params, &done)
	if done.Turn.Status != "completed" {
		t.Fatalf("turn status %q", done.Turn.Status)
	}
}

func TestClientPermission(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, err := c.StartThread(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, "fuyu/runtime")
	if _, err := c.Send(ctx, thread.Thread.ID, "run perm"); err != nil {
		t.Fatal(err)
	}
	var requestID json.RawMessage
	deadline := time.After(10 * time.Second)
	for requestID == nil {
		select {
		case event := <-c.Events():
			if event.Method == "item/commandExecution/requestApproval" && len(event.ID) > 0 {
				requestID = event.ID
				var p struct {
					Decisions []string `json:"availableDecisions"`
					Command   string   `json:"command"`
				}
				if json.Unmarshal(event.Params, &p) != nil || p.Command != "sudo make" || len(p.Decisions) != 4 {
					t.Fatalf("approval params %s", event.Params)
				}
			}
		case <-deadline:
			t.Fatal("等待审批请求超时")
		}
	}
	if err := c.Respond(requestID, map[string]any{"decision": "accept"}); err != nil {
		t.Fatal(err)
	}
	events := collectUntil(t, c, "turn/completed")
	resolved := false
	for _, event := range events {
		if event.Method == "serverRequest/resolved" {
			resolved = true
		}
	}
	if !resolved {
		t.Fatal("审批缺少 resolved 事件")
	}
}

func TestClientInterrupt(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, err := c.StartThread(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, "fuyu/runtime")
	turn, err := c.Send(ctx, thread.Thread.ID, "please cancel")
	if err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, "turn/started")
	if err := c.Interrupt(ctx, thread.Thread.ID, turn.ID); err != nil {
		t.Fatal(err)
	}
	events := collectUntil(t, c, "turn/completed")
	var done struct {
		Turn struct {
			Status string `json:"status"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(events[len(events)-1].Params, &done)
	if done.Turn.Status != "interrupted" {
		t.Fatalf("turn status %q", done.Turn.Status)
	}
	if err := c.Interrupt(ctx, thread.Thread.ID, turn.ID); err == nil {
		t.Fatal("重复停止应返回错误")
	}
}

func TestClientBusySession(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, err := c.StartThread(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, "fuyu/runtime")
	turn, err := c.Send(ctx, thread.Thread.ID, "cancel me")
	if err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, "turn/started")
	if _, err := c.Send(ctx, thread.Thread.ID, "second"); err == nil {
		t.Fatal("在途会话上的发送应失败")
	}
	if err := c.Interrupt(ctx, thread.Thread.ID, "ignored"); err == nil {
		t.Fatal("错误任务标识的停止应失败")
	}
	if err := c.Interrupt(ctx, thread.Thread.ID, turn.ID); err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, "turn/completed")
}

func TestClientHistory(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	page, err := c.ListThreads(ctx, nil)
	if err != nil || len(page.Data) != 2 {
		t.Fatalf("list: %+v err=%v", page, err)
	}
	if page.Data[0].ID != "old-1" || page.Data[0].Preview != "Earlier session" || page.Data[0].Cwd != "/tmp" {
		t.Fatalf("thread meta %+v", page.Data[0])
	}
	resumed, err := c.ResumeThread(ctx, "old-1")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ThreadMeta.ID != "old-1" || resumed.Cwd != "/tmp" || resumed.Model != "m1" || resumed.ThreadMeta.Preview != "Earlier session" {
		t.Fatalf("resumed %+v", resumed)
	}
	if raw, err := os.ReadFile(filepath.Join(c.stateDir, "old-1.json")); err != nil || !strings.Contains(string(raw), `"cwd":"/tmp"`) {
		t.Fatalf("检查点未保存会话目录: %s err=%v", raw, err)
	}
	turns, err := c.ListTurns(ctx, "old-1", nil)
	if err != nil || len(turns.Data) != 1 {
		t.Fatalf("turns: %+v err=%v", turns, err)
	}
	var user, assistant bool
	for _, raw := range turns.Data[0].Items {
		var item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		if item.Type == "userMessage" {
			user = true
		}
		if item.Type == "agentMessage" && item.Text == "previous answer" {
			assistant = true
		}
	}
	if !user || !assistant {
		t.Fatalf("replayed items %s", turns.Data[0].Items)
	}
	if _, err := c.Send(ctx, "old-1", "follow up"); err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, "turn/completed")
}

func TestClientResumeUnknownCwd(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.ResumeThread(ctx, "ghost"); err == nil || !strings.Contains(err.Error(), "工作目录未知") {
		t.Fatalf("目录未知的会话应明确报错: %v", err)
	}
}

func TestClientResumeLockedSession(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := c.ResumeThread(ctx, "locked-1")
	if err == nil || !agent.IsActiveWriter(err, "locked-1") || !strings.Contains(err.Error(), "Devin RPC") {
		t.Fatalf("被占用会话应归类为外部写入并保留 Devin 前缀: %v", err)
	}
}

func TestClientOptions(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, err := c.StartThread(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, "fuyu/runtime")
	_, err = c.SendWithOptions(ctx, thread.Thread.ID, "hi", nil, nil, agent.TurnOptions{Model: "m2", Effort: "high", ServiceTier: "fast"})
	if err != nil {
		t.Fatal(err)
	}
	var runtimeModel string
	events := collectUntil(t, c, "turn/completed")
	for _, event := range events {
		if event.Method != "fuyu/runtime" {
			continue
		}
		var p struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(event.Params, &p) == nil {
			runtimeModel = p.Model
		}
	}
	if runtimeModel != "m2" {
		t.Fatalf("运行时模型更新 %q", runtimeModel)
	}
	var submitted *agent.NotSubmittedError
	if _, err := c.SendWithOptions(ctx, thread.Thread.ID, "hi", nil, nil, agent.TurnOptions{Model: "unknown"}); !errors.As(err, &submitted) {
		t.Fatalf("未知模型应返回未提交错误: %v", err)
	}
}

func TestClientDisconnect(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	thread, err := c.StartThread(ctx, "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, "fuyu/runtime")
	if _, err := c.Send(ctx, thread.Thread.ID, "cancel me"); err != nil {
		t.Fatal(err)
	}
	collectUntil(t, c, "turn/started")
	if err := c.conn.notify("debug/exit", nil); err != nil {
		t.Fatal(err)
	}
	var disconnected, failed bool
	deadline := time.After(10 * time.Second)
	for !disconnected {
		select {
		case event, ok := <-c.Events():
			if !ok {
				t.Fatal("事件通道已关闭")
			}
			if event.Method == "fuyu/disconnected" {
				disconnected = true
			}
			if event.Method == "turn/completed" {
				var p struct {
					Turn struct{ Status string } `json:"turn"`
				}
				if json.Unmarshal(event.Params, &p) == nil && p.Turn.Status == "failed" {
					failed = true
				}
			}
		case <-deadline:
			t.Fatal("等待断开事件超时")
		}
	}
	if !failed {
		t.Fatal("在途任务未标记失败")
	}
	if _, err := c.Send(ctx, thread.Thread.ID, "again"); err == nil {
		t.Fatal("断开后发送应失败")
	}
}

var _ = fmt.Sprintf
