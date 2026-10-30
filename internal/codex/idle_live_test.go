package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Uses the installed Codex, an isolated CODEX_HOME, and a controlled MCP server.
// It never submits a model turn or reads the user's credentials.
func TestLiveIdleThreadReleasesMCPProcesses(t *testing.T) {
	if os.Getenv("FUYU_LIVE_IDLE") != "1" {
		t.Skip("set FUYU_LIVE_IDLE=1 to check native Codex resource disposal")
	}
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	script := filepath.Join(dir, "mcp.py")
	pidFile := filepath.Join(dir, "mcp-pids")
	source := `import json, os, pathlib, sys
with pathlib.Path(sys.argv[1]).open("a") as f:
    f.write(str(os.getpid()) + "\n")
for line in sys.stdin:
    m = json.loads(line)
    if "id" not in m:
        continue
    if m.get("method") == "initialize":
        result = {"protocolVersion": "2024-11-05", "capabilities": {"tools": {}},
                  "serverInfo": {"name": "fuyu-idle-fixture", "version": "1"}}
    elif m.get("method") == "tools/list":
        result = {"tools": []}
    else:
        result = {}
    print(json.dumps({"jsonrpc": "2.0", "id": m["id"], "result": result}), flush=True)
`
	if err := os.WriteFile(script, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal([]string{script, pidFile})
	config := fmt.Sprintf("[mcp_servers.idle_fixture]\ncommand = \"/usr/bin/python3\"\nargs = %s\n", args)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, err := Start(ctx, "codex", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	go func() {
		for range client.Events() {
		}
	}()
	var ids []string
	for range 3 {
		thread, err := client.StartThread(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, thread.Thread.ID)
	}
	wait := func(condition func() bool) {
		t.Helper()
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for !condition() {
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-ticker.C:
			}
		}
	}
	var pids []int
	wait(func() bool {
		raw, _ := os.ReadFile(pidFile)
		pids = nil
		for _, line := range strings.Fields(string(raw)) {
			pid, _ := strconv.Atoi(line)
			pids = append(pids, pid)
		}
		return len(pids) == 3
	})
	for _, id := range ids[:2] {
		if released, err := client.ReleaseIdleThread(ctx, id); err != nil || !released {
			t.Fatalf("release %s: released=%v err=%v", id, released, err)
		}
	}
	var loaded struct {
		Data []string `json:"data"`
	}
	wait(func() bool {
		if err := client.Call(ctx, "thread/loaded/list", map[string]any{}, &loaded); err != nil {
			t.Fatal(err)
		}
		return len(loaded.Data) == 1
	})
	if loaded.Data[0] != ids[2] {
		t.Fatal("release affected the retained session")
	}
	wait(func() bool {
		live := 0
		for _, pid := range pids {
			if syscall.Kill(pid, 0) == nil {
				live++
			}
		}
		return live == 1
	})
	t.Logf("native loaded threads 3 -> 1; controlled MCP processes 3 -> 1; thread_unload_delay_secs=0; no model turn")
	client.Close()
	wait(func() bool {
		for _, pid := range pids {
			if syscall.Kill(pid, 0) == nil {
				return false
			}
		}
		return true
	})
}
