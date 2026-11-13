package codex

import (
	"context"
	"fmt"

	"github.com/shenming0821/Fuyu/internal/agent"
	"github.com/shenming0821/Fuyu/internal/attachment"
)

type Skill = agent.Skill

type SkillsEntry = agent.SkillsEntry

func (c *Client) ListSkills(ctx context.Context, cwd string) ([]SkillsEntry, error) {
	return c.listSkills(ctx, cwd, false)
}

func (c *Client) ReloadSkills(ctx context.Context, cwd string) ([]SkillsEntry, error) {
	return c.listSkills(ctx, cwd, true)
}

func (c *Client) listSkills(ctx context.Context, cwd string, forceReload bool) ([]SkillsEntry, error) {
	var result struct {
		Data []SkillsEntry `json:"data"`
	}
	err := c.Call(ctx, "skills/list", map[string]any{"cwds": []string{cwd}, "forceReload": forceReload}, &result)
	return result.Data, err
}

func (c *Client) SendWithSkills(ctx context.Context, threadID, text string, skills []Skill) (Turn, error) {
	return c.SendWithImages(ctx, threadID, text, skills, nil)
}

func (c *Client) SendWithImages(ctx context.Context, threadID, text string, skills []Skill, images []attachment.Image) (Turn, error) {
	return c.SendWithModel(ctx, threadID, text, skills, images, "")
}

func (c *Client) SendWithModel(ctx context.Context, threadID, text string, skills []Skill, images []attachment.Image, model string) (Turn, error) {
	return c.SendWithOptions(ctx, threadID, text, skills, images, agent.TurnOptions{Model: model})
}

func (c *Client) SendWithOptions(ctx context.Context, threadID, text string, skills []Skill, images []attachment.Image, options agent.TurnOptions) (Turn, error) {
	if !options.Mode.Valid() {
		return Turn{}, &agent.NotSubmittedError{Err: fmt.Errorf("无效的协作模式：%s", options.Mode)}
	}
	input := userInput(text, skills, images)
	var result struct {
		Turn Turn `json:"turn"`
	}
	params := map[string]any{"threadId": threadID, "input": input}
	if options.Model != "" {
		params["model"] = options.Model
	}
	if options.Effort != "" {
		params["effort"] = options.Effort
	}
	if options.ServiceTier != "" {
		params["serviceTierForTurn"] = options.ServiceTier
	}
	if options.Mode != "" {
		model := options.Model
		if model == "" {
			return Turn{}, &agent.NotSubmittedError{Err: fmt.Errorf("协作模式需要已确认的模型，本次未发送")}
		}
		var effort any
		if options.Effort != "" {
			effort = options.Effort
		}
		params["collaborationMode"] = map[string]any{
			"mode":     options.Mode,
			"settings": map[string]any{"model": model, "reasoning_effort": effort, "developer_instructions": nil},
		}
	}
	err := c.Call(ctx, "turn/start", params, &result)
	return result.Turn, err
}

func (c *Client) Steer(ctx context.Context, threadID, turnID, messageID, text string, skills []Skill, images []attachment.Image) error {
	var result struct {
		TurnID string `json:"turnId"`
	}
	err := c.Call(ctx, "turn/steer", map[string]any{
		"threadId": threadID, "expectedTurnId": turnID, "clientUserMessageId": messageID,
		"input": userInput(text, skills, images),
	}, &result)
	if err == nil && result.TurnID != turnID {
		return fmt.Errorf("追加响应的任务标识不匹配：%s", result.TurnID)
	}
	return err
}

func userInput(text string, skills []Skill, images []attachment.Image) []any {
	input := []any{map[string]any{"type": "text", "text": text, "textElements": []any{}}}
	for _, skill := range skills {
		input = append(input, map[string]any{"type": "skill", "name": skill.Name, "path": skill.Path})
	}
	for _, image := range images {
		if image.Path != "" {
			input = append(input, map[string]any{"type": "localImage", "path": image.Path})
		} else {
			input = append(input, map[string]any{"type": "image", "url": image.URL})
		}
	}
	return input
}
