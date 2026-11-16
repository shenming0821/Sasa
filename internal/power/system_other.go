//go:build !darwin || !cgo

package power

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("保持唤醒需要 macOS 和启用 cgo 的构建")

type systemBackend struct{}

func (systemBackend) read() (State, error) {
	return State{}, errUnsupported
}

func (systemBackend) write(_ context.Context, _ State) error {
	return errUnsupported
}
