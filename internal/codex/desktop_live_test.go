package codex

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	process "github.com/shenming0821/Sasa/internal/runtime"
)

// Only initializes the real App Server; never creates a thread or model turn.
func TestLiveDesktopRuntimeDiscovery(t *testing.T) {
	if os.Getenv("SASA_LIVE") != "1" {
		t.Skip("set SASA_LIVE=1 to check installed Codex with Finder-like PATH")
	}
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	executable, path, err := process.ResolveCodex("codex")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", path)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	version, err := Version(ctx, executable)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := exec.LookPath("node")
	t.Logf("executable=%s version=%s node=%s", executable, strings.TrimSpace(version), node)
	client, err := Start(ctx, executable, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client.Close()
	if client.process.Cmd.ProcessState == nil || !client.process.Cmd.ProcessState.Exited() {
		t.Fatal("App Server exit was not confirmed")
	}
	t.Log("real App Server initialized and closed with Finder-like PATH; no thread or turn created")
}
