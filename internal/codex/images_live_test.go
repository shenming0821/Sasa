package codex

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Sasa/internal/attachment"
)

// This opt-in sends only a generated geometric image in an ephemeral thread.
func TestLiveImageInput(t *testing.T) {
	if os.Getenv("SASA_LIVE_IMAGES") != "1" {
		t.Skip("set SASA_LIVE_IMAGES=1 for generated-image native verification")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "shape.png")
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(64, 64, 192, 192), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client, err := Start(ctx, "codex", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var thread Thread
	if err := client.Call(ctx, "thread/start", map[string]any{"cwd": dir, "ephemeral": true, "sandbox": "read-only", "approvalPolicy": "never"}, &thread); err != nil {
		t.Fatal(err)
	}
	turn, err := client.SendWithImages(ctx, thread.Thread.ID, "仅识别所附图片中居中图形的颜色和形状。不要使用任何工具或命令。请只回复中文短语。", nil, []attachment.Image{{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native model=%s turn=%s sandbox=%s approval=%s", thread.Model, turn.ID, thread.Sandbox, thread.ApprovalPolicy)
	var reply string
	for {
		select {
		case event, ok := <-client.Events():
			if !ok {
				t.Fatal("native stream closed")
			}
			var p struct {
				ThreadID, Delta string
				Item            struct{ Type, Text string }
				Turn            Turn
			}
			if err := json.Unmarshal(event.Params, &p); err != nil {
				continue
			}
			if p.ThreadID != "" && p.ThreadID != thread.Thread.ID {
				continue
			}
			if event.Method == "item/completed" && p.Item.Type == "agentMessage" {
				reply += p.Item.Text
			}
			if event.Method == "turn/completed" && p.Turn.ID == turn.ID {
				if p.Turn.Status != "completed" {
					t.Fatal("image turn failed", p.Turn)
				}
				t.Logf("native image reply=%s", reply)
				if !strings.Contains(reply, "红") || !strings.Contains(reply, "方") {
					t.Fatal("generated shape not identified", reply)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
