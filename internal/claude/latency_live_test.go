package claude

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func TestLiveClaudeProcessLatency(t *testing.T) {
	if os.Getenv("FUYU_LIVE_CLAUDE") != "1" {
		t.Skip("set FUYU_LIVE_CLAUDE=1 for native latency samples")
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	thread, err := client.StartThread(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var last *execution
	for _, mode := range []string{"cold", "warm", "cold-resume"} {
		if mode == "cold-resume" {
			last.expireIdle()
		}
		started := time.Now()
		turn, err := client.Send(ctx, thread.Thread.ID, "Without tools or memory files, reply exactly FUYU_LATENCY_READY.")
		if err != nil {
			t.Fatal(err)
		}
		submitted := time.Since(started)
		var firstVisible time.Duration
		text := ""
		for {
			select {
			case event := <-client.Events():
				if len(event.ID) > 0 {
					t.Fatal("no-tool latency probe requested an interaction")
				}
				var p struct {
					Turn  agent.Turn `json:"turn"`
					Delta string     `json:"delta"`
					Item  struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"item"`
				}
				_ = json.Unmarshal(event.Params, &p)
				if p.Delta != "" || p.Item.Type == "agentMessage" && p.Item.Text != "" {
					if firstVisible == 0 {
						firstVisible = time.Since(started)
					}
					if p.Item.Type == "agentMessage" {
						text = p.Item.Text
					}
				}
				if event.Method == "turn/completed" && p.Turn.ID == turn.ID {
					if p.Turn.Status != "completed" || !strings.Contains(text, "FUYU_LATENCY_READY") {
						t.Fatalf("mode=%s terminal=%+v text=%q", mode, p.Turn, text)
					}
					current := idleExecution(t, client, thread.Thread.ID)
					if mode == "warm" && current.process != last.process ||
						mode == "cold-resume" && current.process == last.process {
						t.Fatal("sample did not use its intended process strategy")
					}
					t.Logf("mode=%s PID=%d submit=%s first-visible=%s completed=%s", mode, current.process.Cmd.Process.Pid, submitted, firstVisible, time.Since(started))
					last = current
					goto next
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	next:
	}
}
