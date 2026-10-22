package runtime

import (
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"
)

// Process owns only the child and process group it started.
type Process struct {
	Cmd    *exec.Cmd
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

func Start(executable string, args []string, cwd string, stderr io.Writer) (*Process, error) {
	cmd := exec.Command(executable, args...)
	cmd.Dir, cmd.Stderr = cwd, stderr
	configureGroup(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	return &Process{Cmd: cmd, Stdin: in, Stdout: out, done: make(chan struct{})}, nil
}

// Wait is called by the protocol reader after stdout reaches EOF.
func (p *Process) Wait() error {
	// Reap only after clearing the group, while its leader PID cannot be reused.
	killGroup(p.Cmd)
	err := p.Cmd.Wait()
	p.mu.Lock()
	p.err = err
	p.mu.Unlock()
	close(p.done)
	return err
}

func (p *Process) Close() error {
	p.Stdin.Close()
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		killGroup(p.Cmd)
		<-p.done
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var exit *exec.ExitError
	if errors.As(p.err, &exit) {
		return nil
	}
	return p.err
}
