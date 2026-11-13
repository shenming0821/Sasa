package agent

import (
	"context"
	"fmt"
	"time"
)

type AgentKind string

const (
	Codex  AgentKind = "codex"
	Claude AgentKind = "claude"
	// Devin remains valid for stored sessions and the legacy adapter. Desktop
	// startup and new-session controls only offer Codex and Claude.
	Devin AgentKind = "devin"
)

func (k AgentKind) Normalized() AgentKind {
	if k == "" {
		return Codex
	}
	return k
}

func (k AgentKind) Validate() error {
	switch k.Normalized() {
	case Codex, Claude, Devin:
		return nil
	default:
		return fmt.Errorf("不支持的 Agent：%s", k)
	}
}

func (k AgentKind) String() string {
	switch k.Normalized() {
	case Codex:
		return "Codex"
	case Claude:
		return "Claude Code"
	case Devin:
		return "Devin"
	default:
		return string(k)
	}
}

// SessionKey separates equal native IDs from different runtimes.
type SessionKey struct {
	Agent     AgentKind
	Runtime   string
	SessionID string
}

type ProcessGeneration uint64

type Capabilities struct {
	Models, Skills, Commands, Images           bool
	Approval, SessionRules, PermissionMode     bool
	History, Resume, Reconcile, Occupied, Fork bool
}

// CacheKey includes the effective runtime configuration and working directory.
type CacheKey struct {
	Agent   AgentKind
	Runtime string
	Cwd     string
}

const (
	MaxFrameBytes      = 16 * 1024 * 1024
	MaxQueueBytes      = 4 * 1024 * 1024
	MaxQueueEvents     = 4096
	MaxRequestBytes    = 256 * 1024
	MaxPendingRequests = 16
)

func RequestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}
