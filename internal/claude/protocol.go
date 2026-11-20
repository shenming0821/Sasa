package claude

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
	"github.com/shenming0821/Sasa/internal/attachment"
	process "github.com/shenming0821/Sasa/internal/runtime"
)

type controlReply struct {
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Response  json.RawMessage `json:"response"`
	Error     string          `json:"error"`
}

type permission struct {
	ID     string
	Tool   string          `json:"tool_name"`
	Input  json.RawMessage `json:"input"`
	ToolID string          `json:"tool_use_id"`
}

type execution struct {
	client                *Client
	session               *session
	identity              agent.TurnIdentity
	process               *process.Process
	mu                    sync.Mutex
	writeMu               *sync.Mutex
	controls              *agent.RequestTable[controlReply]
	pending               map[string]permission
	resolved              map[string]bool
	done                  chan struct{}
	result                *frame
	problem               error
	interrupted           bool
	interruptID           string
	interruptAcknowledged bool
	userAttempted         bool
	compactRequested      bool
	localCommand          bool
	compactBoundary       bool
	lastUUID              string
	messageID             string
	blocks                map[int]*contentBlock
	tools                 map[string]map[string]any
	seen                  map[string]bool
	background            map[string]bool
	offsets               map[string]int
	diagnostics           *diagnosticBuffer
	connection            *sessionProcess
	turnDone              chan struct{}
	finishOnce            sync.Once
	reusable              bool
	idleTimer             *time.Timer
	idleSince             time.Time
	projectionDigest      string
}

type diagnosticBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (buffer *diagnosticBuffer) Write(raw []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	count := len(raw)
	buffer.data = append(buffer.data, raw[:min(len(raw), max(0, (64<<10)-len(buffer.data)))]...)
	return count, nil
}

func (buffer *diagnosticBuffer) hint() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	text := strings.ToLower(string(buffer.data))
	for _, category := range []struct {
		markers []string
		hint    string
	}{
		{[]string{"authentication", "unauthorized", "not logged in", "invalid api key", "please log in"}, "Claude 认证不可用，请在 Claude Code CLI 检查登录与当前 provider 配置"},
		{[]string{"onboarding", "trust this", "trust dialog"}, "Claude 初始化尚需确认，请在该目录运行 Claude Code CLI 完成初始设置"},
		{[]string{"econn", "enotfound", "network", "fetch failed", "certificate", "tls"}, "Claude 网络连接失败，请检查当前 provider 的网络与证书配置"},
	} {
		for _, marker := range category.markers {
			if strings.Contains(text, marker) {
				return category.hint
			}
		}
	}
	return ""
}

type frame struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype"`
	UUID       string          `json:"uuid"`
	SessionID  string          `json:"session_id"`
	ParentTool string          `json:"parent_tool_use_id"`
	RequestID  string          `json:"request_id"`
	Request    json.RawMessage `json:"request"`
	Response   controlReply    `json:"response"`
	Message    struct {
		ID      string        `json:"id"`
		Role    string        `json:"role"`
		Content contentBlocks `json:"content"`
	} `json:"message"`
	Event struct {
		Type    string `json:"type"`
		Index   int    `json:"index"`
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
		Block contentBlock `json:"content_block"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	} `json:"event"`
	IsError        bool     `json:"is_error"`
	TerminalReason string   `json:"terminal_reason"`
	Result         string   `json:"result"`
	Errors         []string `json:"errors"`
	TaskID         string   `json:"task_id"`
	ToolUseID      string   `json:"tool_use_id"`
	Status         string   `json:"status"`
	Summary        string   `json:"summary"`
	Model          string   `json:"model"`
	Version        string   `json:"claude_code_version"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Source    struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

type contentBlocks []contentBlock

func (blocks *contentBlocks) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		*blocks = contentBlocks{{Type: "text", Text: text}}
		return nil
	}
	var items []contentBlock
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	*blocks = items
	return nil
}

