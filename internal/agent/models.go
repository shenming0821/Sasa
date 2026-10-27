package agent

type Model struct {
	ID          string `json:"id"`
	Model       string `json:"model"`
	DisplayName string `json:"displayName"`
	Hidden      bool   `json:"hidden"`
	IsDefault   bool   `json:"isDefault"`
}
