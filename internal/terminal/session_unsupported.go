//go:build !darwin || !cgo

package terminal

import (
	"errors"

	native "github.com/egoist/mygo/ui"
)

func (s *Session) Present(uintptr, native.Rect, bool, bool) error {
	return errors.New("Ghostty 原生终端需要 macOS 和启用 cgo 的构建")
}

func (s *Session) Hide()  {}
func (s *Session) Close() { s.closed.Store(true) }
