package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/shenming0821/Fuyu/internal/agent"
)

func (c *Client) startCatalog(ctx context.Context, cwd string) (*execution, json.RawMessage, error) {
	if cwd == "" {
		var err error
		cwd, err = os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
	}
	return c.spawn(ctx, &session{Cwd: cwd}, agent.TurnIdentity{}, "", "")
}

func (c *Client) catalog(ctx context.Context, cwd string) (json.RawMessage, error) {
	run, raw, err := c.startCatalog(ctx, cwd)
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
	return c.models(ctx, cwd, false)
}

func (c *Client) ReloadModelsForDirectory(ctx context.Context, cwd string) ([]agent.Model, error) {
	return c.models(ctx, cwd, true)
}

func (c *Client) models(ctx context.Context, cwd string, force bool) ([]agent.Model, error) {
	entry, err := c.directoryCatalog(ctx, cwd, force, true)
	if err != nil {
		return nil, err
	}
	return append([]agent.Model(nil), entry.models...), entry.modelErr
}

func catalogModels(raw json.RawMessage) ([]agent.Model, error) {
	var response struct {
		Models []struct {
			Value    string   `json:"value"`
			Name     string   `json:"displayName"`
			Resolved string   `json:"resolvedModel"`
			Efforts  []string `json:"supportedEffortLevels"`
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
		entry := agent.Model{ID: model.Value, Model: model.Value, DisplayName: name}
		for _, level := range model.Efforts {
			entry.SupportedReasoningEfforts = append(entry.SupportedReasoningEfforts, agent.ReasoningEffortOption{ReasoningEffort: level})
		}
		models = append(models, entry)
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
	return c.commands(ctx, cwd, false)
}

func (c *Client) ReloadCommands(ctx context.Context, cwd string) ([]agent.Command, error) {
	return c.commands(ctx, cwd, true)
}

func (c *Client) commands(ctx context.Context, cwd string, force bool) ([]agent.Command, error) {
	entry, err := c.directoryCatalog(ctx, cwd, force, false)
	if err != nil {
		return nil, err
	}
	commands := append([]agent.Command(nil), entry.commands...)
	for i := range commands {
		commands[i].Arguments = append([]string(nil), commands[i].Arguments...)
		if cwd != "" {
			commands[i].Cwd = cwd
		}
	}
	return commands, entry.commandErr
}

func catalogCommands(ctx context.Context, run *execution, raw json.RawMessage, cwd string) ([]agent.Command, error) {
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
	rawSkills, err := run.control(ctx, map[string]any{"subtype": "get_skills_dialog"})
	if err != nil {
		return nil, fmt.Errorf("读取 Claude 技能目录：%w", err)
	}
	var skills struct {
		Skills []struct {
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
			State       string `json:"state"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(rawSkills, &skills); err != nil {
		return nil, fmt.Errorf("解析 Claude 技能目录：%w", err)
	}
	skillNames := make(map[string]bool)
	for _, skill := range skills.Skills {
		if skill.State != "off" {
			skillNames[skill.Name] = true
			if skill.DisplayName != "" {
				skillNames[skill.DisplayName] = true
			}
		}
	}
	commands := []agent.Command{}
	for _, command := range response.Commands {
		commands = append(commands, agent.Command{Name: command.Name, Description: command.Description, Source: "Claude Code", Cwd: cwd, Arguments: []string{command.Arguments}, IsSkill: skillNames[command.Name]})
	}
	return commands, nil
}
