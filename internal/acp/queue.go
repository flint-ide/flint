package acp

import "sync"

// msgQueue is an unbounded FIFO between the stdout reader and the
// dispatcher. It lets the reader keep draining the agent's stdout even while
// a slow Handler.SessionUpdate is running, so the agent never blocks on a
// full pipe while we are blocked writing to it.
type msgQueue struct {
	mu     sync.Mutex
	items  []*inboundMessage
	closed bool
	signal chan struct{} // capacity 1: "something changed"
}

func newMsgQueue() *msgQueue {
	return &msgQueue{signal: make(chan struct{}, 1)}
}

// push appends a message; it is a no-op after close.
func (q *msgQueue) push(m *inboundMessage) {
	q.mu.Lock()
	if !q.closed {
		q.items = append(q.items, m)
	}
	q.mu.Unlock()
	q.wake()
}

// close marks the end of input; pop drains remaining items then reports done.
func (q *msgQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.wake()
}

// pop blocks until a message is available or the queue is closed and empty.
func (q *msgQueue) pop() (*inboundMessage, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			m := q.items[0]
			q.items[0] = nil
			q.items = q.items[1:]
			q.mu.Unlock()
			return m, true
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return nil, false
		}
		<-q.signal
	}
}

func (q *msgQueue) wake() {
	select {
	case q.signal <- struct{}{}:
	default:
	}
}
