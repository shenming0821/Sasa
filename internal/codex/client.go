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

	process "github.com/shenming0821/Fuyu/internal/runtime"
)

type Event struct {
	Method string
	Params json.RawMessage
	Err    error
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("Codex RPC %d: %s", e.Code, e.Message) }

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
	pending map[string]chan envelope
	nextID  atomic.Int64
	done    chan struct{}
	abort   chan struct{}
	close   sync.Once

	queueMu sync.Mutex
	queue   []Event
	wake    chan struct{}
	events  chan Event
}

func Start(ctx context.Context, executable, cwd string) (*Client, error) {
	c, err := startProcess(executable, []string{"app-server", "--listen", "stdio://"}, cwd)
	if err != nil {
		return nil, err
	}
	var result json.RawMessage
	err = c.Call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "fuyu", "title": "Fuyu", "version": "0.1.0"},
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
		process: p, pending: make(map[string]chan envelope), done: make(chan struct{}), abort: make(chan struct{}),
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
	response := make(chan envelope, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return errors.New("Codex connection closed")
	default:
	}
	c.pending[id] = response
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
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
	_, err = c.process.Stdin.Write(append(data, '\n'))
	return err
}

func (c *Client) read() {
	scanner := bufio.NewScanner(c.process.Stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var readErr error
	for scanner.Scan() {
		var msg envelope
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			readErr = fmt.Errorf("invalid Codex protocol message: %w", err)
			break
		}
		switch {
		case msg.Method != "" && len(msg.ID) > 0:
			c.rejectUnsupported(msg)
		case msg.Method != "":
			c.enqueue(Event{Method: msg.Method, Params: msg.Params})
		case len(msg.ID) > 0:
			c.mu.Lock()
			ch := c.pending[string(msg.ID)]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- msg:
				default:
				}
			}
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
	// P1 surfaces the request and fails closed; interactive forms arrive in P2.
	var result any
	switch msg.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		result = map[string]string{"decision": "cancel"}
	case "item/permissions/requestApproval":
		result = map[string]any{"permissions": map[string]any{}, "scope": "turn"}
	case "mcpServer/elicitation/request":
		result = map[string]string{"action": "decline"}
	default:
		err := c.write(map[string]any{"id": msg.ID, "error": RPCError{Code: -32601, Message: "Fuyu P1 does not support this interaction"}})
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

func (c *Client) enqueue(e Event) {
	c.queueMu.Lock()
	c.queue = append(c.queue, e)
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
		if len(c.queue) > 0 {
			event := c.queue[0]
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

type Thread struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
	Model          string          `json:"model"`
	Cwd            string          `json:"cwd"`
	ApprovalPolicy json.RawMessage `json:"approvalPolicy"`
	Sandbox        json.RawMessage `json:"sandbox"`
}

type Turn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) StartThread(ctx context.Context, cwd string) (Thread, error) {
	var thread Thread
	err := c.Call(ctx, "thread/start", map[string]any{"cwd": cwd}, &thread)
	return thread, err
}

func (c *Client) Send(ctx context.Context, threadID, text string) (Turn, error) {
	var result struct {
		Turn Turn `json:"turn"`
	}
	err := c.Call(ctx, "turn/start", map[string]any{
		"threadId": threadID,
		"input": []any{map[string]any{
			"type": "text", "text": text, "textElements": []any{},
		}},
	}, &result)
	return result.Turn, err
}

func (c *Client) Interrupt(ctx context.Context, threadID, turnID string) error {
	return c.Call(ctx, "turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID}, nil)
}

func RequestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}
