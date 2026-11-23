package ui

import (
	"errors"
	"fmt"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"github.com/shenming0821/Sasa/internal/power"
	"github.com/shenming0821/Sasa/internal/storage"
)

type controlledPowerSettings struct {
	started   chan string
	release   chan error
	pending   bool
	confirmed bool
}

func (p *controlledPowerSettings) Set(mode string) error {
	p.started <- mode
	return <-p.release
}
func (p *controlledPowerSettings) Sync() (string, error) { return power.Never, nil }
func (p *controlledPowerSettings) Err() error            { return nil }
func (p *controlledPowerSettings) CurrentState() (power.State, error) {
	return power.State{AC: 12, Battery: 3, HasBattery: true}, nil
}
func (p *controlledPowerSettings) RestoreState(state power.State) error {
	if state != (power.State{AC: 12, Battery: 3, HasBattery: true}) {
		return fmt.Errorf("rollback used a different state: %+v", state)
	}
	return p.Set(power.Never)
}
func (p *controlledPowerSettings) PendingBaseline() (power.Baseline, bool) {
	return power.Baseline{AC: 12, Battery: 3}, p.pending
}
func (p *controlledPowerSettings) ConfirmBaseline() {
	p.pending, p.confirmed = false, true
}
func (p *controlledPowerSettings) RestoreBaseline(power.Baseline) {}
func (p *controlledPowerSettings) Close() error                   { return nil }

func waitPowerCall(t *testing.T, p *controlledPowerSettings, want string) {
	t.Helper()
	select {
	case got := <-p.started:
		if got != want {
			t.Fatalf("power mode=%s want=%s", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("power operation did not start")
	}
}

func powerSettingsApp(t *testing.T) (*App, *controlledPowerSettings, chan func(), *storage.Store) {
	t.Helper()
	a := New(stubBackend{}, "test", "/workspace", nil)
	store := imageStore(t, a)
	queue := make(chan func(), 8)
	a.dispatch = func(fn func()) { queue <- fn }
	p := &controlledPowerSettings{started: make(chan string, 4), release: make(chan error, 4), pending: true}
	a.powerController = p
	return a, p, queue, store
}

func TestKeepAwakeAuthorizationKeepsUIResponsive(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprint(denied), func(t *testing.T) {
			a, p, queue, store := powerSettingsApp(t)
			a.openSettings()
			tester := native.NewTester(a.view, 1000, 760)
			if err := tester.Click("设置分类 通用"); err != nil {
				t.Fatal(err)
			}
			if err := tester.Click("保持唤醒 永不"); err != nil {
				t.Fatal(err)
			}
			if err := tester.Click("保持唤醒选项 始终"); err != nil {
				t.Fatal(err)
			}
			waitPowerCall(t, p, power.Always)
			if !a.awakeSaving || a.preferences.KeepAwake != power.Never {
				t.Fatal("pending authorization presented as committed", a.preferences)
			}
			// Rendering, reopening settings and editing another preference must
			// remain usable while the backend waits for authorization.
			tester.Frame()
			a.openSettings()
			previous := a.preferences
			a.preferences.Theme = "dark"
			a.savePreferences(previous)
			a.saveKeepAwake(power.ACOnly)
			select {
			case mode := <-p.started:
				t.Fatal("duplicate authorization", mode)
			default:
			}
			if denied {
				p.release <- errors.New("授权已取消")
			} else {
				p.release <- nil
			}
			pumpUpdate(t, queue)
			if a.awakeSaving || a.preferences.Theme != "dark" {
				t.Fatal("completion lost unrelated edit or stayed busy")
			}
			snapshot, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			if denied {
				if a.preferences.KeepAwake != power.Never || p.confirmed || a.settingsError == "" {
					t.Fatal("cancel did not retain original selection and pending baseline")
				}
			} else if snapshot.Preferences.KeepAwake != power.Always ||
				snapshot.Preferences.KeepAwakeBaseline == nil ||
				snapshot.Preferences.KeepAwakeBaseline.AC != 12 || !p.confirmed {
				t.Fatal("successful authorization did not persist mode and baseline", snapshot.Preferences)
			}
		})
	}
}

func TestKeepAwakeSaveFailureRetryPersistsBaseline(t *testing.T) {
	a, p, queue, store := powerSettingsApp(t)
	dir := store.Directory()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	a.saveKeepAwake(power.Always)
	waitPowerCall(t, p, power.Always)
	p.release <- nil
	pumpUpdate(t, queue)
	waitPowerCall(t, p, power.Never)
	if !p.pending || p.confirmed || a.preferences.KeepAwakeBaseline != nil {
		t.Fatal("failed persistence consumed the baseline")
	}
	p.release <- nil
	pumpUpdate(t, queue)
	if a.awakeSaving || a.settingsError == "" {
		t.Fatal("failed save did not finish rollback")
	}
	reopened, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	a.store = reopened
	a.saveKeepAwake(power.Always)
	waitPowerCall(t, p, power.Always)
	p.release <- nil
	pumpUpdate(t, queue)
	snapshot, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Preferences.KeepAwakeBaseline == nil ||
		*snapshot.Preferences.KeepAwakeBaseline != (storage.SleepBaseline{AC: 12, Battery: 3}) ||
		!p.confirmed {
		t.Fatal("retry lost the baseline", snapshot.Preferences)
	}
}

func TestGeneralKeepAwakeSettings(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, size := range [][2]int{{640, 480}, {1000, 760}} {
			t.Run(fmt.Sprintf("%v/%v", dark, size), func(t *testing.T) {
				app := New(stubBackend{}, "test", "/workspace", nil)
				store := imageStore(t, app)
				app.openSettings()
				tester := native.NewTester(app.view, size[0], size[1])
				tester.SetDark(dark)
				if err := tester.Click("设置分类 通用"); err != nil {
					t.Fatal(err)
				}
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
