package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/shenming0821/Sasa/internal/agent"
)

// These tests validate captured evidence, not a production Claude adapter.
func TestRecordedHelloFrames(t *testing.T) {
	for _, name := range []string{"stream-json", "text"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/hello-" + name + ".jsonl")
			if err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(bytes.NewReader(raw))
			scanner.Buffer(make([]byte, 64*1024), agent.MaxFrameBytes)
			count, assistant, results := 0, 0, 0
			session, modelMessage := "", ""
			uuids := make(map[string]bool)
			text := ""
			for scanner.Scan() {
				var frame struct {
					Type           string `json:"type"`
					Subtype        string `json:"subtype"`
					SessionID      string `json:"session_id"`
					UUID           string `json:"uuid"`
					Version        string `json:"claude_code_version"`
					PermissionMode string `json:"permissionMode"`
					IsError        bool   `json:"is_error"`
					TerminalReason string `json:"terminal_reason"`
					Result         string `json:"result"`
					Message        struct {
						ID      string `json:"id"`
						Content []struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"content"`
					} `json:"message"`
				}
				if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
					t.Fatal(err)
				}
				if count == 0 {
					if frame.Type != "system" || frame.Subtype != "init" || frame.Version != "2.1.311" ||
						frame.PermissionMode != "bypassPermissions" || frame.SessionID == "" {
						t.Fatal("首帧缺少实测初始化证据")
					}
					session = frame.SessionID
				}
				if frame.SessionID != session || frame.UUID == "" || uuids[frame.UUID] {
					t.Fatal("会话或帧 UUID 不一致")
				}
				uuids[frame.UUID] = true
				switch frame.Type {
				case "assistant":
					if modelMessage != "" && frame.Message.ID != modelMessage {
						t.Fatal("该录制应展示同一个模型消息的多个块")
					}
					modelMessage = frame.Message.ID
					assistant++
					for _, block := range frame.Message.Content {
						if block.Type == "text" {
							text += block.Text
						}
					}
				case "result":
					results++
					if frame.Subtype != "success" || frame.IsError || frame.TerminalReason != "completed" ||
						frame.Result != "你好" || assistant == 0 {
						t.Fatal("真实终态不是预期的成功回复")
					}
				}
				count++
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			if results != 1 || assistant < 2 || text != "你好" {
				t.Fatal("录制不完整", count, assistant, results, text)
			}
		})
	}
}

func TestRecordedUserAndPlainOutput(t *testing.T) {
	raw, err := os.ReadFile("testdata/hello-user.jsonl")
	if err != nil || !json.Valid(bytes.TrimSpace(raw)) {
		t.Fatal("真实 stdin 不是有效 JSON", err)
	}
	plain, err := os.ReadFile("testdata/hello-plain.txt")
	if err != nil || strings.TrimSpace(string(plain)) != "你好" {
		t.Fatal("普通文本对照输出不符", err)
	}
}
