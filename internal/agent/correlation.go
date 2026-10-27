package agent

import "sync"

// RequestTable routes replies without waiting for a user or event consumer.
// Its owner removes entries on completion, timeout or connection shutdown.
type RequestTable[T any] struct {
	mu      sync.Mutex
	pending map[string]chan T
}

func (t *RequestTable[T]) Register(id string) (<-chan T, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if id == "" || t.pending[id] != nil {
		return nil, false
	}
	if t.pending == nil {
		t.pending = make(map[string]chan T)
	}
	ch := make(chan T, 1)
	t.pending[id] = ch
	return ch, true
}

func (t *RequestTable[T]) Remove(id string) {
	t.mu.Lock()
	delete(t.pending, id)
	t.mu.Unlock()
}

func (t *RequestTable[T]) Route(id string, value T) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	ch := t.pending[id]
	if ch == nil {
		return false
	}
	select {
	case ch <- value:
		return true
	default:
		return false
	}
}
