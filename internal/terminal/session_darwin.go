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
	"fmt"
	"os"
	"runtime/cgo"
	"unsafe"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
)

var configurationGeneration uint64

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
			if message := C.sasa_terminal_load(libraryName, resourcesName); message != nil {
				result = errors.New(C.GoString(message))
				return
			}
			token := cgo.NewHandle(s)
			root := C.CString(s.root)
			defer C.free(unsafe.Pointer(root))
			configurationGeneration++
			s.handle = unsafe.Pointer(C.sasa_terminal_new(C.uintptr_t(window), C.uintptr_t(token), root, C.double(bounds.X), C.double(bounds.Y), C.double(bounds.W), C.double(bounds.H), C.bool(dark)))
			if s.handle == nil {
				token.Delete()
				result = errors.New(C.GoString(C.sasa_terminal_error()))
				return
			}
			s.token = uintptr(token)
			s.theme = themeState{dark: dark, generation: configurationGeneration}
		}
		C.sasa_terminal_present(C.sasa_terminal_t(s.handle), C.double(bounds.X), C.double(bounds.Y), C.double(bounds.W), C.double(bounds.H), C.bool(dark), C.bool(focus))
	})
	return result
}

func (s *Session) SetTheme(colors Theme, dark bool) error {
	var result error
	mygo.RunOnMain(func() {
		if s.handle == nil {
			return
		}
		if s.theme.colors == colors && s.theme.dark == dark && s.theme.generation == configurationGeneration {
			result = s.theme.err
			return
		}
		result = s.applyTheme(colors, dark)
		s.theme = themeState{colors: colors, dark: dark, generation: configurationGeneration, err: result}
	})
	return result
}

func (s *Session) applyTheme(colors Theme, dark bool) error {
	var path *C.char
	if config := colors.config(); config != "" {
		file, err := os.CreateTemp("", "sasa-ghostty-theme-*.conf")
		if err != nil {
			return fmt.Errorf("创建临时配色配置：%w", err)
		}
		defer os.Remove(file.Name())
		_, writeErr := file.WriteString(config)
		if err := errors.Join(writeErr, file.Close()); err != nil {
			return fmt.Errorf("写入临时配色配置：%w", err)
		}
		path = C.CString(file.Name())
		defer C.free(unsafe.Pointer(path))
	}
	if message := C.sasa_terminal_set_theme(C.sasa_terminal_t(s.handle), path, C.bool(dark)); message != nil {
		return errors.New(C.GoString(message))
	}
	return nil
}

func (s *Session) Hide() {
	mygo.RunOnMain(func() {
		if s.handle != nil {
			C.sasa_terminal_hide(C.sasa_terminal_t(s.handle))
		}
	})
}

func (s *Session) Close() {
	mygo.RunOnMain(func() {
		if s.closed.Swap(true) {
			return
		}
		if s.handle != nil {
			C.sasa_terminal_free(C.sasa_terminal_t(s.handle))
			s.handle = nil
			cgo.Handle(s.token).Delete()
			s.token = 0
		}
	})
}

//export sasaTerminalEvent
func sasaTerminalEvent(token C.uintptr_t, event C.int) {
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
