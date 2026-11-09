package devin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/attachment"
)

const protocolVersion = 1

// processGeneration is the TurnMap identity generation for the single hosted
// process; outbound events omit generation since there is exactly one stream.
const processGeneration = 1

type Client struct {
	mu          sync.Mutex
	executable  string
	apiKey      string
	home        string
	stateDir    string
	conn        *conn
	version     string
	sessions    map[string]*acpSession
	commands    []agent.Command
	permissions map[string]*permission
	turns       agent.TurnMap
	queue       eventQueue
	wg          sync.WaitGroup
	closed      bool
	failed      bool
}

// acpSession tracks one Devin ACP session's attachment, options, open tool
// items, replay capture and the application-allocated turn in flight.
type acpSession struct {
	id        string
	cwd       string
	title     string
	mode      string
	catalog   bool
	loaded    bool
	loading   bool
	options   map[string]*configOption
	tools     map[string]*toolState
	replayBuf []json.RawMessage
	replay    []agent.HistoryTurn
	turn      agent.TurnIdentity
	state     *sessionState
	sendMu    sync.Mutex
}

// configOption mirrors one ACP session config option (select kind only).
type configOption struct {
	ID      string
	Current string
	Choices map[string]string // value → display name
}

// New connects the Devin ACP subprocess and authenticates it. The handshake is
// synchronous so a bad binary, protocol mismatch or credential failure keeps
// the backend out of the registry instead of surfacing later.
func New(ctx context.Context, executable, apiKey, home, stateDir string) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Devin API key 为空，请设置 DEVIN_API_KEY 或登录 Devin")
	}
	if home == "" {
		var err error
		home, err = Home()
		if err != nil {
			return nil, err
		}
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, err
	}
	c := &Client{executable: executable, apiKey: apiKey, home: home, stateDir: stateDir, sessions: make(map[string]*acpSession)}
	c.queue.start()
	if err := c.connect(ctx); err != nil {
		c.queue.close()
		return nil, err
	}
	return c, nil
}

func (c *Client) connect(ctx context.Context) error {
	link, err := spawn(c.executable, c)
	if err != nil {
		return fmt.Errorf("Devin ACP 启动失败：%w", err)
	}
	result, err := link.call(ctx, "initialize", map[string]any{
		"protocolVersion":    protocolVersion,
		"clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": false, "writeTextFile": false}, "terminal": false},
	})
	if err != nil {
		link.close()
		return fmt.Errorf("Devin ACP 初始化失败：%w", err)
	}
	var init struct {
		ProtocolVersion int `json:"protocolVersion"`
		AgentInfo       struct {
			Version string `json:"version"`
		} `json:"agentInfo"`
	}
	if json.Unmarshal(result, &init) != nil || init.ProtocolVersion < protocolVersion {
		link.close()
		return fmt.Errorf("Devin ACP 协议版本不受支持")
	}
	c.version = init.AgentInfo.Version
	if _, err := link.call(ctx, "authenticate", map[string]any{
		"methodId": "devin-browser",
		"_meta":    map[string]any{"api_key": c.apiKey},
	}); err != nil {
		link.close()
		return fmt.Errorf("Devin 凭据不可用，请重新登录 Devin 或设置 DEVIN_API_KEY：%w", err)
	}
	c.conn = link
	return nil
}

func (c *Client) Home() string { return c.home }

func (c *Client) Version() string { return c.version }

func (c *Client) Capabilities() agent.Capabilities {
	return agent.Capabilities{Models: true, Commands: true, Images: true, Approval: true, History: true, Resume: true, Reconcile: true}
}

func (c *Client) Events() <-chan agent.Event { return c.queue.events }

func (c *Client) sessionKey(id string) agent.SessionKey {
	return agent.SessionKey{Agent: agent.Devin, Runtime: c.home, SessionID: id}
}

func (c *Client) emit(s *acpSession, method string, value any) {
	c.emitID(s, method, nil, value)
}

func (c *Client) emitID(s *acpSession, method string, id json.RawMessage, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	// One process hosts every session, so events carry no process generation;
	// sessions match by agent, runtime and session id.
	event := agent.Event{Session: c.sessionKey(s.id), ID: id, Method: method, Params: raw}
	if !c.queue.push(event) {
		c.mu.Lock()
		if !c.failed {
			c.failed = true
			if c.conn != nil {
				c.conn.close()
			}
		}
		c.mu.Unlock()
	}
}

