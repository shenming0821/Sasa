package codex

import "context"

type Skill struct {
	Name             string `json:"name"`
	Path             string `json:"path"`
	Description      string `json:"description"`
	ShortDescription string `json:"shortDescription"`
	Enabled          bool   `json:"enabled"`
	Interface        *struct {
		DisplayName      string `json:"displayName"`
		ShortDescription string `json:"shortDescription"`
	} `json:"interface"`
}

type SkillsEntry struct {
	Cwd    string  `json:"cwd"`
	Skills []Skill `json:"skills"`
	Errors []struct {
		Path    string `json:"path"`
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *Client) ListSkills(ctx context.Context, cwd string) ([]SkillsEntry, error) {
	var result struct {
		Data []SkillsEntry `json:"data"`
	}
	err := c.Call(ctx, "skills/list", map[string]any{"cwds": []string{cwd}, "forceReload": true}, &result)
	return result.Data, err
}

func (c *Client) SendWithSkills(ctx context.Context, threadID, text string, skills []Skill) (Turn, error) {
	input := []any{map[string]any{"type": "text", "text": text, "textElements": []any{}}}
	for _, skill := range skills {
		input = append(input, map[string]any{"type": "skill", "name": skill.Name, "path": skill.Path})
	}
	var result struct {
		Turn Turn `json:"turn"`
	}
	err := c.Call(ctx, "turn/start", map[string]any{"threadId": threadID, "input": input}, &result)
	return result.Turn, err
}
