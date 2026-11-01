package ui

import (
	"fmt"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/agent"
)

func (a *App) SetAgentError(kind agent.AgentKind, err error) {
	if a.agentErrors == nil {
		a.agentErrors = make(map[agent.AgentKind]string)
	}
	if err != nil {
		a.agentErrors[kind] = err.Error()
	}
}

func (a *App) agentRuntime(kind agent.AgentKind) string {
	for key := range a.application().backends {
		if key.kind == kind {
			return key.runtime
		}
	}
	return ""
}

func (a *App) newAgentSession(kind agent.AgentKind) error {
	root := a.application()
	kind = kind.Normalized()
	if err := kind.Validate(); err != nil {
		return err
	}
	if kind == agent.Claude && root.backends[backendKey{kind: kind, runtime: a.agentRuntime(kind)}] == nil {
		message := root.agentErrors[kind]
		if message == "" {
			message = "Claude Code 尚未连接，请核验 CLI 安装和运行时配置"
		}
		return fmt.Errorf("%s", message)
	}
	if kind == agent.Codex && root.backend == nil {
		message := root.agentErrors[kind]
		if message == "" {
			message = "Codex 尚未连接，请核验运行时配置"
		}
		return fmt.Errorf("%s", message)
	}
	root.defaultAgent = kind
	root.newSession(a.workspaceCwd())
	root.completion.focus = true
	return root.persist()
}

func (a *App) agentSessionButtons(c *native.Context) {
	native.Row(c).Gap(4).AlignItems(native.Center).Children(func() {
		for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
			button := quietIconButton(c.Key(string(kind)), "使用 "+kind.String()+" 新建会话").Size(30, 30)
			button.Children(func() {
				agentIcon(c, kind, 16)
			})
			if button.Clicked() {
				if err := a.newAgentSession(kind); err != nil {
					a.modelError = err.Error()
				}
			}
		}
		a.terminalButton(c)
	})
}

func agentIcon(c *native.Context, kind agent.AgentKind, size float32) native.Element {
	glyph, color := codexAgentIcon, c.Theme().Text
	if kind.Normalized() == agent.Claude {
		glyph, color = claudeAgentIcon, native.Hex("#D97757")
	}
	return native.Icon(c, glyph).Size(size, size).TextColor(color)
}
