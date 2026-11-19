#import <AppKit/AppKit.h>
#import <Metal/Metal.h>
#import <QuartzCore/QuartzCore.h>
#include <dlfcn.h>
#include <stdatomic.h>
#include "bridge.h"
#include "native/ghostty.h"

extern void sasaTerminalEvent(uintptr_t, int);

#define GHOSTTY_FUNCTIONS(X) \
    X(init) X(config_new) X(config_free) X(config_load_default_files) \
    X(config_load_recursive_files) X(config_finalize) \
    X(config_diagnostics_count) X(config_get_diagnostic) \
    X(app_new) X(app_tick) X(app_set_focus) X(app_update_config) \
    X(app_set_color_scheme) X(surface_config_new) X(surface_new) \
    X(surface_free) X(surface_userdata) X(surface_set_content_scale) \
    X(surface_set_size) X(surface_set_focus) X(surface_set_occlusion) \
    X(surface_set_color_scheme) X(surface_set_display_id) X(surface_draw) \
    X(surface_process_exited) X(surface_key) X(surface_key_is_binding) \
    X(surface_key_translation_mods) X(surface_text) X(surface_preedit) \
    X(surface_mouse_pos) X(surface_mouse_button) X(surface_mouse_scroll) \
    X(surface_ime_point) X(surface_binding_action) \
    X(surface_complete_clipboard_request) X(surface_read_text) X(surface_free_text)

#define DECLARE_FUNCTION(name) static __typeof__(ghostty_##name) *gt_##name;
GHOSTTY_FUNCTIONS(DECLARE_FUNCTION)

static ghostty_app_t terminalApp;
static char lastError[4096];
static atomic_bool tickPending;

@interface SasaGhosttyView : NSView <NSTextInputClient>
@property(nonatomic, assign) ghostty_surface_t surface;
@property(nonatomic, assign) uintptr_t token;
@property(nonatomic, assign) BOOL ended;
@property(nonatomic, strong) NSMutableAttributedString *marked;
@property(nonatomic, strong) NSMutableArray<NSString *> *keyText;
@property(nonatomic, strong) NSTrackingArea *tracking;
@property(nonatomic, strong) NSMutableArray *observers;
@property(nonatomic, weak) NSView *coordinateView;
- (void)syncSize;
- (void)sendEvent:(NSEvent *)event action:(ghostty_input_action_e)action text:(NSString *)text composing:(BOOL)composing;
@end

static NSString *escapedPath(NSString *path) {
    static NSCharacterSet *safe;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        safe = [NSCharacterSet characterSetWithCharactersInString:@"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-"];
    });
    if ([path rangeOfCharacterFromSet:safe.invertedSet].location == NSNotFound) return path;
    return [NSString stringWithFormat:@"'%@'", [path stringByReplacingOccurrencesOfString:@"'" withString:@"'\\''"]];
}

static ghostty_input_mods_e modifiers(NSEventModifierFlags flags) {
    unsigned value = 0;
    if (flags & NSEventModifierFlagShift) value |= GHOSTTY_MODS_SHIFT;
    if (flags & NSEventModifierFlagControl) value |= GHOSTTY_MODS_CTRL;
    if (flags & NSEventModifierFlagOption) value |= GHOSTTY_MODS_ALT;
    if (flags & NSEventModifierFlagCommand) value |= GHOSTTY_MODS_SUPER;
    if (flags & NSEventModifierFlagCapsLock) value |= GHOSTTY_MODS_CAPS;
    return value;
}

static void wakeup(void *userdata) {
    if (atomic_exchange(&tickPending, true)) return;
    dispatch_async(dispatch_get_main_queue(), ^{
        atomic_store(&tickPending, false);
        if (terminalApp) gt_app_tick(terminalApp);
    });
}

static void closeSurface(void *userdata, bool processAlive) {
    SasaGhosttyView *view = (__bridge SasaGhosttyView *)userdata;
    if (!view.token) return;
    if (!processAlive) view.ended = YES;
    sasaTerminalEvent(view.token, processAlive ? 1 : 0);
}

