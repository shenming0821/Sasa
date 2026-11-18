package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shenming0821/Sasa/internal/agent"
)

func TestAgentSpecificErrorsKeepProjectionSource(t *testing.T) {
	for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
		t.Run(string(kind), func(t *testing.T) {
			s := New("/project")
			s.ThreadID = "session"
			source := agent.SessionKey{Agent: kind, SessionID: "session"}
			s.Apply(agent.Event{Session: source, Method: "item/completed", Params: json.RawMessage(`{`)})
			if !strings.Contains(s.Error, kind.String()) {
				t.Fatal("解析失败文案丢失 Agent 归属", s.Error)
			}
			s.Apply(agent.Event{Session: source, Method: "sasa/disconnected"})
			if !s.Disconnected || !strings.HasPrefix(s.Error, kind.String()+" 连接已断开") {
				t.Fatal("断线文案归属错误", s.Error)
			}
		})
	}
}
