package claude

import (
	"sync"

	"flint/internal/agentsession"
)

// queued is one event waiting for delivery; after (optional) runs once the
// sink has returned.
type queued struct {
	ev    agentsession.Event
	after func()
}

// eventQueue delivers events to a sink sequentially, in push order, from one
// goroutine. Pushing never blocks, so the sink may call back into the driver
// (Respond, Cancel) without deadlocking the stdout reader.
type eventQueue struct {
	mu     sync.Mutex
	items  []queued
	closed bool
	wake   chan struct{}
	done   chan struct{}
}

func newEventQueue(sink agentsession.Sink) *eventQueue {
	q := &eventQueue{wake: make(chan struct{}, 1), done: make(chan struct{})}
	go q.run(sink)
	return q
}

// push enqueues ev; it reports false (and drops ev) after close.
func (q *eventQueue) push(ev agentsession.Event, after func()) bool {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return false
	}
	q.items = append(q.items, queued{ev: ev, after: after})
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return true
}

// close stops accepting events; already queued ones are still delivered.
func (q *eventQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// drained is closed once the queue is closed and empty.
func (q *eventQueue) drained() <-chan struct{} { return q.done }

func (q *eventQueue) run(sink agentsession.Sink) {
	defer close(q.done)
	for {
		q.mu.Lock()
		batch := q.items
		q.items = nil
		closed := q.closed
		q.mu.Unlock()
		for _, it := range batch {
			sink(it.ev)
			if it.after != nil {
				it.after()
			}
		}
		if len(batch) > 0 {
			continue
		}
		if closed {
			return
		}
		<-q.wake
	}
}
