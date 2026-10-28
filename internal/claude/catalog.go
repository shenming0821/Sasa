package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func (c *Client) catalog(ctx context.Context, cwd string) (json.RawMessage, error) {
	if cwd == "" {
		var err error
		cwd, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	run, raw, err := c.spawn(ctx, &session{Cwd: cwd}, agent.TurnIdentity{}, "")
	if err != nil {
		return nil, err
	}
	run.stopProcess()
	return raw, nil
}

func (c *Client) ListModels(ctx context.Context) ([]agent.Model, error) {
	return c.ListModelsForDirectory(ctx, "")
}

func (c *Client) ListModelsForDirectory(ctx context.Context, cwd string) ([]agent.Model, error) {
	raw, err := c.catalog(ctx, cwd)
	if err != nil {
		return nil, err
	}
	var response struct {
		Models []struct {
			Value    string `json:"value"`
			Name     string `json:"displayName"`
			Resolved string `json:"resolvedModel"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	models := []agent.Model{}
	seen := map[string]bool{}
	for _, model := range response.Models {
		if model.Value == "" || model.Value == "default" || strings.HasSuffix(model.Value, "[1m]") {
			continue
		}
		name := model.Resolved
		if name == "" {
			name = model.Name
		}
		if name == "" {
			name = model.Value
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		models = append(models, agent.Model{ID: model.Value, Model: model.Value, DisplayName: name})
	}
	return models, nil
}

func (c *Client) DefaultModel(ctx context.Context, cwd string) (string, error) {
	models, err := c.ListModelsForDirectory(ctx, cwd)
	if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "", fmt.Errorf("Claude Code 未返回可用的具体模型，请检查模型配置")
	}
	return models[0].Model, nil
}

func (c *Client) ListCommands(ctx context.Context, cwd string) ([]agent.Command, error) {
	raw, err := c.catalog(ctx, cwd)
	if err != nil {
		return nil, err
	}
	var response struct {
		Commands []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Arguments   string `json:"argumentHint"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	commands := []agent.Command{}
	for _, command := range response.Commands {
		commands = append(commands, agent.Command{Name: command.Name, Description: command.Description, Source: "Claude Code", Cwd: cwd, Arguments: []string{command.Arguments}})
	}
	return commands, nil
}
