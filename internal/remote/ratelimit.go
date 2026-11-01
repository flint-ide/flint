package remote

import (
	"sync"
	"time"
)

// limiter is a token bucket per key (a device id, or anonKey for requests
// without a valid token). Keys are bounded by the device limit.
type limiter struct {
	now func() time.Time

	mu      sync.Mutex
	perMin  int
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// anonKey shares one budget among requests without a valid token: through a
// tunnel they all come from 127.0.0.1, so there is no address to tell them by.
const anonKey = ""

// pairKey is the budget of pairing attempts (POST /api/pair with JSON).
const pairKey = "\x00pair"

// Fixed budgets (security limits): tokens of unknown devices or wrong
// secrets, and pairing attempts (a pairing secret also dies after five wrong
// tries). Requests without any token are charged nothing.
const (
	anonPerMinute = 20
	anonBurst     = 20
	pairPerMinute = 10
	pairBurst     = 10
)

func newLimiter(perMin int) *limiter {
	return &limiter{now: time.Now, perMin: perMin, buckets: map[string]*bucket{}}
}

// setRate changes the per-device rate; buckets keep their tokens.
func (l *limiter) setRate(perMin int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.perMin = perMin
}

// burst lets a client catch up after connecting (chats, tasks, the stream)
// without waiting: a sixth of a minute's budget, at least 5.
func burst(perMin int) float64 { return float64(max(5, perMin/6)) }

// allow takes one token from key's bucket.
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	rate, capacity := l.perMin, burst(l.perMin)
	switch key {
	case anonKey:
		rate, capacity = anonPerMinute, anonBurst
	case pairKey:
		rate, capacity = pairPerMinute, pairBurst
	}
	now := l.now()
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: capacity, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(capacity, b.tokens+now.Sub(b.last).Minutes()*float64(rate))
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// forget drops a revoked device's bucket.
func (l *limiter) forget(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}
