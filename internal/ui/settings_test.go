package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/codex"
)

func TestSettingsAppearancePersistenceAndFontScope(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	store := imageStore(t, a)
	tt := native.NewTester(a.view, 1000, 760)
	a.openSettings()
	a.fontFamilies = []string{latinFont, "Menlo", "Helvetica", "PingFang SC"}
	tt.Frame()
	if !tt.HasText("字体预览") || !tt.HasText("代码字体") {
		t.Fatal("font settings missing")
	}
	if err := tt.Click("主题 深色"); err != nil {
		t.Fatal(err)
	}
	if a.preferences.Theme != "dark" {
		t.Fatal("theme change failed")
	}
	tt.Frame()
	if err := tt.Click("选择英文字体"); err != nil {
		t.Fatal(err)
	}
	tt.Click("搜索英文字体")
	tt.Type("Helvetica")
	if tt.HasText("英文字体 Menlo") || !tt.HasText("英文字体 Helvetica") {
		t.Fatal("font search failed")
	}
	if err := tt.Click("英文字体 Helvetica"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.preferences.EnglishFont != "Helvetica" || a.preferences.CodeFont != latinFont || !strings.HasPrefix(a.md.Theme.Font, "Helvetica,") || !strings.HasPrefix(a.md.Theme.CodeFont, latinFont+",") {
		t.Fatal("font scope incorrect", a.preferences)
	}
	tt.Click("选择中文字体")
	tt.Click("中文字体 PingFang SC")
	tt.Frame()
	if a.preferences.ChineseFont != "PingFang SC" || !strings.Contains(a.codeFontFamily(), "PingFang SC") {
		t.Fatal("Chinese fallback missing")
	}
	tt.Click("选择代码字体")
	tt.Click("代码字体 Menlo")
	tt.Frame()
	if a.preferences.CodeFont != "Menlo" || a.preferences.EnglishFont != "Helvetica" {
		t.Fatal("code and English coupled")
	}
	if a.codeFontFamily() != a.md.Theme.CodeFont {
		t.Fatal("Markdown code font stale")
	}
	tt.Click("设置分类 通知")
	tt.Click("切换提示音")
	if !a.preferences.NotificationSound {
		t.Fatal("sound toggle failed")
	}
	restored := New(stubBackend{}, "test", "/workspace", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.preferences != a.preferences {
		t.Fatal("preferences not restored", restored.preferences, a.preferences)
	}
	tt.Click("关闭设置")
	if a.settingsOpen {
		t.Fatal("settings did not close")
	}
	tt.Key(native.Cmd, native.KeyComma)
	if !a.settingsOpen {
		t.Fatal("settings shortcut failed")
	}
	tt.Key(native.Cmd, native.KeyW)
	if a.settingsOpen || !a.open {
		t.Fatal("close settings closed session")
	}
	// The same shaper must size the scrollable editor and draw the selected font.
	want, _ := native.ShapeText("WWii 你好", native.Font{Family: a.codeFontFamily(), Size: 13}, 0).Size()
	got := editorTextWidthForFont(context.Background(), "WWii 你好", a.codeFontFamily(), 13, 90)
	if got != want+90 {
		t.Fatal("editor width differs from selected font", got, want)
	}
}

func TestSettingsLayoutAndDefaultMigration(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	store := imageStore(t, a)
	snapshot, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Preferences = nil
	if err = store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	restored := New(stubBackend{}, "test", "/workspace", nil)
	if err = restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.preferences != defaultPreferences() {
		t.Fatal("old settings default changed", restored.preferences)
	}
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{640, 480}, {1000, 760}} {
			t.Run(fmt.Sprintf("%v/%v", dark, size), func(t *testing.T) {
				a := New(stubBackend{}, "test", "/workspace", nil)
				tt := native.NewTester(a.view, size[0], size[1])
				tt.SetDark(dark)
				a.openSettings()
				tt.Frame()
				for _, label := range []string{"关闭设置", "主题 跟随系统", "选择英文字体", "选择中文字体", "选择代码字体"} {
					r, ok := tt.Find(label)
					if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > float32(size[0]) || r.Y+r.H > float32(size[1]) {
						t.Fatal("settings clipped", label, r)
					}
				}
				tt.Click("主题 深色")
				tt.Click("主题 跟随系统")
				if a.preferences.Theme != "system" {
					t.Fatal("system theme not restored")
				}
				tt.Click("设置分类 通知")
				tt.Click("切换后台任务完成通知")
				if a.preferences.CompletionNotification {
					t.Fatal("notification toggle failed")
				}
				tt.Click("切换提示音")
				if a.preferences.NotificationSound {
					t.Fatal("disabled sound changed")
				}
				tt.Key(0, native.KeyEscape)
				if a.settingsOpen {
					t.Fatal("Escape did not close settings")
				}
			})
		}
	}
	if path := os.Getenv("FUYU_SETTINGS_SCREENSHOT"); path != "" {
		a.openSettings()
		tt := native.NewTester(a.view, 1000, 760)
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompletionNotificationsRespectForegroundPreferencesAndDuplicates(t *testing.T) {
	cases := []struct {
		name                             string
		focused, current, enabled, sound bool
		status                           string
		want                             bool
	}{
		{"foreground", true, true, true, false, "completed", false},
		{"other session", true, false, true, false, "completed", true},
		{"background silent", false, true, true, false, "completed", true},
		{"background sound", false, true, true, true, "completed", true},
		{"disabled", false, true, false, true, "completed", false},
		{"interrupted", false, true, true, true, "interrupted", false},
		{"failed", false, true, true, true, "failed", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := New(stubBackend{}, "test", "/workspace", nil)
			a.preferences.CompletionNotification, a.preferences.NotificationSound = tc.enabled, tc.sound
			a.State.ThreadID, a.State.TurnID, a.State.Busy = "thread", "turn", true
			owner := a.session
			if !tc.current {
				a.newSession("/other")
			}
			a.windowFocused = func() bool { return tc.focused }
			calls := make(chan mygo.NotificationOptions, 2)
			a.notificationSender = func(o mygo.NotificationOptions) error { calls <- o; return nil }
			queue := make(chan func(), 2)
			a.dispatch = func(fn func()) { queue <- fn }
			params, _ := json.Marshal(map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": tc.status}})
			event := codex.Event{Method: "turn/completed", Params: params}
			a.applyBatch([]codex.Event{event, event})
			if owner.State.Busy {
				t.Fatal("turn not completed")
			}
			if tc.want {
				select {
				case o := <-calls:
					if o.Silent == tc.sound || o.ID != fmt.Sprintf("fuyu-session-%d", owner.id) {
						t.Fatal("notification options incorrect", o)
					}
				case <-time.After(time.Second):
					t.Fatal("missing completion notification")
				}
				pumpUpdate(t, queue)
			}
			select {
			case o := <-calls:
				t.Fatal("unexpected or duplicate notification", o)
			case <-time.After(20 * time.Millisecond):
			}
		})
	}
}

func TestSettingsSaveFailureRollsBack(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	store := imageStore(t, a)
	previous := a.preferences
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	a.preferences.EnglishFont = "Helvetica"
	a.savePreferences(previous)
	if a.preferences != previous || a.settingsError == "" {
		t.Fatal("failed save kept new settings", a.preferences)
	}
}

func TestSettingsLabelsAndFontDropdownAlign(t *testing.T) {
	for _, size := range [][2]int{{640, 480}, {1000, 760}} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/%v", size, dark), func(t *testing.T) {
				a := New(stubBackend{}, "test", "/workspace", nil)
				a.openSettings()
				a.fontFamilies = []string{latinFont, "Menlo", strings.Repeat("Very Long Font Family ", 5)}
				tt := native.NewTester(a.view, size[0], size[1])
				tt.SetDark(dark)
				var controlX, controlRight float32
				for i, label := range []string{"英文字体", "中文字体", "代码字体"} {
					caption, ok := tt.Find("设置标签 " + label)
					if !ok {
						t.Fatal("missing setting label", label)
					}
					trigger, ok := tt.Find("选择" + label)
					if !ok {
						t.Fatal("missing font trigger", label)
					}
					if abs32(caption.Y+caption.H/2-trigger.Y-trigger.H/2) > 1 || caption.X+caption.W >= trigger.X {
						t.Fatal("label and control not on same row", label, caption, trigger)
					}
					if i == 0 {
						controlX, controlRight = trigger.X, trigger.X+trigger.W
					}
					if abs32(trigger.X-controlX) > 1 || abs32(trigger.X+trigger.W-controlRight) > 1 {
						t.Fatal("font controls not aligned", label, trigger)
					}
					if err := tt.Click("选择" + label); err != nil {
						t.Fatal(err)
					}
					tt.Frame()
					panel, ok := tt.Find("字体列表 " + label)
					if !ok || abs32(panel.W-trigger.W) > 1 || abs32(panel.X-trigger.X) > 1 {
						t.Fatal("dropdown differs from trigger", label, panel, trigger)
					}
					if panel.X < 0 || panel.Y < 0 || panel.X+panel.W > float32(size[0]) || panel.Y+panel.H > float32(size[1]) {
						t.Fatal("dropdown outside window", panel)
					}
					search, ok := tt.Find("搜索" + label)
					if !ok || search.X < panel.X || search.X+search.W > panel.X+panel.W {
						t.Fatal("font search clipped", search, panel)
					}
					tt.Key(0, native.KeyEscape)
				}
				caption, _ := tt.Find("设置标签 主题")
				theme, _ := tt.Find("主题 跟随系统")
				if abs32(caption.Y+caption.H/2-theme.Y-theme.H/2) > 1 || abs32(theme.X-controlX) > 1 {
					t.Fatal("theme row not aligned", caption, theme)
				}
			})
		}
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
