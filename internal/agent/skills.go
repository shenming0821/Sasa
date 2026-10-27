package agent

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
