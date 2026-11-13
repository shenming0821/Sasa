package claude

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

// Only initialize and native mode controls; no prompt or model task is sent.
func TestLiveClaudePlanModeControl(t *testing.T) {
	if os.Getenv("FUYU_LIVE_PLAN") != "1" {
		t.Skip("set FUYU_LIVE_PLAN=1 for native mode control verification")
	}
	executable, err := ResolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(executable, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s := &session{Cwd: t.TempDir()}
	run, raw, err := c.spawn(ctx, s, agent.TurnIdentity{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer run.stopProcess()
	var initial struct {
		Mode string `json:"current_permission_mode"`
	}
	if err := json.Unmarshal(raw, &initial); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []agent.InteractionMode{agent.ModePlan, agent.ModeDefault} {
		if err := run.setMode(ctx, mode); err != nil {
			t.Fatal(err)
		}
	}
	c.mu.Lock()
	final := run.connection.permissionMode
	c.mu.Unlock()
	expected := initial.Mode
	if expected == "plan" {
		expected = "default"
	}
	if final != expected {
		t.Fatal("did not restore native permission mode", final, expected)
	}
	t.Logf("native permission modes: %s -> plan -> %s; no user task sent", initial.Mode, final)
}
