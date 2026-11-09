package devin

import (
	"errors"
	"sync"

	"github.com/shenming0821/Fuyu/internal/agent"
)

type eventQueue struct {
	mu       sync.Mutex
	pending  []agent.Event
	bytes    int
	wake     chan struct{}
	done     chan struct{}
	events   chan agent.Event
	closed   bool
	gap      bool
	overflow bool
}

func eventBytes(event agent.Event) int {
	return len(event.Params) + len(event.ID) + len(event.Method) + 128
}

func (q *eventQueue) start() {
	q.wake, q.done, q.events = make(chan struct{}, 1), make(chan struct{}), make(chan agent.Event)
	go func() {
		defer close(q.events)
		for {
			q.mu.Lock()
			if q.gap {
				q.gap = false
				q.mu.Unlock()
				select {
				case q.events <- agent.Event{Method: "fuyu/eventGap"}:
				case <-q.done:
					return
				}
				continue
			}
			if len(q.pending) == 0 {
				q.mu.Unlock()
				select {
				case <-q.wake:
					continue
				case <-q.done:
					return
				}
			}
			event := q.pending[0]
			q.pending[0] = agent.Event{}
			q.pending = q.pending[1:]
			q.bytes -= eventBytes(event)
			q.mu.Unlock()
			select {
			case q.events <- event:
			case <-q.done:
				return
			}
		}
	}()
}

func (q *eventQueue) push(event agent.Event) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.overflow {
		return false
	}
	defer func() {
		select {
		case q.wake <- struct{}{}:
		default:
		}
	}()
	for len(q.pending) >= agent.MaxQueueEvents || q.bytes+eventBytes(event) > agent.MaxQueueBytes {
		drop := -1
		for index, pending := range q.pending {
			if pending.Method == "item/agentMessage/delta" {
				drop = index
				break
			}
		}
		q.gap = true
		if drop < 0 {
			if event.Method == "item/agentMessage/delta" {
				return true
			}
			failure := agent.Event{Method: "fuyu/disconnected", Err: errors.New("Devin 控制事件队列超过容量，结果需要重新核验")}
			q.pending, q.bytes, q.overflow = []agent.Event{failure}, eventBytes(failure), true
			return false
		}
		q.bytes -= eventBytes(q.pending[drop])
		copy(q.pending[drop:], q.pending[drop+1:])
		q.pending[len(q.pending)-1] = agent.Event{}
		q.pending = q.pending[:len(q.pending)-1]
	}
	q.pending = append(q.pending, event)
	q.bytes += eventBytes(event)
	return true
}

func (q *eventQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		close(q.done)
	}
}
