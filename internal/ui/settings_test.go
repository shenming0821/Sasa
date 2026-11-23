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
	"github.com/shenming0821/Sasa/internal/codex"
)

func TestSettingsDefaultChineseFontRemainsSelectable(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	store := imageStore(t, a)
	tt := native.NewTester(a.view, 1000, 760)
	a.openSettings()
	a.fontFamilies = []string{latinFont, "PingFang SC"}
	tt.Frame()
	for _, label := range []string{"设置分类 字体", "选择中文字体", "中文字体 PingFang SC", "选择中文字体", "中文字体 .PingFang UI SC"} {
		if err := tt.Click(label); err != nil {
			t.Fatalf("default Chinese font must remain selectable after changing fonts: %s: %v", label, err)
		}
		tt.Frame()
	}
	if a.preferences.ChineseFont != defaultPreferences().ChineseFont {
		t.Fatal("default Chinese font was not restored", a.preferences.ChineseFont)
	}
	restored := New(stubBackend{}, "test", "/workspace", nil)
	if err := restored.AttachStore(store); err != nil {
		t.Fatal(err)
	}
	if restored.preferences.ChineseFont != defaultPreferences().ChineseFont {
		t.Fatal("restored default font did not persist", restored.preferences.ChineseFont)
	}
}

func TestSettingsAppearancePersistenceAndFontScope(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	store := imageStore(t, a)
	tt := native.NewTester(a.view, 1000, 760)
	a.openSettings()
	a.fontFamilies = []string{latinFont, "Menlo", "Helvetica", "PingFang SC"}
	tt.Frame()
	tt.Click("设置分类 外观")
	if err := tt.Click("主题 深色"); err != nil {
		t.Fatal(err)
	}
	if a.preferences.Theme != "dark" {
		t.Fatal("theme change failed")
	}
	tt.Frame()
	if err := tt.Click("设置分类 字体"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("字体预览") || !tt.HasText("代码字体") {
		t.Fatal("font settings missing")
	}
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
				tt.Click("设置分类 外观")
				for _, label := range []string{"关闭设置", "配色 默认", "配色 Claude", "主题 跟随系统"} {
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
				tt.Click("设置分类 字体")
				for _, label := range []string{"选择英文字体", "选择中文字体", "选择代码字体"} {
					bounds, found := tt.Find(label)
					if !found || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > float32(size[0]) || bounds.Y+bounds.H > float32(size[1]) {
						t.Fatal("font setting clipped", label, bounds)
					}
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
	if path := os.Getenv("SASA_SETTINGS_SCREENSHOT"); path != "" {
		a.openSettings()
		tt := native.NewTester(func(c *native.Context) {
			a.applyAppearance(c)
			a.settingsPanel(c, false)
		}, 940, 648)
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
					if o.Silent == tc.sound || o.ID != fmt.Sprintf("sasa-session-%d", owner.id) {
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
				tt.Click("设置分类 字体")
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
				tt.Click("设置分类 外观")
				caption, hasCaption := tt.Find("设置标签 明暗模式")
				modes, hasModes := tt.Find("明暗模式")
				if !hasCaption || !hasModes || caption.Y+caption.H < modes.Y || caption.Y > modes.Y+modes.H ||
					caption.X+caption.W >= modes.X || abs32(modes.X+modes.W-controlRight) > 1 {
					t.Fatal("appearance mode row not aligned", caption, modes)
				}
				defaultCard, hasDefault := tt.Find("配色 默认")
				claudeCard, hasClaude := tt.Find("配色 Claude")
				if !hasDefault || !hasClaude || defaultCard.W <= 0 || abs32(defaultCard.W-claudeCard.W) > 1 ||
					abs32(defaultCard.Y-claudeCard.Y) > 1 || defaultCard.X+defaultCard.W > claudeCard.X ||
					claudeCard.X+claudeCard.W > controlRight {
					t.Fatal("palette cards not aligned", defaultCard, claudeCard)
				}
			})
		}
	}
}

func TestSettingsPaletteCardRepaintsWithAppearance(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	a.openSettings()
	tt := native.NewTester(a.view, 1000, 760)
	if err := tt.Click("设置分类 外观"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []struct {
		label string
		theme *native.Theme
	}{
		{"主题 深色", native.DarkTheme()},
		{"主题 浅色", native.LightTheme()},
	} {
		if err := tt.Click(mode.label); err != nil {
			t.Fatal(err)
		}
		card, ok := tt.Find("配色 默认")
		if !ok {
			t.Fatal("default palette card missing")
		}
		want := themeByID("").paletteFor(mode.theme.Dark).editorFor(mode.theme.Dark).background
		pixel := tt.Image().RGBAAt(int(card.X+card.W/2), int(card.Y+60))
		if got := (native.Color{R: pixel.R, G: pixel.G, B: pixel.B, A: pixel.A}); got != want {
			t.Fatalf("%s: palette preview background is %v, want %v", mode.label, got, want)
		}
	}
}

func TestSettingsSearchFindsControlsAcrossCategories(t *testing.T) {
	a := New(stubBackend{}, "test", "/workspace", nil)
	a.openSettings()
	tt := native.NewTester(a.view, 640, 480)
	if err := tt.Click("搜索设置"); err != nil {
		t.Fatal(err)
	}
	tt.Type("代码字体")
	if !tt.HasText("搜索结果") || !tt.HasText("代码字体") || tt.HasText("英文字体") {
		t.Fatal("search did not filter the settings fields")
	}
	if err := tt.Click("选择代码字体"); err != nil {
		t.Fatal(err)
	}
	if _, found := tt.Find("字体列表 代码字体"); !found {
		t.Fatal("control in search results could not be opened")
	}
	tt.Key(0, native.KeyEscape)
	if err := tt.Click("设置分类 外观"); err != nil {
		t.Fatal(err)
	}
	if a.settingsQuery != "" || tt.HasText("搜索结果") || !tt.HasText("明暗模式") {
		t.Fatal("category selection did not leave search")
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
