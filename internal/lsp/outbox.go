package lsp

import (
	"context"
	"sync"
)

// outbox writes notifications to a server in FIFO order on its own goroutine, so
// callers never block on a full pipe (a busy Node server can stall stdin reads)
// while holding the manager's lock. Requests wait on a barrier so they reach the
// server after every edit queued before them.
type outbox struct {
	conn *conn

	mu     sync.Mutex
	cond   *sync.Cond
	queue  []outMsg
	closed bool
}

type outMsg struct {
	method string
	params any
	done   chan struct{} // barrier when non-nil
}

func newOutbox(c *conn) *outbox {
	o := &outbox{conn: c}
	o.cond = sync.NewCond(&o.mu)
	go o.run()
	go func() {
		<-c.done
		o.mu.Lock()
		o.closed = true
		o.mu.Unlock()
		o.cond.Broadcast()
	}()
	return o
}

// notify queues a notification; it never blocks.
func (o *outbox) notify(method string, params any) {
	o.push(outMsg{method: method, params: params})
}

// barrier waits until everything queued before it has been written.
func (o *outbox) barrier(ctx context.Context) error {
	done := make(chan struct{})
	o.push(outMsg{done: done})
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-o.conn.done:
		return ErrClosed
	}
}

func (o *outbox) push(m outMsg) {
	o.mu.Lock()
	o.queue = append(o.queue, m)
	o.mu.Unlock()
	o.cond.Signal()
}

func (o *outbox) run() {
	for {
		o.mu.Lock()
		for len(o.queue) == 0 && !o.closed {
			o.cond.Wait()
		}
		if o.closed {
			o.mu.Unlock()
			return
		}
		batch := o.queue
		o.queue = nil
		o.mu.Unlock()
		for _, m := range batch {
			if m.done != nil {
				close(m.done)
				continue
			}
			_ = o.conn.Notify(m.method, m.params)
		}
	}
}
