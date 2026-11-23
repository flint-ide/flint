package orchestrate

import (
	"context"
	"errors"
	"testing"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
)

type waitOut struct {
	live agenttask.Live
	err  error
}

// waitAsync starts Wait and returns once it is registered as a waiter.
func waitAsync(t *testing.T, r *Registry, ctx context.Context, id string, until ...string) <-chan waitOut {
	t.Helper()
	ch := make(chan waitOut, 1)
	go func() {
		l, err := r.Wait(ctx, id, until)
		ch <- waitOut{l, err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for r.waiterCount(id) == 0 {
		select {
		case o := <-ch: // returned without blocking
			ch <- o
			return ch
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("Wait never registered")
		}
		time.Sleep(time.Millisecond)
	}
	return ch
}

func (r *Registry) waiterCount(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.nodes[id]; e != nil {
		return len(e.waiters)
	}
	return 0
}

func result(t *testing.T, ch <-chan waitOut) waitOut {
	t.Helper()
	select {
	case o := <-ch:
		return o
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not return")
		return waitOut{}
	}
}

func TestWaitImmediate(t *testing.T) {
	r := newWith(t, Node{ID: "a"})
	l, err := r.Wait(context.Background(), "a", []string{agenttask.Idle, agenttask.Finished})
	if err != nil || l.State != agenttask.Idle {
		t.Fatalf("got %+v, %v", l, err)
	}
}

func TestWaitLater(t *testing.T) {
	r := newWith(t, Node{ID: "a"})
	r.Prompted("a")
	ch := waitAsync(t, r, context.Background(), "a", agenttask.Finished, agenttask.Failed)
	r.Observe(chunk("a", "x"))
	select {
	case <-ch:
		t.Fatal("returned while working")
	case <-time.After(20 * time.Millisecond):
	}
	r.Observe(turnEnd("a", as.StopEndTurn))
	if o := result(t, ch); o.err != nil || o.live.State != agenttask.Finished {
		t.Fatalf("got %+v", o)
	}
	if n := r.waiterCount("a"); n != 0 {
		t.Errorf("%d waiters left", n)
	}
}

func TestWaitManyWaitersAllWake(t *testing.T) {
	r := newWith(t, Node{ID: "a"})
	r.Prompted("a")
	var chans []<-chan waitOut
	for i := 0; i < 5; i++ {
		chans = append(chans, waitAsync(t, r, context.Background(), "a", agenttask.Waiting))
	}
	r.Observe(ev("a", as.KindPermission))
	for _, ch := range chans {
		if o := result(t, ch); o.err != nil || o.live.State != agenttask.Waiting {
			t.Fatalf("got %+v", o)
		}
	}
}

func TestWaitTimeoutAndCancel(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		stop func(cancel context.CancelFunc)
		want error
	}{
		{"deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 30*time.Millisecond)
		}, func(context.CancelFunc) {}, context.DeadlineExceeded},
		{"cancel", func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		}, func(c context.CancelFunc) { c() }, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newWith(t, Node{ID: "a"})
			r.Prompted("a")
			ctx, cancel := tc.ctx()
			defer cancel()
			ch := waitAsync(t, r, ctx, "a", agenttask.Finished)
			tc.stop(cancel)
			if o := result(t, ch); !errors.Is(o.err, tc.want) {
				t.Fatalf("err = %v, want %v", o.err, tc.want)
			}
			if n := r.waiterCount("a"); n != 0 {
				t.Errorf("%d waiters left", n)
			}
		})
	}
	t.Run("already cancelled", func(t *testing.T) {
		r := newWith(t, Node{ID: "a"})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := r.Wait(ctx, "a", []string{agenttask.Idle}); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestWaitNodeRemovedWhileWaiting(t *testing.T) {
	r := newWith(t, Node{ID: "a"})
	r.Prompted("a")
	ch := waitAsync(t, r, context.Background(), "a", agenttask.Finished)
	r.Remove("a")
	if o := result(t, ch); !errors.Is(o.err, ErrRemoved) {
		t.Fatalf("err = %v", o.err)
	}
}

func TestWaitErrors(t *testing.T) {
	r := newWith(t, Node{ID: "a"})
	r.Prompted("a")
	if _, err := r.Wait(context.Background(), "nope", []string{agenttask.Idle}); !errors.Is(err, ErrUnknown) {
		t.Errorf("unknown: %v", err)
	}
	if _, err := r.Wait(context.Background(), "a", nil); err == nil {
		t.Error("no states accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < maxWaiters; i++ {
		go func() { _, _ = r.Wait(ctx, "a", []string{agenttask.Finished}) }()
	}
	deadline := time.Now().Add(3 * time.Second)
	for r.waiterCount("a") < maxWaiters && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := r.Wait(context.Background(), "a", []string{agenttask.Finished}); !errors.Is(err, ErrTooManyWaiters) {
		t.Errorf("over the waiter limit: %v", err)
	}
	cancel()
	deadline = time.Now().Add(3 * time.Second)
	for r.waiterCount("a") > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n := r.waiterCount("a"); n != 0 {
		t.Errorf("%d waiters left after cancel", n)
	}
}

// Concurrent Observe, Wait and Remove must be race-free (run with -race).
func TestConcurrentUse(t *testing.T) {
	r := newWith(t, Node{ID: "a"}, Node{ID: "b", Parent: "a"})
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Prompted("b")
		for i := 0; i < 200; i++ {
			r.Observe(chunk("b", "x\n"))
			_ = r.Output("b", 5)
			_ = r.Children("a")
			_ = r.Descendant("a", "b")
		}
		r.Observe(turnEnd("b", as.StopEndTurn))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if l, err := r.Wait(ctx, "b", []string{agenttask.Finished}); err != nil || l.State != agenttask.Finished {
		t.Fatalf("got %+v, %v", l, err)
	}
	<-done
}

// waitTurnAsync starts WaitFor(TurnOver) and returns once it is registered
// as a waiter (or returned at once).
func waitTurnAsync(t *testing.T, r *Registry, id string) <-chan waitOut {
	t.Helper()
	ch := make(chan waitOut, 1)
	go func() {
		l, err := r.WaitFor(context.Background(), id, TurnOver)
		ch <- waitOut{l, err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for r.waiterCount(id) == 0 {
		select {
		case o := <-ch:
			ch <- o
			return ch
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("WaitFor never registered")
		}
		time.Sleep(time.Millisecond)
	}
	return ch
}

// A non-fatal error in the middle of a turn makes the state failed, which
// wakes a Wait for "failed"; the turn is not over, so WaitFor(TurnOver)
// sleeps on until the turn really ends.
func TestWaitForTurnOverSeesThroughAnErrorMidTurn(t *testing.T) {
	for _, tc := range []struct {
		name, stop, want string
	}{
		{"finished", as.StopEndTurn, agenttask.Finished},
		{"cancelled", as.StopCancelled, agenttask.Idle},
		{"failed", "refusal", agenttask.Failed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newWith(t, Node{ID: "a"})
			r.Prompted("a")
			ch := waitTurnAsync(t, r, "a")
			r.Observe(as.Event{SessionID: "a", Kind: as.KindError, Text: "auto-approve failed"})
			select {
			case o := <-ch:
				t.Fatalf("returned mid-turn: %+v", o)
			case <-time.After(20 * time.Millisecond):
			}
			r.Observe(chunk("a", "going on"))
			r.Observe(turnEnd("a", tc.stop))
			if o := result(t, ch); o.err != nil || o.live.State != tc.want || o.live.Turn {
				t.Fatalf("got %+v", o)
			}
		})
	}
}

func TestWaitForTurnOverImmediateAndErrors(t *testing.T) {
	r := newWith(t, Node{ID: "a"})
	if l, err := r.WaitFor(context.Background(), "a", TurnOver); err != nil || l.State != agenttask.Idle {
		t.Fatalf("an idle chat: %+v, %v", l, err)
	}
	if _, err := r.WaitFor(context.Background(), "nope", TurnOver); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := r.WaitFor(context.Background(), "a", nil); err == nil {
		t.Fatal("a nil condition was accepted")
	}
	r.Prompted("a")
	ch := waitTurnAsync(t, r, "a")
	r.Remove("a")
	if o := result(t, ch); !errors.Is(o.err, ErrRemoved) {
		t.Fatalf("removed: %+v", o)
	}
}
