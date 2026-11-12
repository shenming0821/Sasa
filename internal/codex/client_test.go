package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type failedPipe struct{ io.WriteCloser }

func (failedPipe) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// The subprocess speaks only the protocol; it never launches Codex or reads credentials.
func TestProtocolHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	scenario := os.Args[len(os.Args)-1]
	dec, enc := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	var held envelope
	var modelPage int
	for {
		var m envelope
		if dec.Decode(&m) != nil {
			if scenario == "hang" {
				time.Sleep(time.Hour)
			}
			os.Exit(0)
		}
		switch scenario {
		case "steer":
			var wire struct {
				ThreadID            string                                    `json:"threadId"`
				ExpectedTurnID      string                                    `json:"expectedTurnId"`
				ClientUserMessageID string                                    `json:"clientUserMessageId"`
				Input               []struct{ Type, Text, Name, Path string } `json:"input"`
			}
			if m.Method != "turn/steer" || json.Unmarshal(m.Params, &wire) != nil || wire.ThreadID != "thread" || wire.ExpectedTurnID != "active" || wire.ClientUserMessageID != "message" || len(wire.Input) != 3 {
				os.Exit(21)
			}
			if wire.Input[0].Text != "supplement" || wire.Input[1].Type != "skill" || wire.Input[1].Name != "design" || wire.Input[2].Path != "/owned/image.png" {
				os.Exit(22)
			}
			enc.Encode(map[string]any{"id": m.ID, "result": map[string]any{"turnId": "active"}})
			continue
		case "models-empty", "models-empty-recover", "models-empty-timeout":
			if m.Method != "model/list" {
				os.Exit(11)
			}
			models := []any{}
			if scenario == "models-empty-timeout" {
				os.Stderr.WriteString("ERROR codex_models_manager::manager: failed to refresh available models: request timed out https://private.example/secret-token\n")
			}
			if scenario == "models-empty-recover" && modelPage > 0 {
				models = append(models, map[string]any{"model": "recovered-model", "displayName": "Recovered model"})
			}
			modelPage++
			enc.Encode(map[string]any{"id": m.ID, "result": map[string]any{"data": models, "nextCursor": nil}})
			continue
		case "models", "models-default", "models-cycle":
			var result any
			switch m.Method {
			case "config/read":
				var params struct {
					Cwd           string `json:"cwd"`
					IncludeLayers bool   `json:"includeLayers"`
				}
				if json.Unmarshal(m.Params, &params) != nil || params.Cwd != "/workspace" || params.IncludeLayers {
					os.Exit(5)
				}
				model := "provider/config-model"
				if scenario == "models-default" {
					model = ""
				}
				result = map[string]any{"config": map[string]any{"model": model}}
			case "model/list":
				var params struct {
					Cursor        *string `json:"cursor"`
					IncludeHidden bool    `json:"includeHidden"`
					Limit         int     `json:"limit"`
				}
				if json.Unmarshal(m.Params, &params) != nil || params.IncludeHidden || params.Limit != 100 {
					os.Exit(6)
				}
				if modelPage == 0 {
					if params.Cursor != nil {
						os.Exit(7)
					}
					result = map[string]any{"data": []any{map[string]any{"id": "catalog-id", "model": "wire-model", "displayName": "Visible model", "isDefault": true}, map[string]any{"model": "hidden", "hidden": true}}, "nextCursor": "page-2"}
				} else {
					if params.Cursor == nil || *params.Cursor != "page-2" {
						os.Exit(8)
					}
					next := any(nil)
					if scenario == "models-cycle" {
						next = "page-2"
					}
					result = map[string]any{"data": []any{map[string]any{"id": "second", "model": "wire-second", "displayName": "Second model"}}, "nextCursor": next}
				}
				modelPage++
			case "turn/start":
				var params struct {
					Model  string                                    `json:"model"`
					Thread string                                    `json:"threadId"`
					Input  []struct{ Type, Text, Name, Path string } `json:"input"`
				}
				if json.Unmarshal(m.Params, &params) != nil || params.Model != "wire-model" || params.Thread != "thread" || len(params.Input) != 3 || params.Input[0].Text != "task" || params.Input[1].Type != "skill" || params.Input[2].Type != "localImage" {
					os.Exit(9)
				}
				result = map[string]any{"turn": map[string]any{"id": "model-turn", "status": "inProgress"}}
			default:
				os.Exit(10)
			}
			enc.Encode(map[string]any{"id": m.ID, "result": result})
			continue
		case "options":
			var params struct {
				Model  string                                    `json:"model"`
				Effort string                                    `json:"effort"`
				Tier   string                                    `json:"serviceTierForTurn"`
				Thread string                                    `json:"threadId"`
				Input  []struct{ Type, Text, Name, Path string } `json:"input"`
			}
			if m.Method != "turn/start" || json.Unmarshal(m.Params, &params) != nil || params.Thread != "thread" || params.Model != "wire-model" ||
				params.Effort != "high" || params.Tier != "priority" || len(params.Input) != 1 || params.Input[0].Text != "task" {
				os.Exit(9)
			}
			enc.Encode(map[string]any{"id": m.ID, "result": map[string]any{"turn": map[string]any{"id": "option-turn", "status": "inProgress"}}})
			continue
		case "images":
			var params struct {
				Thread string                                         `json:"threadId"`
				Input  []struct{ Type, Name, Path, Text, URL string } `json:"input"`
			}
			if m.Method != "turn/start" || json.Unmarshal(m.Params, &params) != nil || params.Thread != "thread" || len(params.Input) != 4 ||
				params.Input[0].Type != "text" || params.Input[0].Text != "请看图" ||
				params.Input[1].Type != "skill" || params.Input[1].Name != "design" ||
				params.Input[2].Type != "localImage" || params.Input[2].Path != "/owned/截图.png" ||
				params.Input[3].Type != "image" || params.Input[3].URL != "data:image/png;base64,fixture" {
				os.Exit(4)
			}
			enc.Encode(map[string]any{"id": m.ID, "result": map[string]any{"turn": map[string]any{"id": "image-turn", "status": "inProgress"}}})
			continue
		case "skills", "skills-reload":
			if m.Method == "skills/list" {
				var params struct {
					Cwds        []string `json:"cwds"`
					ForceReload bool     `json:"forceReload"`
				}
				if json.Unmarshal(m.Params, &params) != nil || len(params.Cwds) != 1 || params.Cwds[0] != "/workspace" || params.ForceReload != (scenario == "skills-reload") {
					os.Exit(2)
				}
				enc.Encode(map[string]any{"id": m.ID, "result": map[string]any{"data": []any{map[string]any{"cwd": "/workspace", "skills": []any{map[string]any{"name": "design", "path": "/skills/SKILL.md", "description": "Design", "enabled": true}}, "errors": []any{map[string]any{"path": "/bad/SKILL.md", "message": "invalid metadata"}}}}}})
			} else if m.Method == "turn/start" {
				var params struct {
					Thread string                                    `json:"threadId"`
					Input  []struct{ Type, Name, Path, Text string } `json:"input"`
				}
				if json.Unmarshal(m.Params, &params) != nil || params.Thread != "thread" || len(params.Input) != 2 || params.Input[0].Type != "text" || params.Input[0].Text != "$design task" || params.Input[1].Type != "skill" || params.Input[1].Name != "design" || params.Input[1].Path != "/skills/SKILL.md" {
					os.Exit(3)
				}
				enc.Encode(map[string]any{"id": m.ID, "result": map[string]any{"turn": map[string]any{"id": "turn", "status": "inProgress"}}})
			}
			continue
		case "flood":
			for i := 0; i < 1024; i++ {
				enc.Encode(map[string]any{"method": "notice", "params": map[string]int{"n": i}})
			}
		case "reverse":
			if held.ID == nil {
				held = m
				continue
			}
			enc.Encode(map[string]any{"id": m.ID, "result": string(m.Params)})
			enc.Encode(map[string]any{"id": held.ID, "result": string(held.Params)})
			held = envelope{}
			continue
		case "interactive":
			if m.Method == "trigger" {
				held = m
				var request envelope
				json.Unmarshal(m.Params, &request)
				enc.Encode(request)
				continue
			}
			if m.Method == "" {
				enc.Encode(map[string]any{"method": "serverRequest/resolved", "params": map[string]any{"threadId": "thread", "requestId": m.ID}})
				enc.Encode(map[string]any{"id": held.ID, "result": m.Result, "error": m.Error})
				continue
			}
		case "pendingflood":
			if m.Method == "trigger" {
				held = m
				for i := 0; i <= MaxPendingRequests; i++ {
					enc.Encode(map[string]any{"id": 100 + i, "method": "item/fileChange/requestApproval",
						"params": map[string]string{"threadId": "thread", "turnId": "turn", "itemId": "file"}})
				}
				continue
			}
			if m.Method == "" {
				enc.Encode(map[string]any{"id": held.ID, "result": m.Error})
				continue
			}
		case "deltaflood":
			for i := 0; i < 1000; i++ {
				enc.Encode(map[string]any{"method": "item/commandExecution/outputDelta", "params": map[string]string{"threadId": "thread", "turnId": "turn", "itemId": "tool", "delta": strings.Repeat("x", 8192)}})
			}
			enc.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]string{"id": "turn", "status": "completed"}}})
		case "controlflood":
			for i := 0; i < MaxQueueEvents+100; i++ {
				if enc.Encode(map[string]any{"method": "item/started", "params": map[string]int{"n": i}}) != nil {
					os.Exit(0)
				}
			}
		case "timeout":
			if m.Method == "ignore" {
				continue
			}
		case "invalid":
			os.Stdout.WriteString("{bad json}\n")
			continue
		case "exit":
			os.Exit(0)
		}
		enc.Encode(map[string]any{"id": m.ID, "result": string(m.Params)})
	}
}

