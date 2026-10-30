package claude

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

const sessionIdleTimeout = 2 * time.Minute
const sessionIdleLimit = 4

type nextExecution struct {
	run   *execution
	ready chan struct{}
}

// One reader and writer belong to the process; every turn gets a new execution
// so old approvals, interrupt timers and projections keep their original owner.
type sessionProcess struct {
	next          chan nextExecution
	model, config string
}

func (r *execution) expireIdle() {
	c := r.client
	c.mu.Lock()
	if r.session.idle != r {
		c.mu.Unlock()
		return
	}
	r.session.idle = nil
	r.session.retiring = r
	if r.idleTimer != nil {
		r.idleTimer.Stop()
	}
	c.mu.Unlock()
	r.stopProcess()
}

func (c *Client) transcriptProjection(id string) (string, error) {
	raw, err := c.transcript(id)
	if err != nil {
		return "", err
	}
	chain, err := conversation(raw, id)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(chain)
	if err != nil {
		return "", err
	}
	return digest(encoded), nil
}

// The CLI can append bookkeeping when an idle process closes. Accept that
// change only when the complete conversation projection remains identical.
func (r *execution) refreshIdleDigest() {
	c := r.client
	c.mu.Lock()
	defer c.mu.Unlock()
	s := r.session
	if len(s.Turns) == 0 || s.Turns[len(s.Turns)-1].ID != string(r.identity.ID) || s.Turns[len(s.Turns)-1].Status != "completed" || r.projectionDigest == "" {
		return
	}
	projection, err := c.transcriptProjection(s.ID)
	if err != nil || projection != r.projectionDigest {
		return
	}
	if current, err := c.transcriptDigest(s.ID); err == nil && current != s.Digest {
		s.Digest = current
		_ = c.save(s)
	}
}

// Called with the client lock; active turns never count toward the idle limit.
func (c *Client) trimIdleProcesses() {
	var idle []*execution
	for _, s := range c.sessions {
		if s.idle != nil {
			idle = append(idle, s.idle)
		}
	}
	if len(idle) <= sessionIdleLimit {
		return
	}
	oldest := idle[0]
	for _, run := range idle[1:] {
		if run.idleSince.Before(oldest.idleSince) {
			oldest = run
		}
	}
	oldest.session.idle = nil
	oldest.session.retiring = oldest
	oldest.idleTimer.Stop()
	go oldest.stopProcess()
}

func (r *execution) continueWith(ctx context.Context, identity agent.TurnIdentity) (*execution, error) {
	next := &execution{
		client: r.client, session: r.session, identity: identity,
		process: r.process, done: r.done, writeMu: r.writeMu, controls: r.controls,
		diagnostics: r.diagnostics, connection: r.connection, turnDone: make(chan struct{}),
		pending: make(map[string]permission), resolved: make(map[string]bool),
		blocks: make(map[int]*contentBlock), tools: make(map[string]map[string]any),
		seen: make(map[string]bool), background: make(map[string]bool),
	}
	request := nextExecution{run: next, ready: make(chan struct{})}
	select {
	case r.connection.next <- request:
	case <-ctx.Done():
		r.stopProcess()
		return nil, ctx.Err()
	case <-r.done:
		// The idle process exited before any user frame was written.
		return nil, nil
	}
	select {
	case <-request.ready:
		return next, nil
	case <-r.done:
		return nil, fmt.Errorf("Claude 空闲进程在准备续发时退出，本次未发送")
	}
}

func (c *Client) processConfiguration(cwd string) string {
	hash := sha256.New()
	for _, path := range []string{c.executable, filepath.Join(c.home, "settings.json"), filepath.Join(c.home, "settings.local.json"), filepath.Join(c.home, ".credentials.json"), filepath.Join(c.home, "CLAUDE.md")} {
		writeConfigurationStamp(hash, path)
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		for _, name := range []string{"settings.json", "settings.local.json"} {
			writeConfigurationStamp(hash, filepath.Join(dir, ".claude", name))
		}
		for _, name := range []string{"CLAUDE.md", "CLAUDE.local.md", ".mcp.json", filepath.Join(".claude", "CLAUDE.md")} {
			writeConfigurationStamp(hash, filepath.Join(dir, name))
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "CLAUDE_") || strings.HasPrefix(value, "ANTHROPIC_") {
			fmt.Fprintln(hash, value)
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func writeConfigurationStamp(hash interface{ Write([]byte) (int, error) }, path string) {
	if info, err := os.Stat(path); err == nil {
		fmt.Fprintf(hash, "%s:%d:%d\n", path, info.Size(), info.ModTime().UnixNano())
	} else {
		fmt.Fprintf(hash, "%s:%v\n", path, err)
	}
}
