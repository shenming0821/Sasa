package agent

import "encoding/json"

type Thread struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
	Model          string          `json:"model"`
	Cwd            string          `json:"cwd"`
	ApprovalPolicy json.RawMessage `json:"approvalPolicy"`
	Sandbox        json.RawMessage `json:"sandbox"`
}

type Turn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}
