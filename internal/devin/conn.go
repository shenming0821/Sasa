package devin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/shenming0821/Fuyu/internal/agent"
	runtime "github.com/shenming0821/Fuyu/internal/runtime"
)

type rpcResult struct {
	result json.RawMessage
	err    error
}

// connEvents receives inbound ACP traffic routed by the connection reader.
type connEvents interface {
	onUpdate(sessionID string, update json.RawMessage)
	onRequest(id json.RawMessage, method string, params json.RawMessage)
	onExit(err error)
}

// conn owns one `devin acp` process and its newline JSON-RPC stream. Requests
// the client sends carry integer ids; requests the agent sends carry its own
// (string) ids and are answered verbatim.
type conn struct {
	process *runtime.Process
	stderr  *tailBuffer
	events  connEvents

	seq      int64
	calls    agent.RequestTable[rpcResult]
	writeMu  sync.Mutex
	done     chan struct{}
	closeErr error
	once     sync.Once
	exitErr  error
}

type rpcFrame struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *agent.RPCError `json:"error,omitempty"`
}

// tailBuffer keeps the last bytes of stderr for connection diagnostics without
// letting chatty logging grow memory.
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

const tailLimit = 16 * 1024

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > tailLimit {
		b.data = append([]byte(nil), b.data[len(b.data)-tailLimit:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

func spawn(executable string, events connEvents) (*conn, error) {
	stderr := &tailBuffer{}
	process, err := runtime.Start(executable, []string{"acp"}, "", stderr)
	if err != nil {
		return nil, err
	}
	c := &conn{process: process, stderr: stderr, events: events, done: make(chan struct{})}
	go c.read()
	return c, nil
}

func requestKey(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var text string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return "", false
		}
		return text, text != ""
	}
	var id int64
	if err := json.Unmarshal(raw, &id); err != nil {
		return "", false
	}
	return strconv.FormatInt(id, 10), true
}

func (c *conn) write(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(raw) > agent.MaxFrameBytes {
		return fmt.Errorf("Devin 请求超过协议上限")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closeErr != nil {
		return c.closeErr
	}
	if _, err := c.process.Stdin.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("Devin 写入失败：%w", err)
	}
	return nil
}

// call issues a request and waits for its response, context cancellation or
// connection loss. The caller's deadline only releases the waiter; the pending
// slot is removed so a late response is dropped.
func (c *conn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	reply, key, err := c.beginCall(method, params)
	if err != nil {
		return nil, err
	}
	defer c.calls.Remove(key)
	select {
	case result := <-reply:
		return result.result, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.disconnectErr()
	}
}

// beginCall registers and writes a request without waiting; the reply channel
// must be drained or removed by the caller.
func (c *conn) beginCall(method string, params any) (<-chan rpcResult, string, error) {
	c.writeMu.Lock()
	c.seq++
	id := c.seq
	c.writeMu.Unlock()
	key := strconv.FormatInt(id, 10)
	reply, ok := c.calls.Register(key)
	if !ok {
		return nil, "", fmt.Errorf("Devin 请求编号冲突")
	}
	err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		c.calls.Remove(key)
		return nil, "", err
	}
	return reply, key, nil
}

func (c *conn) notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *conn) respond(id json.RawMessage, result any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (c *conn) respondError(id json.RawMessage, code int, message string) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func (c *conn) disconnectErr() error {
	if c.exitErr != nil {
		return fmt.Errorf("Devin 连接已断开：%w", c.exitErr)
	}
	return errors.New("Devin 连接已断开")
}

func (c *conn) read() {
	scanner := bufio.NewScanner(c.process.Stdout)
	scanner.Buffer(make([]byte, 64<<10), agent.MaxFrameBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		var frame rpcFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			continue
		}
		switch {
		case frame.Method != "" && len(frame.ID) > 0:
			go c.events.onRequest(frame.ID, frame.Method, frame.Params)
		case frame.Method != "":
			var params struct {
				SessionID string          `json:"sessionId"`
				Update    json.RawMessage `json:"update"`
			}
			if frame.Method == "session/update" && json.Unmarshal(frame.Params, &params) == nil && len(params.Update) > 0 {
				c.events.onUpdate(params.SessionID, params.Update)
			}
		case len(frame.ID) > 0:
			key, ok := requestKey(frame.ID)
			if !ok {
				continue
			}
			result := rpcResult{result: frame.Result}
			if frame.Error != nil {
				result.err = fmt.Errorf("Devin %w", frame.Error)
			}
			c.calls.Route(key, result)
		}
	}
	exitErr := scanner.Err()
	if exitErr == nil {
		exitErr = io.EOF
	}
	waitErr := c.process.Wait()
	c.exitErr = exitErr
	if waitErr != nil && errors.Is(exitErr, io.EOF) {
		c.exitErr = waitErr
	}
	c.once.Do(func() { close(c.done) })
	c.events.onExit(c.disconnectErr())
}

func (c *conn) close() {
	c.writeMu.Lock()
	c.closeErr = errors.New("Devin 连接已关闭")
	c.writeMu.Unlock()
	_ = c.process.Close()
	c.once.Do(func() { close(c.done) })
}
