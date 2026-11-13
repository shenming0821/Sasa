package power

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

const (
	Never  = "never"
	Always = "always"
	ACOnly = "ac-only"
)

func Normalize(mode string) string {
	switch mode {
	case Always, ACOnly:
		return mode
	default:
		return Never
	}
}

type State struct {
	AC         int
	Battery    int
	HasBattery bool
}

type Baseline struct {
	AC      int
	Battery int
}

type backend interface {
	read() (State, error)
	write(context.Context, State) error
}

func modeOf(state State) string {
	switch {
	case state.AC == 0 && (!state.HasBattery || state.Battery == 0):
		return Always
	case state.AC == 0:
		return ACOnly
	default:
		return Never
	}
}

type Controller struct {
	operation       sync.Mutex
	mu              sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	backend         backend
	mode            string
	baseline        Baseline
	hasBaseline     bool
	baselineChanged bool
	err             error
	closed          bool
}

func New() *Controller {
	return newController(&systemBackend{})
}

func newController(driver backend) *Controller {
	ctx, cancel := context.WithCancel(context.Background())
	return &Controller{backend: driver, mode: Never, ctx: ctx, cancel: cancel}
}

// Sync reads the current system power profile and returns the nearest mode.
func (controller *Controller) Sync() (string, error) {
	controller.operation.Lock()
	defer controller.operation.Unlock()
	controller.mu.Lock()
	if controller.closed {
		defer controller.mu.Unlock()
		return controller.mode, errors.New("保持唤醒服务已关闭")
	}
	controller.mu.Unlock()
	state, err := controller.backend.read()
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err != nil {
		return controller.mode, err
	}
	controller.mode = modeOf(state)
	return controller.mode, nil
}

// RestoreBaseline loads the values recorded before Sasa first rewrote pmset.
func (controller *Controller) RestoreBaseline(baseline Baseline) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if !controller.hasBaseline {
		controller.baseline, controller.hasBaseline = baseline, true
	}
}

// PendingBaseline stays available until its persistence has succeeded.
func (controller *Controller) PendingBaseline() (Baseline, bool) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.baseline, controller.baselineChanged
}

func (controller *Controller) ConfirmBaseline() {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.baselineChanged = false
}

func (controller *Controller) target(mode string, hasBattery bool) State {
	b := controller.baseline
	// A zero baseline disables sleep; use ten minutes when the requested
	// mode needs a positive timer and there is no previous positive value.
	if b.AC <= 0 {
		b.AC = 10
	}
	if b.Battery <= 0 {
		b.Battery = 10
	}
	switch mode {
	case Always:
		return State{AC: 0, Battery: 0, HasBattery: hasBattery}
	case ACOnly:
		return State{AC: 0, Battery: b.Battery, HasBattery: hasBattery}
	default:
		return State{AC: b.AC, Battery: b.Battery, HasBattery: hasBattery}
	}
}

// Set rewrites the system power profile for the requested mode. Changes made
// through pmset are persistent and survive Sasa restarts.
func (controller *Controller) Set(mode string) error {
	controller.operation.Lock()
	defer controller.operation.Unlock()
	controller.mu.Lock()
	if controller.closed {
		controller.mu.Unlock()
		return errors.New("保持唤醒服务已关闭")
	}
	controller.mu.Unlock()
	mode = Normalize(mode)
	current, err := controller.backend.read()
	controller.mu.Lock()
	if err != nil {
		controller.err = err
		controller.mu.Unlock()
		return err
	}
	if !controller.hasBaseline {
		controller.baseline = Baseline{AC: current.AC, Battery: current.Battery}
		if !current.HasBattery {
			controller.baseline.Battery = -1
		}
		controller.hasBaseline, controller.baselineChanged = true, true
	}
	target := controller.target(mode, current.HasBattery)
	if target.Battery < 0 || !current.HasBattery {
		target.Battery = current.Battery
	}
	if target.AC == current.AC && target.Battery == current.Battery {
		controller.mode, controller.err = modeOf(target), nil
		controller.mu.Unlock()
		return nil
	}
	controller.mu.Unlock()
	err = controller.backend.write(controller.ctx, target)
	var actual State
	if err == nil {
		actual, err = controller.backend.read()
		if err == nil && actual != target {
			err = fmt.Errorf("系统电源配置未达到目标：AC=%d，电池=%d", actual.AC, actual.Battery)
		}
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.err = err
	if err == nil {
		controller.mode = modeOf(actual)
	}
	return err
}

func (controller *Controller) Err() error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.err
}

// CurrentState captures the exact timers for a failed-preference-save rollback.
func (controller *Controller) CurrentState() (State, error) {
	controller.operation.Lock()
	defer controller.operation.Unlock()
	if err := controller.ctx.Err(); err != nil {
		return State{}, err
	}
	return controller.backend.read()
}

// RestoreState restores exact timers, including zero, independently of the
// user-facing modes' positive-timer fallback.
func (controller *Controller) RestoreState(state State) error {
	controller.operation.Lock()
	defer controller.operation.Unlock()
	if err := controller.ctx.Err(); err != nil {
		return err
	}
	err := controller.backend.write(controller.ctx, state)
	if err == nil {
		var actual State
		actual, err = controller.backend.read()
		if err == nil && actual != state {
			err = errors.New("系统电源配置未恢复为修改前的计时")
		}
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.err = err
	if err == nil {
		controller.mode = modeOf(state)
	}
	return err
}

func (controller *Controller) Close() error {
	controller.cancel()
	controller.operation.Lock()
	defer controller.operation.Unlock()
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.closed = true
	return nil
}
