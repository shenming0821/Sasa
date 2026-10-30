package claude

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/shenming0821/Fuyu/internal/agent"
)

type Client struct {
	mu                         sync.Mutex
	executable, home, stateDir string
	sessions                   map[string]*session
	executions                 map[*execution]bool
	closed                     bool
	failed                     bool
	turns                      agent.TurnMap
	queue                      eventQueue
	catalogs                   map[string]*catalogEntry
	catalogContext             context.Context
	cancelCatalogs             context.CancelFunc
	catalogWorkers             sync.WaitGroup
}

type session struct {
	ID             string                  `json:"id"`
	Cwd            string                  `json:"cwd"`
	Title          string                  `json:"title"`
	Model          string                  `json:"model"`
	EffectiveModel string                  `json:"effectiveModel,omitempty"`
	Version        string                  `json:"version,omitempty"`
	Turns          []checkpoint            `json:"turns"`
	Generation     agent.ProcessGeneration `json:"generation"`
	Digest         string                  `json:"digest"`
	run            *execution
	idle           *execution
	retiring       *execution
}

type checkpoint struct {
	ID       string `json:"id"`
	UserUUID string `json:"userUuid"`
	Status   string `json:"status"`
	LastUUID string `json:"lastUuid"`
	Command  string `json:"command,omitempty"`
}

func ResolveExecutable() (string, error) {
	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, path := range []string{filepath.Join(home, ".local/bin/claude"), "/opt/homebrew/bin/claude", "/usr/local/bin/claude"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("未找到 Claude Code，请安装 Claude Code CLI 后重新启动 Fuyu")
}

func New(executable, home, stateDir string) (*Client, error) {
	var err error
	if home == "" {
		home = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		home = filepath.Join(userHome, ".claude")
	}
	home, err = canonical(home)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, err
	}
	c := &Client{executable: executable, home: home, stateDir: stateDir, sessions: make(map[string]*session), executions: make(map[*execution]bool)}
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(stateDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var s session
		if len(raw) > 4<<20 || json.Unmarshal(raw, &s) != nil || !validID(s.ID) || s.Cwd == "" {
			return nil, fmt.Errorf("Claude 会话检查点损坏：%s", entry.Name())
		}
		c.sessions[s.ID] = &s
	}
	c.catalogContext, c.cancelCatalogs = context.WithCancel(context.Background())
	c.queue.start()
	return c, nil
}

func (c *Client) Home() string { return c.home }

func (c *Client) Capabilities() agent.Capabilities {
	return agent.Capabilities{Models: true, Commands: true, Images: true, Approval: true, History: true, Resume: true, Reconcile: true}
}

func canonical(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

func uuid() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	data[6], data[8] = data[6]&15|64, data[8]&63|128
	return fmt.Sprintf("%x-%x-%x-%x-%x", data[:4], data[4:6], data[6:8], data[8:10], data[10:]), nil
}

func validID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for index, character := range id {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func (c *Client) save(s *session) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(c.stateDir, ".checkpoint-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(c.stateDir, s.ID+".json"))
}

func (c *Client) StartThread(ctx context.Context, cwd string) (agent.Thread, error) {
	var thread agent.Thread
	if err := ctx.Err(); err != nil {
		return thread, err
	}
	path, err := canonical(cwd)
	if err != nil {
		return thread, err
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return thread, fmt.Errorf("Claude 工作目录不可用：%s", path)
	}
	id, err := uuid()
	if err != nil {
		return thread, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.failed {
		return thread, fmt.Errorf("Claude 连接已关闭")
	}
	s := &session{ID: id, Cwd: path, Model: "default"}
	if err := c.save(s); err != nil {
		return thread, err
	}
	c.sessions[id] = s
	thread.Thread.ID, thread.Cwd, thread.Model = id, path, "default"
	thread.ApprovalPolicy, thread.Sandbox = json.RawMessage(`"default"`), json.RawMessage(`{"type":"claudePermissions"}`)
	return thread, nil
}

func (c *Client) Events() <-chan agent.Event { return c.queue.events }

func (c *Client) failQueue() {
	c.mu.Lock()
	if c.failed {
		c.mu.Unlock()
		return
	}
	c.failed = true
	var running []*execution
	for run := range c.executions {
		running = append(running, run)
	}
	c.mu.Unlock()
	for _, run := range running {
		run.stopProcess()
	}
}

func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.cancelCatalogs()
	for _, s := range c.sessions {
		if s.idle != nil && s.idle.idleTimer != nil {
			s.idle.idleTimer.Stop()
		}
		s.idle = nil
	}
	var running []*execution
	for run := range c.executions {
		running = append(running, run)
	}
	c.mu.Unlock()
	for _, run := range running {
		run.stopProcess()
	}
	c.catalogWorkers.Wait()
	c.queue.close()
}
