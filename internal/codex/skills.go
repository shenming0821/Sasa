package codex

import (
	"context"

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
	var result struct {
		Turn Turn `json:"turn"`
	}
	params := map[string]any{"threadId": threadID, "input": input}
	if model != "" {
		params["model"] = model
	}
	err := c.Call(ctx, "turn/start", params, &result)
	return result.Turn, err
}