func (c *Client) spawn(ctx context.Context, s *session, identity agent.TurnIdentity, model, effort string) (*execution, json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	args := []string{"-p", "--verbose", "--input-format", "stream-json", "--output-format", "stream-json", "--include-partial-messages", "--permission-prompt-tool", "stdio"}
	if identity.ID != "" {
		if s.ResumeSource != "" {
			args = append(args, "--resume", s.ResumeSource, "--resume-session-at", s.ResumeAt, "--fork-session", "--session-id", s.ID)
		} else if len(s.Turns) > 1 {
			args = append(args, "--resume", s.ID)
		} else {
			args = append(args, "--session-id", s.ID)
		}
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	c.mu.Lock()
	if c.closed || c.failed {
		c.mu.Unlock()
		return nil, nil, fmt.Errorf("Claude 连接已关闭")
	}
	environment := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CLAUDE_CONFIG_DIR=") {
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "CLAUDE_CONFIG_DIR="+c.home)
	diagnostics := &diagnosticBuffer{}
	p, err := process.StartWithEnvironment(c.executable, args, s.Cwd, diagnostics, environment)
	if err != nil {
		c.mu.Unlock()
		return nil, nil, fmt.Errorf("启动 Claude Code：%w", err)
	}
	run := &execution{client: c, session: s, identity: identity, process: p, done: make(chan struct{}), pending: make(map[string]permission), resolved: make(map[string]bool), blocks: make(map[int]*contentBlock), tools: make(map[string]map[string]any), seen: make(map[string]bool), background: make(map[string]bool)}
	run.writeMu = &sync.Mutex{}
	run.controls = &agent.RequestTable[controlReply]{}
	run.turnDone = make(chan struct{})
	run.connection = &sessionProcess{next: make(chan nextExecution), model: model, effort: effort, config: c.processConfiguration(s.Cwd)}
	run.diagnostics = diagnostics
	if identity.ID != "" {
		s.run = run
	}
	c.executions[run] = true
	c.mu.Unlock()
	go run.read()
	raw, err := run.control(ctx, map[string]any{"subtype": "initialize", "hooks": nil})
	if err != nil {
		run.stopProcess()
		if hint := diagnostics.hint(); hint != "" {
			return nil, nil, fmt.Errorf("%s：%w", hint, err)
		}
		return nil, nil, fmt.Errorf("Claude 初始化失败，请检查登录、网络和 CLI 配置：%w", err)
	}
	var settings struct {
		Mode string `json:"current_permission_mode"`
	}
	if json.Unmarshal(raw, &settings) != nil || strings.TrimSpace(settings.Mode) == "" {
		run.stopProcess()
		return nil, nil, fmt.Errorf("Claude 未确认实际权限模式，未发送任务")
	}
	c.mu.Lock()
	run.connection.permissionMode = settings.Mode
	if settings.Mode != "plan" {
		s.BasePermissionMode = settings.Mode
	}
	c.mu.Unlock()
	return run, raw, nil
}

