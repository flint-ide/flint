package completions

import (
	"context"
	"sync"
)

// maxTracked bounds the editors Latest remembers; past it every tracked
// request is cancelled (an editor that keeps typing simply asks again).
const maxTracked = 64

// Latest keeps one request per editor: starting a request cancels the
// editor's previous one, which is stale once the user typed on.
type Latest struct {
	mu       sync.Mutex
	inflight map[string]*entry
}

type entry struct{ cancel context.CancelFunc }

// Start returns the context of key's new request and the function that ends
// it; the request before it for key is cancelled.
func (l *Latest) Start(parent context.Context, key string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	e := &entry{cancel: cancel}
	l.mu.Lock()
	if l.inflight == nil {
		l.inflight = map[string]*entry{}
	}
	if prev, ok := l.inflight[key]; ok {
		prev.cancel()
	} else if len(l.inflight) >= maxTracked {
		for k, other := range l.inflight {
			other.cancel()
			delete(l.inflight, k)
		}
	}
	l.inflight[key] = e
	l.mu.Unlock()
	return ctx, func() {
		cancel()
		l.mu.Lock()
		if l.inflight[key] == e {
			delete(l.inflight, key)
		}
		l.mu.Unlock()
	}
}

// Cancel ends key's request, if any (the editor closed or the user moved on).
func (l *Latest) Cancel(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.inflight[key]; ok {
		e.cancel()
		delete(l.inflight, key)
	}
}

// Len is the number of requests in flight.
func (l *Latest) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.inflight)
}
