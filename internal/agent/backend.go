package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/shenming0821/Fuyu/internal/attachment"
)

var ErrUnsupported = errors.New("当前 Agent 不支持此能力")

type NotSubmittedError struct {
	Err error
}

func (err *NotSubmittedError) Error() string { return err.Err.Error() }

func (err *NotSubmittedError) Unwrap() error { return err.Err }

// Backend is the minimum task loop. Optional capabilities use interface assertions.
type Backend interface {
	StartThread(context.Context, string) (Thread, error)
	Send(context.Context, string, string) (Turn, error)
	Interrupt(context.Context, string, string) error
	Events() <-chan Event
}

type ModelCatalog interface {
	ListModels(context.Context) ([]Model, error)
}

type ModelSender interface {
	SendWithModel(context.Context, string, string, []Skill, []attachment.Image, string) (Turn, error)
	DefaultModel(context.Context, string) (string, error)
}

type SkillCatalog interface {
	ListSkills(context.Context, string) ([]SkillsEntry, error)
}

type ApprovalResponder interface {
	Respond(json.RawMessage, any) error
}

type IntentResponder interface {
	RespondApproval(context.Context, ApprovalIntent) error
}

type HistoryBackend interface {
	ResumeThread(context.Context, string) (ResumedThread, error)
	ListTurns(context.Context, string, *string) (TurnsPage, error)
	ListThreads(context.Context, *string) (ThreadsPage, error)
}

type HistoryResponder interface {
	RespondHistory(context.Context, string) (RecoveryResult, error)
}

type Reconciler interface {
	ReadThread(context.Context, string) (json.RawMessage, error)
}

type ThreadReader interface {
	ReadThreadMeta(context.Context, string) (ThreadMeta, error)
}

type Forker interface {
	ForkThread(context.Context, string) (ResumedThread, error)
}

type Command struct {
	Name, Description, Source, Cwd string
	Arguments                      []string
}

type CommandCatalog interface {
	ListCommands(context.Context, string) ([]Command, error)
}

// History visibility does not imply authority to resume a previous process.
type RecoveryResult struct {
	Thread            ResumedThread
	History           TurnsPage
	CanSend           bool
	TerminalConfirmed bool
	WriterConfirmed   bool
	PendingKnown      bool
	Uncertainty       string
}

type CapabilityProvider interface {
	Capabilities() Capabilities
}
