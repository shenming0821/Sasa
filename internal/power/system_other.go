//go:build !darwin || !cgo

package power

import "errors"

var errUnsupported = errors.New("保持唤醒需要 macOS 和启用 cgo 的构建")

type systemBackend struct{}

func (driver *systemBackend) onAC() (bool, error) {
	return true, errUnsupported
}

func (driver *systemBackend) acquire() error {
	return errUnsupported
}

func (driver *systemBackend) release() error {
	return errUnsupported
}
