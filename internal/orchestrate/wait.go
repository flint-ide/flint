package orchestrate

import (
	"context"
	"errors"

	"flint/internal/agenttask"
)

// Wait blocks until the chat is in one of the states, then returns its state.
// It returns at once when the chat already is in one. It does not poll: the
// registry wakes it from Observe. ctx's cancellation or deadline ends the wait
// with ctx's error; removing the chat ends it with ErrRemoved. Whatever the
// outcome, the waiter is gone from the registry when Wait returns.
func (r *Registry) Wait(ctx context.Context, id string, until []string) (agenttask.Live, error) {
	if len(until) == 0 {
		return agenttask.Live{}, errors.New("orchestrate: no states to wait for")
	}
	if err := ctx.Err(); err != nil {
		return agenttask.Live{}, err
	}
	r.mu.Lock()
	e := r.nodes[id]
	if e == nil {
		r.mu.Unlock()
		return agenttask.Live{}, ErrUnknown
	}
	if contains(until, e.node.Live.State) {
		l := e.node.Live
		r.mu.Unlock()
		return l, nil
	}
	if len(e.waiters) >= maxWaiters {
		r.mu.Unlock()
		return agenttask.Live{}, ErrTooManyWaiters
	}
	w := &waiter{until: until, res: make(chan waitResult, 1)}
	e.waiters[w] = struct{}{}
	r.mu.Unlock()

	select {
	case res := <-w.res:
		return res.live, res.err
	case <-ctx.Done():
		r.mu.Lock()
		if cur := r.nodes[id]; cur == e {
			delete(e.waiters, w)
		}
		r.mu.Unlock()
		// A wake-up may have raced with the cancellation; it is still the
		// better answer.
		select {
		case res := <-w.res:
			return res.live, res.err
		default:
		}
		return agenttask.Live{}, ctx.Err()
	}
}
