package codex

import (
	"bytes"
	"strings"
	"sync"
	"time"
)

type modelDiagnostics struct {
	mu      sync.Mutex
	partial string
	message string
	checked time.Time
}

func (d *modelDiagnostics) Write(data []byte) (int, error) {
	count := len(data)
	d.mu.Lock()
	defer d.mu.Unlock()
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			end = len(data)
		}
		segment := data[:end]
		if len(segment) > 8192 {
			segment = segment[len(segment)-8192:]
		}
		d.partial += string(segment)
		if len(d.partial) > 8192 {
			d.partial = d.partial[len(d.partial)-8192:]
		}
		if end == len(data) {
			break
		}
		d.record(d.partial)
		d.partial = ""
		data = data[end+1:]
	}
	return count, nil
}

func (d *modelDiagnostics) record(line string) {
	line = strings.ToLower(line)
	if !strings.Contains(line, "codex_models_manager::manager") || !strings.Contains(line, "failed to refresh available models:") {
		return
	}
	_, line, _ = strings.Cut(line, "failed to refresh available models:")
	message := "Codex 原生模型目录刷新失败，请检查模型服务连接后重试"
	switch {
	case strings.Contains(line, "timed out"), strings.Contains(line, "timeout"):
		message = "Codex 模型目录请求超时，请检查模型服务连接后重试"
	case strings.Contains(line, "status 401"), strings.Contains(line, "status 403"), strings.Contains(line, "unauthorized"), strings.Contains(line, "forbidden"):
		message = "Codex 模型服务拒绝访问，请检查登录状态和服务凭据"
	case strings.Contains(line, "connection refused"), strings.Contains(line, "no route to host"), strings.Contains(line, "network is unreachable"):
		message = "Codex 无法连接模型服务，请检查网络和服务地址后重试"
	}
	d.message, d.checked = message, time.Now()
}

func (d *modelDiagnostics) since(started time.Time) string {
	if d == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.checked.Before(started) {
		return ""
	}
	return d.message
}
