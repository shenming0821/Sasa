package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

type TurnID string

// TurnIdentity is allocated before stdin/RPC submission. Native message and
// transcript IDs are separate evidence and may arrive after the send completes.
type TurnIdentity struct {
	Session        SessionKey
	Generation     ProcessGeneration
	ID             TurnID
	UserUUID       string
	NativeID       string
	TranscriptUUID string
	MessageID      string
	Completed      bool
}

type TurnMap struct {
	mu     sync.Mutex
	active map[SessionKey]TurnIdentity
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	raw := hex.EncodeToString(b[:])
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:], nil
}

func (m *TurnMap) Begin(key SessionKey, generation ProcessGeneration) (TurnIdentity, error) {
	key.Agent = key.Agent.Normalized()
	if err := key.Agent.Validate(); err != nil {
		return TurnIdentity{}, err
	}
	if key.SessionID == "" || generation == 0 {
		return TurnIdentity{}, fmt.Errorf("发送前必须确定会话与进程代次")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.active[key]; ok && !old.Completed {
		return TurnIdentity{}, fmt.Errorf("当前会话已有在途发送，须先核验终态")
	}
	id, err := newUUID()
	if err != nil {
		return TurnIdentity{}, err
	}
	user, err := newUUID()
	if err != nil {
		return TurnIdentity{}, err
	}
	turn := TurnIdentity{Session: key, Generation: generation, ID: TurnID(id), UserUUID: user}
	if m.active == nil {
		m.active = make(map[SessionKey]TurnIdentity)
	}
	m.active[key] = turn
	return turn, nil
}

func (m *TurnMap) Bind(turn TurnIdentity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.active[turn.Session]
	if !ok || current.ID != turn.ID || current.Generation != turn.Generation || current.Completed ||
		current.UserUUID != turn.UserUUID || turn.Completed {
		return fmt.Errorf("原生标识不属于当前在途任务")
	}
	if current.NativeID != "" && current.NativeID != turn.NativeID {
		return fmt.Errorf("原生任务标识已绑定")
	}
	m.active[turn.Session] = turn
	return nil
}

func (m *TurnMap) Lookup(key SessionKey, generation ProcessGeneration, id TurnID) (TurnIdentity, bool) {
	key.Agent = key.Agent.Normalized()
	m.mu.Lock()
	defer m.mu.Unlock()
	turn, ok := m.active[key]
	if !ok || turn.Generation != generation || turn.ID != id {
		return TurnIdentity{}, false
	}
	return turn, true
}

func (m *TurnMap) Complete(key SessionKey, generation ProcessGeneration, id TurnID) bool {
	key.Agent = key.Agent.Normalized()
	m.mu.Lock()
	defer m.mu.Unlock()
	turn, ok := m.active[key]
	if !ok || turn.Generation != generation || turn.ID != id || turn.Completed {
		return false
	}
	turn.Completed = true
	m.active[key] = turn
	return true
}
