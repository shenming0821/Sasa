package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

var (
	_ agent.Backend            = (*Client)(nil)
	_ agent.ModelCatalog       = (*Client)(nil)
	_ agent.ModelSender        = (*Client)(nil)
	_ agent.SkillCatalog       = (*Client)(nil)
	_ agent.ApprovalResponder  = (*Client)(nil)
	_ agent.HistoryBackend     = (*Client)(nil)
	_ agent.HistoryResponder   = (*Client)(nil)
	_ agent.CapabilityProvider = (*Client)(nil)
	_ agent.Reconciler         = (*Client)(nil)
	_ agent.ThreadReader       = (*Client)(nil)
	_ agent.Forker             = (*Client)(nil)
)

func TestAgentRecoverySeparatesHistoryAndSendAuthority(t *testing.T) {
	for _, status := range []string{"completed", "inProgress"} {
		t.Run(status, func(t *testing.T) {
			dir := t.TempDir()
			raw := `{"session":{"cwd":"/project","turns":[{"id":"old","status":"` + status + `","items":[]}]}}`
			if err := os.WriteFile(filepath.Join(dir, "native-history.json"), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			script, err := filepath.Abs("testdata/p3_server.py")
			if err != nil {
				t.Fatal(err)
			}
			c, err := startProcess("/usr/bin/python3", []string{script, "--fixture-dir", dir}, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			result, err := c.RespondHistory(ctx, "session")
			if err != nil || len(result.History.Data) != 1 || !result.WriterConfirmed {
				t.Fatal("原生历史证据缺失", result, err)
			}
			if result.CanSend != (status == "completed") || result.TerminalConfirmed != result.CanSend {
				t.Fatal("可展示历史被误当成可发送", result)
			}
			if status == "inProgress" && (result.PendingKnown || result.Uncertainty == "") {
				t.Fatal("未结束任务必须保留不确定性", result)
			}
		})
	}
}
