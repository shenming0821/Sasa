//go:build darwin && cgo && sasa_terminal_probe

package terminal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
)

func init() { runtime.LockOSThread() }

func TestMain(tests *testing.M) {
	if os.Getenv("SASA_TERMINAL_HELPER") == "1" {
		if err := nativeProbe(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	os.Exit(tests.Run())
}

func TestLiveNativeMetalTerminal(t *testing.T) {
	if os.Getenv("SASA_LIVE_TERMINAL") != "1" {
		t.Skip("set SASA_LIVE_TERMINAL=1 after building the native library")
	}
	root := t.TempDir()
	contents := filepath.Join(root, "Probe.app", "Contents")
	executable := filepath.Join(contents, "MacOS", "terminal.test")
	for _, dir := range []string{filepath.Dir(executable), filepath.Join(contents, "Frameworks"), filepath.Join(contents, "Resources"), filepath.Join(root, "config", "ghostty"), filepath.Join(root, "home")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, data, 0700); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleExecutable</key><string>terminal.test</string><key>CFBundleIdentifier</key><string>com.shenming.sasa.terminal-probe</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0600); err != nil {
		t.Fatal(err)
	}
	cache, err := filepath.Abs("../../.cache/ghostty-native")
	if err != nil {
		t.Fatal(err)
	}
	for target, source := range map[string]string{
		filepath.Join(contents, "Frameworks", "libghostty.dylib"): filepath.Join(cache, "libghostty.dylib"),
		filepath.Join(contents, "Resources", "ghostty"):           filepath.Join(cache, "install", "share", "ghostty"),
		filepath.Join(contents, "Resources", "terminfo"):          filepath.Join(cache, "install", "share", "terminfo"),
	} {
		if err := os.Symlink(source, target); err != nil {
			t.Fatal(err)
		}
	}
	config := "command = /bin/sh\nfont-family = Menlo\nfont-size = 13\ntheme = Gitlab Light\nshell-integration = none\nconfig-file = extra.conf\n"
	if err := os.WriteFile(filepath.Join(root, "config", "ghostty", "config"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "ghostty", "extra.conf"), []byte("cursor-style = bar\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "HOME=") && !strings.HasPrefix(entry, "XDG_CONFIG_HOME=") && !strings.HasPrefix(entry, "SASA_TERMINAL_HELPER=") && !strings.HasPrefix(entry, "NO_COLOR=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "HOME="+filepath.Join(root, "home"), "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "SASA_TERMINAL_HELPER=1", "SASA_TERMINAL_PROBE_ROOT="+root, "NO_COLOR=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("native Metal terminal probe: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "NATIVE_TERMINAL_PASS") {
		t.Fatalf("native probe did not finish: %s", output)
	}
	t.Log(string(output))
}

func nativeProbe() error {
	outcome := make(chan error, 1)
	mygo.App.WhenReady(func() {
		window := mygo.NewWindow(mygo.WindowOptions{Title: "Sasa 原生终端验证", Width: 1000, Height: 760, Content: native.View(func(c *native.Context) { native.Box(c).Fill() })})
		go func() {
			outcome <- exerciseNativeTerminal(window, os.Getenv("SASA_TERMINAL_PROBE_ROOT"))
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		return err
	}
	return <-outcome
}

func probeWait(check func() bool) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return nil
		}
		time.Sleep(30 * time.Millisecond)
	}
	return fmt.Errorf("native terminal observation timed out")
}

func exerciseNativeTerminal(window *mygo.Window, root string) error {
	first, err := Start(root)
	if err != nil {
		return err
	}
	defer first.Close()
	bounds := native.Rect{X: 10, Y: 10, W: 700, H: 400}
	if err := first.Present(window.NativeHandle(), bounds, false, true); err != nil {
		return err
	}
	if !first.probeBackingScale(false) {
		return fmt.Errorf("initial terminal layer scale does not match the display")
	}
	if !first.probeBackingScale(true) {
		return fmt.Errorf("terminal layer scale did not follow backing change without animation")
	}
	first.probeText("pwd > cwd; echo $$ > pid; sleep 60 & echo $! > child; stty size > size; printf '\\033[32mNATIVE_%s_%s\\033[0m\\n' METAL 中文\r")
	if err := probeWait(func() bool {
		return strings.Contains(first.probeScreen(), "NATIVE_METAL_中文") && fileExists(filepath.Join(root, "size"))
	}); err != nil {
		return fmt.Errorf("input/output: %w; screen=%s", err, first.probeScreen())
	}
	cwd, _ := os.ReadFile(filepath.Join(root, "cwd"))
	if strings.TrimSpace(string(cwd)) != root {
		return fmt.Errorf("wrong shell directory: %s", cwd)
	}
	first.probeText("printf '%s' \"${NO_COLOR+set}\" > no-color; printf '%s' \"$COLORTERM\" > colorterm; printf '%s' \"$SASA_TERMINAL\" > sasa-terminal; echo ready > color-env-ready\r")
	if err := probeWait(func() bool { return fileExists(filepath.Join(root, "color-env-ready")) }); err != nil {
		return fmt.Errorf("terminal color environment: %w; screen=%s", err, first.probeScreen())
	}
	noColor, err := os.ReadFile(filepath.Join(root, "no-color"))
	if err != nil || len(noColor) != 0 {
		return fmt.Errorf("terminal should have no NO_COLOR variable (presence=%q): %v", noColor, err)
	}
	colorTerm, err := os.ReadFile(filepath.Join(root, "colorterm"))
	if err != nil || string(colorTerm) != "truecolor" {
		return fmt.Errorf("terminal COLORTERM=%q: %v", colorTerm, err)
	}
	terminalMarker, err := os.ReadFile(filepath.Join(root, "sasa-terminal"))
	if err != nil || string(terminalMarker) != "1" {
		return fmt.Errorf("terminal SASA_TERMINAL=%q: %v", terminalMarker, err)
	}
	if os.Getenv("NO_COLOR") != "1" {
		return fmt.Errorf("terminal creation changed the host NO_COLOR")
	}
	first.probeText("stty raw -echo; printf 'RAW_%s\\n' READY; dd bs=1 count=1 of=key 2>/dev/null; stty sane; printf 'RAW_%s\\n' DONE\r")
	if err := probeWait(func() bool { return strings.Contains(first.probeScreen(), "RAW_READY") }); err != nil {
		return err
	}
	first.probeKey("q", 12)
	if err := probeWait(func() bool { return fileExists(filepath.Join(root, "key")) }); err != nil {
		return fmt.Errorf("native raw keyboard: %w; screen=%s", err, first.probeScreen())
	}
	key, _ := os.ReadFile(filepath.Join(root, "key"))
	if string(key) != "q" {
		return fmt.Errorf("native keyboard encoded %q", key)
	}
	first.probeText("printf '")
	first.probeComposition("中文组合提交")
	first.probeText("\\n' > composition\r")
	if err := probeWait(func() bool { return fileExists(filepath.Join(root, "composition")) }); err != nil {
		return fmt.Errorf("composition text lost: %w", err)
	}
	committed, _ := os.ReadFile(filepath.Join(root, "composition"))
	if strings.TrimSpace(string(committed)) != "中文组合提交" {
		return fmt.Errorf("composition text changed: %q", committed)
	}
	started := time.Now()
	first.probeText("awk 'BEGIN {for (row = 0; row < 20000; row++) print row, \"terminal output\"; print \"BULK_\" \"COMPLETE\"}'\r")
	if err := probeWait(func() bool { return strings.Contains(first.probeScreen(), "BULK_COMPLETE") }); err != nil {
		return fmt.Errorf("large output stalled: %w; screen=%s", err, first.probeScreen())
	}
	bulkDuration := time.Since(started)
	initial, _ := os.ReadFile(filepath.Join(root, "size"))
	first.Hide()
	bounds.W, bounds.H = 900, 600
	if err := first.Present(window.NativeHandle(), bounds, true, true); err != nil {
		return err
	}
	if !first.probeBackingScale(false) {
		return fmt.Errorf("resized terminal layer scale does not match the display")
	}
	var rows, columns, newRows, newColumns int
	fmt.Sscanf(string(initial), "%d %d", &rows, &columns)
	if err := probeWait(func() bool {
		resized, _ := os.ReadFile(filepath.Join(root, "resized"))
		fmt.Sscanf(string(resized), "%d %d", &newRows, &newColumns)
		if newRows > rows && newColumns > columns {
			return true
		}
		first.probeText("stty size > resized\r")
		return false
	}); err != nil {
		return fmt.Errorf("PTY resize did not follow native view: %dx%d -> %dx%d", columns, rows, newColumns, newRows)
	}
	second, err := Start(root)
	if err != nil {
		return err
	}
	defer second.Close()
	first.Hide()
	if err := second.Present(window.NativeHandle(), bounds, false, true); err != nil {
		return err
	}
	second.probeText("echo $$ > second; printf 'SECOND_SURFACE\\n'\r")
	if err := probeWait(func() bool { return fileExists(filepath.Join(root, "second")) }); err != nil {
		return err
	}
	first.Close()
	first.Close()
	for _, name := range []string{"pid", "child"} {
		data, _ := os.ReadFile(filepath.Join(root, name))
		pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if pid <= 0 {
			return fmt.Errorf("missing controlled process id: %s", name)
		}
		if err := probeWait(func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH }); err != nil {
			return fmt.Errorf("native close left %s running: %d", name, pid)
		}
	}
	if second.Exited() || !strings.Contains(second.probeScreen(), "SECOND_SURFACE") {
		return fmt.Errorf("closing first terminal damaged second terminal")
	}
	second.probeText("exit\r")
	if err := probeWait(second.Exited); err != nil {
		return fmt.Errorf("native shell exit not reported: %w", err)
	}
	fmt.Printf("NATIVE_TERMINAL_PASS cwd, Chinese/ANSI terminal state, NO_COLOR absent in shell and preserved in host, COLORTERM=truecolor, raw keyboard input, mutable composition commit, backing scale restoration without animation, native view resize %dx%d -> %dx%d, hidden/visible surfaces, independent shells, process cleanup, shell exit\n", columns, rows, newColumns, newRows)
	fmt.Printf("NATIVE_OUTPUT_SAMPLE 20000 lines observed in %s (local sample, includes shell/awk/polling; not a renderer benchmark)\n", bulkDuration.Round(time.Millisecond))
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}