// session returns the attached, promptable session or an error.
func (c *Client) session(id string) (*acpSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.failed || c.conn == nil {
		return nil, fmt.Errorf("Devin 连接已关闭")
	}
	s := c.sessions[id]
	if s == nil || s.catalog || !s.loaded || s.loading {
		return nil, fmt.Errorf("Devin 会话不存在或尚未恢复")
	}
	return s, nil
}

func (c *Client) StartThread(ctx context.Context, cwd string) (agent.Thread, error) {
	var thread agent.Thread
	path, err := filepath.Abs(cwd)
	if err != nil {
		return thread, err
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return thread, fmt.Errorf("Devin 工作目录不可用：%s", path)
	}
	c.mu.Lock()
	ready := !c.closed && !c.failed && c.conn != nil
	link := c.conn
	c.mu.Unlock()
	if !ready {
		return thread, fmt.Errorf("Devin 连接已关闭")
	}
	result, err := link.call(ctx, "session/new", map[string]any{"cwd": path, "mcpServers": []any{}})
	if err != nil {
		return thread, err
	}
	var created struct {
		SessionID     string `json:"sessionId"`
		Modes         struct {
			Current string `json:"currentModeId"`
		} `json:"modes"`
		ConfigOptions json.RawMessage `json:"configOptions"`
	}
	if json.Unmarshal(result, &created) != nil || created.SessionID == "" {
		return thread, fmt.Errorf("Devin 会话响应不完整")
	}
	s := &acpSession{id: created.SessionID, cwd: path, mode: created.Modes.Current, loaded: true,
		options: parseConfigOptions(created.ConfigOptions), tools: make(map[string]*toolState), state: c.loadState(created.SessionID)}
	c.mu.Lock()
	if c.closed || c.failed {
		c.mu.Unlock()
		return thread, fmt.Errorf("Devin 连接已关闭")
	}
	c.sessions[s.id] = s
	c.mu.Unlock()
	model := ""
	if s.options["model"] != nil {
		model = s.options["model"].Current
	}
	c.emit(s, "fuyu/runtime", map[string]any{"threadId": s.id, "model": model, "version": c.version})
	thread.Thread.ID, thread.Cwd, thread.Model = s.id, path, model
	thread.ApprovalPolicy = json.RawMessage(fmt.Sprintf("%q", "devin:"+s.mode))
	thread.Sandbox = json.RawMessage(`{"type":"devinAcp"}`)
	return thread, nil
}

func (c *Client) Send(ctx context.Context, id, text string) (agent.Turn, error) {
	return c.send(ctx, id, text, nil, agent.TurnOptions{})
}

func (c *Client) SendWithImages(ctx context.Context, id, text string, skills []agent.Skill, images []attachment.Image) (agent.Turn, error) {
	return c.send(ctx, id, text, images, agent.TurnOptions{})
}

func (c *Client) SendWithModel(ctx context.Context, id, text string, skills []agent.Skill, images []attachment.Image, model string) (agent.Turn, error) {
	return c.send(ctx, id, text, images, agent.TurnOptions{Model: model})
}

func (c *Client) SendWithOptions(ctx context.Context, id, text string, skills []agent.Skill, images []attachment.Image, options agent.TurnOptions) (agent.Turn, error) {
	return c.send(ctx, id, text, images, options)
}

func (c *Client) send(ctx context.Context, id, text string, images []attachment.Image, options agent.TurnOptions) (turn agent.Turn, sendErr error) {
	attempted := false
	defer func() {
		if sendErr != nil && !attempted {
			sendErr = &agent.NotSubmittedError{Err: sendErr}
		}
	}()
	s, err := c.session(id)
	if err != nil {
		return turn, err
	}
	content := []any{}
	for _, ref := range images {
		raw, err := attachment.Read(ref)
		if err != nil {
			return turn, err
		}
		content = append(content, map[string]any{"type": "image", "data": base64.StdEncoding.EncodeToString(raw), "mimeType": http.DetectContentType(raw)})
	}
	if text != "" || len(content) == 0 {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if err := c.applyOptions(ctx, s, options); err != nil {
		return turn, err
	}
	identity, err := c.turns.Begin(c.sessionKey(id), processGeneration)
	if err != nil {
		return turn, err
	}
	c.mu.Lock()
	s.turn = identity
	c.mu.Unlock()
	s.state.appendTurn(string(identity.ID), text)
	if err := c.saveState(s.state); err != nil {
		c.mu.Lock()
		s.turn = agent.TurnIdentity{}
		c.mu.Unlock()
		s.state.dropTurn(string(identity.ID))
		c.turns.Complete(identity.Session, identity.Generation, identity.ID)
		return turn, err
	}
	c.emit(s, "turn/started", map[string]any{"threadId": id, "turn": map[string]any{"id": string(identity.ID), "status": "inProgress"}})
	reply, requestKey, err := c.conn.beginCall("session/prompt", map[string]any{"sessionId": id, "prompt": content})
	if err != nil {
		c.mu.Lock()
		s.turn = agent.TurnIdentity{}
		c.mu.Unlock()
		s.state.dropTurn(string(identity.ID))
		_ = c.saveState(s.state)
		c.turns.Complete(identity.Session, identity.Generation, identity.ID)
		return turn, err
	}
	attempted = true
	c.emit(s, "item/completed", map[string]any{"threadId": id, "turnId": string(identity.ID), "item": map[string]any{
		"id": identity.UserUUID, "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": text}},
	}})
	c.wg.Add(1)
	go c.finishPrompt(s, identity, reply, requestKey)
	turn.ID, turn.Status = string(identity.ID), "inProgress"
	return turn, nil
}

