package ui

import (
	"context"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/attachment"
)

type startupStopBackend struct {
	stubBackend
	stage   string
	started chan struct{}
	stops   chan string
}

func (backend *startupStopBackend) DefaultModel(ctx context.Context, _ string) (string, error) {
	if backend.stage != "model" {
		return "default", nil
	}
	backend.started <- struct{}{}
	<-ctx.Done()
	return "", ctx.Err()
}

func (backend *startupStopBackend) SendWithModel(ctx context.Context, _, _ string, _ []agent.Skill, _ []attachment.Image, _ string) (agent.Turn, error) {
	backend.started <- struct{}{}
	<-ctx.Done()
	if backend.stage == "delivered" {
		return agent.Turn{ID: "native-turn"}, nil
	}
	return agent.Turn{}, &agent.NotSubmittedError{Err: ctx.Err()}
}

func (backend *startupStopBackend) Interrupt(_ context.Context, thread, turn string) error {
	backend.stops <- thread + "/" + turn
	return nil
}

func TestStopBeforeTurnCancelsPreparationAndPreservesOwner(t *testing.T) {
	for _, stage := range []string{"model", "send", "delivered"} {
		t.Run(stage, func(t *testing.T) {
			backend := &startupStopBackend{stage: stage, started: make(chan struct{}, 2), stops: make(chan string, 2)}
			desktop := New(backend, "test", "/project", nil)
			desktop.State.ThreadID, desktop.State.Draft = "thread", "preserve task"
			desktop.modelResolvedCwd = "/project"
			queue := make(chan func(), 8)
			desktop.dispatch = func(update func()) { queue <- update }
			owner := desktop.session
			desktop.send()
			select {
			case <-backend.started:
			case <-time.After(3 * time.Second):
				t.Fatal("preparation did not start")
			}
			tester := native.NewTester(desktop.view, 1000, 760)
			if err := tester.Click("停止"); err != nil {
				t.Fatal(err)
			}
			desktop.newSession("/other")
			desktop.State.Draft = "other draft"
			pumpUpdate(t, queue)
			if desktop.State.Draft != "other draft" || desktop.State.Busy {
				t.Fatal("preparation cancellation affected the active session")
			}
			if stage == "delivered" {
				select {
				case stopped := <-backend.stops:
					if stopped != "thread/native-turn" || !owner.State.Busy || owner.State.Phase != "正在停止" {
						t.Fatal("delivered request did not receive a native interrupt", stopped, owner.State.Phase)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("delivery race did not request native interruption")
				}
			} else if owner.State.Busy || owner.State.Disconnected || owner.State.Draft != "preserve task" || owner.State.Phase != "已取消" || owner.submitCancel != nil {
				t.Fatalf("cancelled preparation lost certainty or draft: %+v", owner.State)
			}
		})
	}
}
