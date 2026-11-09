package devin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/shenming0821/Fuyu/internal/agent"
)

// parseConfigOptions reads ACP select-typed config options into a lookup by
// option id; other types are kept out rather than guessed.
func parseConfigOptions(raw json.RawMessage) map[string]*configOption {
	var options []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Current  string `json:"currentValue"`
		Choices  []struct {
			Value string `json:"value"`
			Name  string `json:"name"`
		} `json:"options"`
	}
	parsed := make(map[string]*configOption)
	if json.Unmarshal(raw, &options) != nil {
		return parsed
	}
	for _, option := range options {
		entry := &configOption{ID: option.ID, Current: option.Current, Choices: make(map[string]string)}
		for _, choice := range option.Choices {
			entry.Choices[choice.Value] = choice.Name
		}
		parsed[option.ID] = entry
	}
	return parsed
}

// optionIDs maps app send options onto Devin session config ids.
var optionIDs = map[string]string{
	"model": "model", "effort": "thought_level", "tier": "speed",
}

// applyOptions validates the per-send picks against the session's advertised
// choices, then writes changed values before the prompt is submitted.
func (c *Client) applyOptions(ctx context.Context, s *acpSession, options agent.TurnOptions) error {
	updates := []struct{ id, value, label string }{}
	for _, pair := range []struct {
		id, value, label string
	}{
		{optionIDs["model"], options.Model, "模型"},
		{optionIDs["effort"], options.Effort, "推理强度"},
		{optionIDs["tier"], options.ServiceTier, "速度档位"},
	} {
		if pair.value == "" {
			continue
		}
		c.mu.Lock()
		option := s.options[pair.id]
		c.mu.Unlock()
		if option == nil {
			return fmt.Errorf("Devin 会话不支持选择%s", pair.label)
		}
		if _, ok := option.Choices[pair.value]; !ok && len(option.Choices) > 0 {
			return fmt.Errorf("Devin 会话不认识%s：%s", pair.label, pair.value)
		}
		if option.Current != pair.value {
			updates = append(updates, pair)
		}
	}
	for _, update := range updates {
		result, err := c.conn.call(ctx, "session/set_config_option", map[string]any{
			"sessionId": s.id, "configId": update.id, "value": update.value,
		})
		if err != nil {
			return fmt.Errorf("设置 Devin %s失败：%w", update.label, err)
		}
		var response struct {
			Options json.RawMessage `json:"configOptions"`
		}
		if json.Unmarshal(result, &response) == nil && len(response.Options) > 0 {
			c.mu.Lock()
			for id, option := range parseConfigOptions(response.Options) {
				s.options[id] = option
			}
			c.mu.Unlock()
		}
	}
	return nil
}

// catalog returns the config options of one promptable session, preferring a
// session in the requested directory so cwd-scoped options are reflected.
func (c *Client) catalog(ctx context.Context, cwd string) (map[string]*configOption, error) {
	c.mu.Lock()
	var selected *acpSession
	for _, s := range c.sessions {
		if s.catalog || !s.loaded || s.loading || len(s.options) == 0 {
			continue
		}
		if selected == nil || s.cwd == cwd {
			selected = s
		}
	}
	ready := !c.closed && !c.failed && c.conn != nil
	link := c.conn
	c.mu.Unlock()
	if selected != nil {
		return selected.options, nil
	}
	if !ready {
		return nil, fmt.Errorf("Devin 连接已关闭")
	}
	// No live session exposes options yet; a probe session carries them. Empty
	// sessions are filtered from history and closed with the client.
	dir := cwd
	if dir == "" {
		var err error
		dir, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	result, err := link.call(ctx, "session/new", map[string]any{"cwd": dir, "mcpServers": []any{}})
	if err != nil {
		return nil, err
	}
	var created struct {
		SessionID     string          `json:"sessionId"`
		ConfigOptions json.RawMessage `json:"configOptions"`
	}
	if json.Unmarshal(result, &created) != nil || created.SessionID == "" {
		return nil, fmt.Errorf("Devin 会话响应不完整")
	}
	options := parseConfigOptions(created.ConfigOptions)
	c.mu.Lock()
	c.sessions[created.SessionID] = &acpSession{id: created.SessionID, cwd: dir, catalog: true, loaded: true, options: options, state: c.loadState(created.SessionID)}
	c.mu.Unlock()
	return options, nil
}

func catalogModels(options map[string]*configOption) []agent.Model {
	model := options["model"]
	if model == nil {
		return nil
	}
	var efforts []agent.ReasoningEffortOption
	if option := options["thought_level"]; option != nil {
		for value, name := range option.Choices {
			efforts = append(efforts, agent.ReasoningEffortOption{ReasoningEffort: value, Description: name})
		}
	}
	var tiers []agent.ModelServiceTier
	if option := options["speed"]; option != nil {
		for value, name := range option.Choices {
			tiers = append(tiers, agent.ModelServiceTier{ID: value, Name: name})
		}
	}
	models := make([]agent.Model, 0, len(model.Choices))
	for value, name := range model.Choices {
		entry := agent.Model{ID: value, Model: value, DisplayName: name, IsDefault: value == model.Current}
		if entry.DisplayName == "" {
			entry.DisplayName = value
		}
		entry.SupportedReasoningEfforts = efforts
		entry.ServiceTiers = tiers
		models = append(models, entry)
	}
	return models
}

func (c *Client) ListModels(ctx context.Context) ([]agent.Model, error) {
	return c.ListModelsForDirectory(ctx, "")
}

func (c *Client) ListModelsForDirectory(ctx context.Context, cwd string) ([]agent.Model, error) {
	options, err := c.catalog(ctx, cwd)
	if err != nil {
		return nil, err
	}
	return catalogModels(options), nil
}

func (c *Client) ReloadModelsForDirectory(ctx context.Context, cwd string) ([]agent.Model, error) {
	return c.ListModelsForDirectory(ctx, cwd)
}

func (c *Client) DefaultModel(ctx context.Context, cwd string) (string, error) {
	options, err := c.catalog(ctx, cwd)
	if err != nil {
		return "", err
	}
	if model := options["model"]; model != nil && model.Current != "" {
		return model.Current, nil
	}
	models := catalogModels(options)
	if len(models) == 0 {
		return "", fmt.Errorf("Devin 未返回可用模型")
	}
	return models[0].Model, nil
}

func (c *Client) ListCommands(ctx context.Context, cwd string) ([]agent.Command, error) {
	c.mu.Lock()
	commands := append([]agent.Command(nil), c.commands...)
	c.mu.Unlock()
	return commands, nil
}
