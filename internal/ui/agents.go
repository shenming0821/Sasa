package ui

import (
	"fmt"
	"reflect"

	"github.com/shenming0821/Fuyu/internal/agent"
)

type backendKey struct {
	kind    agent.AgentKind
	runtime string
}

// RegisterBackend is called on the UI thread before Run. Each event stream is
// consumed once; sessions select their transport by Agent and runtime identity.
func (a *App) RegisterBackend(kind agent.AgentKind, runtime string, backend Backend) error {
	kind = kind.Normalized()
	if err := kind.Validate(); err != nil {
		return err
	}
	if backend == nil {
		return fmt.Errorf("Agent 连接不能为空")
	}
	root := a.application()
	if root.win != nil {
		return fmt.Errorf("请在启动窗口前注册 Agent")
	}
	key := backendKey{kind: kind, runtime: runtime}
	if _, exists := root.backends[key]; exists || kind == agent.Codex && runtime == "" {
		return fmt.Errorf("Agent 运行时已注册")
	}
	if reflect.TypeOf(backend).Comparable() && root.backend == backend {
		return fmt.Errorf("同一连接不能注册到多个运行时")
	}
	for _, existing := range root.backends {
		if reflect.TypeOf(existing).Comparable() && existing == backend {
			return fmt.Errorf("同一连接不能注册到多个运行时")
		}
	}
	if root.backends == nil {
		root.backends = make(map[backendKey]Backend)
	}
	root.backends[key] = backend
	return nil
}

func (a *App) sessionBackend() Backend {
	root := a.application()
	kind := a.kind.Normalized()
	if backend := root.backends[backendKey{kind: kind, runtime: a.runtimeKey}]; backend != nil {
		return backend
	}
	if kind == agent.Codex && a.runtimeKey == "" {
		return a.backend
	}
	return nil
}

func (s *session) accepts(e agent.Event) bool {
	if e.Session.Agent == "" && (s.kind.Normalized() != agent.Codex || s.runtimeKey != "") {
		return false
	}
	if e.Session.Agent != "" && (e.Session.Agent.Normalized() != s.kind.Normalized() || e.Session.Runtime != s.runtimeKey) {
		return false
	}
	if e.Session.Runtime != "" && e.Session.Runtime != s.runtimeKey {
		return false
	}
	if e.Session.SessionID != "" && e.Session.SessionID != s.State.ThreadID {
		return false
	}
	if e.Generation != 0 && e.Generation != s.generation {
		return false
	}
	return true
}
