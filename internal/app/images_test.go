package app

import (
	"encoding/json"
	"testing"

	"github.com/shenming0821/Fuyu/internal/attachment"
	"github.com/shenming0821/Fuyu/internal/codex"
)

func TestImageMessageBeginAndHistory(t *testing.T) {
	state := New("/workspace")
	state.DraftImages = []attachment.Image{{Path: "/owned/screenshot.png", Name: "截图.png"}}
	state.Begin("")
	if len(state.DraftImages) != 0 || len(state.Messages[0].Images) != 1 || state.Messages[0].Text != "" {
		t.Fatal("image-only begin lost attachment")
	}
	raw := json.RawMessage(`{"id":"user","type":"userMessage","content":[{"type":"text","text":"请分析图片"},{"type":"localImage","path":"/owned/screenshot.png"},{"type":"image","url":"data:image/png;base64,abc"}]}`)
	recovered, _, err := History("/workspace", "thread", []codex.HistoryTurn{{ID: "turn", Status: "completed", Items: []json.RawMessage{raw}}})
	if err != nil || len(recovered.Messages) != 1 || len(recovered.Messages[0].Images) != 2 || recovered.Messages[0].Text != "请分析图片" {
		t.Fatal("image history lost", recovered, err)
	}
	if recovered.Messages[0].Images[0].Path != "/owned/screenshot.png" || recovered.Messages[0].Images[1].URL != "data:image/png;base64,abc" {
		t.Fatal("image references changed")
	}
}
