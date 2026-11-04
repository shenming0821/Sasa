package agent

type ReasoningEffortOption struct {
	ReasoningEffort string `json:"reasoningEffort"`
	Description     string `json:"description"`
}

type ModelServiceTier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Model struct {
	ID                        string                  `json:"id"`
	Model                     string                  `json:"model"`
	DisplayName               string                  `json:"displayName"`
	Hidden                    bool                    `json:"hidden"`
	IsDefault                 bool                    `json:"isDefault"`
	SupportedReasoningEfforts []ReasoningEffortOption `json:"supportedReasoningEfforts"`
	DefaultReasoningEffort    string                  `json:"defaultReasoningEffort"`
	ServiceTiers              []ModelServiceTier      `json:"serviceTiers"`
	DefaultServiceTier        string                  `json:"defaultServiceTier"`
}
