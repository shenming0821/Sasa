package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"
)

func TestRecordedMacTaskLifecycle(t *testing.T) {
	for _, mode := range []string{"subagent", "background", "subagent-tool"} {
		t.Run(mode, func(t *testing.T) {
			file, err := os.Open(filepath.Join("testdata", "mac-"+mode+"-events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			client := &Client{}
			client.queue.start()
			defer client.queue.close()
			run := &execution{
				client: client, session: &session{Cwd: "/isolated-recording"},
				identity: agent.TurnIdentity{ID: "recorded-turn"},
				blocks:   map[int]*contentBlock{}, tools: map[string]map[string]any{}, seen: map[string]bool{}, background: map[string]bool{},
			}
			scanner := bufio.NewScanner(file)
			scanner.Buffer(make([]byte, 64<<10), agent.MaxFrameBytes)
			for scanner.Scan() {
				var record struct {
					Direction string `json:"direction"`
					Frame     frame  `json:"frame"`
				}
				if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
					t.Fatal(err)
				}
				if record.Direction != "stdout" || record.Frame.SessionID == "" {
					t.Fatal("recording lost native frame origin")
				}
				if run.session.ID == "" {
					run.session.ID = record.Frame.SessionID
					run.identity.Session = agent.SessionKey{Agent: agent.Claude, SessionID: run.session.ID}
				}
				run.project(record.Frame)
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			state := app.New(run.session.Cwd)
			state.ThreadID = run.session.ID
			state.Begin("replay recorded lifecycle")
			run.emit("turn/completed", map[string]any{"threadId": run.session.ID, "turn": map[string]any{"id": run.identity.ID, "status": "completed"}})
			deadline := time.After(2 * time.Second)
			for {
				select {
				case event := <-client.Events():
					state.Apply(event)
					if event.Method != "turn/completed" {
						continue
					}
				case <-deadline:
					t.Fatal("recorded events did not drain")
				}
				break
			}
			childTools, backgroundTools := 0, 0
			for _, message := range state.Messages {
				if message.Tool == nil {
					continue
				}
				if !message.Done || message.Tool.Status == "inProgress" || message.Tool.AwaitingFinal {
					t.Fatalf("recorded tool remains active: %+v", message.Tool)
				}
				if message.Tool.ParentToolID != "" {
					childTools++
				}
				var input struct {
					Background bool `json:"run_in_background"`
				}
				if json.Unmarshal([]byte(message.Tool.Input), &input) == nil && input.Background {
					backgroundTools++
				}
			}
			if mode == "subagent-tool" && childTools == 0 || mode == "background" && backgroundTools == 0 {
				t.Fatalf("missing recorded task: children=%d background=%d", childTools, backgroundTools)
			}
		})
	}
}

func TestSubagentToolIdentityAndLateBackgroundResult(t *testing.T) {
	client := &Client{}
	client.queue.start()
	defer client.queue.close()
	run := &execution{
		client: client, session: &session{ID: "session", Cwd: "/project"},
		identity: agent.TurnIdentity{Session: agent.SessionKey{Agent: agent.Claude, SessionID: "session"}, ID: "turn"},
		blocks:   map[int]*contentBlock{}, tools: map[string]map[string]any{}, seen: map[string]bool{}, background: map[string]bool{},
	}
	frames := []string{
		`{"type":"assistant","uuid":"parent","message":{"id":"parent-message","content":[{"type":"tool_use","id":"parent-tool","name":"Agent","input":{}}]}}`,
		`{"type":"assistant","uuid":"child","parent_tool_use_id":"parent-tool","message":{"id":"child-message","content":[{"type":"tool_use","id":"child-tool","name":"Bash","input":{"command":"printf child"}}]}}`,
		`{"type":"assistant","uuid":"child-text","parent_tool_use_id":"parent-tool","message":{"id":"child-text-message","content":[{"type":"text","text":"private child narration"}]}}`,
		`{"type":"user","parent_tool_use_id":"parent-tool","message":{"content":[{"type":"tool_result","tool_use_id":"child-tool","content":"child"}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"parent-tool","content":"parent complete"}]}}`,
		`{"type":"assistant","uuid":"background","message":{"id":"background-message","content":[{"type":"tool_use","id":"background-tool","name":"Bash","input":{"command":"sleep 2","run_in_background":true}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"background-tool","content":"running in background"}]}}`,
	}
	state := app.New("/project")
	state.ThreadID = "session"
	state.Begin("task")
	for _, raw := range frames {
		var message frame
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			t.Fatal(err)
		}
		run.project(message)
	}
	run.emit("turn/completed", map[string]any{"threadId": "session", "turn": map[string]any{"id": "turn", "status": "completed"}})
	run.project(frame{Type: "system", Subtype: "task_notification", ToolUseID: "background-tool", TaskID: "background-task", Status: "completed", Summary: "background complete"})
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-client.Events():
			state.Apply(event)
			if event.Method != "item/completed" || !strings.Contains(string(event.Params), "background complete") {
				continue
			}
			if state.Busy || len(state.Messages) != 4 {
				t.Fatalf("busy=%t messages=%+v", state.Busy, state.Messages)
			}
			child := state.Messages[2]
			if child.ID != "child-tool" || child.Tool.ParentToolID != "parent-tool" || child.Tool.Output != "child" || !child.Done {
				t.Fatalf("child=%+v tool=%+v", child, child.Tool)
			}
			background := state.Messages[3]
			if background.ID != "background-tool" || background.Tool.Status != "completed" || !background.Done || background.Tool.AwaitingFinal {
				t.Fatalf("background=%+v tool=%+v", background, background.Tool)
			}
			return
		case <-deadline:
			t.Fatal("late background result was not applied")
		}
	}
}