func TestPendingInteractionLimitKeepsRPCUsable(t *testing.T) {
	c := fixture(t, "pendingflood")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var rejected RPCError
	if err := c.Call(ctx, "trigger", nil, &rejected); err != nil || rejected.Code != -32000 {
		t.Fatalf("overflow response=%+v err=%v", rejected, err)
	}
	c.mu.Lock()
	pending := len(c.server)
	c.mu.Unlock()
	if pending != MaxPendingRequests {
		t.Fatalf("pending requests=%d", pending)
	}
	if err := c.Call(ctx, "echo", 42, nil); err != nil {
		t.Fatal("pending interactions blocked independent RPC:", err)
	}
	for i := 0; i < MaxPendingRequests+1; i++ {
		select {
		case e := <-c.Events():
			if i < MaxPendingRequests && e.Method != "item/fileChange/requestApproval" {
				t.Fatal(e)
			}
			if i == MaxPendingRequests && e.Method != "fuyu/unsupported" {
				t.Fatal("missing visible overflow notice")
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestControlOverflowDisconnectsExplicitly(t *testing.T) {
	c := fixture(t, "controlflood")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_ = c.Call(ctx, "trigger", nil, nil)
	c.queueMu.Lock()
	overflow, count, bytes := c.overflow, len(c.queue), c.queueBytes
	c.queueMu.Unlock()
	if !overflow || count > MaxQueueEvents || bytes > MaxQueueBytes {
		t.Fatalf("overflow=%v count=%d bytes=%d", overflow, count, bytes)
	}
	for {
		select {
		case e, ok := <-c.Events():
			if !ok {
				t.Fatal("closed without explicit disconnect")
			}
			if e.Method == "fuyu/disconnected" {
				if e.Err == nil || !strings.Contains(e.Err.Error(), "buffer exceeded") {
					t.Fatal(e.Err)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestFailedApprovalWriteCannotBeRetried(t *testing.T) {
	c := fixture(t, "interactive")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- c.Call(ctx, "trigger", map[string]any{
			"id": "approval", "method": "item/fileChange/requestApproval",
			"params": map[string]string{"threadId": "thread", "turnId": "turn", "itemId": "file"},
		}, nil)
	}()
	select {
	case e := <-c.Events():
		if e.Method != "item/fileChange/requestApproval" {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	c.process.Stdin = failedPipe{c.process.Stdin}
	if err := c.Respond(json.RawMessage(`"approval"`), map[string]string{"decision": "accept"}); err == nil {
		t.Fatal("closed pipe reported success")
	}
	if err := c.Respond(json.RawMessage(`"approval"`), map[string]string{"decision": "accept"}); err == nil || !strings.Contains(err.Error(), "resolved, expired or already submitted") {
		t.Fatalf("uncertain approval was retried: %v", err)
	}
	c.Close()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("disconnection did not release waiting RPC")
	}
}

func TestMalformedAndUnsupportedInteractionsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name, method string
		params       map[string]any
		code         int
	}{
		{"missing identity", "item/fileChange/requestApproval", map[string]any{"threadId": "thread"}, -32602},
		{"missing permissions", "item/permissions/requestApproval", map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "p"}, -32602},
		{"missing blocking mode", "item/tool/requestUserInput", map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "q",
			"questions": []any{map[string]string{"id": "q"}}}, -32602},
		{"duplicate question", "item/tool/requestUserInput", map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "q", "isBlocking": true,
			"questions": []any{map[string]string{"id": "q"}, map[string]string{"id": "q"}}}, -32602},
		{"unknown method", "future/request", map[string]any{}, -32601},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t, "interactive")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := c.Call(ctx, "trigger", map[string]any{"id": "request", "method": tc.method, "params": tc.params}, nil)
			var rpc *RPCError
			if !errors.As(err, &rpc) || rpc.Code != tc.code {
				t.Fatalf("rejection=%v expected %d", err, tc.code)
			}
			select {
			case e := <-c.Events():
				if e.Method != "fuyu/unsupported" || e.Err == nil {
					t.Fatal(e)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func fixture(t *testing.T, scenario string) *Client {
	t.Helper()
	c, err := startProcess(os.Args[0], []string{"-test.run=^TestProtocolHelper$", "--", scenario}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func TestNotificationFloodDoesNotBlockResponse(t *testing.T) {
	c := fixture(t, "flood")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result string
	if err := c.Call(ctx, "echo", 42, &result); err != nil || result != "42" {
		t.Fatalf("%q %v", result, err)
	}
	for i := 0; i < 1024; i++ {
		select {
		case e := <-c.Events():
			if e.Method != "notice" {
				t.Fatal(e)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
func TestConcurrentResponsesUseRequestIDs(t *testing.T) {
	c := fixture(t, "reverse")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errs := make(chan error, 2)
	for _, value := range []string{"first", "second"} {
		go func(v string) {
			var result string
			err := c.Call(ctx, "echo", v, &result)
			expected, _ := json.Marshal(v)
			if err == nil && result != string(expected) {
				err = errors.New("response crossed request IDs")
			}
			errs <- err
		}(value)
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}
func TestInteractiveRequestDoesNotBlockRPCAndRespondsOnce(t *testing.T) {
	for _, method := range []string{"item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval", "item/tool/requestUserInput"} {
		t.Run(method, func(t *testing.T) {
			c := fixture(t, "interactive")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			params := map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "tool"}
			if method == "item/tool/requestUserInput" {
				params["isBlocking"] = true
				params["questions"] = []any{map[string]any{"id": "q", "header": "Question", "question": "Choose"}}
			}
			if method == "item/permissions/requestApproval" {
				params["permissions"] = map[string]any{"network": map[string]bool{"enabled": true}}
			}
			request := map[string]any{"id": json.RawMessage(`"native-server-id"`), "method": method, "params": params}
			done := make(chan error, 1)
			var result json.RawMessage
			go func() { done <- c.Call(ctx, "trigger", request, &result) }()
			var e Event
			select {
			case e = <-c.Events():
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if e.Method != method || string(e.ID) != `"native-server-id"` {
				t.Fatal(e)
			}
			var echo string
			if err := c.Call(ctx, "echo", 42, &echo); err != nil || echo != "42" {
				t.Fatalf("RPC stalled while awaiting approval: %s %v", echo, err)
			}
			response := map[string]any{"decision": "accept"}
			if err := c.Respond(e.ID, response); err != nil {
				t.Fatal(err)
			}
			if err := c.Respond(e.ID, response); err == nil {
				t.Fatal("duplicate response allowed")
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if string(result) != `{"decision":"accept"}` {
				t.Fatal(string(result))
			}
			select {
			case resolved := <-c.Events():
				if resolved.Method != "serverRequest/resolved" {
					t.Fatal(resolved)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err := c.Respond(e.ID, response); err == nil {
				t.Fatal("resolved request accepted")
			}
		})
	}
}

func TestDeltaFloodPreservesRPCAndTerminalEvent(t *testing.T) {
	c := fixture(t, "deltaflood")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Call(ctx, "trigger", nil, nil); err != nil {
		t.Fatal(err)
	}
	c.queueMu.Lock()
	bytes, count := c.queueBytes, len(c.queue)
	c.queueMu.Unlock()
	if bytes > MaxQueueBytes || count > MaxQueueEvents {
		t.Fatalf("unbounded queue: %d/%d", bytes, count)
	}
	gap := false
	for {
		select {
		case e := <-c.Events():
			if e.Method == "fuyu/eventGap" {
				gap = true
			}
			if e.Method == "turn/completed" {
				if !gap {
					t.Fatal("gap not reported")
				}
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
func TestTimeoutDoesNotPoisonNextRequest(t *testing.T) {
	c := fixture(t, "timeout")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.Call(ctx, "ignore", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	var result string
	if err := c.Call(ctx2, "echo", 7, &result); err != nil || result != "7" {
		t.Fatalf("%q %v", result, err)
	}
}
func TestDisconnectReleasesRequestsAndEvents(t *testing.T) {
	for _, scenario := range []string{"invalid", "exit"} {
		t.Run(scenario, func(t *testing.T) {
			c := fixture(t, scenario)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.Call(ctx, "trigger", nil, nil); err == nil {
				t.Fatal("expected disconnect")
			}
			select {
			case e := <-c.Events():
				if e.Method != "fuyu/disconnected" {
					t.Fatal(e)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case _, ok := <-c.Events():
				if ok {
					t.Fatal("events still open")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			c.Close()
			c.Close()
		})
	}
}

func TestCloseKillsUnresponsiveOwnedServer(t *testing.T) {
	c := fixture(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Call(ctx, "ready", nil, nil); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("unresponsive server was not cleaned up")
	}
	if c.process.Cmd.ProcessState == nil {
		t.Fatal("owned process was not reaped")
	}
}
