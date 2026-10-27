package codex

import (
	"context"
	"fmt"
)

type Model struct {
	ID          string `json:"id"`
	Model       string `json:"model"`
	DisplayName string `json:"displayName"`
	Hidden      bool   `json:"hidden"`
	IsDefault   bool   `json:"isDefault"`
}

func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
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
	models, err := c.ListModels(ctx)
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