func (r *execution) write(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > agent.MaxFrameBytes {
		return fmt.Errorf("Claude 输入超过 16 MiB 协议上限")
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	timer := time.AfterFunc(10*time.Second, func() { _ = r.process.Close() })
	defer timer.Stop()
	raw = append(raw, '\n')
	for len(raw) > 0 {
		count, err := r.process.Stdin.Write(raw)
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		raw = raw[count:]
	}
	return nil
}

func (r *execution) control(ctx context.Context, request any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	id, err := uuid()
	if err != nil {
		return nil, err
	}
	reply, _ := r.controls.Register(id)
	defer r.controls.Remove(id)
	if value, ok := request.(map[string]any); ok && value["subtype"] == "interrupt" {
		r.mu.Lock()
		r.interruptID = id
		r.mu.Unlock()
	}
	if err := r.write(map[string]any{"type": "control_request", "request_id": id, "request": request}); err != nil {
		return nil, err
	}
	select {
	case result := <-reply:
		if result.Subtype != "success" {
			return nil, fmt.Errorf("Claude 控制请求失败：%s", result.Error)
		}
		return result.Response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
		return nil, fmt.Errorf("Claude 进程在控制响应前退出")
	}
}

func (r *execution) stopProcess() { _ = r.process.Close(); <-r.done }

func (r *execution) emit(method string, value any) {
	if r.identity.ID == "" {
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	event := agent.Event{Session: r.identity.Session, Generation: r.identity.Generation, Method: method, Params: raw}.ForDisplay()
	if !r.client.queue.push(event) {
		r.mu.Lock()
		if r.problem == nil {
			r.problem = fmt.Errorf("Claude 事件队列超过容量，请恢复原生历史核验")
		}
		r.mu.Unlock()
		go r.client.failQueue()
	}
}

func (r *execution) item(method string, item map[string]any) {
	r.emit(method, map[string]any{"threadId": r.session.ID, "turnId": r.identity.ID, "item": item})
}

func (root *execution) read() {
	type input struct {
		frame frame
		size  int
		err   error
	}
	incoming := make(chan input, 1)
	stopScan, scanned := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(scanned)
		defer close(incoming)
		scanner := bufio.NewScanner(root.process.Stdout)
		scanner.Buffer(make([]byte, 64<<10), agent.MaxFrameBytes)
		for scanner.Scan() {
			message := input{size: len(scanner.Bytes())}
			if err := json.Unmarshal(scanner.Bytes(), &message.frame); err != nil {
				message.err = fmt.Errorf("Claude JSON 帧损坏：%w", err)
			}
			select {
			case incoming <- message:
			case <-stopScan:
				return
			}
			if message.err != nil {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case incoming <- input{err: fmt.Errorf("读取 Claude 协议：%w", err)}:
			case <-stopScan:
			}
		}
	}()
	r := root
reading:
	for {
		select {
		case next := <-root.connection.next:
			r = next.run
			r.client.mu.Lock()
			r.session.run = r
			r.client.mu.Unlock()
			close(next.ready)
		case input, ok := <-incoming:
			if !ok {
				break reading
			}
			if input.err != nil {
				r.mu.Lock()
				r.problem = input.err
				r.mu.Unlock()
				break reading
			}
			message := input.frame
			if message.Type == "control_response" {
				r.mu.Lock()
				if message.Response.RequestID == r.interruptID && message.Response.Subtype == "success" {
					r.interruptAcknowledged = true
				}
				r.mu.Unlock()
				r.controls.Route(message.Response.RequestID, message.Response)
				continue
			}
			if message.Type == "control_request" {
				r.permissionRequest(message, input.size)
				continue
			}
			if message.Type == "control_cancel_request" {
				r.cancelRequest(message.RequestID)
				continue
			}
			if message.SessionID != "" && message.SessionID != r.session.ID && r.identity.ID != "" {
				continue
			}
			if message.Type == "result" {
				r.mu.Lock()
				if r.result == nil {
					copy := message
					r.result = &copy
				}
				ordinary := !r.localCommand && !r.interrupted && len(r.background) == 0 && message.Subtype == "success" && !message.IsError && message.TerminalReason == "completed" && r.problem == nil
				r.reusable = ordinary
				r.mu.Unlock()
				if ordinary {
					r.finish(nil)
					if !r.reusable {
						_ = r.process.Stdin.Close()
						time.AfterFunc(2*time.Second, func() { _ = root.process.Close() })
					}
				} else {
					_ = r.process.Stdin.Close()
					time.AfterFunc(2*time.Second, func() { _ = root.process.Close() })
				}
				continue
			}
			if message.Type == "system" && message.Subtype == "compact_boundary" {
				r.compactBoundary = true
				r.mu.Lock()
				manual := r.compactRequested
				r.mu.Unlock()
				itemID := message.UUID
				if manual || itemID == "" {
					itemID = "compact-" + string(r.identity.ID)
				}
				r.item("item/completed", map[string]any{"id": itemID, "type": "contextCompaction"})
			}
			r.project(message)
		}
	}
	close(stopScan)
	_ = root.process.Stdin.Close()
	_ = root.process.Stdout.Close()
	<-scanned
	timer := time.AfterFunc(2*time.Second, func() { _ = root.process.Close() })
	exitErr := root.process.Wait()
	timer.Stop()
	r.finish(exitErr)
	if r.reusable && exitErr == nil {
		r.refreshIdleDigest()
	}
	r.client.mu.Lock()
	if r.session.idle == r {
		r.session.idle = nil
		if r.idleTimer != nil {
			r.idleTimer.Stop()
		}
	}
	if r.session.retiring == r {
		r.session.retiring = nil
	}
	delete(r.client.executions, root)
	r.client.mu.Unlock()
	close(root.done)
}

func (c *Client) Send(ctx context.Context, id, text string) (agent.Turn, error) {
	return c.SendWithModel(ctx, id, text, nil, nil, "")
}

func (c *Client) SendWithModel(ctx context.Context, id, text string, skills []agent.Skill, images []attachment.Image, model string) (agent.Turn, error) {
	return c.send(ctx, id, text, skills, images, model, "", "")
}

// SendWithOptions passes the per-send model and effort picks to the CLI.
// Claude reports no service tiers, so TurnOptions.ServiceTier is ignored.
func (c *Client) SendWithOptions(ctx context.Context, id, text string, skills []agent.Skill, images []attachment.Image, options agent.TurnOptions) (agent.Turn, error) {
	return c.send(ctx, id, text, skills, images, options.Model, options.Effort, options.Mode)
}

func (c *Client) send(ctx context.Context, id, text string, skills []agent.Skill, images []attachment.Image, model, effort string, mode agent.InteractionMode) (turn agent.Turn, sendErr error) {
	attempted := false
	defer func() {
		if sendErr != nil && !attempted {
			sendErr = &agent.NotSubmittedError{Err: sendErr}
		}
	}()
	if !mode.Valid() {
		return turn, fmt.Errorf("无效的协作模式：%s", mode)
	}
	if len(skills) > 0 {
		return turn, fmt.Errorf("Claude 命令应使用 /命令名 输入")
	}
	content := []any{}
	for _, image := range images {
		raw, err := attachment.Read(image)
		if err != nil {
			return turn, err
		}
		content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": http.DetectContentType(raw), "data": base64.StdEncoding.EncodeToString(raw)}})
	}
	content = append(content, map[string]any{"type": "text", "text": text})
	encoded, err := json.Marshal(content)
	if err != nil || len(encoded) > agent.MaxFrameBytes-1024 {
		return turn, fmt.Errorf("Claude 消息和图片超过 16 MiB 协议上限，请减少附件")
	}
	c.mu.Lock()
	s := c.sessions[id]
	if c.closed || c.failed || s == nil {
		c.mu.Unlock()
		return turn, fmt.Errorf("Claude 会话不存在或已关闭")
	}
	if s.run != nil || len(s.Turns) > 0 && (s.Turns[len(s.Turns)-1].Status == "inProgress" || s.Turns[len(s.Turns)-1].Status == "unknown") {
		c.mu.Unlock()
		return turn, fmt.Errorf("Claude 旧任务尚待核验，不能重复发送")
	}
	if len(s.Turns) > 0 {
		digest, err := c.transcriptDigest(s.ID)
		if s.ResumeSource != "" {
			_, digest, err = c.history(*s)
		}
		source := s.idle
		if source == nil {
			source = s.retiring
		}
		if err == nil && digest != s.Digest && source != nil {
			if projection, projectionErr := c.transcriptProjection(s.ID); projectionErr == nil && projection == source.projectionDigest {
				s.Digest = digest
			}
		}
		if err != nil || digest != s.Digest {
			c.mu.Unlock()
			return turn, fmt.Errorf("Claude 原生记录已变化，请先重新核验历史")
		}
	}
	s.Generation++
	identity, err := c.turns.Begin(agent.SessionKey{Agent: agent.Claude, Runtime: c.home, SessionID: id}, s.Generation)
	if err != nil {
		c.mu.Unlock()
		return turn, err
	}
	s.Turns = append(s.Turns, checkpoint{ID: string(identity.ID), UserUUID: identity.UserUUID, Status: "inProgress"})
	if strings.HasPrefix(strings.TrimSpace(text), "/") && len(text) <= 4096 {
		s.Turns[len(s.Turns)-1].Command = text
	}
	if s.Title == "" {
		s.Title = strings.TrimSpace(text)
		if len([]rune(s.Title)) > 60 {
			s.Title = string([]rune(s.Title)[:60])
		}
	}
	if model != "" {
		s.Model = model
	}
	if err := c.save(s); err != nil {
		s.Turns = s.Turns[:len(s.Turns)-1]
		c.turns.Complete(identity.Session, identity.Generation, identity.ID)
		c.mu.Unlock()
		return turn, err
	}
	model = s.Model
	idle := s.idle
	retiring := s.retiring
	s.idle = nil
	s.retiring = nil
	if idle != nil && idle.idleTimer != nil {
		idle.idleTimer.Stop()
	}
	c.mu.Unlock()
	if retiring != nil {
		retiring.stopProcess()
	}
	var run *execution
	if idle != nil {
		if idle.connection.model == model && idle.connection.effort == effort && idle.connection.config == c.processConfiguration(s.Cwd) {
			run, err = idle.continueWith(ctx, identity)
		} else {
			idle.stopProcess()
		}
	}
	if run == nil && err == nil {
		run, _, err = c.spawn(ctx, s, identity, model, effort)
	}
	if err == nil {
		err = run.setMode(ctx, mode)
		if err != nil {
			run.stopProcess()
		}
	}
	if err == nil && ctx.Err() != nil {
		run.stopProcess()
		err = ctx.Err()
	}
	if err != nil {
		c.mu.Lock()
		if s.run == nil && len(s.Turns) > 0 && s.Turns[len(s.Turns)-1].ID == string(identity.ID) {
			s.Turns = s.Turns[:len(s.Turns)-1]
			c.turns.Complete(identity.Session, identity.Generation, identity.ID)
			if saveErr := c.save(s); saveErr != nil {
				err = fmt.Errorf("%w；保存未发送状态：%v", err, saveErr)
			}
		}
		c.mu.Unlock()
		return turn, err
	}
	run.emit("turn/started", map[string]any{"threadId": id, "turn": map[string]any{"id": identity.ID, "status": "inProgress"}})
	command := strings.Fields(text)
	compactRequested := len(command) > 0 && command[0] == "/compact"
	run.mu.Lock()
	run.localCommand = strings.HasPrefix(strings.TrimSpace(text), "/")
	run.compactRequested = compactRequested
	run.userAttempted = true
	run.mu.Unlock()
	if compactRequested {
		run.item("item/started", map[string]any{"id": "compact-" + string(identity.ID), "type": "contextCompaction"})
	}
	attempted = true
	if err := run.write(map[string]any{"type": "user", "uuid": identity.UserUUID, "session_id": id, "parent_tool_use_id": nil, "message": map[string]any{"role": "user", "content": content}}); err != nil {
		run.stopProcess()
		return turn, err
	}
	run.emit("item/completed", map[string]any{"threadId": id, "turnId": identity.ID, "item": map[string]any{"id": identity.UserUUID, "type": "userMessage", "content": content}})
	turn.ID, turn.Status = string(identity.ID), "inProgress"
	return turn, nil
}

func (c *Client) Interrupt(ctx context.Context, id, turnID string) error {
	c.mu.Lock()
	s := c.sessions[id]
	var run *execution
	if s != nil {
		run = s.run
	}
	c.mu.Unlock()
	if run == nil || string(run.identity.ID) != turnID {
		return fmt.Errorf("Claude 当前任务标识已失效")
	}
	run.mu.Lock()
	run.interrupted = true
	run.mu.Unlock()
	_, err := run.control(ctx, map[string]any{"subtype": "interrupt"})
	if err != nil {
		go run.stopProcess()
		return err
	}
	time.AfterFunc(5*time.Second, func() {
		select {
		case <-run.turnDone:
			return
		default:
			_ = run.process.Close()
		}
	})
	return nil
}

func (r *execution) finish(exitErr error) {
	r.finishOnce.Do(func() {
		r.finishTurn(exitErr)
		if r.turnDone != nil {
			close(r.turnDone)
		}
	})
}

func (r *execution) finishTurn(exitErr error) {
	if r.identity.ID == "" {
		return
	}
	r.mu.Lock()
	attempted := r.userAttempted
	result, problem, interrupted, acknowledged, compactRequested := r.result, r.problem, r.interrupted, r.interruptAcknowledged, r.compactRequested
	r.mu.Unlock()
	if !attempted {
		r.client.mu.Lock()
		if r.session.run == r {
			r.session.run = nil
		}
		r.client.mu.Unlock()
		return
	}
	status := "unknown"
	localCommandCompleted := false
	if result != nil && problem == nil && result.Subtype == "success" && !result.IsError && result.TerminalReason == "" && exitErr == nil {
		if saved, err := r.client.sessionCopy(r.session.ID); err == nil && len(saved.Turns) > 0 {
			saved.Turns[len(saved.Turns)-1].LastUUID = r.lastUUID
			_, localCommandCompleted = r.client.localCommandCompletion(saved, "")
		}
	}
	if result != nil && problem == nil {
		switch {
		case result.TerminalReason == "interrupted" || result.Subtype == "error_interrupted" || acknowledged && (result.TerminalReason == "aborted_tools" || result.TerminalReason == "aborted_streaming"):
			status = "interrupted"
		case result.Subtype == "success" && !result.IsError && result.TerminalReason == "completed" && exitErr == nil:
			status = "completed"
		case result.Subtype == "success" && !result.IsError && result.TerminalReason == "" && compactRequested && r.compactBoundary && exitErr == nil:
			status = "completed"
		case localCommandCompleted:
			status = "completed"
		case result.IsError || strings.HasPrefix(result.Subtype, "error_"):
			status = "failed"
			problem = fmt.Errorf("Claude 任务失败：%s，terminal_reason=%s，%s", result.Subtype, result.TerminalReason, strings.Join(result.Errors, "; "))
		}
	}
	if status == "unknown" && problem == nil {
		if result != nil {
			problem = fmt.Errorf("Claude 原生终态未确认：subtype=%s, is_error=%t, terminal_reason=%s；进程结果=%v", result.Subtype, result.IsError, result.TerminalReason, exitErr)
		} else {
			problem = fmt.Errorf("Claude 进程退出但原生任务终态未确认；停止请求=%t，进程结果=%v", interrupted, exitErr)
		}
	}
	if problem != nil && r.diagnostics != nil {
		if hint := r.diagnostics.hint(); hint != "" {
			problem = fmt.Errorf("%s；%w", hint, problem)
		}
	}
	r.mu.Lock()
	var requests []string
	for id := range r.pending {
		requests = append(requests, id)
	}
	r.mu.Unlock()
	for _, id := range requests {
		r.cancelRequest(id)
	}
	c := r.client
	c.mu.Lock()
	s := r.session
	if len(s.Turns) > 0 && s.Turns[len(s.Turns)-1].ID == string(r.identity.ID) {
		checkpoint := &s.Turns[len(s.Turns)-1]
		checkpoint.Status, checkpoint.LastUUID = status, r.lastUUID
		s.Digest, _ = c.transcriptDigest(s.ID)
		if s.Digest != "" {
			s.ResumeSource, s.ResumeAt, s.ResumeDigest = "", "", ""
		}
		if r.reusable {
			r.projectionDigest, _ = c.transcriptProjection(s.ID)
		}
		if err := c.save(s); err != nil {
			status = "unknown"
			checkpoint.Status = status
			problem = fmt.Errorf("保存 Claude 终态失败：%w", err)
		}
	}
	if s.run == r {
		s.run = nil
	}
	r.reusable = r.reusable && status == "completed" && problem == nil
	if r.reusable && !c.closed && !c.failed {
		s.idle = r
		r.idleSince = time.Now()
		r.idleTimer = time.AfterFunc(sessionIdleTimeout, r.expireIdle)
		c.trimIdleProcesses()
	}
	c.mu.Unlock()
	c.turns.Complete(r.identity.Session, r.identity.Generation, r.identity.ID)
	turn := map[string]any{"id": r.identity.ID, "status": status}
	if status == "unknown" {
		turn["status"] = "failed"
	}
	if problem != nil {
		turn["error"] = map[string]any{"message": problem.Error()}
	}
	r.emit("turn/completed", map[string]any{"threadId": s.ID, "turn": turn})
	if status == "unknown" {
		raw, _ := json.Marshal(map[string]any{"threadId": s.ID})
		if !c.queue.push(agent.Event{Session: r.identity.Session, Generation: r.identity.Generation, Method: "sasa/disconnected", Params: raw, Err: problem}) {
			go c.failQueue()
		}
	}
}
