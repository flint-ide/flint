package codex

import "sync"

// eventQueue delivers a session's events to its Sink on one goroutine, in
// the order they were queued. Producers never block on the Sink, so a Sink
// may call back into the Driver (e.g. an auto-approve policy calling
// Respond) without deadlocking.
type eventQueue struct {
	mu     sync.Mutex
	items  []func()
	closed bool
	signal chan struct{}
	done   chan struct{}
}

func newEventQueue() *eventQueue {
	q := &eventQueue{signal: make(chan struct{}, 1), done: make(chan struct{})}
	go q.run()
	return q
}

// push queues fn; it reports false (and drops fn) after close.
func (q *eventQueue) push(fn func()) bool {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return false
	}
	q.items = append(q.items, fn)
	q.mu.Unlock()
	wake(q.signal)
	return true
}

// close stops accepting work; queued items are still delivered.
func (q *eventQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	wake(q.signal)
}

func (q *eventQueue) run() {
	defer close(q.done)
	for {
		q.mu.Lock()
		if len(q.items) == 0 {
			closed := q.closed
			q.mu.Unlock()
			if closed {
				return
			}
			<-q.signal
			continue
		}
		fn := q.items[0]
		q.items[0] = nil
		q.items = q.items[1:]
		q.mu.Unlock()
		fn()
	}
}
