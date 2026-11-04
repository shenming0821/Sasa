package ui

import (
	"context"
	"fmt"

	"github.com/shenming0821/Fuyu/internal/agent"
)

// RuntimeConnection is published once per backend. A version-only update has no Kind.
type RuntimeConnection struct {
	Kind    agent.AgentKind
	Runtime string
	Backend Backend
	Version string
	Err     error
}

type startupState struct {
	startup func(context.Context, func(RuntimeConnection))
}

// SetStartup schedules discovery after the window exists. The loader owns its
// processes until cancellation and must return after releasing them.
func (a *App) SetStartup(load func(context.Context, func(RuntimeConnection))) {
	a.startup = load
	a.State.Disconnected = true
}

func (a *App) startRuntimeLoading() func() {
	if a.startup == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.startup(ctx, func(result RuntimeConnection) {
			if ctx.Err() != nil {
				return
			}
			a.update(func() {
				if ctx.Err() == nil {
					a.installRuntime(result)
				}
			})
		})
	}()
	return func() {
		cancel()
		<-done
	}
}

func (a *App) installRuntime(result RuntimeConnection) {
	if result.Version != "" {
		a.version = result.Version
	}
	if result.Kind == "" {
		return
	}
	if result.Err == nil && result.Backend == nil {
		result.Err = fmt.Errorf("Agent 未返回连接")
	}
	if result.Err == nil && result.Kind != agent.Codex && a.store != nil {
		result.Err = a.store.BindAgentRuntime(result.Kind, result.Runtime)
	}
	if result.Err == nil {
		if result.Kind == agent.Codex {
			a.backend = result.Backend
		} else {
			if a.backends == nil {
				a.backends = make(map[backendKey]Backend)
			}
			a.backends[backendKey{kind: result.Kind, runtime: result.Runtime}] = result.Backend
		}
		delete(a.agentErrors, result.Kind)
		go a.receiveBackend(result.Backend, backendKey{kind: result.Kind, runtime: result.Runtime})
	} else {
		a.SetAgentError(result.Kind, result.Err)
	}
	for _, s := range a.sessions {
		if s.kind.Normalized() != result.Kind {
			continue
		}
		if result.Err != nil {
			s.State.Disconnected = true
			s.State.Error = "连接 Agent 失败：" + result.Err.Error()
			continue
		}
		if s.runtimeKey == "" && s.State.ThreadID == "" && result.Kind == agent.Claude {
			s.runtimeKey = result.Runtime
			if s.selectedModel == "" {
				s.selectedModel = a.lastClaudeModels[result.Runtime]
			}
		}
		if s.runtimeKey != result.Runtime {
			continue
		}
		s.State.Disconnected, s.State.Error = false, ""
		s.modelsError, s.modelError = "", ""
	}
	if a.threadList.loading {
		a.threadList.reload = true
	}
	a.recoverSelectedSession()
	if a.historyOpen {
		a.loadThreadList()
	}
}
