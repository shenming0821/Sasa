package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func auxiliaryFixture(t *testing.T) (*Client, string) {
	t.Helper()
	dir := t.TempDir()
	executable := filepath.Join(dir, "claude")
	script := `#!/usr/bin/env python3
import json, os, sys, time
args=sys.argv[1:]
model=args[args.index("--model")+1]
home=os.environ["CLAUDE_CONFIG_DIR"]
prompt=sys.stdin.read()
with open(os.path.join(home,"request.json"),"w") as f:
    json.dump({"args":args,"cwd":os.getcwd(),"prompt":prompt},f)
if model=="blocked":
    time.sleep(60)
elif model=="invalid":
    print("not json")
elif model=="error":
    print(json.dumps({"type":"result","subtype":"success","is_error":True,"result":"不可用标题"}))
elif model=="missing-status":
    print(json.dumps({"type":"result","subtype":"success","result":"状态未确认"}))
elif model=="oversize":
    print("x"*70000)
else:
    print(json.dumps({"type":"result","subtype":"success","is_error":False,"result":"会话标题生成"}))
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := New(executable, dir, filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, dir
}

func TestAuxiliaryTitleIsolatedAndNoHistory(t *testing.T) {
	client, dir := auxiliaryFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	title, err := client.GenerateSessionTitle(ctx, "provider/cheap", "对话正文")
	if err != nil || title != "会话标题生成" {
		t.Fatal(title, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Args        []string
		Cwd, Prompt string
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(request.Args, "\x00")
	for _, required := range []string{
		"--model\x00provider/cheap", "--tools\x00\x00", "--no-session-persistence",
		"--disable-slash-commands", "--strict-mcp-config", "--system-prompt",
		`{"disableAllHooks":true}`, `{"mcpServers":{}}`,
	} {
		if !strings.Contains(args, required) {
			t.Fatal("missing isolated execution flag", required)
		}
	}
	if request.Prompt != "对话正文" || request.Cwd == dir {
		t.Fatal("request not isolated", request)
	}
	if _, err := os.Stat(request.Cwd); !os.IsNotExist(err) {
		t.Fatal("temporary directory retained", err)
	}
	entries, err := os.ReadDir(client.stateDir)
	if err != nil || len(entries) != 0 || len(client.sessions) != 0 {
		t.Fatal("auxiliary title polluted chat history", entries, err)
	}
}

func TestAuxiliaryTitleRejectsInvalidResults(t *testing.T) {
	for _, model := range []string{"invalid", "error", "oversize", "missing-status"} {
		t.Run(model, func(t *testing.T) {
			client, _ := auxiliaryFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := client.GenerateSessionTitle(ctx, model, "正文"); err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
}

func TestAuxiliaryTitleCancellationAndClientCloseReapProcess(t *testing.T) {
	for _, closeClient := range []bool{false, true} {
		name := "cancel"
		if closeClient {
			name = "close"
		}
		t.Run(name, func(t *testing.T) {
			client, dir := auxiliaryFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := client.GenerateSessionTitle(ctx, "blocked", "正文"); done <- err }()
			deadline := time.Now().Add(3 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(dir, "request.json")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("fixture did not start")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if closeClient {
				client.Close()
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancelled title succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("auxiliary process did not exit")
			}
			if closeClient {
				if _, err := client.GenerateSessionTitle(context.Background(), "cheap", "正文"); err == nil {
					t.Fatal("closed client started process")
				}
			}
		})
	}
}

func TestLiveClaudeAuxiliaryTitle(t *testing.T) {
	if os.Getenv("FUYU_LIVE_CLAUDE_TITLE") != "1" {
		t.Skip("set FUYU_LIVE_CLAUDE_TITLE=1 for a real auxiliary title request")
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
	model := os.Getenv("FUYU_LIVE_TITLE_MODEL")
	if model == "" {
		t.Fatal("FUYU_LIVE_TITLE_MODEL must name an explicitly selected model")
	}
	started := time.Now()
	text, err := client.GenerateSessionTitle(ctx, model, `{"currentTitle":"新会话","messages":[{"role":"user","text":"修复会话切换后输入框草稿丢失的问题"},{"role":"assistant","text":"已经修复，并验证切换会话与重启后的草稿恢复"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(text) == "" || strings.Contains(text, "\n") || len([]rune(text)) > 28 {
		t.Fatal("invalid live title", text)
	}
	t.Logf("model=%s elapsed=%s title=%s", model, time.Since(started).Round(time.Millisecond), text)
}
