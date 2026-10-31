package github

import (
	"sync"
	"time"
)

// cache keeps answers for a short while, so switching tabs or reopening the view
// doesn't ask GitHub again. Errors are never cached.
type cache[T any] struct {
	ttl time.Duration
	max int
	now func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry[T]
}

type cacheEntry[T any] struct {
	value T
	at    time.Time
}

func newCache[T any](ttl time.Duration, max int) *cache[T] {
	return &cache[T]{ttl: ttl, max: max, now: time.Now, entries: map[string]cacheEntry[T]{}}
}

// get returns the cached value for key, or loads (and caches) a fresh one when it is
// missing, expired or force is set.
func (c *cache[T]) get(key string, force bool, load func() (T, error)) (T, error) {
	if !force {
		c.mu.Lock()
		e, ok := c.entries[key]
		c.mu.Unlock()
		if ok && c.now().Sub(e.at) < c.ttl {
			return e.value, nil
		}
	}
	v, err := load()
	if err != nil {
		return v, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		c.evict()
	}
	c.entries[key] = cacheEntry[T]{value: v, at: c.now()}
	return v, nil
}

// evict drops expired entries, or the oldest one when all are fresh. Called with mu held.
func (c *cache[T]) evict() {
	oldest, at := "", time.Time{}
	for k, e := range c.entries {
		if c.now().Sub(e.at) >= c.ttl {
			delete(c.entries, k)
			continue
		}
		if oldest == "" || e.at.Before(at) {
			oldest, at = k, e.at
		}
	}
	if len(c.entries) >= c.max && oldest != "" {
		delete(c.entries, oldest)
	}
}

func (c *cache[T]) drop(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

func (c *cache[T]) clear() {
	c.mu.Lock()
	c.entries = map[string]cacheEntry[T]{}
	c.mu.Unlock()
}
