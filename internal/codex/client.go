package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shenming0821/Fuyu/internal/agent"
	process "github.com/shenming0821/Fuyu/internal/runtime"
)

type Event = agent.Event

type RPCError = agent.RPCError

type envelope struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

type Client struct {
	process *process.Process
	writeMu sync.Mutex
	mu      sync.Mutex
	pending agent.RequestTable[envelope]
	server  map[string]*serverRequest
	nextID  atomic.Int64
	done    chan struct{}
	abort   chan struct{}
	close   sync.Once

	queueMu    sync.Mutex
	queue      []Event
	queueBytes int
	gap        bool
	overflow   bool
	wake       chan struct{}
	events     chan Event
}

func Start(ctx context.Context, executable, cwd string) (*Client, error) {
	// Fuyu applies its own close grace period. Once it unsubscribes, release
	// native resources immediately rather than retaining another idle cache.
	c, err := startProcess(executable, []string{"-c", "thread_unload_delay_secs=0", "app-server", "--listen", "stdio://"}, cwd)
	if err != nil {
		return nil, err
	}
	var result json.RawMessage
	err = c.Call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "fuyu", "title": "Fuyu", "version": "0.1.0"},
		"capabilities": map[string]bool{"experimentalApi": true},
	}, &result)
	if err == nil {
		err = c.write(map[string]any{"method": "initialized"})
	}
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize Codex: %w", err)
	}
	return c, nil
}

func Version(ctx context.Context, executable string) (string, error) {
	data, err := exec.CommandContext(ctx, executable, "--version").Output()
	return string(data), err
}

func startProcess(executable string, args []string, cwd string) (*Client, error) {
	// stderr may contain private configuration or tool output; do not persist it.
	p, err := process.Start(executable, args, cwd, io.Discard)
	if err != nil {
		return nil, fmt.Errorf("start Codex: %w", err)
	}
	c := &Client{
		process: p, server: make(map[string]*serverRequest), done: make(chan struct{}), abort: make(chan struct{}),
		wake: make(chan struct{}, 1), events: make(chan Event),
	}
	go c.dispatch()
	go c.read()
	return c, nil
}

func (c *Client) Events() <-chan Event { return c.events }

func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := fmt.Sprint(c.nextID.Add(1))
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return errors.New("Codex connection closed")
	default:
	}
	response, _ := c.pending.Register(id)
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.pending.Remove(id)
		c.mu.Unlock()
	}()
	if err := c.write(map[string]any{"id": json.RawMessage(id), "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case msg := <-response:
		if msg.Error != nil {
			return msg.Error
		}
		if result != nil {
			return json.Unmarshal(msg.Result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		// EOF may follow a response already routed to this request.
		select {
		case msg := <-response:
			if msg.Error != nil {
				return msg.Error
			}
			if result != nil {
				return json.Unmarshal(msg.Result, result)
			}
			return nil
		default:
			return errors.New("Codex disconnected before responding")
		}
	}
}

func (c *Client) write(message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if pipe, ok := c.process.Stdin.(interface{ SetWriteDeadline(time.Time) error }); ok {
		if err := pipe.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return err
		}
		defer pipe.SetWriteDeadline(time.Time{})
	}
	_, err = c.process.Stdin.Write(append(data, '\n'))
	return err
}

func (c *Client) read() {
	scanner := bufio.NewScanner(c.process.Stdout)
	scanner.Buffer(make([]byte, 64*1024), agent.MaxFrameBytes)
	var readErr error
	for scanner.Scan() {
		var msg envelope
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			readErr = fmt.Errorf("invalid Codex protocol message: %w", err)
			break
		}
		switch {
		case msg.Method != "" && len(msg.ID) > 0:
			c.handleRequest(msg)
		case msg.Method != "":
			c.expireRequests(msg)
			c.enqueue(displayEvent(Event{Method: msg.Method, Params: msg.Params}))
		case len(msg.ID) > 0:
			c.pending.Route(string(msg.ID), msg)
		}
	}
	if readErr == nil {
		readErr = scanner.Err()
	}
	if err := c.process.Wait(); readErr == nil {
		readErr = err
	}
	c.enqueue(Event{Method: "fuyu/disconnected", Err: readErr})
	c.mu.Lock()
	close(c.done)
	c.mu.Unlock()
	c.signal()
}

