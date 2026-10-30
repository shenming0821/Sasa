package power

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

type fakeBackend struct {
	connected                        bool
	active                           bool
	acquires, releases               int
	queryErr, acquireErr, releaseErr error
}

func (driver *fakeBackend) onAC() (bool, error) { return driver.connected, driver.queryErr }
func (driver *fakeBackend) acquire() error {
	if driver.acquireErr != nil {
		return driver.acquireErr
	}
	driver.acquires++
	driver.active = true
	return nil
}
func (driver *fakeBackend) release() error {
	if driver.releaseErr != nil {
		return driver.releaseErr
	}
	driver.releases++
	driver.active = false
	return nil
}

func TestModesPowerTransitionsAndCleanup(t *testing.T) {
	driver := &fakeBackend{}
	controller := newController(driver)
	t.Cleanup(func() { controller.Close() })
	for _, step := range []struct {
		mode              string
		connected, active bool
	}{
		{Never, false, false}, {Always, false, true}, {Always, true, true},
		{ACOnly, true, true}, {ACOnly, false, false}, {ACOnly, true, true},
		{Never, true, false}, {"unknown", true, false}, {Always, false, true},
	} {
		driver.connected = step.connected
		if err := controller.Set(step.mode); err != nil {
			t.Fatal(err)
		}
		if driver.active != step.active {
			t.Fatalf("%+v: active=%v", step, driver.active)
		}
	}
	if driver.acquires != 3 || driver.releases != 2 {
		t.Fatal("duplicate or missing assertion", driver)
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	if driver.active || driver.releases != 3 {
		t.Fatal("assertion leaked")
	}
	if err := controller.Set(Always); err == nil {
		t.Fatal("closed controller accepted update")
	}
}

func TestACRefreshAndFailures(t *testing.T) {
	driver := &fakeBackend{connected: true}
	controller := newController(driver)
	t.Cleanup(func() { controller.Close() })
	if err := controller.Set(ACOnly); err != nil {
		t.Fatal(err)
	}
	driver.connected = false
	controller.refresh()
	if driver.active {
		t.Fatal("battery kept awake")
	}
	driver.acquireErr = errors.New("acquire failed")
	driver.connected = true
	controller.refresh()
	if controller.Err() == nil || driver.active {
		t.Fatal("acquire failure hidden")
	}
	driver.acquireErr = nil
	controller.refresh()
	if controller.Err() != nil || !driver.active {
		t.Fatal("retry failed")
	}
	driver.queryErr = errors.New("unknown power source")
	controller.refresh()
	if driver.active || controller.Err() == nil {
		t.Fatal("unknown power retained assertion")
	}
	driver.queryErr = nil
	if err := controller.Set(Always); err != nil {
		t.Fatal(err)
	}
	driver.queryErr = errors.New("query failed")
	if err := controller.Set(ACOnly); err == nil {
		t.Fatal("missing query failure")
	}
	if controller.mode != Always || !driver.active {
		t.Fatal("previous policy not restored")
	}
	driver.releaseErr = errors.New("release failed")
	if err := controller.Set(Never); err == nil || !driver.active {
		t.Fatal("release failure hidden")
	}
	driver.releaseErr = nil
}

func TestConcurrentUpdatesAndClose(t *testing.T) {
	controller := newController(&fakeBackend{connected: true})
	var workers sync.WaitGroup
	for range 10 {
		workers.Go(func() {
			for range 20 {
				controller.Set(Always)
				controller.Set(ACOnly)
				controller.Err()
			}
		})
	}
	workers.Go(func() { controller.Close() })
	workers.Wait()
	controller.Close()
}

func TestLiveMacAssertion(t *testing.T) {
	if os.Getenv("FUYU_LIVE_POWER") != "1" {
		t.Skip("requires explicit native power verification")
	}
	controller := New()
	t.Cleanup(func() { controller.Close() })
	driver := controller.backend.(*systemBackend)
	connected, err := driver.onAC()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("AC connected: %v", connected)
	for _, mode := range []string{Always, ACOnly, Never} {
		if err := controller.Set(mode); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command("/usr/bin/pmset", "-g", "assertions").Output()
		if err != nil {
			t.Fatal(err)
		}
		found := strings.Contains(string(output), "Fuyu: keep Mac awake")
		want := mode == Always || mode == ACOnly && connected
		if found != want {
			t.Fatalf("mode %s: assertion=%v, want=%v", mode, found, want)
		}
		t.Logf("mode=%s: native assertion=%v", mode, found)
	}
	if err := controller.Set(Always); err != nil {
		t.Fatal(err)
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("/usr/bin/pmset", "-g", "assertions").Output()
	if err != nil || strings.Contains(string(output), "Fuyu: keep Mac awake") {
		t.Fatal("native assertion survived close", err)
	}
}
