package power

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

type fakeBackend struct {
	state             State
	readErr, writeErr error
	writes            []State
}

func (driver *fakeBackend) read() (State, error) { return driver.state, driver.readErr }

func (driver *fakeBackend) write(_ context.Context, target State) error {
	if driver.writeErr != nil {
		return driver.writeErr
	}
	driver.writes = append(driver.writes, target)
	driver.state.AC, driver.state.Battery = target.AC, target.Battery
	return nil
}

const sampleCustom = `Battery Power:
 Sleep On Power Button 1
 standby              1
 displaysleep         0
 sleep                5
AC Power:
 Sleep On Power Button 1
 displaysleep         10
 sleep                0 (sleep prevented by powerd)
`

func TestParseCustom(t *testing.T) {
	state, err := parseCustom(sampleCustom)
	if err != nil {
		t.Fatal(err)
	}
	if state.AC != 0 || state.Battery != 5 || !state.HasBattery {
		t.Fatalf("%+v", state)
	}
	desktop := `AC Power:
 sleep                10
`
	state, err = parseCustom(desktop)
	if err != nil || state.AC != 10 || state.HasBattery {
		t.Fatalf("%+v %v", state, err)
	}
	if _, err = parseCustom("garbage"); err == nil {
		t.Fatal("accepted output without AC profile")
	}
}

func TestModeOf(t *testing.T) {
	for _, step := range []struct {
		state State
		mode  string
	}{
		{State{AC: 0, Battery: 0, HasBattery: true}, Always},
		{State{AC: 0, Battery: 5, HasBattery: true}, ACOnly},
		{State{AC: 0, Battery: 30, HasBattery: true}, ACOnly},
		{State{AC: 10, Battery: 0, HasBattery: true}, Never},
		{State{AC: 10, Battery: 5, HasBattery: true}, Never},
		{State{AC: 0, HasBattery: false}, Always},
		{State{AC: 10, HasBattery: false}, Never},
	} {
		if mode := modeOf(step.state); mode != step.mode {
			t.Fatalf("%+v: got %s want %s", step.state, mode, step.mode)
		}
	}
}

func TestSetRecordsBaselineAndWrites(t *testing.T) {
	driver := &fakeBackend{state: State{AC: 10, Battery: 5, HasBattery: true}}
	controller := newController(driver)
	defer controller.Close()
	if err := controller.Set(ACOnly); err != nil {
		t.Fatal(err)
	}
	baseline, ok := controller.PendingBaseline()
	if !ok || baseline != (Baseline{AC: 10, Battery: 5}) {
		t.Fatal("baseline not recorded", baseline, ok)
	}
	controller.ConfirmBaseline()
	if _, ok = controller.PendingBaseline(); ok {
		t.Fatal("baseline consumed twice")
	}
	if len(driver.writes) != 1 || driver.writes[0] != (State{AC: 0, Battery: 5, HasBattery: true}) {
		t.Fatalf("%+v", driver.writes)
	}
	if err := controller.Set(Always); err != nil {
		t.Fatal(err)
	}
	if len(driver.writes) != 2 || driver.writes[1].Battery != 0 {
		t.Fatalf("%+v", driver.writes)
	}
	if err := controller.Set(Never); err != nil {
		t.Fatal(err)
	}
	if last := driver.writes[len(driver.writes)-1]; last.AC != 10 || last.Battery != 5 {
		t.Fatal("never did not restore baseline", last)
	}
}

func TestSetSkipsUnchangedProfile(t *testing.T) {
	driver := &fakeBackend{state: State{AC: 0, Battery: 5, HasBattery: true}}
	controller := newController(driver)
	defer controller.Close()
	if err := controller.Set(ACOnly); err != nil {
		t.Fatal(err)
	}
	if len(driver.writes) != 0 {
		t.Fatal("wrote unchanged profile", driver.writes)
	}
	if controller.mode != ACOnly {
		t.Fatal("mode not updated")
	}
}

func TestModesWithZeroSleepBaseline(t *testing.T) {
	for _, initial := range []State{
		{AC: 0, Battery: 5, HasBattery: true},
		{AC: 0, Battery: 0, HasBattery: true},
		{AC: 12, Battery: 0, HasBattery: true},
	} {
		t.Run(fmt.Sprintf("%d/%d", initial.AC, initial.Battery), func(t *testing.T) {
			driver := &fakeBackend{state: initial}
			controller := newController(driver)
			defer controller.Close()
			for _, want := range []string{Always, Never, ACOnly, Never} {
				if err := controller.Set(want); err != nil {
					t.Fatal(err)
				}
				if got, err := controller.Sync(); err != nil || got != want {
					t.Fatalf("selected=%s readback=%s state=%+v err=%v", want, got, driver.state, err)
				}
				if want == Never && (driver.state.AC <= 0 || driver.state.Battery <= 0) {
					t.Fatal("never must allow idle sleep on both power sources", driver.state)
				}
				if want != Always && initial.Battery > 0 && driver.state.Battery != initial.Battery {
					t.Fatal("positive battery baseline changed", driver.state)
				}
				if want == Never && initial.AC > 0 && driver.state.AC != initial.AC {
					t.Fatal("positive AC baseline changed", driver.state)
				}
			}
		})
	}
}

