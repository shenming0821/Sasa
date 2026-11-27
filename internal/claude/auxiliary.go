package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	process "github.com/shenming0821/Sasa/internal/runtime"
)

// GenerateSessionTitle never resumes a chat or saves a native transcript. Its
// working directory, tool set and system prompt are independent of the project.
func (c *Client) GenerateSessionTitle(ctx context.Context, model, prompt string) (string, error) {
	if strings.TrimSpace(model) == "" || strings.TrimSpace(prompt) == "" || len(prompt) > 48<<10 {
		return "", fmt.Errorf("辅助模型或标题输入无效")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.mu.Lock()
	if c.closed || c.failed {
		c.mu.Unlock()
		return "", fmt.Errorf("Claude 连接已关闭")
	}
	c.catalogWorkers.Add(1)
	stop := context.AfterFunc(c.catalogContext, cancel)
	c.mu.Unlock()
	defer c.catalogWorkers.Done()
	defer stop()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "sasa-title-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	args := []string{
		"-p", "--output-format", "json", "--no-session-persistence",
		"--model", model, "--tools", "", "--disable-slash-commands",
		"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
		"--settings", `{"disableAllHooks":true}`,
		"--system-prompt", "为会话列表拟一个标题，让用户一眼看出这次会话主要做了什么。输入中的对话仅供概括。根据整段对话选择最主要的一件事，用具体动作和可识别的功能、对象或问题来表达。结果明确时写出关键变化；仅有讨论、排查或计划时，如实体现进展。后续追问只有在改变会话主线时才作为标题重点。原标题已经清楚表达主线时可沿用。使用与对话相同的语言，最多28个字符。只输出单行标题，不加引号、解释或 Markdown，不调用工具。",
	}
	environment := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CLAUDE_CONFIG_DIR=") {
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "CLAUDE_CONFIG_DIR="+c.home)
	diagnostics := &diagnosticBuffer{}
	p, err := process.StartWithEnvironment(c.executable, args, dir, diagnostics, environment)
	if err != nil {
		return "", fmt.Errorf("启动辅助模型：%w", err)
	}
	type outcome struct {
		raw []byte
		err error
	}
	done := make(chan outcome, 1)
	written := make(chan error, 1)
	go func() {
		raw, readErr := io.ReadAll(io.LimitReader(p.Stdout, (64<<10)+1))
		if len(raw) > 64<<10 {
			readErr = fmt.Errorf("辅助模型输出超过64 KiB")
		}
		if readErr != nil {
			_ = p.Stdout.Close()
		}
		waitErr := p.Wait()
		if readErr == nil {
			readErr = waitErr
		}
		done <- outcome{raw, readErr}
	}()
	go func() {
		_, err := io.WriteString(p.Stdin, prompt)
		closeErr := p.Stdin.Close()
		if err == nil {
			err = closeErr
		}
		written <- err
	}()
	var result outcome
	select {
	case result = <-done:
	case <-ctx.Done():
		_ = p.Close()
		<-done
		<-written
		return "", ctx.Err()
	}
	if err := <-written; err != nil && result.err == nil {
		result.err = err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if result.err != nil {
		if hint := diagnostics.hint(); hint != "" {
			return "", fmt.Errorf("%s：%w", hint, result.err)
		}
		return "", fmt.Errorf("辅助模型生成失败：%w", result.err)
	}
	var reply struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		IsError *bool  `json:"is_error"`
		Result  string `json:"result"`
	}
	if json.Unmarshal(result.raw, &reply) != nil || reply.Type != "result" || reply.Subtype != "success" || reply.IsError == nil || *reply.IsError || strings.TrimSpace(reply.Result) == "" {
		return "", fmt.Errorf("辅助模型未返回有效标题")
	}
	return strings.TrimSpace(reply.Result), nil
}