// finishPrompt converts the prompt response into the terminal turn event. ACP
// resolves session/prompt when the turn ends; stopReason maps to the app
// status vocabulary.
func (c *Client) finishPrompt(s *acpSession, identity agent.TurnIdentity, reply <-chan rpcResult, requestKey string) {
	defer c.wg.Done()
	defer c.conn.calls.Remove(requestKey)
	var outcome rpcResult
	select {
	case outcome = <-reply:
	case <-c.conn.done:
		outcome = rpcResult{err: c.conn.disconnectErr()}
	}
	status, message := "failed", ""
	if outcome.err != nil {
		message = outcome.err.Error()
	} else {
		var result struct {
			StopReason string `json:"stopReason"`
		}
		if json.Unmarshal(outcome.result, &result) != nil {
			result.StopReason = ""
		}
		switch result.StopReason {
		case "end_turn":
			status = "completed"
		case "cancelled":
			status = "interrupted"
		case "refusal":
			message = "Devin 拒绝了本次请求"
		case "max_tokens":
			message = "Devin 达到输出上限"
		case "max_turn_requests":
			message = "Devin 达到请求次数上限"
		default:
			message = "未知的 Devin 终态：" + result.StopReason
		}
	}
	c.mu.Lock()
	if s.turn.ID == identity.ID {
		s.turn = agent.TurnIdentity{}
	}
	c.mu.Unlock()
	s.state.finishTurn(string(identity.ID), status)
	_ = c.saveState(s.state)
	c.expirePermissions(s)
	c.turns.Complete(identity.Session, identity.Generation, identity.ID)
	completed := map[string]any{"id": string(identity.ID), "status": status}
	if message != "" {
		completed["error"] = map[string]any{"message": message}
	}
	c.emit(s, "turn/completed", map[string]any{"threadId": s.id, "turn": completed})
}

func (c *Client) Interrupt(ctx context.Context, id, turnID string) error {
	s, err := c.session(id)
	if err != nil {
		return err
	}
	c.mu.Lock()
	current := s.turn
	c.mu.Unlock()
	if current.ID == "" || string(current.ID) != turnID {
		return fmt.Errorf("Devin 当前任务标识已失效")
	}
	return c.conn.notify("session/cancel", map[string]any{"sessionId": id})
}

func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	link := c.conn
	c.mu.Unlock()
	if link != nil {
		link.close()
	}
	c.wg.Wait()
	c.queue.close()
}

// onExit converts an unexpected process exit into a backend-scoped disconnect;
// sessions reload through session/load on the next connection.
func (c *Client) onExit(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.failed = true
	type pendingTurn struct {
		s        *acpSession
		identity agent.TurnIdentity
	}
	var pending []pendingTurn
	for _, s := range c.sessions {
		if s.turn.ID != "" {
			pending = append(pending, pendingTurn{s: s, identity: s.turn})
			s.turn = agent.TurnIdentity{}
		}
		s.loaded = false
	}
	c.mu.Unlock()
	for _, p := range pending {
		c.turns.Complete(p.identity.Session, p.identity.Generation, p.identity.ID)
		c.emit(p.s, "turn/completed", map[string]any{"threadId": p.s.id, "turn": map[string]any{
			"id": string(p.identity.ID), "status": "failed", "error": map[string]any{"message": err.Error()},
		}})
	}
	c.queue.push(agent.Event{Method: "fuyu/disconnected", Err: err})
}
