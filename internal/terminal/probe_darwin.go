//go:build darwin && cgo && fuyu_terminal_probe

package terminal

/*
#import <AppKit/AppKit.h>
#import <QuartzCore/QuartzCore.h>
#include "bridge.h"
static bool probe_backing_scale(fuyu_terminal_t pointer, bool change) {
    NSView *view = (__bridge NSView *)pointer;
    CGFloat scale = view.window.backingScaleFactor;
    if (change) {
        [CATransaction begin];
        [CATransaction setDisableActions:YES];
        view.layer.contentsScale = scale == 1 ? 2 : 1;
        [CATransaction commit];
        [view viewDidChangeBackingProperties];
    }
    return view.layer && view.layer.contentsScale == scale && view.layer.animationKeys.count == 0;
}
static char *probe_screen(fuyu_terminal_t pointer) {
    NSView *view = (__bridge NSView *)pointer;
    NSString *value = [view accessibilityValue];
    return strdup(value.UTF8String ?: "");
}
static void probe_key(fuyu_terminal_t pointer, const char *characters, unsigned short keycode) {
    NSView *view = (__bridge NSView *)pointer;
    NSString *text = [NSString stringWithUTF8String:characters];
    NSEvent *event = [NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint modifierFlags:0 timestamp:NSProcessInfo.processInfo.systemUptime windowNumber:view.window.windowNumber context:nil characters:text charactersIgnoringModifiers:text isARepeat:NO keyCode:keycode];
    [view keyDown:event];
}
static void probe_composition(fuyu_terminal_t pointer, const char *characters) {
    id<NSTextInputClient> view = (__bridge id)pointer;
    NSString *text = [NSString stringWithUTF8String:characters];
    [view setMarkedText:text selectedRange:NSMakeRange(text.length, 0) replacementRange:NSMakeRange(NSNotFound, 0)];
    [view insertText:[(NSView *)view valueForKey:@"marked"] replacementRange:NSMakeRange(NSNotFound, 0)];
}
*/
import "C"

import (
	"unsafe"

	"github.com/egoist/mygo"
)

func (s *Session) probeBackingScale(change bool) bool {
	var valid bool
	mygo.RunOnMain(func() {
		valid = bool(C.probe_backing_scale(C.fuyu_terminal_t(s.handle), C.bool(change)))
	})
	return valid
}

func (s *Session) probeText(text string) {
	mygo.RunOnMain(func() {
		value := C.CString(text)
		defer C.free(unsafe.Pointer(value))
		C.fuyu_terminal_text(C.fuyu_terminal_t(s.handle), value)
	})
}

func (s *Session) probeKey(text string, keycode uint16) {
	mygo.RunOnMain(func() {
		value := C.CString(text)
		defer C.free(unsafe.Pointer(value))
		C.probe_key(C.fuyu_terminal_t(s.handle), value, C.ushort(keycode))
	})
}

func (s *Session) probeComposition(text string) {
	mygo.RunOnMain(func() {
		value := C.CString(text)
		defer C.free(unsafe.Pointer(value))
		C.probe_composition(C.fuyu_terminal_t(s.handle), value)
	})
}

func (s *Session) probeScreen() string {
	var screen string
	mygo.RunOnMain(func() {
		value := C.probe_screen(C.fuyu_terminal_t(s.handle))
		defer C.free(unsafe.Pointer(value))
		screen = C.GoString(value)
	})
	return screen
}
