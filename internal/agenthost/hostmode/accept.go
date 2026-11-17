package hostmode

import (
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// acceptLoop serves every connection whose peer is wantUID, each in its own
// goroutine, until ln is closed. A peer of another uid is closed before a
// byte is read from it, and so is one over maxConns: the host's goroutines
// and descriptors are bounded no matter what connects. The token check
// (hello) comes after this, inside serve.
func acceptLoop(ln net.Listener, wantUID uint32, maxConns int, serve func(net.Conn)) {
	var open atomic.Int32
	backoff := 5 * time.Millisecond
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// Out of descriptors and the like: wait instead of spinning.
			slog.Warn("agent host: accept", "error", err)
			time.Sleep(backoff)
			backoff = min(backoff*2, time.Second)
			continue
		}
		backoff = 5 * time.Millisecond

		uc, ok := c.(*net.UnixConn)
		if !ok {
			_ = c.Close()
			continue
		}
		uid, err := peerUID(uc)
		if err != nil || uid != wantUID {
			slog.Warn("agent host: refused a connection", "peerUid", uid, "error", err)
			_ = c.Close()
			continue
		}
		if int(open.Add(1)) > maxConns {
			open.Add(-1)
			slog.Warn("agent host: too many connections", "max", maxConns)
			_ = c.Close()
			continue
		}
		go func() {
			defer open.Add(-1)
			serve(c)
		}()
	}
}

// idleWatch fires once when the host has been idle for d. It is driven by
// Poke, which the server's OnChange hook calls, so nothing polls: the timer
// runs only while the host is idle.
type idleWatch struct {
	d    time.Duration
	idle func() bool
	fire func()

	mu      sync.Mutex
	timer   *time.Timer
	armed   bool
	gen     uint64 // which arming a firing timer belongs to
	stopped bool
}

// newIdleWatch starts watching. A host that begins idle is armed at once: it
// exits too if its starter never connects.
func newIdleWatch(d time.Duration, idle func() bool, fire func()) *idleWatch {
	w := &idleWatch{d: d, idle: idle, fire: fire}
	w.Poke()
	return w
}

// Poke re-evaluates after sessions or clients changed.
func (w *idleWatch) Poke() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	switch idle := w.idle(); {
	case idle && !w.armed:
		w.armed = true
		w.gen++
		gen := w.gen
		w.timer = time.AfterFunc(w.d, func() { w.expire(gen) })
	case !idle && w.armed:
		w.armed = false
		w.timer.Stop()
	}
}

// expire runs when the timer fires. It looks again, because a client may have
// connected a moment before and its Poke may still be waiting for the lock.
func (w *idleWatch) expire(gen uint64) {
	w.mu.Lock()
	if w.stopped || !w.armed || w.gen != gen {
		w.mu.Unlock()
		return
	}
	if !w.idle() {
		w.armed = false
		w.mu.Unlock()
		return
	}
	w.stopped = true
	w.mu.Unlock()
	w.fire()
}

// Stop ends the watch; fire is not called afterwards.
func (w *idleWatch) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	if w.timer != nil {
		w.timer.Stop()
	}
}
