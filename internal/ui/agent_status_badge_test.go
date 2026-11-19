package ui

import (
	"testing"

	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/app"

	native "github.com/egoist/mygo/ui"
)

func TestSessionStatusDot(t *testing.T) {
	cases := []struct {
		name  string
		setup func(s *session)
		want  string // "warning", "danger", "accent", "" for none
	}{
		{"idle", func(s *session) {}, ""},
		{"busy", func(s *session) { s.State.Busy = true }, "accent"},
		{"error", func(s *session) { s.State.Error = "boom" }, "danger"},
		{"busy and error", func(s *session) { s.State.Busy = true; s.State.Error = "boom" }, "danger"},
		{"pending", func(s *session) {
			s.State.Messages = append(s.State.Messages, &app.Message{
				Role:        "interaction",
				Interaction: &app.Interaction{Status: "待处理"},
			})
		}, "warning"},
		{"pending beats error", func(s *session) {
			s.State.Error = "boom"
			s.State.Messages = append(s.State.Messages, &app.Message{
				Role:        "interaction",
				Interaction: &app.Interaction{Status: "待处理"},
			})
		}, "warning"},
		{"resolved pending falls back to busy", func(s *session) {
			s.State.Busy = true
			s.State.Messages = append(s.State.Messages, &app.Message{
				Role:        "interaction",
				Interaction: &app.Interaction{Status: "已解决"},
			})
		}, "accent"},
	}
	for _, tc := range cases {
		s := &session{kind: agent.Codex}
		tc.setup(s)
		var dot *agentStatusDot
		var theme *native.Theme
		tester := native.NewTester(func(c *native.Context) {
			dot = sessionStatusDot(c, s)
			theme = c.Theme()
		}, 200, 100)
		tester.Frame()
		got := ""
		if dot != nil {
			switch dot.color {
			case theme.Warning:
				got = "warning"
			case theme.Danger:
				got = "danger"
			case theme.Accent:
				got = "accent"
			}
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