func TestSetFailureKeepsMode(t *testing.T) {
	driver := &fakeBackend{state: State{AC: 10, Battery: 5, HasBattery: true}}
	controller := newController(driver)
	defer controller.Close()
	driver.writeErr = errors.New("denied")
	if err := controller.Set(Always); err == nil || controller.Err() == nil {
		t.Fatal("write failure hidden")
	}
	if controller.mode != Never || driver.state.AC != 10 {
		t.Fatal("mode or system mutated on failure")
	}
	driver.writeErr = nil
	if err := controller.Set(Always); err != nil {
		t.Fatal(err)
	}
	if controller.Err() != nil || controller.mode != Always {
		t.Fatal("retry did not recover")
	}
	driver.readErr = errors.New("pmset missing")
	if err := controller.Set(Never); err == nil {
		t.Fatal("missing read failure")
	}
	if controller.mode != Always {
		t.Fatal("mode mutated on read failure")
	}
}

func TestSyncMapsSystemState(t *testing.T) {
	driver := &fakeBackend{state: State{AC: 0, Battery: 30, HasBattery: true}}
	controller := newController(driver)
	defer controller.Close()
	mode, err := controller.Sync()
	if err != nil || mode != ACOnly {
		t.Fatalf("%s %v", mode, err)
	}
	driver.state = State{AC: 0, Battery: 0, HasBattery: true}
	if mode, err = controller.Sync(); err != nil || mode != Always {
		t.Fatalf("%s %v", mode, err)
	}
	driver.readErr = errors.New("unavailable")
	if mode, err = controller.Sync(); err == nil || mode != Always {
		t.Fatal("read failure changed mode")
	}
}

func TestRestoredBaselineDrivesNever(t *testing.T) {
	driver := &fakeBackend{state: State{AC: 0, Battery: 0, HasBattery: true}}
	controller := newController(driver)
	defer controller.Close()
	controller.RestoreBaseline(Baseline{AC: 12, Battery: 3})
	if err := controller.Set(Never); err != nil {
		t.Fatal(err)
	}
	if last := driver.writes[len(driver.writes)-1]; last.AC != 12 || last.Battery != 3 {
		t.Fatal("restored baseline ignored", last)
	}
	if _, ok := controller.PendingBaseline(); ok {
		t.Fatal("restored baseline reported as new")
	}
	controller.RestoreBaseline(Baseline{AC: 1, Battery: 1})
	if err := controller.Set(Never); err != nil {
		t.Fatal(err)
	}
	if driver.writes[len(driver.writes)-1].AC != 12 {
		t.Fatal("second restore overwrote baseline")
	}
}

func TestBaselineRemainsPendingUntilPersistenceConfirmed(t *testing.T) {
	driver := &fakeBackend{state: State{AC: 12, Battery: 3, HasBattery: true}}
	controller := newController(driver)
	defer controller.Close()
	for _, mode := range []string{Always, Never, Always} {
		if err := controller.Set(mode); err != nil {
			t.Fatal(err)
		}
		// Reading for a failed save, rolling back, then retrying must all
		// expose the original values until the save is acknowledged.
		for range 2 {
			baseline, pending := controller.PendingBaseline()
			if !pending || baseline != (Baseline{AC: 12, Battery: 3}) {
				t.Fatal("baseline lost before persistence confirmation", baseline, pending)
			}
		}
	}
	controller.ConfirmBaseline()
	if _, pending := controller.PendingBaseline(); pending {
		t.Fatal("persisted baseline still pending")
	}
}

func TestRollbackRestoresExactZeroTimers(t *testing.T) {
	original := State{AC: 12, Battery: 0, HasBattery: true}
	driver := &fakeBackend{state: original}
	controller := newController(driver)
	defer controller.Close()
	state, err := controller.CurrentState()
	if err != nil {
		t.Fatal(err)
	}
	if err = controller.Set(ACOnly); err != nil {
		t.Fatal(err)
	}
	if err = controller.RestoreState(state); err != nil || driver.state != original {
		t.Fatal("rollback changed the original timer", driver.state, err)
	}
}

type waitingBackend struct {
	fakeBackend
	started chan struct{}
}

func (driver *waitingBackend) write(ctx context.Context, _ State) error {
	close(driver.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestAuthorizationDoesNotBlockStatusAndCloseCancels(t *testing.T) {
	driver := &waitingBackend{
		fakeBackend: fakeBackend{state: State{AC: 12, Battery: 3, HasBattery: true}},
		started:     make(chan struct{}),
	}
	controller := newController(driver)
	defer controller.Close()
	done := make(chan error, 1)
	go func() { done <- controller.Set(Always) }()
	<-driver.started
	status := make(chan error, 1)
	go func() { status <- controller.Err() }()
	select {
	case err := <-status:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("status read blocked behind administrator authorization")
	}
	controller.Close()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("close did not cancel authorization", err)
		}
	case <-time.After(time.Second):
		t.Fatal("authorization worker did not exit")
	}
}

func TestConcurrentUpdatesAndClose(t *testing.T) {
	controller := newController(&fakeBackend{state: State{AC: 10, Battery: 5, HasBattery: true}})
	var workers sync.WaitGroup
	for range 10 {
		workers.Go(func() {
			for range 20 {
				controller.Set(Always)
				controller.Set(ACOnly)
				controller.Sync()
				controller.Err()
			}
		})
	}
	workers.Go(func() { controller.Close() })
	workers.Wait()
	controller.Close()
	if err := controller.Set(Always); err == nil {
		t.Fatal("closed controller accepted update")
	}
}

func TestLiveRead(t *testing.T) {
	if os.Getenv("FUYU_LIVE_POWER") != "1" {
		t.Skip("requires explicit native power verification")
	}
	controller := New()
	defer controller.Close()
	mode, err := controller.Sync()
	if err != nil {
		t.Fatal(err)
	}
	state, err := controller.backend.read()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("system sleep: ac=%d battery=%d hasBattery=%v mode=%s", state.AC, state.Battery, state.HasBattery, mode)
}
