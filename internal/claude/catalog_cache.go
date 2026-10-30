package claude

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
)

type catalogEntry struct {
	done        chan struct{}
	modelsReady chan struct{}
	checked     time.Time
	models      []agent.Model
	commands    []agent.Command
	modelErr    error
	commandErr  error
}

// The client identity already includes executable and configuration root.
// Concurrent readers share a bounded probe; canceling one reader does not
// cancel another session's request.
func (c *Client) directoryCatalog(ctx context.Context, cwd string, force, modelsOnly bool) (*catalogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cwd == "" {
		var err error
		cwd, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	cwd, err := canonical(cwd)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.closed || c.failed {
		c.mu.Unlock()
		return nil, fmt.Errorf("Claude 连接已关闭")
	}
	entry := c.catalogs[cwd]
	loading := entry != nil && entry.checked.IsZero()
	ttl := 10 * time.Minute
	if entry != nil && !loading && (entry.modelErr != nil || entry.commandErr != nil) {
		ttl = 30 * time.Second
	}
	if !loading && (entry == nil || force || time.Since(entry.checked) >= ttl) {
		if c.catalogs == nil {
			c.catalogs = make(map[string]*catalogEntry)
		}
		if entry == nil && len(c.catalogs) >= 32 {
			oldest := ""
			for key, candidate := range c.catalogs {
				if !candidate.checked.IsZero() && (oldest == "" || candidate.checked.Before(c.catalogs[oldest].checked)) {
					oldest = key
				}
			}
			if oldest == "" {
				c.mu.Unlock()
				return nil, fmt.Errorf("Claude 目录读取繁忙，请稍后重试")
			}
			delete(c.catalogs, oldest)
		}
		entry = &catalogEntry{done: make(chan struct{}), modelsReady: make(chan struct{})}
		c.catalogs[cwd] = entry
		c.catalogWorkers.Add(1)
		go c.readDirectoryCatalog(cwd, entry)
	}
	c.mu.Unlock()
	ready := entry.done
	if modelsOnly {
		ready = entry.modelsReady
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ready:
		return entry, nil
	}
}

func (c *Client) readDirectoryCatalog(cwd string, entry *catalogEntry) {
	defer c.catalogWorkers.Done()
	ctx, cancel := context.WithTimeout(c.catalogContext, 30*time.Second)
	defer cancel()
	run, raw, err := c.startCatalog(ctx, cwd)
	var models []agent.Model
	var commands []agent.Command
	modelErr, commandErr := err, err
	if err == nil {
		models, modelErr = catalogModels(raw)
	}
	c.mu.Lock()
	entry.models, entry.modelErr = models, modelErr
	close(entry.modelsReady)
	c.mu.Unlock()
	if err == nil {
		commands, commandErr = catalogCommands(ctx, run, raw, cwd)
		run.stopProcess()
	}
	c.mu.Lock()
	entry.commands, entry.commandErr = commands, commandErr
	entry.checked = time.Now()
	close(entry.done)
	c.mu.Unlock()
}
