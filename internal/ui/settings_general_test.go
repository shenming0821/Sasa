package ui

import (
	"fmt"
	"testing"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Fuyu/internal/power"
)

func TestGeneralKeepAwakeSettings(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{640, 480}, {1000, 760}} {
			t.Run(fmt.Sprintf("%v/%v", dark, size), func(t *testing.T) {
				app := New(stubBackend{}, "test", "/workspace", nil)
				store := imageStore(t, app)
				app.openSettings()
				tester := native.NewTester(app.view, size[0], size[1])
				tester.SetDark(dark)
				if app.settingsSection != "通用" || app.preferences.KeepAwake != power.Never {
					t.Fatal("general defaults")
				}
				for _, choice := range awakeChoices {
					current := "永不"
					for _, candidate := range awakeChoices {
						if candidate.mode == app.preferences.KeepAwake {
							current = candidate.label
						}
					}
					if err := tester.Click("保持唤醒 " + current); err != nil {
						t.Fatal(err)
					}
					bounds, found := tester.Find("保持唤醒选项 " + choice.label)
					if !found || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > float32(size[0]) || bounds.Y+bounds.H > float32(size[1]) {
						t.Fatal("option clipped", choice, bounds)
					}
					if err := tester.Click("保持唤醒选项 " + choice.label); err != nil {
						t.Fatal(err)
					}
					if app.preferences.KeepAwake != choice.mode || app.awakePicker {
						t.Fatal("choice not applied")
					}
					restored := New(stubBackend{}, "test", "/workspace", nil)
					if err := restored.AttachStore(store); err != nil {
						t.Fatal(err)
					}
					if restored.preferences.KeepAwake != choice.mode {
						t.Fatal("choice not persisted")
					}
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				app.preferences.KeepAwake = power.Always
				previous := app.preferences
				app.preferences.KeepAwake = power.Never
				app.savePreferences(previous)
				if app.preferences != previous || app.settingsError == "" {
					t.Fatal("save failure did not restore previous choice")
				}
			})
		}
	}
}
