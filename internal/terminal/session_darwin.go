//go:build darwin && cgo

package terminal

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -fblocks
#cgo LDFLAGS: -framework AppKit -framework Metal -framework QuartzCore
#include "bridge.h"
*/
import "C"

import (
	"errors"
	"os"
	"runtime/cgo"
	"unsafe"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
)

func (s *Session) Present(window uintptr, bounds native.Rect, dark, focus bool) error {
	if s.closed.Load() || window == 0 || bounds.W <= 0 || bounds.H <= 0 {
		return nil
	}
	var result error
	mygo.RunOnMain(func() {
		if s.handle == nil {
			executable, err := os.Executable()
			if err != nil {
				result = err
				return
			}
			library, resources := libraryPaths(executable)
			libraryName, resourcesName := C.CString(library), C.CString(resources)
			defer C.free(unsafe.Pointer(libraryName))
			defer C.free(unsafe.Pointer(resourcesName))
			if message := C.fuyu_terminal_load(libraryName, resourcesName); message != nil {
				result = errors.New(C.GoString(message))
				return
			}
			token := cgo.NewHandle(s)
			root := C.CString(s.root)
			defer C.free(unsafe.Pointer(root))
			s.handle = unsafe.Pointer(C.fuyu_terminal_new(C.uintptr_t(window), C.uintptr_t(token), root, C.double(bounds.X), C.double(bounds.Y), C.double(bounds.W), C.double(bounds.H), C.bool(dark)))
			if s.handle == nil {
				token.Delete()
				result = errors.New(C.GoString(C.fuyu_terminal_error()))
				return
			}
			s.token = uintptr(token)
		}
		C.fuyu_terminal_present(C.fuyu_terminal_t(s.handle), C.double(bounds.X), C.double(bounds.Y), C.double(bounds.W), C.double(bounds.H), C.bool(dark), C.bool(focus))
	})
	return result
}

func (s *Session) Hide() {
	mygo.RunOnMain(func() {
		if s.handle != nil {
			C.fuyu_terminal_hide(C.fuyu_terminal_t(s.handle))
		}
	})
}

func (s *Session) Close() {
	mygo.RunOnMain(func() {
		if s.closed.Swap(true) {
			return
		}
		if s.handle != nil {
			C.fuyu_terminal_free(C.fuyu_terminal_t(s.handle))
			s.handle = nil
			cgo.Handle(s.token).Delete()
			s.token = 0
		}
	})
}

//export fuyuTerminalEvent
func fuyuTerminalEvent(token C.uintptr_t, event C.int) {
	session := cgo.Handle(token).Value().(*Session)
	switch event {
	case 0:
		session.exited.Store(true)
		session.notify("exit")
	case 1:
		session.notify("w")
	case 2:
		session.notify(",")
	case 3:
		session.notify("t")
	case 4:
		session.notify("focus")
	case 5:
		session.notify("p")
	}
}
