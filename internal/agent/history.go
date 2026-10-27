package agent

import "encoding/json"

type HistoryTurn struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	ItemsView string            `json:"itemsView"`
	Items     []json.RawMessage `json:"items"`
	Error     json.RawMessage   `json:"error"`
}

type ThreadMeta struct {
	ID      string        `json:"id"`
	Cwd     string        `json:"cwd"`
	Preview string        `json:"preview"`
	Name    *string       `json:"name"`
	Status  ThreadStatus  `json:"status"`
	Turns   []HistoryTurn `json:"turns"`
}

type ThreadStatus struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags"`
}

type ResumedThread struct {
	ThreadMeta ThreadMeta      `json:"thread"`
	Model      string          `json:"model"`
	Cwd        string          `json:"cwd"`
	Policy     json.RawMessage `json:"approvalPolicy"`
	Sandbox    json.RawMessage `json:"sandbox"`
}

type TurnsPage struct {
	Data       []HistoryTurn `json:"data"`
	NextCursor *string       `json:"nextCursor"`
}

type ThreadsPage struct {
	Data       []ThreadMeta `json:"data"`
	NextCursor *string      `json:"nextCursor"`
}
