package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
)

func fixtureClient(t *testing.T) *Client {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	script, err := filepath.Abs("testdata/server.py")
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec '"+python+"' -I '"+script+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := New(executable, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func waitTerminal(t *testing.T, client *Client, id string) agent.Turn {
	t.Helper()
	for {
		select {
		case event := <-client.Events():
			if event.Method != "turn/completed" {
				continue
			}
			var params struct {
				Thread string     `json:"threadId"`
				Turn   agent.Turn `json:"turn"`
			}
			_ = json.Unmarshal(event.Params, &params)
			if params.Thread == id {
				return params.Turn
			}
		case <-time.After(10 * time.Second):
			t.Fatal("terminal not received")
		}
	}
}

func TestPermissionDenialAndQuestionMapping(t *testing.T) {
	for _, kind := range []string{"deny", "question"} {
		t.Run(kind, func(t *testing.T) {
			client := fixtureClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cwd := t.TempDir()
			thread, err := client.StartThread(ctx, cwd)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Send(ctx, thread.Thread.ID, kind); err != nil {
				t.Fatal(err)
			}
			var id json.RawMessage
			for id == nil {
				select {
				case event := <-client.Events():
					if len(event.ID) == 0 {
						continue
					}
					id = event.ID
					request, err := agent.DecodeInteraction(event)
					if err != nil {
						t.Fatal(err)
					}
					if kind == "deny" {
						if err := client.Respond(id, map[string]string{"decision": "acceptForSession"}); err == nil {
							t.Fatal("unsupported session authorization accepted")
						}
					}
					var response any = map[string]string{"decision": "decline", "feedback": "leave unchanged"}
					if kind == "question" {
						if len(request.Questions) != 1 || !request.Questions[0].MultiSelect {
							t.Fatal("question structure lost", request)
						}
						response = map[string]any{"answers": map[string]any{"0": map[string]any{"answers": []string{"Mac", "Linux"}}}}
					}
					if err := client.Respond(id, response); err != nil {
						t.Fatal(err)
					}
					if err := client.Respond(id, response); err == nil {
						t.Fatal("duplicate response accepted")
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if terminal := waitTerminal(t, client, thread.Thread.ID); terminal.Status != "completed" {
				t.Fatal(terminal)
			}
			raw, err := os.ReadFile(filepath.Join(cwd, "decision.json"))
			if err != nil {
				t.Fatal(err)
			}
			var decision struct {
				Behavior string `json:"behavior"`
				Message  string `json:"message"`
				Input    struct {
					Answers map[string]string `json:"answers"`
				} `json:"updatedInput"`
			}
			if err := json.Unmarshal(raw, &decision); err != nil {
				t.Fatal(err)
			}
			if kind == "deny" {
				if decision.Behavior != "deny" || decision.Message != "leave unchanged" {
					t.Fatal("deny became allow")
				}
				if _, err := os.Stat(filepath.Join(cwd, "approved.txt")); !os.IsNotExist(err) {
					t.Fatal("denied side effect occurred", err)
				}
			} else if decision.Behavior != "allow" || decision.Input.Answers["Which targets?"] != "Mac, Linux" {
				t.Fatal("native question response mismatch", string(raw))
			}
		})
	}
}

func TestCatalogUsesBoundConfigurationHome(t *testing.T) {
	client := fixtureClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := client.catalog(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Home string `json:"configHome"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Home != client.Home() {
		t.Fatal("runtime configuration root differs from history root", string(raw))
	}
}

func TestFrameSupportsCompactionStringMessages(t *testing.T) {
	var message frame
	if err := json.Unmarshal([]byte(`{"type":"user","message":{"role":"user","content":"compact summary"}}`), &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Message.Content) != 1 || message.Message.Content[0].Text != "compact summary" {
		t.Fatal("string message content lost")
	}
	if err := json.Unmarshal([]byte(`{"type":"user","message":{"content":{"bad":"object"}}}`), &message); err == nil {
		t.Fatal("invalid message content accepted")
	}
}

func TestCompactionProjectsRunningAndCompletedStatus(t *testing.T) {
	client := fixtureClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cwd := t.TempDir()
	thread, err := client.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := client.Send(ctx, thread.Thread.ID, "/compact\tkeep focus")
	if err != nil {
		t.Fatal(err)
	}
	state := app.New(cwd)
	state.ThreadID = thread.Thread.ID
	itemID := "compact-" + turn.ID
	for _, want := range []string{"正在压缩上下文", "上下文已压缩"} {
		for {
			select {
			case event := <-client.Events():
				state.Apply(event)
			case <-ctx.Done():
				t.Fatalf("compaction did not reach %q", want)
			}
			if len(state.Messages) > 0 && state.Messages[0].ID == itemID && state.Messages[0].Text == want {
				break
			}
		}
	}
	if terminal := waitTerminal(t, client, thread.Thread.ID); terminal.Status != "completed" {
		t.Fatalf("unexpected terminal: %+v", terminal)
	}
}

func TestCompactionReconnectsPreservedSegmentAndLocalCommand(t *testing.T) {
	client := fixtureClient(t)
	thread, err := client.StartThread(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"type":"user","uuid":"old-user","parentUuid":null,"message":{"content":"superseded"}}
{"type":"assistant","uuid":"preserved","parentUuid":"old-user","message":{"id":"response","content":[{"type":"text","text":"preserved answer"}]}}
{"type":"system","subtype":"compact_boundary","uuid":"boundary","parentUuid":null,"compactMetadata":{"preservedSegment":{"headUuid":"preserved","anchorUuid":"summary","tailUuid":"preserved"},"preservedMessages":{"anchorUuid":"summary","uuids":["preserved"],"allUuids":["preserved","non-message-marker"]}}}
{"type":"user","uuid":"summary","parentUuid":"boundary","isCompactSummary":true,"message":{"content":"summary text"}}
{"type":"user","uuid":"caveat","parentUuid":"preserved","isMeta":true,"message":{"content":"internal caveat"}}
{"type":"user","uuid":"command","parentUuid":"caveat","promptId":"compact-input","message":{"content":"<command-name>/compact</command-name>"}}
{"type":"user","uuid":"stdout","parentUuid":"command","repeatsCompactSummary":true,"message":{"content":"<local-command-stdout>private hook details</local-command-stdout>"}}
`)
	chain, err := conversation(raw, thread.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 6 || chain[0].UUID != "boundary" || chain[1].UUID != "summary" || chain[2].UUID != "preserved" {
		t.Fatal("compaction did not replace old context and reconnect preserved content", chain)
	}
	dir := filepath.Join(client.home, "projects", "fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, thread.Thread.ID+".jsonl"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s := session{ID: thread.Thread.ID, Cwd: "/project", Turns: []checkpoint{{ID: "compact-turn", UserUUID: "compact-input", Status: "completed", Command: "/compact"}}}
	turns, _, err := client.history(s)
	if err != nil || len(turns) != 2 || turns[0].Status != "completed" || turns[1].ID != "compact-turn" || turns[1].Status != "completed" {
		t.Fatalf("compact history=%+v err=%v", turns, err)
	}
	encoded, _ := json.Marshal(turns)
	if strings.Contains(string(encoded), "private hook") || strings.Contains(string(encoded), "internal caveat") || strings.Contains(string(encoded), "superseded") {
		t.Fatal("internal command metadata leaked into history", string(encoded))
	}
	if !strings.Contains(string(encoded), `"type":"contextCompaction"`) {
		t.Fatal("compaction marker was missing from history", string(encoded))
	}
}

func TestEqualNativeRequestIDsRemainSessionScoped(t *testing.T) {
	client := fixtureClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var threads []agent.Thread
	for range 2 {
		thread, err := client.StartThread(ctx, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		threads = append(threads, thread)
		if _, err := client.Send(ctx, thread.Thread.ID, "wait"); err != nil {
			t.Fatal(err)
		}
	}
	requests := map[string]json.RawMessage{}
	for len(requests) != 2 {
		select {
		case event := <-client.Events():
			if len(event.ID) > 0 {
				requests[event.Session.SessionID] = event.ID
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if string(requests[threads[0].Thread.ID]) == string(requests[threads[1].Thread.ID]) {
		t.Fatal("same native ID collided across processes")
	}
	if err := client.Respond(requests[threads[0].Thread.ID], map[string]string{"decision": "accept"}); err != nil {
		t.Fatal(err)
	}
	if terminal := waitTerminal(t, client, threads[0].Thread.ID); terminal.Status != "completed" {
		t.Fatal(terminal)
	}
	if _, err := os.Stat(filepath.Join(client.sessions[threads[1].Thread.ID].Cwd, "approved.txt")); !os.IsNotExist(err) {
		t.Fatal("response affected the other process")
	}
	if err := client.Respond(requests[threads[1].Thread.ID], map[string]string{"decision": "decline"}); err != nil {
		t.Fatal(err)
	}
	if terminal := waitTerminal(t, client, threads[1].Thread.ID); terminal.Status != "completed" {
		t.Fatal(terminal)
	}
}

func TestDiagnosticsRemainBoundedAndDoNotExposeRawCredentials(t *testing.T) {
	buffer := &diagnosticBuffer{}
	_, _ = buffer.Write([]byte("authentication failed secret-token-value\n"))
	_, _ = buffer.Write([]byte(strings.Repeat("x", 100000)))
	if len(buffer.data) != 64<<10 || !strings.Contains(buffer.hint(), "认证") || strings.Contains(buffer.hint(), "secret-token") {
		t.Fatal("diagnostics leaked raw output or exceeded limit")
	}
}

func TestApprovalWaitDoesNotBlockInterruptOrOtherSession(t *testing.T) {
	client := fixtureClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, err := client.StartThread(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := client.Send(ctx, first.Thread.ID, "wait")
	if err != nil {
		t.Fatal(err)
	}
	var requestID json.RawMessage
	for requestID == nil {
		select {
		case event := <-client.Events():
			if len(event.ID) > 0 {
				requestID = event.ID
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if err := client.Interrupt(ctx, first.Thread.ID, turn.ID); err != nil {
		t.Fatal(err)
	}
	if terminal := waitTerminal(t, client, first.Thread.ID); terminal.Status != "interrupted" {
		t.Fatalf("terminal=%+v", terminal)
	}
	if err := client.Respond(requestID, map[string]string{"decision": "accept"}); err == nil {
		t.Fatal("expired permission accepted")
	}
	second, err := client.StartThread(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Send(ctx, second.Thread.ID, "failed-result"); err != nil {
		t.Fatal(err)
	}
	if terminal := waitTerminal(t, client, second.Thread.ID); terminal.Status != "failed" {
		t.Fatalf("terminal=%+v", terminal)
	}
}

func TestStreamingAbortRequiresInterruptAcknowledgement(t *testing.T) {
	for _, acknowledged := range []bool{false, true} {
		t.Run(fmt.Sprint(acknowledged), func(t *testing.T) {
			client := fixtureClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			thread, err := client.StartThread(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			prompt := "failed-result-streaming"
			if acknowledged {
				prompt = "wait-streaming"
			}
			turn, err := client.Send(ctx, thread.Thread.ID, prompt)
			if err != nil {
				t.Fatal(err)
			}
			if acknowledged {
				for {
					select {
					case event := <-client.Events():
						if len(event.ID) == 0 {
							continue
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					break
				}
				if err := client.Interrupt(ctx, thread.Thread.ID, turn.ID); err != nil {
					t.Fatal(err)
				}
			}
			expected := "failed"
			if acknowledged {
				expected = "interrupted"
			}
			if terminal := waitTerminal(t, client, thread.Thread.ID); terminal.Status != expected {
				t.Fatalf("terminal=%+v expected=%s", terminal, expected)
			}
		})
	}
}

func TestUnexpectedExitAndMalformedFramesRemainUnknown(t *testing.T) {
	for _, prompt := range []string{"unexpected-exit", "bad-json", "truncated-json", "overlong", "unknown-result"} {
		t.Run(prompt, func(t *testing.T) {
			client := fixtureClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			thread, err := client.StartThread(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Send(ctx, thread.Thread.ID, prompt); err != nil {
				t.Fatal(err)
			}
			terminal := waitTerminal(t, client, thread.Thread.ID)
			if terminal.Status != "failed" || terminal.Error == nil {
				t.Fatalf("unknown exit presented as success: %+v", terminal)
			}
			s, _ := client.sessionCopy(thread.Thread.ID)
			if s.Turns[0].Status != "unknown" {
				t.Fatal("uncertain native terminal lost")
			}
			if _, err := client.Send(ctx, thread.Thread.ID, "replay"); err == nil || !strings.Contains(err.Error(), "核验") {
				t.Fatal("uncertain task replayed", err)
			}
		})
	}
}

func TestFragmentedFramesAndCancelledPermissionReplay(t *testing.T) {
	for _, prompt := range []string{"fragmented", "cancel-permission"} {
		t.Run(prompt, func(t *testing.T) {
			client := fixtureClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cwd := t.TempDir()
			thread, err := client.StartThread(ctx, cwd)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Send(ctx, thread.Thread.ID, prompt); err != nil {
				t.Fatal(err)
			}
			requests, resolutions := 0, 0
			var requestID json.RawMessage
			for {
				select {
				case event := <-client.Events():
					if len(event.ID) > 0 {
						requests++
						requestID = event.ID
					}
					if event.Method == "serverRequest/resolved" {
						resolutions++
					}
					if event.Method != "turn/completed" {
						continue
					}
					var params struct {
						Turn agent.Turn `json:"turn"`
					}
					if err := json.Unmarshal(event.Params, &params); err != nil || params.Turn.Status != "completed" {
						t.Fatalf("terminal=%s err=%v", event.Params, err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				break
			}
			if prompt == "cancel-permission" {
				if requests != 1 || resolutions != 1 {
					t.Fatalf("requests=%d resolutions=%d", requests, resolutions)
				}
				if err := client.Respond(requestID, map[string]string{"decision": "accept"}); err == nil {
					t.Fatal("cancelled permission accepted a late answer")
				}
			}
			if _, err := os.Stat(filepath.Join(cwd, "approved.txt")); !os.IsNotExist(err) {
				t.Fatal("unsolicited permission side effect", err)
			}
		})
	}
}

func TestProjectionReplacesStreamedTextAndPairsTools(t *testing.T) {
	c := &Client{}
	c.queue.start()
	defer c.queue.close()
	r := &execution{client: c, session: &session{ID: "session", Cwd: "/project"}, identity: agent.TurnIdentity{Session: agent.SessionKey{Agent: agent.Claude, SessionID: "session"}, ID: "turn"}, blocks: map[int]*contentBlock{}, tools: map[string]map[string]any{}, seen: map[string]bool{}, background: map[string]bool{}}
	frames := []string{
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"message"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}}`,
		`{"type":"assistant","uuid":"assistant","message":{"id":"message","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
		`{"type":"assistant","uuid":"tool","message":{"id":"tools","content":[{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"pwd"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"/project"}]}}`,
	}
	state := app.New("/project")
	state.ThreadID = "session"
	state.Begin("test")
	for _, raw := range frames {
		var message frame
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			t.Fatal(err)
		}
		r.project(message)
	}
	r.emit("turn/completed", map[string]any{"threadId": "session", "turn": map[string]any{"id": "turn", "status": "completed"}})
	for {
		select {
		case event := <-c.Events():
			state.Apply(event)
			if event.Method == "turn/completed" {
				if len(state.Messages) != 3 || state.Messages[1].Text != "hello" || state.Messages[2].Tool.Output != "/project" {
					t.Fatalf("projection=%+v", state.Messages)
				}
				return
			}
		case <-time.After(time.Second):
			t.Fatal("missing events")
		}
	}
}

func TestConversationUsesActiveBranchAndRecoversParallelResults(t *testing.T) {
	raw := []byte(`{"type":"user","uuid":"user","parentUuid":null,"message":{"content":[{"type":"text","text":"task"}]}}
{"type":"assistant","uuid":"call-a","parentUuid":"user","message":{"id":"response","content":[{"type":"tool_use","id":"a","name":"Read","input":{}}]}}
{"type":"assistant","uuid":"call-b","parentUuid":"call-a","message":{"id":"response","content":[{"type":"tool_use","id":"b","name":"Read","input":{}}]}}
{"type":"user","uuid":"result-a","parentUuid":"call-a","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"a"}]}}
{"type":"user","uuid":"result-b","parentUuid":"call-b","message":{"content":[{"type":"tool_result","tool_use_id":"b","content":"b"}]}}
{"type":"assistant","uuid":"final","parentUuid":"result-a","message":{"id":"final","content":[{"type":"text","text":"done"}]}}
{"type":"assistant","uuid":"side","parentUuid":"user","isSidechain":true,"message":{"id":"side","content":[{"type":"text","text":"ignore"}]}}
`)
	chain, err := conversation(raw, "session")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range chain {
		seen[entry.UUID] = true
	}
	if !seen["result-b"] || !seen["call-b"] || seen["side"] {
		t.Fatal("parallel result lost or sidechain selected", seen)
	}
	if _, err := conversation([]byte("{bad\n"), "session"); err == nil {
		t.Fatal("corrupt history accepted")
	}
	if _, err := conversation([]byte(`{"type":"user","uuid":"a","parentUuid":"missing"}`), "session"); err == nil {
		t.Fatal("missing parent accepted")
	}
}

func TestCheckpointKeepsUnknownTaskBlockedAfterRestart(t *testing.T) {
	c, err := New("unused", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	thread, err := c.StartThread(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	s := c.sessions[thread.Thread.ID]
	s.Turns = []checkpoint{{ID: "pending", UserUUID: "user", Status: "unknown"}}
	if err := c.save(s); err != nil {
		t.Fatal(err)
	}
	c.mu.Unlock()
	if _, err := c.Send(context.Background(), thread.Thread.ID, "do not replay"); err == nil {
		t.Fatal("unknown task replayed")
	}
}

func TestHistoryPaginationKeepsWholeTurnsAndRejectsChangedCursor(t *testing.T) {
	home := t.TempDir()
	client, err := New("unused", home, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	thread, err := client.StartThread(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "projects", "project")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, thread.Thread.ID+".jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	parent := ""
	s := client.sessions[thread.Thread.ID]
	for index := range 25 {
		user := fmt.Sprintf("user-%d", index)
		assistant := fmt.Sprintf("assistant-%d", index)
		for _, entry := range []map[string]any{
			{"type": "user", "uuid": user, "parentUuid": parent, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "task"}}}},
			{"type": "assistant", "uuid": assistant, "parentUuid": user, "message": map[string]any{"id": assistant, "content": []any{map[string]any{"type": "text", "text": "answer"}}}},
		} {
			raw, _ := json.Marshal(entry)
			fmt.Fprintln(file, string(raw))
		}
		s.Turns = append(s.Turns, checkpoint{ID: fmt.Sprintf("turn-%d", index), UserUUID: user, Status: "completed"})
		parent = assistant
	}
	file.Close()
	first, err := client.ListTurns(context.Background(), s.ID, nil)
	if err != nil || len(first.Data) != 20 || first.NextCursor == nil {
		t.Fatalf("first=%+v %v", first, err)
	}
	second, err := client.ListTurns(context.Background(), s.ID, first.NextCursor)
	if err != nil || len(second.Data) != 5 || second.NextCursor != nil {
		t.Fatalf("second=%+v %v", second, err)
	}
	file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(file, `{"type":"cost-state"}`)
	file.Close()
	if _, err := client.ListTurns(context.Background(), s.ID, first.NextCursor); err == nil {
		t.Fatal("stale cursor accepted")
	}
}
