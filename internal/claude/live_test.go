package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/app"
)

func TestLiveClaudeFileApprovalResume(t *testing.T) {
	if os.Getenv("FUYU_LIVE_CLAUDE") != "1" {
		t.Skip("set FUYU_LIVE_CLAUDE=1 for real Claude Code verification")
	}
	executable, err := ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	cwd := t.TempDir()
	client, err := New(executable, "", state)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	models, err := client.ListModelsForDirectory(ctx, cwd)
	if err != nil || len(models) == 0 {
		t.Fatalf("models=%v err=%v", models, err)
	}
	thread, err := client.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	wait := func(turn agent.Turn) string {
		t.Helper()
		messages := map[string]string{}
		order := []string{}
		for {
			select {
			case event := <-client.Events():
				if len(event.ID) > 0 {
					request, err := agent.DecodeInteraction(event)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("approval method=%s command=%s", request.Method, request.Command)
					if err := client.Respond(event.ID, map[string]string{"decision": "accept"}); err != nil {
						t.Fatal(err)
					}
				}
				var params struct {
					Turn agent.Turn `json:"turn"`
					Item struct {
						ID   string `json:"id"`
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"item"`
				}
				_ = json.Unmarshal(event.Params, &params)
				if event.Method == "item/completed" && params.Item.Type == "agentMessage" {
					if _, exists := messages[params.Item.ID]; !exists {
						order = append(order, params.Item.ID)
					}
					messages[params.Item.ID] = params.Item.Text
				}
				if event.Method == "turn/completed" && params.Turn.ID == turn.ID {
					if params.Turn.Status != "completed" {
						t.Fatalf("terminal=%s error=%+v", params.Turn.Status, params.Turn.Error)
					}
					text := ""
					for _, id := range order {
						text += messages[id]
					}
					return text
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	first, err := client.Send(ctx, thread.Thread.ID, "In this working directory only, use Bash to write proof.txt containing exactly FUYU_CLAUDE_LIVE followed by a newline. Keep the token FUYU_CLAUDE_LIVE in this conversation for our next turn. Do not save it to memory files, do not use background tasks, and do not change any other files.")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("first reply:", wait(first))
	raw, err := os.ReadFile(filepath.Join(cwd, "proof.txt"))
	if err != nil || string(raw) != "FUYU_CLAUDE_LIVE\n" {
		t.Fatalf("proof=%q err=%v", raw, err)
	}
	second, err := client.Send(ctx, thread.Thread.ID, "Without tools, reply with only the exact token I asked you to remember.")
	if err != nil {
		t.Fatal(err)
	}
	if reply := wait(second); !strings.Contains(reply, "FUYU_CLAUDE_LIVE") {
		t.Fatalf("context=%q", reply)
	}
	page, err := client.ListTurns(ctx, thread.Thread.ID, nil)
	if err != nil || len(page.Data) != 2 {
		t.Fatalf("history=%+v err=%v", page, err)
	}
	client.Close()
	reopened, err := New(executable, "", state)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	resumed, err := reopened.ResumeThread(ctx, thread.Thread.ID)
	if err != nil || resumed.ThreadMeta.Status.Type != "idle" {
		t.Fatalf("resumed=%+v err=%v", resumed, err)
	}
	client = reopened
	third, err := reopened.SendWithModel(ctx, thread.Thread.ID, "Without tools, reply with only the exact token I asked you to remember.", nil, nil, "default")
	if err != nil {
		t.Fatal(err)
	}
	if reply := wait(third); !strings.Contains(reply, "FUYU_CLAUDE_LIVE") {
		t.Fatalf("restart context=%q", reply)
	}
	if os.Getenv("FUYU_LIVE_CLAUDE_COMPACT") == "1" {
		compact, err := reopened.Send(ctx, thread.Thread.ID, "/compact Preserve the exact token FUYU_CLAUDE_LIVE and the proof.txt result. Do not write memory files.")
		if err != nil {
			t.Fatal(err)
		}
		t.Log("compact:", wait(compact))
		page, err := reopened.ListTurns(ctx, thread.Thread.ID, nil)
		if err != nil || len(page.Data) == 0 {
			t.Fatalf("compact history=%+v err=%v", page, err)
		}
		for _, turn := range page.Data {
			if turn.Status == "inProgress" || turn.Status == "unknown" {
				t.Fatal("compact history introduced an unconfirmed turn", turn.ID)
			}
		}
		resumed, err := reopened.ResumeThread(ctx, thread.Thread.ID)
		if err != nil || resumed.ThreadMeta.Status.Type != "idle" {
			t.Fatalf("compact resume=%+v err=%v", resumed, err)
		}
		after, err := reopened.Send(ctx, thread.Thread.ID, "Without tools, reply with only the exact token from before compaction.")
		if err != nil {
			t.Fatal(err)
		}
		if reply := wait(after); !strings.Contains(reply, "FUYU_CLAUDE_LIVE") {
			t.Fatal("compact lost context", reply)
		}
	}
	t.Logf("thread=%s models=%d turns=%d restart=%s", thread.Thread.ID, len(models), len(page.Data), resumed.ThreadMeta.Status.Type)
}

func TestLiveClaudeDenialQuestionAndInterrupt(t *testing.T) {
	if os.Getenv("FUYU_LIVE_CLAUDE") != "1" {
		t.Skip("set FUYU_LIVE_CLAUDE=1 for real Claude Code verification")
	}
	executable, err := ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(executable, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	for _, mode := range []string{"deny", "question", "interrupt"} {
		t.Run(mode, func(t *testing.T) {
			cwd := t.TempDir()
			thread, err := client.StartThread(ctx, cwd)
			if err != nil {
				t.Fatal(err)
			}
			prompt := "Use Bash to write denied.txt containing FUYU_DENIED in this directory. If permission is denied, do not retry or use other tools; just say denied. Do not change other files."
			if mode == "question" {
				prompt = "Use AskUserQuestion now to ask me which platform to target. Provide choices Mac and Linux, with multiSelect true. Do not answer it yourself and do not use any other tools. After my answer, repeat my selected platforms."
			}
			turn, err := client.SendWithModel(ctx, thread.Thread.ID, prompt, nil, nil, "default")
			if err != nil {
				t.Fatal(err)
			}
			interacted := false
			for {
				select {
				case event := <-client.Events():
					if event.Session.SessionID != thread.Thread.ID {
						continue
					}
					if len(event.ID) > 0 {
						request, err := agent.DecodeInteraction(event)
						if err != nil {
							t.Fatal(err)
						}
						interacted = true
						if mode == "interrupt" {
							err = client.Interrupt(ctx, thread.Thread.ID, turn.ID)
						} else if mode == "question" {
							if request.Method != "item/tool/requestUserInput" {
								t.Fatal("expected native question", request.Method)
							}
							answers := map[string]any{}
							for _, question := range request.Questions {
								answers[question.ID] = map[string]any{"answers": []string{"Mac", "Linux"}}
							}
							err = client.Respond(event.ID, map[string]any{"answers": answers})
						} else {
							err = client.Respond(event.ID, map[string]string{"decision": "decline", "feedback": "FUYU_DENIED_FEEDBACK: leave the directory unchanged and do not retry"})
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					if event.Method == "turn/completed" {
						var params struct {
							Turn agent.Turn `json:"turn"`
						}
						_ = json.Unmarshal(event.Params, &params)
						t.Logf("terminal=%s", event.Params)
						if !interacted || mode != "interrupt" && params.Turn.Status != "completed" || mode == "interrupt" && params.Turn.Status != "interrupted" {
							t.Fatalf("interaction=%t terminal=%+v", interacted, params.Turn)
						}
						if _, err := os.Stat(filepath.Join(cwd, "denied.txt")); !os.IsNotExist(err) {
							t.Fatal("denied or interrupted write occurred", err)
						}
						t.Logf("thread=%s interaction=%s status=%s no-write=true", thread.Thread.ID, mode, params.Turn.Status)
						return
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
		})
	}
}

func TestLiveClaudeProcessFailureIsSessionScoped(t *testing.T) {
	if os.Getenv("FUYU_LIVE_CLAUDE") != "1" {
		t.Skip("set FUYU_LIVE_CLAUDE=1 for real process failure verification")
	}
	executable, err := ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	client, err := New(executable, "", state)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	broken, err := client.StartThread(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Send(ctx, broken.Thread.ID, "Use Bash to create failure-proof.txt containing FUYU_FAILURE. Do not write other files, use background tools, or save memory."); err != nil {
		t.Fatal(err)
	}
	var permission json.RawMessage
	for permission == nil {
		select {
		case event := <-client.Events():
			if len(event.ID) > 0 {
				permission = event.ID
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	client.mu.Lock()
	run := client.sessions[broken.Thread.ID].run
	client.mu.Unlock()
	if run == nil {
		t.Fatal("permission has no owning process")
	}
	if err := run.process.Cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	healthy, err := client.StartThread(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Send(ctx, healthy.Thread.ID, "Without tools or memory files reply exactly FUYU_HEALTHY."); err != nil {
		t.Fatal(err)
	}
	brokenDone, healthyDone, disconnected := false, false, false
	for !brokenDone || !healthyDone || !disconnected {
		select {
		case event := <-client.Events():
			if event.Method == "fuyu/disconnected" {
				if event.Session.SessionID != broken.Thread.ID {
					t.Fatal("process failure disconnected other sessions", event.Session)
				}
				disconnected = true
			}
			if event.Method != "turn/completed" {
				continue
			}
			var params struct {
				Turn agent.Turn `json:"turn"`
			}
			_ = json.Unmarshal(event.Params, &params)
			if event.Session.SessionID == broken.Thread.ID {
				brokenDone = params.Turn.Status == "failed"
			} else if event.Session.SessionID == healthy.Thread.ID {
				if params.Turn.Status != "completed" {
					t.Fatal("healthy session failed", string(event.Params))
				}
				healthyDone = true
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err(), brokenDone, healthyDone, disconnected)
		}
	}
	if err := client.Respond(permission, map[string]string{"decision": "accept"}); err == nil {
		t.Fatal("dead process permission remained active")
	}
	if _, err := client.Send(ctx, broken.Thread.ID, "do not replay"); err == nil {
		t.Fatal("unconfirmed task replayed")
	}
	client.Close()
	reopened, err := New(executable, "", state)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Send(ctx, broken.Thread.ID, "do not replay after restart"); err == nil {
		t.Fatal("restart cleared the unknown task guard")
	}
	resumed, err := reopened.ResumeThread(ctx, healthy.Thread.ID)
	if err != nil || resumed.ThreadMeta.Status.Type != "idle" {
		t.Fatalf("healthy recovery=%+v err=%v", resumed, err)
	}
	t.Logf("broken=%s healthy=%s isolated=true restart-guard=true healthy-idle=true", broken.Thread.ID, healthy.Thread.ID)
}

func TestLiveClaudeInterruptStreaming(t *testing.T) {
	if os.Getenv("FUYU_LIVE_CLAUDE") != "1" {
		t.Skip("set FUYU_LIVE_CLAUDE=1 for real streaming interruption")
	}
	executable, err := ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	client, err := New(executable, "", state)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	thread, err := client.StartThread(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := client.Send(ctx, thread.Thread.ID, "Without tools or memory files, write 300 numbered paragraphs explaining trees. Each paragraph must contain at least 40 words. Start directly with paragraph 1.")
	if err != nil {
		t.Fatal(err)
	}
	interrupted := false
	for {
		select {
		case event := <-client.Events():
			if event.Session.SessionID != thread.Thread.ID {
				continue
			}
			if len(event.ID) > 0 {
				t.Fatal("unexpected tool interaction", event.Method)
			}
			if event.Method == "item/agentMessage/delta" && !interrupted {
				var params struct {
					Delta string `json:"delta"`
				}
				if err := json.Unmarshal(event.Params, &params); err != nil {
					t.Fatal(err)
				}
				if params.Delta != "" {
					if err := client.Interrupt(ctx, thread.Thread.ID, turn.ID); err != nil {
						t.Fatal(err)
					}
					interrupted = true
				}
			}
			if event.Method == "turn/completed" {
				var params struct {
					Turn agent.Turn `json:"turn"`
				}
				if err := json.Unmarshal(event.Params, &params); err != nil {
					t.Fatal(err)
				}
				if !interrupted || params.Turn.ID != turn.ID || params.Turn.Status != "interrupted" {
					t.Fatal("streaming interruption failed", string(event.Params))
				}
				client.Close()
				reopened, err := New(executable, "", state)
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.Close()
				resumed, err := reopened.ResumeThread(ctx, thread.Thread.ID)
				if err != nil || resumed.ThreadMeta.Status.Type != "idle" {
					t.Fatalf("interrupted recovery=%+v err=%v", resumed, err)
				}
				page, err := reopened.ListTurns(ctx, thread.Thread.ID, nil)
				if err != nil {
					t.Fatal(err)
				}
				_, unfinished, err := app.History(resumed.Cwd, thread.Thread.ID, page.Data)
				if err != nil || unfinished || len(page.Data) != 1 {
					t.Fatalf("interrupted history=%+v unfinished=%t err=%v", page, unfinished, err)
				}
				t.Logf("thread=%s streaming-delta=true interrupted=true reopened-idle=true", thread.Thread.ID)
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestLiveClaudeInterruptRunningTool(t *testing.T) {
	for _, background := range []bool{false, true} {
		name := "foreground"
		if background {
			name = "background"
		}
		t.Run(name, func(t *testing.T) { testLiveClaudeInterruptTool(t, background, false) })
	}
	t.Run("resumed", func(t *testing.T) { testLiveClaudeInterruptTool(t, false, true) })
}

func testLiveClaudeInterruptTool(t *testing.T, background, resumed bool) {
	t.Helper()
	if os.Getenv("FUYU_LIVE_CLAUDE") != "1" {
		t.Skip("set FUYU_LIVE_CLAUDE=1 for running tool interruption")
	}
	executable, err := ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(executable, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cwd := t.TempDir()
	thread, err := client.StartThread(ctx, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if resumed {
		warmup, err := client.Send(ctx, thread.Thread.ID, "Reply only FUYU_RESUME_STOP_READY. Do not use tools or write memory.")
		if err != nil {
			t.Fatal(err)
		}
		ready := false
		for !ready {
			select {
			case event := <-client.Events():
				if len(event.ID) > 0 {
					t.Fatal("unexpected warmup interaction", event.Method)
				}
				if event.Method == "turn/completed" {
					var params struct {
						Turn agent.Turn `json:"turn"`
					}
					if err := json.Unmarshal(event.Params, &params); err != nil {
						t.Fatal(err)
					}
					if params.Turn.ID != warmup.ID || params.Turn.Status != "completed" {
						t.Fatal("warmup failed", string(event.Params))
					}
					ready = true
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	prompt := "Use Bash to run exactly this foreground command in the current directory: printf '%s' $$ > task.pid; printf started > started.txt; sleep 30; printf forbidden > forbidden.txt . Do not change other files, use background tools, or write memory. We will interrupt the running command."
	if background {
		prompt = "Use Bash with run_in_background=true to run exactly this command in the current directory: printf '%s' $$ > task.pid; printf started > started.txt; sleep 30; printf forbidden > forbidden.txt . Then wait using TaskOutput. Do not change other files or write memory. We will interrupt while the background command is running."
	}
	turn, err := client.Send(ctx, thread.Thread.ID, prompt)
	if err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	interrupted := false
	backgroundObserved := !background
	for {
		select {
		case event := <-client.Events():
			if event.Method == "item/started" {
				var params struct {
					Item struct {
						Arguments struct {
							Background bool `json:"run_in_background"`
						} `json:"arguments"`
					} `json:"item"`
				}
				if err := json.Unmarshal(event.Params, &params); err == nil && params.Item.Arguments.Background {
					backgroundObserved = true
				}
			}
			if len(event.ID) > 0 {
				if err := client.Respond(event.ID, map[string]string{"decision": "accept"}); err != nil {
					t.Fatal(err)
				}
			}
			if event.Method == "turn/completed" {
				var params struct {
					Turn agent.Turn `json:"turn"`
				}
				_ = json.Unmarshal(event.Params, &params)
				if !interrupted || !backgroundObserved || params.Turn.Status != "interrupted" {
					t.Fatal("running tool interruption failed", string(event.Params))
				}
				if _, err := os.Stat(filepath.Join(cwd, "forbidden.txt")); !os.IsNotExist(err) {
					t.Fatal("interrupted command reached its side effect", err)
				}
				raw, err := os.ReadFile(filepath.Join(cwd, "task.pid"))
				if err != nil {
					t.Fatal(err)
				}
				pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
				if err != nil || pid <= 1 {
					t.Fatal("invalid owned tool process", string(raw), err)
				}
				if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
					t.Fatalf("owned Bash pid %d is still present: %v", pid, err)
				}
				t.Logf("thread=%s resumed=%t stopped-running-tool=true bash-pid=%d absent=true forbidden-file=false", thread.Thread.ID, resumed, pid)
				return
			}
		case <-ticker.C:
			if !interrupted {
				if _, err := os.Stat(filepath.Join(cwd, "started.txt")); err == nil {
					interrupted = true
					if err := client.Interrupt(ctx, thread.Thread.ID, turn.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
