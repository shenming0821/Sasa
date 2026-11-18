package codex

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/attachment"
)

func TestSteerPassesActiveTurnMessageSkillsAndImages(t *testing.T) {
	client := fixture(t, "steer")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Steer(ctx, "thread", "active", "message", "supplement", []Skill{{Name: "design", Path: "/skills/design"}}, []attachment.Image{{Path: "/owned/image.png"}}); err != nil {
		t.Fatal(err)
	}
}

func TestLiveContinuousSteer(t *testing.T) {
	if os.Getenv("SASA_LIVE_STEER") != "1" {
		t.Skip("set SASA_LIVE_STEER=1 for native continuous input verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client, err := Start(ctx, "codex", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if threadID := os.Getenv("SASA_LIVE_STEER_THREAD"); threadID != "" {
		raw, err := client.ReadThread(ctx, threadID)
		if err != nil {
			t.Fatal(err)
		}
		verifySteerHistory(t, raw, "")
		return
	}
	thread, err := client.StartThread(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := client.Send(ctx, thread.Thread.ID, "请详细解释 Go 并发模型，分 20 点，每点写三句。不使用工具。")
	if err != nil {
		t.Fatal(err)
	}
	steered := false
	for {
		select {
		case event, open := <-client.Events():
			if !open {
				t.Fatal("runtime disconnected")
			}
			if !steered && event.Method == "item/agentMessage/delta" {
				for _, supplement := range []struct{ id, text string }{
					{"a777f4d0-83fa-48d0-a434-02ba7a395701", "补充：最后单独写一行 FIRST_FOLLOWUP。"},
					{"a777f4d0-83fa-48d0-a434-02ba7a395702", "再补充：将解释缩短到三点，末尾保留 FIRST_FOLLOWUP，再写 SECOND_FOLLOWUP。"},
				} {
					if err := client.Steer(ctx, thread.Thread.ID, turn.ID, supplement.id, supplement.text, nil, nil); err != nil {
						t.Fatal(err)
					}
				}
				steered = true
			}
			if event.Method != "turn/completed" {
				continue
			}
			if !steered {
				t.Fatal("task completed before supplementary input")
			}
			raw, err := client.ReadThread(ctx, thread.Thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			verifySteerHistory(t, raw, turn.ID)
			return
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func verifySteerHistory(t *testing.T, raw json.RawMessage, turnID string) {
	t.Helper()
	var snapshot struct {
		Thread struct {
			Turns []struct {
				ID, Status string
				Items      []struct {
					ID, Type, Text string
					Content        []struct{ Type, Text string }
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil || len(snapshot.Thread.Turns) != 1 || turnID != "" && snapshot.Thread.Turns[0].ID != turnID {
		t.Fatal("supplementary input created a different turn", err)
	}
	users := make(map[string]bool)
	texts := make(map[string]bool)
	response := ""
	for _, item := range snapshot.Thread.Turns[0].Items {
		if item.Type == "userMessage" {
			users[item.ID] = true
			for _, content := range item.Content {
				texts[content.Text] = true
			}
		} else if item.Type == "agentMessage" {
			response += item.Text
		}
	}
	if len(users) != 3 || !texts["补充：最后单独写一行 FIRST_FOLLOWUP。"] || !texts["再补充：将解释缩短到三点，末尾保留 FIRST_FOLLOWUP，再写 SECOND_FOLLOWUP。"] {
		t.Fatal("supplemental messages missing from the native turn")
	}
	if snapshot.Thread.Turns[0].Status != "completed" || !strings.Contains(response, "FIRST_FOLLOWUP") || !strings.Contains(response, "SECOND_FOLLOWUP") {
		t.Fatal("runtime did not incorporate both supplements")
	}
	t.Logf("one native turn, %d acknowledged user messages, status=%s", len(users), snapshot.Thread.Turns[0].Status)
}
