package agent

import "encoding/json"

// NormalizeUpdatedAtMillis returns the timestamp in milliseconds or nil when
// the value is absent or implausible. Native backends report either seconds
// or milliseconds; a threshold at year 2001 in milliseconds separates them.
func NormalizeUpdatedAtMillis(stamp *int64) *int64 {
	if stamp == nil || *stamp <= 0 {
		return nil
	}
	value := *stamp
	if value < 978307200000 {
		value *= 1000
	}
	return &value
}

type HistoryTurn struct {
	ID        string            `json:"id"`
	Status    string            `json:"status"`
	ItemsView string            `json:"itemsView"`
	Items     []json.RawMessage `json:"items"`
	Error     json.RawMessage   `json:"error"`
	StartedAt *int64            `json:"startedAt,omitempty"`
}

type ThreadMeta struct {
	Agent     AgentKind     `json:"-"`
	Runtime   string        `json:"-"`
	ID        string        `json:"id"`
	Cwd       string        `json:"cwd"`
	Preview   string        `json:"preview"`
	Name      *string       `json:"name"`
	Status    ThreadStatus  `json:"status"`
	UpdatedAt *int64        `json:"updatedAt,omitempty"`
	Turns     []HistoryTurn `json:"turns"`
}

type ThreadStatus struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags"`
}

type ResumedThread struct {
	ThreadMeta     ThreadMeta      `json:"thread"`
	Model          string          `json:"model"`
	Cwd            string          `json:"cwd"`
	Policy         json.RawMessage `json:"approvalPolicy"`
	Sandbox        json.RawMessage `json:"sandbox"`
	RuntimeVersion string          `json:"runtimeVersion,omitempty"`
}

type TurnsPage struct {
	Data       []HistoryTurn `json:"data"`
	NextCursor *string       `json:"nextCursor"`
}

type ThreadsPage struct {
	Data       []ThreadMeta `json:"data"`
	NextCursor *string      `json:"nextCursor"`
}
