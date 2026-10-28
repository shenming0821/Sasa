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

func (a *App) chooseAgent(kind agent.AgentKind) error {
	root := a.application()
	if kind.Normalized() == a.kind.Normalized() {
		a.agentMenuOpen = false
		return nil
	}
	if kind == agent.Claude && root.backends[backendKey{kind: kind, runtime: a.agentRuntime(kind)}] == nil {
		message := root.agentErrors[kind]
		if message == "" {
			message = "Claude Code 尚未连接，请核验 CLI 安装和运行时配置"
		}
		return fmt.Errorf("%s", message)
	}
	if kind == agent.Codex && root.backend == nil {
		return fmt.Errorf("Codex 尚未连接，请核验运行时配置")
	}
	root.defaultAgent = kind
	if a.State.ThreadID != "" || a.State.Busy || a.recovering || a.recoveryBlocked || len(a.State.Messages) > 0 {
		cwd := a.State.Cwd
		root.newSession(cwd)
		if kind == agent.Codex {
			root.kind = agent.Codex
			root.State.Agent = agent.Codex
		}
		return root.persist()
	}
	a.kind, a.runtimeKey = kind, a.agentRuntime(kind)
	a.State.Agent = kind
	a.State.Model, a.State.Policy, a.State.Sandbox, a.State.RuntimeVersion = "", "", "", ""
	a.State.Disconnected, a.State.Error = false, ""
	a.models = nil
	a.modelsLoaded = false
	a.modelsLoading = false
	a.selectedModel = ""
	if kind == agent.Claude {
		a.selectedModel = root.lastClaudeModels[a.runtimeKey]
	}
	a.modelResolvedCwd = ""
	a.modelResolving = false
	a.completion = inputCompletion{}
	a.nativeThreads = nil
	a.threadsCursor = nil
	a.threadsError = ""
	a.threadsLoading = false
	a.historyCacheKey = agent.CacheKey{Agent: kind, Runtime: a.runtimeKey, Cwd: a.State.Cwd}
	a.agentMenuOpen = false
	if a.historyOpen {
		a.loadThreadList()
	}
	return root.persist()
}

func (a *App) agentPicker(c *native.Context) {
	root := a.application()
	if len(root.backends) == 0 && len(root.agentErrors) == 0 {
		return
	}
	trigger := quietButton(c, "选择 Agent").Gap(4).Shrink(1)
	trigger.Children(func() {
		native.Text(c, a.kind.String()).FontSize(12).SingleLine()
		native.Icon(c, chevron).Size(12, 12)
	})
	if trigger.Clicked() {
		a.agentMenuOpen = !a.agentMenuOpen
	}
	native.Popover(c.Key("agent-menu"), trigger, &a.agentMenuOpen, func() {
		native.Column(c).Width(260).Gap(4).Children(func() {
			for _, kind := range []agent.AgentKind{agent.Codex, agent.Claude} {
				label := kind.String()
				if (a.State.ThreadID != "" || a.State.Busy || a.recovering || a.recoveryBlocked || len(a.State.Messages) > 0) && kind != a.kind.Normalized() {
					label = "使用 " + label + " 新建会话"
				}
				button := treeButton(c.Key(string(kind)), "选择 Agent "+kind.String()).FillWidth().Padding(8)
				button.Children(func() {
					native.Row(c).FillWidth().Gap(6).Children(func() {
						native.Text(c, label).FontSize(12).Grow(1)
						if kind == a.kind.Normalized() {
							native.Text(c, "✓").FontSize(12)
						}
					})
				})
				if button.Clicked() {
					if err := a.chooseAgent(kind); err != nil {
						a.modelError = err.Error()
					}
				}
				if message := root.agentErrors[kind]; message != "" {
					native.Text(c, message).FontSize(11).TextColor(c.Theme().Danger).Padding(4, 8)
				}
			}
		})
	})
}
