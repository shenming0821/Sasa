package claude

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func TestInitializationInheritsPermissionMode(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		response string
		valid    bool
	}{
		{"default", `{"current_permission_mode":"default"}`, true},
		{"bypass", `{"current_permission_mode":"bypassPermissions"}`, true},
		{"accept-edits", `{"current_permission_mode":"acceptEdits"}`, true},
		{"plan", `{"current_permission_mode":"plan"}`, true},
		{"missing", `{}`, false},
		{"empty", `{"current_permission_mode":""}`, false},
		{"blank", `{"current_permission_mode":" "}`, false},
		{"wrong-type", `{"current_permission_mode":true}`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			python, err := exec.LookPath("python3")
			if err != nil {
				t.Skip("python3 unavailable")
			}
			script, err := filepath.Abs("testdata/permission_server.py")
			if err != nil {
				t.Fatal(err)
			}
			executable := filepath.Join(t.TempDir(), "claude")
			if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec '"+python+"' -I '"+script+"' \"$@\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			client, err := New(executable, t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(client.Close)
			cwd := t.TempDir()
			if err := os.WriteFile(filepath.Join(cwd, "initialize-response.json"), []byte(scenario.response), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if scenario.valid {
				run, raw, err := client.startCatalog(ctx, cwd)
				if err != nil {
					t.Fatal(err)
				}
				run.stopProcess()
				var actual, expected map[string]any
				if json.Unmarshal(raw, &actual) != nil || json.Unmarshal([]byte(scenario.response), &expected) != nil || actual["current_permission_mode"] != expected["current_permission_mode"] {
					t.Fatalf("effective permission mode changed: %s", raw)
				}
			} else {
				thread, err := client.StartThread(ctx, cwd)
				if err != nil {
					t.Fatal(err)
				}
				_, err = client.Send(ctx, thread.Thread.ID, "must not be sent")
				var notSubmitted *agent.NotSubmittedError
				if !errors.As(err, &notSubmitted) || !strings.Contains(err.Error(), "未确认实际权限模式") {
					t.Fatalf("unconfirmed permissions did not block submission: %v", err)
				}
				if _, err := os.Stat(filepath.Join(cwd, "user-received")); !os.IsNotExist(err) {
					t.Fatal("user input submitted without confirmed permissions", err)
				}
			}
			raw, err := os.ReadFile(filepath.Join(cwd, "startup-args.json"))
			if err != nil {
				t.Fatal(err)
			}
			var args []string
			if err := json.Unmarshal(raw, &args); err != nil {
				t.Fatal(err)
			}
			for _, arg := range args {
				if strings.HasPrefix(arg, "--permission-mode") || strings.Contains(arg, "bypass-permissions") {
					t.Fatalf("startup overrides native permission configuration: %v", args)
				}
			}
			if !strings.Contains(strings.Join(args, " "), "--permission-prompt-tool stdio") {
				t.Fatalf("stdio permission handling missing: %v", args)
			}
		})
	}
}

func TestLiveClaudeConfiguredPermissionMode(t *testing.T) {
	expected := os.Getenv("FUYU_LIVE_CLAUDE_PERMISSION_MODE")
	if os.Getenv("FUYU_LIVE_CLAUDE") != "1" || expected == "" {
		t.Skip("set FUYU_LIVE_CLAUDE=1 and FUYU_LIVE_CLAUDE_PERMISSION_MODE for native configuration verification")
	}
	executable, err := ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(executable, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	raw, err := client.catalog(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Mode string `json:"current_permission_mode"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Mode != expected {
		t.Fatalf("native permission mode=%q expected=%q", settings.Mode, expected)
	}
	t.Logf("native current_permission_mode=%s; no user task sent", settings.Mode)
}
