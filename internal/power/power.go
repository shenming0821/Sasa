package power

import (
	"errors"
	"fmt"
	"sync"
	"time"
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

type backend interface {
	onAC() (bool, error)
	acquire() error
	release() error
}

type Controller struct {
	mu      sync.Mutex
	backend backend
	mode    string
	active  bool
	err     error
	closed  bool
	stop    chan struct{}
	done    chan struct{}
}

func New() *Controller {
	return newController(&systemBackend{})
}

func newController(driver backend) *Controller {
	controller := &Controller{backend: driver, mode: Never, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(controller.done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-controller.stop:
				return
			case <-ticker.C:
				controller.refresh()
			}
		}
	}()
	return controller
}

func (controller *Controller) Set(mode string) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.closed {
		return errors.New("保持唤醒服务已关闭")
	}
	previous := controller.mode
	controller.mode = Normalize(mode)
	controller.err = controller.apply()
	if controller.err != nil {
		controller.mode = previous
		controller.err = errors.Join(controller.err, controller.apply())
	}
	return controller.err
}

func (controller *Controller) refresh() {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if !controller.closed && controller.mode == ACOnly {
		controller.err = controller.apply()
	}
}

func (controller *Controller) apply() error {
	wanted := controller.mode == Always
	if controller.mode == ACOnly {
		connected, err := controller.backend.onAC()
		if err != nil {
			if controller.active {
				if releaseErr := controller.backend.release(); releaseErr != nil {
					return errors.Join(err, releaseErr)
				}
				controller.active = false
			}
			return err
		}
		wanted = connected
	}
	if wanted == controller.active {
		return nil
	}
	var err error
	if wanted {
		err = controller.backend.acquire()
	} else {
		err = controller.backend.release()
	}
	if err != nil {
		return fmt.Errorf("更新保持唤醒状态：%w", err)
	}
	controller.active = wanted
	return nil
}

func (controller *Controller) Err() error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.err
}

func (controller *Controller) Close() error {
	controller.mu.Lock()
	if controller.closed {
		controller.mu.Unlock()
		return nil
	}
	controller.closed = true
	close(controller.stop)
	controller.mu.Unlock()
	<-controller.done
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.active {
		if err := controller.backend.release(); err != nil {
			return err
		}
		controller.active = false
	}
	return nil
}
