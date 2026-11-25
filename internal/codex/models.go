package codex

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shenming0821/Sasa/internal/agent"
)

type Model = agent.Model

func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := c.process.Cmd
	catalog, err := Start(ctx, cmd.Path, cmd.Dir)
	if err != nil {
		return nil, err
	}
	defer catalog.Close()
	return catalog.readModelCatalog(ctx)
}

func (c *Client) readModelCatalog(ctx context.Context) ([]Model, error) {
	started := time.Now()
	for attempt := 0; attempt < 2; attempt++ {
		models, err := c.listModels(ctx)
		if err != nil || len(models) > 0 {
			return models, err
		}
		if attempt == 0 {
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	if message := c.modelDiagnostics.since(started); message != "" {
		return nil, errors.New(message)
	}
	return nil, errors.New("Codex 未返回可用模型，请刷新模型列表重试")
}

func (c *Client) listModels(ctx context.Context) ([]Model, error) {
	var models []Model
	var cursor *string
	seen := map[string]bool{}
	for {
		var page struct {
			Data       []Model `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := c.Call(ctx, "model/list", map[string]any{"cursor": cursor, "limit": 100, "includeHidden": false}, &page); err != nil {
			return nil, err
		}
		for _, m := range page.Data {
			if !m.Hidden && m.Model != "" {
				models = append(models, m)
			}
		}
		if page.NextCursor == nil {
			return models, nil
		}
		if seen[*page.NextCursor] {
			return nil, fmt.Errorf("模型列表分页未前进")
		}
		seen[*page.NextCursor] = true
		cursor = page.NextCursor
	}
}

func (c *Client) DefaultModel(ctx context.Context, cwd string) (string, error) {
	var result struct {
		Config struct {
			Model string `json:"model"`
		} `json:"config"`
	}
	if err := c.Call(ctx, "config/read", map[string]any{"cwd": cwd, "includeLayers": false}, &result); err != nil {
		return "", err
	}
	if result.Config.Model != "" {
		return result.Config.Model, nil
	}
	models, err := c.readModelCatalog(ctx)
	if err != nil {
		return "", err
	}
	for _, m := range models {
		if m.IsDefault {
			return m.Model, nil
		}
	}
	return "", fmt.Errorf("当前配置未提供默认模型")
}