static bool readClipboard(void *userdata, ghostty_clipboard_e clipboard, void *state) {
    SasaGhosttyView *view = (__bridge SasaGhosttyView *)userdata;
    if (!view.surface) return false;
    NSString *text = [[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString] ?: @"";
    gt_surface_complete_clipboard_request(view.surface, text.UTF8String, state, false);
    return true;
}

static void confirmClipboard(void *userdata, const char *text, void *state, ghostty_clipboard_request_e request) {
    SasaGhosttyView *view = (__bridge SasaGhosttyView *)userdata;
    if (!view.surface) return;
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = request == GHOSTTY_CLIPBOARD_REQUEST_PASTE ? @"允许粘贴终端内容？" : @"允许终端程序访问剪贴板？";
    alert.informativeText = @"请确认当前终端程序发起的剪贴板操作。";
    [alert addButtonWithTitle:@"取消"];
    [alert addButtonWithTitle:@"允许"];
    NSString *contents = text ? [NSString stringWithUTF8String:text] : @"";
    [alert beginSheetModalForWindow:view.window completionHandler:^(NSModalResponse response) {
        if (view.surface) gt_surface_complete_clipboard_request(view.surface, response == NSAlertSecondButtonReturn ? contents.UTF8String : "", state, true);
    }];
}

static void writeClipboard(void *userdata, ghostty_clipboard_e clipboard, const ghostty_clipboard_content_s *contents, size_t count, bool confirm) {
    if (clipboard != GHOSTTY_CLIPBOARD_STANDARD) return;
    NSString *text = nil;
    for (size_t index = 0; index < count; index++) {
        if (strcmp(contents[index].mime, "text/plain") == 0) {
            text = [NSString stringWithUTF8String:contents[index].data];
            break;
        }
    }
    if (!text) return;
    void (^write)(void) = ^{
        [[NSPasteboard generalPasteboard] clearContents];
        [[NSPasteboard generalPasteboard] setString:text forType:NSPasteboardTypeString];
    };
    if (!confirm) { write(); return; }
    SasaGhosttyView *view = (__bridge SasaGhosttyView *)userdata;
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = @"允许终端程序写入剪贴板？";
    [alert addButtonWithTitle:@"取消"];
    [alert addButtonWithTitle:@"允许"];
    [alert beginSheetModalForWindow:view.window completionHandler:^(NSModalResponse response) {
        if (response == NSAlertSecondButtonReturn) write();
    }];
}

static bool action(ghostty_app_t app, ghostty_target_s target, ghostty_action_s action) {
    SasaGhosttyView *view = target.tag == GHOSTTY_TARGET_SURFACE ? (__bridge SasaGhosttyView *)gt_surface_userdata(target.target.surface) : nil;
    switch (action.tag) {
    case GHOSTTY_ACTION_RENDER:
        if (view) [view setNeedsDisplay:YES];
        return true;
    case GHOSTTY_ACTION_SHOW_CHILD_EXITED:
        if (view && view.token) { view.ended = YES; sasaTerminalEvent(view.token, 0); }
        return true;
    case GHOSTTY_ACTION_RING_BELL:
        NSBeep();
        return true;
    case GHOSTTY_ACTION_SET_TITLE:
    case GHOSTTY_ACTION_SET_TAB_TITLE:
        if (view && action.action.set_title.title) [view setAccessibilityLabel:[NSString stringWithUTF8String:action.action.set_title.title]];
        return true;
    case GHOSTTY_ACTION_NEW_TAB:
    case GHOSTTY_ACTION_NEW_WINDOW:
        if (view.token) sasaTerminalEvent(view.token, 3);
        return view != nil;
    case GHOSTTY_ACTION_QUIT:
        [NSApp terminate:nil];
        return true;
    case GHOSTTY_ACTION_CLOSE_TAB:
    case GHOSTTY_ACTION_CLOSE_WINDOW:
        if (view.token) sasaTerminalEvent(view.token, 1);
        return view != nil;
    case GHOSTTY_ACTION_OPEN_URL:
        if (action.action.open_url.url) {
            NSString *text = [[NSString alloc] initWithBytes:action.action.open_url.url length:action.action.open_url.len encoding:NSUTF8StringEncoding];
            NSURL *url = [NSURL URLWithString:text];
            if (url) [[NSWorkspace sharedWorkspace] openURL:url];
        }
        return true;
    default:
        return false;
    }
}

static ghostty_config_t configuration(void) {
    ghostty_config_t config = gt_config_new();
    if (!config) return NULL;
    gt_config_load_default_files(config);
    gt_config_load_recursive_files(config);
    gt_config_finalize(config);
    uint32_t count = gt_config_diagnostics_count(config);
    if (count) {
        ghostty_diagnostic_s diagnostic = gt_config_get_diagnostic(config, 0);
        snprintf(lastError, sizeof(lastError), "Ghostty 配置错误：%s", diagnostic.message);
        gt_config_free(config);
        return NULL;
    }
    return config;
}

const char *sasa_terminal_load(const char *path, const char *resources) {
    if (terminalApp) return NULL;
    void *library = dlopen(path, RTLD_NOW | RTLD_LOCAL);
    if (!library) { snprintf(lastError, sizeof(lastError), "%s", dlerror()); return lastError; }
#define LOAD_FUNCTION(name) gt_##name = dlsym(library, "ghostty_" #name); if (!gt_##name) { snprintf(lastError, sizeof(lastError), "缺少 Ghostty 接口：%s", #name); return lastError; }
    GHOSTTY_FUNCTIONS(LOAD_FUNCTION)
    if (resources && *resources) setenv("GHOSTTY_RESOURCES_DIR", resources, 1);
    if (gt_init(0, NULL) != GHOSTTY_SUCCESS) { snprintf(lastError, sizeof(lastError), "无法初始化 Ghostty"); return lastError; }
    ghostty_config_t config = configuration();
    if (!config) return lastError;
    ghostty_runtime_config_s options = {
        .wakeup_cb = wakeup,
        .action_cb = action,
        .read_clipboard_cb = readClipboard,
        .confirm_read_clipboard_cb = confirmClipboard,
        .write_clipboard_cb = writeClipboard,
        .close_surface_cb = closeSurface,
    };
    terminalApp = gt_app_new(&options, config);
    gt_config_free(config);
    if (!terminalApp) { snprintf(lastError, sizeof(lastError), "无法创建 Ghostty 原生终端"); return lastError; }
    return NULL;
}

static NSView *nativeHost(NSView *parent) {
    if ([NSStringFromClass(parent.class) isEqualToString:@"MyGoSurfaceView"]) return parent;
    for (NSView *child in parent.subviews) {
        NSView *found = nativeHost(child);
        if (found) return found;
    }
    return nil;
}

sasa_terminal_t sasa_terminal_new(uintptr_t window, uintptr_t token, const char *cwd, double x, double y, double width, double height, bool dark) {
    NSWindow *hostWindow = (__bridge NSWindow *)(void *)window;
    NSView *host = nativeHost(hostWindow.contentView);
    if (!host) { snprintf(lastError, sizeof(lastError), "没有找到 Sasa 原生视图"); return NULL; }
    ghostty_config_t config = configuration();
    if (!config) return NULL;
    gt_app_update_config(terminalApp, config);
    gt_config_free(config);
    gt_app_set_color_scheme(terminalApp, dark ? GHOSTTY_COLOR_SCHEME_DARK : GHOSTTY_COLOR_SCHEME_LIGHT);
    NSRect frame = [host convertRect:NSMakeRect(x, y, width, height) toView:host.superview];
    SasaGhosttyView *view = [[SasaGhosttyView alloc] initWithFrame:frame];
    view.coordinateView = host;
    view.token = token;
    view.marked = [[NSMutableAttributedString alloc] init];
    [view setAccessibilityElement:YES];
    [view setAccessibilityRole:NSAccessibilityTextAreaRole];
    [view setAccessibilityLabel:@"工作区终端"];
    [view registerForDraggedTypes:@[NSPasteboardTypeFileURL]];
    [host.superview addSubview:view];
    ghostty_surface_config_s options = gt_surface_config_new();
    options.platform_tag = GHOSTTY_PLATFORM_MACOS;
    options.platform.macos.nsview = (__bridge void *)view;
    options.userdata = (__bridge void *)view;
    options.scale_factor = hostWindow.backingScaleFactor;
    options.working_directory = cwd;
    ghostty_env_var_s terminalEnvironment[] = {{.key = "SASA_TERMINAL", .value = "1"}};
    options.env_vars = terminalEnvironment;
    options.env_var_count = sizeof(terminalEnvironment) / sizeof(terminalEnvironment[0]);
    view.surface = gt_surface_new(terminalApp, &options);
    if (!view.surface) {
        view.token = 0;
        [view removeFromSuperview];
        snprintf(lastError, sizeof(lastError), "Ghostty 无法创建终端视图，请检查配置和 Metal 支持");
        return NULL;
    }
    [view syncSize];
    [view.window makeFirstResponder:view];
    return (__bridge_retained void *)view;
}

const char *sasa_terminal_error(void) { return lastError; }

void sasa_terminal_present(sasa_terminal_t pointer, double x, double y, double width, double height, bool dark, bool focus) {
    SasaGhosttyView *view = (__bridge SasaGhosttyView *)pointer;
    NSRect frame = [view.coordinateView convertRect:NSMakeRect(x, y, width, height) toView:view.superview];
    if (!NSEqualRects(frame, view.frame)) { view.frame = frame; [view syncSize]; }
    if (view.hidden) { view.hidden = NO; gt_surface_set_occlusion(view.surface, true); }
    gt_surface_set_color_scheme(view.surface, dark ? GHOSTTY_COLOR_SCHEME_DARK : GHOSTTY_COLOR_SCHEME_LIGHT);
    if (focus) [view.window makeFirstResponder:view];
}

void sasa_terminal_hide(sasa_terminal_t pointer) {
    SasaGhosttyView *view = (__bridge SasaGhosttyView *)pointer;
    if (view.hidden) return;
    gt_surface_set_focus(view.surface, false);
    gt_surface_set_occlusion(view.surface, false);
    view.hidden = YES;
    if (view.window.firstResponder == view) [view.window makeFirstResponder:view.coordinateView];
}

void sasa_terminal_free(sasa_terminal_t pointer) {
    SasaGhosttyView *view = (__bridge_transfer SasaGhosttyView *)pointer;
    view.token = 0;
    if (view.surface) { gt_surface_free(view.surface); view.surface = NULL; }
    [view removeFromSuperview];
}

bool sasa_terminal_exited(sasa_terminal_t pointer) {
    SasaGhosttyView *view = (__bridge SasaGhosttyView *)pointer;
    return view.ended || !view.surface || gt_surface_process_exited(view.surface);
}

void sasa_terminal_text(sasa_terminal_t pointer, const char *text) {
    SasaGhosttyView *view = (__bridge SasaGhosttyView *)pointer;
    if (view.surface) gt_surface_text(view.surface, text, strlen(text));
}

@implementation SasaGhosttyView
- (BOOL)isFlipped { return YES; }
- (BOOL)acceptsFirstResponder { return YES; }
- (BOOL)becomeFirstResponder {
    if (self.surface) { gt_app_set_focus(terminalApp, NSApp.active); gt_surface_set_focus(self.surface, true); }
    return YES;
}
- (BOOL)resignFirstResponder {
    if (self.surface) gt_surface_set_focus(self.surface, false);
    return YES;
}
- (void)drawRect:(NSRect)rect { if (self.surface) gt_surface_draw(self.surface); }
- (void)viewDidChangeBackingProperties { [super viewDidChangeBackingProperties]; [self syncSize]; }
- (void)viewDidMoveToWindow {
    [super viewDidMoveToWindow];
    for (id observer in self.observers) [[NSNotificationCenter defaultCenter] removeObserver:observer];
    self.observers = [NSMutableArray array];
    if (self.window) {
        __weak SasaGhosttyView *weakView = self;
        for (NSString *name in @[NSWindowDidBecomeKeyNotification, NSWindowDidResignKeyNotification, NSWindowDidChangeOcclusionStateNotification, NSWindowDidChangeScreenNotification]) {
            id observer = [[NSNotificationCenter defaultCenter] addObserverForName:name object:self.window queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *notification) {
                SasaGhosttyView *view = weakView;
                if (!view.surface) return;
                gt_app_set_focus(terminalApp, NSApp.active);
                gt_surface_set_focus(view.surface, view.window.keyWindow && view.window.firstResponder == view);
                gt_surface_set_occlusion(view.surface, !view.hidden && (view.window.occlusionState & NSWindowOcclusionStateVisible));
                [view syncSize];
            }];
            [self.observers addObject:observer];
        }
    }
    [self syncSize];
}
- (void)dealloc { for (id observer in self.observers) [[NSNotificationCenter defaultCenter] removeObserver:observer]; }
- (id)accessibilityValue {
    if (!self.surface) return @"";
    ghostty_selection_s selection = {
        .top_left = {.tag = GHOSTTY_POINT_VIEWPORT, .coord = GHOSTTY_POINT_COORD_TOP_LEFT},
        .bottom_right = {.tag = GHOSTTY_POINT_VIEWPORT, .coord = GHOSTTY_POINT_COORD_BOTTOM_RIGHT},
    };
    ghostty_text_s text;
    if (!gt_surface_read_text(self.surface, selection, &text)) return @"";
    NSString *value = [[NSString alloc] initWithBytes:text.text length:text.text_len encoding:NSUTF8StringEncoding];
    gt_surface_free_text(self.surface, &text);
    return value ?: @"";
}
- (void)syncSize {
    if (!self.window) return;
    CGFloat scale = self.window.backingScaleFactor;
    [CATransaction begin];
    [CATransaction setDisableActions:YES];
    self.layer.contentsScale = scale;
    [CATransaction commit];
    if (!self.surface || self.bounds.size.width <= 0 || self.bounds.size.height <= 0) return;
    NSSize pixels = [self convertSizeToBacking:self.bounds.size];
    gt_surface_set_content_scale(self.surface, pixels.width / self.bounds.size.width, pixels.height / self.bounds.size.height);
    gt_surface_set_size(self.surface, (uint32_t)round(pixels.width), (uint32_t)round(pixels.height));
    NSNumber *display = self.window.screen.deviceDescription[@"NSScreenNumber"];
    if (display) gt_surface_set_display_id(self.surface, display.unsignedIntValue);
}
- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    if (self.tracking) [self removeTrackingArea:self.tracking];
    self.tracking = [[NSTrackingArea alloc] initWithRect:NSZeroRect options:NSTrackingMouseMoved | NSTrackingMouseEnteredAndExited | NSTrackingActiveInKeyWindow | NSTrackingInVisibleRect owner:self userInfo:nil];
    [self addTrackingArea:self.tracking];
}
- (void)sendEvent:(NSEvent *)event action:(ghostty_input_action_e)action text:(NSString *)text composing:(BOOL)composing {
    if (!self.surface) return;
    NSString *unshifted = [event charactersByApplyingModifiers:0] ?: @"";
    uint32_t codepoint = unshifted.length ? [unshifted characterAtIndex:0] : 0;
    ghostty_input_key_s key = {.action = action, .mods = modifiers(event.modifierFlags), .keycode = event.keyCode, .text = text.UTF8String, .unshifted_codepoint = codepoint, .composing = composing};
    if (text.length && !composing) key.consumed_mods = gt_surface_key_translation_mods(self.surface, key.mods) & ~(GHOSTTY_MODS_CTRL | GHOSTTY_MODS_SUPER);
    gt_surface_key(self.surface, key);
}
- (void)keyDown:(NSEvent *)event {
    if (!self.surface) return;
    BOOL wasMarked = self.marked.length > 0;
    self.keyText = [NSMutableArray array];
    ghostty_input_mods_e translated = gt_surface_key_translation_mods(self.surface, modifiers(event.modifierFlags));
    NSEvent *input = event;
    if ((event.modifierFlags & NSEventModifierFlagOption) && !(translated & GHOSTTY_MODS_ALT)) {
        NSEventModifierFlags flags = event.modifierFlags & ~NSEventModifierFlagOption;
        input = [NSEvent keyEventWithType:event.type location:event.locationInWindow modifierFlags:flags timestamp:event.timestamp windowNumber:event.windowNumber context:nil characters:[event charactersByApplyingModifiers:flags] ?: @"" charactersIgnoringModifiers:event.charactersIgnoringModifiers ?: @"" isARepeat:event.ARepeat keyCode:event.keyCode];
    }
    [self interpretKeyEvents:@[input]];
    ghostty_input_action_e action = event.ARepeat ? GHOSTTY_ACTION_REPEAT : GHOSTTY_ACTION_PRESS;
    if (self.keyText.count) {
        for (NSString *text in self.keyText) [self sendEvent:event action:action text:text composing:NO];
    } else {
        NSString *text = input.characters;
        if (text.length && ([text characterAtIndex:0] < 0x20 || [text characterAtIndex:0] == 0x7f || ([text characterAtIndex:0] >= 0xf700 && [text characterAtIndex:0] <= 0xf8ff))) text = nil;
        [self sendEvent:event action:action text:text composing:wasMarked || self.marked.length > 0];
    }
    self.keyText = nil;
}
- (void)keyUp:(NSEvent *)event { [self sendEvent:event action:GHOSTTY_ACTION_RELEASE text:nil composing:self.marked.length > 0]; }
- (void)flagsChanged:(NSEvent *)event {
    NSEventModifierFlags flag = 0;
    switch (event.keyCode) {
    case 56: case 60: flag = NSEventModifierFlagShift; break;
    case 59: case 62: flag = NSEventModifierFlagControl; break;
    case 58: case 61: flag = NSEventModifierFlagOption; break;
    case 55: case 54: flag = NSEventModifierFlagCommand; break;
    case 57: flag = NSEventModifierFlagCapsLock; break;
    default: return;
    }
    [self sendEvent:event action:(event.modifierFlags & flag) ? GHOSTTY_ACTION_PRESS : GHOSTTY_ACTION_RELEASE text:nil composing:self.marked.length > 0];
}
- (BOOL)performKeyEquivalent:(NSEvent *)event {
    if (event.type != NSEventTypeKeyDown || !self.surface) return NO;
    if ((event.modifierFlags & NSEventModifierFlagCommand) && !(event.modifierFlags & (NSEventModifierFlagControl | NSEventModifierFlagOption))) {
        NSString *key = event.charactersIgnoringModifiers.lowercaseString;
        if ([key isEqualToString:@"w"] || [key isEqualToString:@","] || [key isEqualToString:@"p"]) {
            int code = [key isEqualToString:@"w"] ? 1 : [key isEqualToString:@","] ? 2 : 5;
            if (self.token) sasaTerminalEvent(self.token, code);
            return YES;
        }
    }
    ghostty_input_key_s key = {.action = GHOSTTY_ACTION_PRESS, .mods = modifiers(event.modifierFlags), .keycode = event.keyCode, .text = event.characters.UTF8String};
    if (gt_surface_key_is_binding(self.surface, key, NULL)) { [self keyDown:event]; return YES; }
    return NO;
}
- (void)copy:(id)sender { const char *command = "copy_to_clipboard"; gt_surface_binding_action(self.surface, command, strlen(command)); }
- (void)paste:(id)sender { const char *command = "paste_from_clipboard"; gt_surface_binding_action(self.surface, command, strlen(command)); }
- (void)selectAll:(id)sender { const char *command = "select_all"; gt_surface_binding_action(self.surface, command, strlen(command)); }
- (void)doCommandBySelector:(SEL)selector {}
- (void)insertText:(id)value replacementRange:(NSRange)range {
    NSString *text = [[value isKindOfClass:NSAttributedString.class] ? [value string] : value copy];
    [self unmarkText];
    if (self.keyText) [self.keyText addObject:text];
    else if (self.surface) gt_surface_text(self.surface, text.UTF8String, [text lengthOfBytesUsingEncoding:NSUTF8StringEncoding]);
}
- (void)setMarkedText:(id)value selectedRange:(NSRange)selected replacementRange:(NSRange)replacement {
    self.marked = [value isKindOfClass:NSAttributedString.class] ? [value mutableCopy] : [[NSMutableAttributedString alloc] initWithString:value];
    if (self.surface) gt_surface_preedit(self.surface, self.marked.string.UTF8String, [self.marked.string lengthOfBytesUsingEncoding:NSUTF8StringEncoding]);
}
- (void)unmarkText {
    [self.marked setAttributedString:[[NSAttributedString alloc] initWithString:@""]];
    if (self.surface) gt_surface_preedit(self.surface, NULL, 0);
}
- (BOOL)hasMarkedText { return self.marked.length > 0; }
- (NSRange)markedRange { return self.marked.length ? NSMakeRange(0, self.marked.length) : NSMakeRange(NSNotFound, 0); }
- (NSRange)selectedRange { return NSMakeRange(NSNotFound, 0); }
- (NSArray<NSAttributedStringKey> *)validAttributesForMarkedText { return @[]; }
- (NSAttributedString *)attributedSubstringForProposedRange:(NSRange)range actualRange:(NSRangePointer)actual { if (actual) *actual = NSMakeRange(NSNotFound, 0); return nil; }
- (NSUInteger)characterIndexForPoint:(NSPoint)point { return NSNotFound; }
- (NSRect)firstRectForCharacterRange:(NSRange)range actualRange:(NSRangePointer)actual {
    if (actual) *actual = NSMakeRange(0, 0);
    double x = 0, y = 0, width = 0, height = 0;
    if (self.surface) gt_surface_ime_point(self.surface, &x, &y, &width, &height);
    NSRect rect = [self convertRect:NSMakeRect(x, y, width, height) toView:nil];
    return [self.window convertRectToScreen:rect];
}
- (void)mouseMoved:(NSEvent *)event {
    if (!self.surface) return;
    NSPoint position = [self convertPoint:event.locationInWindow fromView:nil];
    gt_surface_mouse_pos(self.surface, position.x, position.y, modifiers(event.modifierFlags));
}
- (void)mouseDragged:(NSEvent *)event { [self mouseMoved:event]; }
- (void)rightMouseDragged:(NSEvent *)event { [self mouseMoved:event]; }
- (void)otherMouseDragged:(NSEvent *)event { [self mouseMoved:event]; }
- (void)mouseDown:(NSEvent *)event {
    [self.window makeFirstResponder:self];
    if (self.token) sasaTerminalEvent(self.token, 4);
    [self mouseMoved:event];
    if (self.surface) gt_surface_mouse_button(self.surface, GHOSTTY_MOUSE_PRESS, (ghostty_input_mouse_button_e)(event.buttonNumber + 1), modifiers(event.modifierFlags));
}
- (void)mouseUp:(NSEvent *)event { if (self.surface) gt_surface_mouse_button(self.surface, GHOSTTY_MOUSE_RELEASE, (ghostty_input_mouse_button_e)(event.buttonNumber + 1), modifiers(event.modifierFlags)); }
- (void)rightMouseDown:(NSEvent *)event { [self mouseDown:event]; }
- (void)rightMouseUp:(NSEvent *)event { [self mouseUp:event]; }
- (void)otherMouseDown:(NSEvent *)event { [self mouseDown:event]; }
- (void)otherMouseUp:(NSEvent *)event { [self mouseUp:event]; }
- (NSDragOperation)draggingEntered:(id<NSDraggingInfo>)sender {
    if ([sender.draggingPasteboard canReadObjectForClasses:@[NSURL.class] options:@{NSPasteboardURLReadingFileURLsOnlyKey: @YES}]) return NSDragOperationCopy;
    return NSDragOperationNone;
}
- (BOOL)performDragOperation:(id<NSDraggingInfo>)sender {
    NSArray *urls = [sender.draggingPasteboard readObjectsForClasses:@[NSURL.class] options:@{NSPasteboardURLReadingFileURLsOnlyKey: @YES}];
    if (!urls.count || !self.surface) return NO;
    NSMutableString *text = [NSMutableString string];
    for (NSURL *url in urls) {
        if (text.length) [text appendString:@" "];
        [text appendString:escapedPath(url.path)];
    }
    gt_surface_text(self.surface, text.UTF8String, [text lengthOfBytesUsingEncoding:NSUTF8StringEncoding]);
    return YES;
}
- (void)scrollWheel:(NSEvent *)event {
    if (!self.surface) return;
    double scale = event.hasPreciseScrollingDeltas ? 2 : 1;
    int momentum = GHOSTTY_MOUSE_MOMENTUM_NONE;
    if (event.momentumPhase & NSEventPhaseBegan) momentum = GHOSTTY_MOUSE_MOMENTUM_BEGAN;
    else if (event.momentumPhase & NSEventPhaseChanged) momentum = GHOSTTY_MOUSE_MOMENTUM_CHANGED;
    else if (event.momentumPhase & NSEventPhaseEnded) momentum = GHOSTTY_MOUSE_MOMENTUM_ENDED;
    else if (event.momentumPhase & NSEventPhaseCancelled) momentum = GHOSTTY_MOUSE_MOMENTUM_CANCELLED;
    gt_surface_mouse_scroll(self.surface, event.scrollingDeltaX * scale, event.scrollingDeltaY * scale, (event.hasPreciseScrollingDeltas ? 1 : 0) | (momentum << 1));
}
@end
