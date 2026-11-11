package codex

import (
	"strings"
	"testing"
	"time"
)

func TestModelDiagnosticsClassifiesWithoutExposingRawStderr(t *testing.T) {
	for _, test := range []struct{ detail, want string }{
		{"request timed out https://private.example/key/401 secret-token", "请求超时"},
		{"unexpected status 401 Unauthorized secret-token", "拒绝访问"},
		{"connection refused secret-token", "无法连接"},
		{"invalid response secret-token", "刷新失败"},
	} {
		t.Run(test.want, func(t *testing.T) {
			var diagnostics modelDiagnostics
			started := time.Now()
			line := "ERROR codex_models_manager::manager: failed to refresh available models: " + test.detail + "\n"
			for _, part := range []string{line[:20], line[20:]} {
				if count, err := diagnostics.Write([]byte(part)); count != len(part) || err != nil {
					t.Fatal(count, err)
				}
			}
			message := diagnostics.since(started)
			if !strings.Contains(message, test.want) || strings.Contains(message, "secret-token") || strings.Contains(message, "private.example") || diagnostics.partial != "" {
				t.Fatal(message)
			}
			if diagnostics.since(time.Now()) != "" {
				t.Fatal("stale diagnostic reused")
			}
		})
	}
}

func TestModelDiagnosticsBoundsAndIgnoresOtherStderr(t *testing.T) {
	var diagnostics modelDiagnostics
	started := time.Now()
	diagnostics.Write([]byte(strings.Repeat("private output", 10000)))
	if len(diagnostics.partial) > 8192 {
		t.Fatal("unbounded stderr buffer")
	}
	diagnostics.Write([]byte("\nrequest timed out\nfailed to refresh available models: timeout\n"))
	if diagnostics.since(started) != "" {
		t.Fatal("unrelated stderr classified as catalog failure")
	}
}