func (c *Client) rejectUnsupported(msg envelope) {
	var result any
	switch msg.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		result = map[string]string{"decision": "cancel"}
	case "item/permissions/requestApproval":
		result = map[string]any{"permissions": map[string]any{}, "scope": "turn"}
	case "mcpServer/elicitation/request":
		result = map[string]string{"action": "decline"}
	default:
		err := c.write(map[string]any{"id": msg.ID, "error": RPCError{Code: -32601, Message: "Fuyu does not support this interaction"}})
		c.unsupported(msg, err)
		return
	}
	err := c.write(map[string]any{"id": msg.ID, "result": result})
	c.unsupported(msg, err)
}

func (c *Client) unsupported(msg envelope, err error) {
	detail := fmt.Errorf("交互已拒绝：%s", msg.Method)
	if err != nil {
		detail = fmt.Errorf("拒绝响应未送达（%s）：%w", msg.Method, err)
	}
	c.enqueue(Event{Method: "fuyu/unsupported", Params: msg.Params, Err: detail})
}

const MaxQueueBytes = agent.MaxQueueBytes
const MaxQueueEvents = agent.MaxQueueEvents

func eventBytes(e Event) int { return len(e.Params) + len(e.ID) + len(e.Method) + 128 }

func isDelta(e Event) bool {
	return e.Method == "item/agentMessage/delta" || e.Method == "item/commandExecution/outputDelta" ||
		e.Method == "item/fileChange/outputDelta"
}

func (c *Client) enqueue(e Event) {
	c.queueMu.Lock()
	if c.overflow {
		c.queueMu.Unlock()
		return
	}
	// Drop only incremental display data. Lifecycle and request events either fit
	// or close the connection explicitly; RPC response routing never waits on UI.
	for len(c.queue) >= MaxQueueEvents || c.queueBytes+eventBytes(e) > MaxQueueBytes {
		drop := -1
		for i, old := range c.queue {
			if isDelta(old) {
				drop = i
				break
			}
		}
		if drop < 0 {
			if isDelta(e) {
				c.gap = true
				c.queueMu.Unlock()
				c.signal()
				return
			}
			c.queue = []Event{{Method: "fuyu/disconnected", Err: errors.New("Codex control event buffer exceeded capacity; results require verification")}}
			c.queueBytes = eventBytes(c.queue[0])
			c.overflow = true
			c.queueMu.Unlock()
			c.signal()
			go c.process.Close()
			return
		}
		c.queueBytes -= eventBytes(c.queue[drop])
		copy(c.queue[drop:], c.queue[drop+1:])
		c.queue[len(c.queue)-1] = Event{}
		c.queue = c.queue[:len(c.queue)-1]
		c.gap = true
	}
	c.queue = append(c.queue, e)
	c.queueBytes += eventBytes(e)
	c.queueMu.Unlock()
	c.signal()
}

func (c *Client) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Client) dispatch() {
	defer close(c.events)
	for {
		c.queueMu.Lock()
		if c.gap {
			c.gap = false
			c.queueMu.Unlock()
			select {
			case c.events <- Event{Method: "fuyu/eventGap"}:
			case <-c.abort:
				return
			}
			continue
		}
		if len(c.queue) > 0 {
			event := c.queue[0]
			c.queueBytes -= eventBytes(event)
			c.queue[0] = Event{}
			c.queue = c.queue[1:]
			c.queueMu.Unlock()
			select {
			case c.events <- event:
			case <-c.abort:
				return
			}
			continue
		}
		c.queueMu.Unlock()
		select {
		case <-c.wake:
		case <-c.done:
			c.queueMu.Lock()
			empty := len(c.queue) == 0
			c.queueMu.Unlock()
			if empty {
				return
			}
		case <-c.abort:
			return
		}
	}
}

func (c *Client) Close() {
	c.close.Do(func() {
		_ = c.process.Close()
		close(c.abort)
	})
}

type Thread = agent.Thread

type Turn = agent.Turn

func (c *Client) StartThread(ctx context.Context, cwd string) (Thread, error) {
	var thread Thread
	err := c.Call(ctx, "thread/start", map[string]any{"cwd": cwd}, &thread)
	return thread, err
}

func (c *Client) Send(ctx context.Context, threadID, text string) (Turn, error) {
	return c.SendWithSkills(ctx, threadID, text, nil)
}

func (c *Client) Interrupt(ctx context.Context, threadID, turnID string) error {
	return c.Call(ctx, "turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID}, nil)
}

func (c *Client) ReadThread(ctx context.Context, threadID string) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.Call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": true}, &result)
	return result, err
}

func RequestContext() (context.Context, context.CancelFunc) {
	return agent.RequestContext()
}
