package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRestartUsesLaunchServicesAndPreservesChannelEnvironment(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Launch Services is macOS only")
	}
	t.Setenv("PATH", "/usr/bin:/custom/bin")
	t.Setenv("CODEX_HOME", "/data/Sasa Preview/codex")
	t.Setenv("CLAUDE_CONFIG_DIR", "/data/Sasa Preview/claude")
	arguments := []string{"-cwd", "/project with spaces", "-data-dir", "/data/Sasa Preview"}
	for _, application := range []string{"/Applications/Sasa.app", "/build/Sasa Preview.app"} {
		actual, err := replacementArguments(filepath.Join(application, "Contents/MacOS/Sasa"), arguments)
		expected := []string{"/usr/bin/open", "-n", "-a", application,
			"--env", "PATH=/usr/bin:/custom/bin", "--env", "CODEX_HOME=/data/Sasa Preview/codex",
			"--env", "CLAUDE_CONFIG_DIR=/data/Sasa Preview/claude", "--args"}
		expected = append(expected, arguments...)
		if err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("launch=%q err=%v", actual, err)
		}
	}
	if _, err := replacementArguments("/tmp/Sasa", arguments); err == nil {
		t.Fatal("non-bundle executable bypassed Launch Services")
	}
}

func TestRestartWaitsForOldProcessExitAndKeepsArgumentsLiteral(t *testing.T) {
	parent := exec.Command("/bin/sh", "-c", "read request")
	input, err := parent.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close(); parent.Process.Kill(); parent.Wait() })
	output := filepath.Join(t.TempDir(), "launch result")
	values := []string{"/data/space and 中文", "literal $(exit 7); *"}
	launch := []string{"/bin/sh", "-c", `output=$1; shift; printf '%s\n' "$@" > "$output"`, "launcher", output}
	command := waitForExitCommand(parent.Process.Pid, append(launch, values...))
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { command.Process.Kill(); command.Wait() })
	time.Sleep(150 * time.Millisecond)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("replacement launched while the old process still held its data lock")
	}
	input.Close()
	parent.Wait()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("replacement did not launch after parent exit")
	}
	actual, err := os.ReadFile(output)
	if err != nil || string(actual) != strings.Join(values, "\n")+"\n" {
		t.Fatalf("arguments changed: %q, %v", actual, err)
	}
}

func TestRestartLaunchesOnlyAfterConfirmedQuit(t *testing.T) {
	application := New(stubBackend{}, "test", "/workspace", nil)
	quitRequested, launches := false, 0
	application.quitApp = func() { quitRequested = true }
	failure := errors.New("launch failed")
	application.requestRestart(func() error { launches++; return failure })
	if !quitRequested || !application.restarting || launches != 0 {
		t.Fatal("restart bypassed the normal quit path")
	}
	if err := application.finishRestart(); err != nil || launches != 0 {
		t.Fatal("unconfirmed quit launched replacement")
	}
	application.quitting = true
	if err := application.finishRestart(); !errors.Is(err, failure) || launches != 1 {
		t.Fatal("launch failure was lost", err, launches)
	}
	if err := application.finishRestart(); err != nil || launches != 1 {
		t.Fatal("replacement launched twice")
	}
}
