//go:build darwin || linux

package runtime

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProcessHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-1] != "fuyu-process-helper" {
		return
	}
	child := exec.Command("sleep", "60")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	fmt.Println(child.Process.Pid)
	// Deliberately ignore stdin EOF to exercise Close's forced group cleanup.
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestForcedCloseAndIndependentGroupBoundary(t *testing.T) {
	p, err := Start(os.Args[0], []string{"-test.run=^TestProcessHelper$", "fuyu-process-helper"}, t.TempDir(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(p.Stdout)
	pids := make(chan string, 1)
	go func() {
		line, _ := reader.ReadString('\n')
		pids <- line
	}()
	var line string
	select {
	case line = <-pids:
	case <-time.After(5 * time.Second):
		killGroup(p.Cmd)
		p.Cmd.Wait()
		t.Fatal("helper did not report child PID")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 1 {
		killGroup(p.Cmd)
		p.Cmd.Wait()
		t.Fatalf("invalid child PID: %q", line)
	}
	// This exact child was created by our helper; never signal shared groups.
	defer syscall.Kill(pid, syscall.SIGKILL)
	waited := make(chan struct{})
	go func() {
		io.Copy(io.Discard, reader)
		p.Wait()
		close(waited)
	}()
	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		killGroup(p.Cmd)
		t.Fatal("forced Close hung")
	}
	<-waited
	if err := syscall.Kill(p.Cmd.Process.Pid, 0); err != syscall.ESRCH {
		t.Fatalf("owned leader still exists: %v", err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("independent child unexpectedly exited: %v", err)
	}
	t.Logf("owned leader %d reaped; independent child %d survives group cleanup and is explicitly cleaned by test", p.Cmd.Process.Pid, pid)
}
