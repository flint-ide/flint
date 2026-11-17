package remote

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"flint/internal/agenthost/client"
	as "flint/internal/agentsession"
	"flint/internal/redact"
	"flint/internal/sessions"
	"flint/internal/settings"
	"flint/internal/terminal"
)

// State is where a Conn stands.
type State string

const (
	// StateIdle: not connected and nothing needs it (never used, or the last
	// session ended after a connection was lost).
	StateIdle State = "idle"
	// StateConnecting: the first connection attempt of a use is running.
	StateConnecting State = "connecting"
	// StateConnected: ssh runs and the remote host said hello.
	StateConnected State = "connected"
	// StateReconnecting: the connection was lost while sessions use it; the
	// next attempt is scheduled or running.
	StateReconnecting State = "reconnecting"
	// StateFailed: the last attempt failed (ssh could not log in, the host
	// key did not verify, the remote has no Flint, …); Err and Stderr say
	// why. While sessions use the machine the next attempt is scheduled.
	StateFailed State = "failed"
	// StateClosed: Close was called.
	StateClosed State = "closed"
)

// Status is what a UI shows about a machine. Err and Stderr hold no secret:
// both pass through redact.Secrets.
type Status struct {
	State State
	// Err is the last failure, empty while connected.
	Err string
	// Stderr is the last StderrLimit bytes of the current or last ssh's
	// standard error: "Permission denied (publickey)." and the like.
	Stderr string
	// Attempt counts the failed attempts in the current run of retries.
	Attempt int
	// RetryIn is the wait before the next attempt, set while one is scheduled.
	RetryIn time.Duration
}

// Errors returned by calls on a Conn.
var (
	// ErrClosed: the Conn was closed.
	ErrClosed = errors.New("remote connection closed")
	// ErrNotConnected: the call needs the connection, which is down (it is
	// being re-established, or it failed). The message names the state.
	ErrNotConnected = errors.New("remote machine not connected")
)

// Config creates a Conn.
type Config struct {
	// Machine must pass settings.CheckMachine; New refuses it otherwise.
	Machine settings.Machine
	// Version is Flint's version, sent in hello.
	Version string
	// Handlers receive terminal output, chat events and resets as with a
	// local client. OnDisconnect is not used: OnStatus tells.
	Handlers client.Handlers
	// OnStatus is told of every change of state, in order, from a goroutine
	// of the Conn. It may call Status but must not block for long.
	OnStatus func(Status)
	// OnLost is told of a session ("terminal" or "chat" and its id) that was
	// in use but that the remote host no longer holds after a reconnect: the
	// host was restarted, or the session was closed from another window.
	OnLost func(kind, id string)
	// Clock and Jitter are for tests. Jitter returns a number in [0, 1) that
	// spreads each backoff over ±20 %.
	Clock  Clock
	Jitter func() float64
	// HelloTimeout bounds one connection attempt after ssh started (default
	// 30 s: ssh logging in, and the remote starting its host if needed).
	HelloTimeout time.Duration
}

const (
	defaultHelloTimeout = 30 * time.Second
	backoffStart        = time.Second
	backoffMax          = 60 * time.Second
	// stableAfter is how long a connection must last for the next loss to
	// start the backoff over; a link that drops at once keeps counting.
	stableAfter = 10 * time.Second
)

// backoff is the wait before attempt number n (from 0): 1, 2, 4 … 60 s.
func backoff(n int) time.Duration {
	if n >= 6 { // 1<<6 is already over the cap
		return backoffMax
	}
	return min(backoffStart<<n, backoffMax)
}

// Conn is the connection to one remote machine. It runs ssh (argument
// vector from HostArgv) as a child process, speaks the host protocol over
// its standard input and output, and implements sessions.Terminals and
// sessions.Agents through Terminals and Agents by delegating to a
// client.Client.
//
// It connects when first used, and reconnects with backoff while a session
// of this machine is in use, attaching each again from where it left off. It
// never leaves an ssh process behind. The remote host outlives all of this:
// Close detaches, it does not end the remote sessions.
type Conn struct {
	cfg    Config
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup // watchers, the supervisor, and attempts in flight

	statusMu sync.Mutex // orders OnStatus calls

	mu          sync.Mutex
	state       State
	lastErr     string
	retryIn     time.Duration
	attempt     int
	tail        *tail
	link        *link
	dialing     chan struct{} // closed when the first-use attempt in flight ends
	supervising bool
	closed      bool

	terms   map[string]struct{} // terminals in use: not closed, not exited
	chats   map[string]struct{} // chats in use: not stopped
	termPos map[string]uint64   // seq of the last output seen, from lost connections
	chatSeq map[string]uint64   // seq of the last chat event seen
	// pendingClose and pendingStop are sessions closed while the connection
	// was down; the next connection closes them on the remote host.
	pendingClose map[string]struct{}
	pendingStop  map[string]struct{}
	policy       as.Policy
	policySet    bool
	poke         chan struct{}
}

// link is one established connection.
type link struct {
	cl    *client.Client
	proc  *sshProc
	since time.Time
}

// New makes a Conn. It does not connect: that happens at the first use.
func New(cfg Config) (*Conn, error) {
	if _, err := HostArgv(cfg.Machine); err != nil {
		return nil, err
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	if cfg.Jitter == nil {
		cfg.Jitter = rand.Float64
	}
	if cfg.HelloTimeout <= 0 {
		cfg.HelloTimeout = defaultHelloTimeout
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Conn{
		cfg: cfg, ctx: ctx, cancel: cancel, state: StateIdle, tail: &tail{},
		terms: map[string]struct{}{}, chats: map[string]struct{}{},
		termPos: map[string]uint64{}, chatSeq: map[string]uint64{},
		pendingClose: map[string]struct{}{}, pendingStop: map[string]struct{}{},
		poke: make(chan struct{}, 1),
	}, nil
}

// Terminals is the terminal side, a sessions.Terminals. Start connects when
// the machine is not connected yet.
func (c *Conn) Terminals() sessions.Terminals { return terminals{c} }

// Agents is the chat side, a sessions.Agents. Start and Resume connect when
// the machine is not connected yet.
func (c *Conn) Agents() sessions.Agents { return agents{c} }

// Status is the current state, with ssh's stderr.
func (c *Conn) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusLocked()
}

func (c *Conn) statusLocked() Status {
	return Status{State: c.state, Err: c.lastErr, Stderr: c.tail.String(), Attempt: c.attempt, RetryIn: c.retryIn}
}

// emit tells OnStatus the current status. Calls are serialised so a receiver
// sees states in the order they happened.
func (c *Conn) emit() {
	if c.cfg.OnStatus == nil {
		return
	}
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	c.cfg.OnStatus(c.Status())
}

// Close detaches from the remote host (its sessions keep running there),
// stops reconnecting, and ends the ssh process and its group. It returns
// when nothing of this Conn is left running. Calls after it fail with
// ErrClosed.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.wg.Wait()
		return nil
	}
	c.closed = true
	c.state, c.retryIn = StateClosed, 0
	l := c.link
	c.link = nil
	c.mu.Unlock()
	c.cancel()
	if l != nil {
		_ = l.cl.Close()
		_ = l.proc.Close()
	}
	c.wg.Wait()
	c.emit()
	return nil
}

func (c *Conn) inUseLocked() bool { return len(c.terms)+len(c.chats) > 0 }

func (c *Conn) wake() {
	select {
	case c.poke <- struct{}{}:
	default:
	}
}

// acquire returns the live client, connecting first when nothing is going on.
// While the connection is being re-established it fails at once: the caller's
// session would otherwise hang, and the supervisor brings it back.
func (c *Conn) acquire(ctx context.Context) (*client.Client, error) {
	for {
		c.mu.Lock()
		switch {
		case c.closed:
			c.mu.Unlock()
			return nil, ErrClosed
		case c.link != nil:
			cl := c.link.cl
			c.mu.Unlock()
			return cl, nil
		case c.supervising:
			err := c.notConnectedLocked()
			c.mu.Unlock()
			return nil, err
		case c.dialing != nil:
			ch := c.dialing
			c.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		ch := make(chan struct{})
		c.dialing = ch
		c.wg.Add(1)
		c.mu.Unlock()

		err := c.connectOnce(StateConnecting, false)

		c.mu.Lock()
		c.dialing = nil
		close(ch)
		var cl *client.Client
		if err == nil && c.link != nil {
			cl = c.link.cl
		}
		c.mu.Unlock()
		c.wg.Done()
		if err != nil {
			return nil, err
		}
		if cl == nil {
			continue // lost again at once: look again
		}
		return cl, nil
	}
}

// live is the connected client, without connecting.
func (c *Conn) live() (*client.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.closed:
		return nil, ErrClosed
	case c.link == nil:
		return nil, c.notConnectedLocked()
	}
	return c.link.cl, nil
}

func (c *Conn) notConnectedLocked() error {
	if c.lastErr != "" {
		return fmt.Errorf("%w (%s): %s", ErrNotConnected, c.state, c.lastErr)
	}
	return fmt.Errorf("%w (%s)", ErrNotConnected, c.state)
}

// watch waits for a link to end, keeps what the client saw, and starts the
// supervisor when sessions still need the machine.
func (c *Conn) watch(l *link) {
	defer c.wg.Done()
	<-l.cl.Done()
	pos := l.cl.Positions()
	l.proc.kill()
	<-l.proc.done
	cause := l.cl.Err()

	c.mu.Lock()
	if c.link != l { // Close took it
		c.mu.Unlock()
		return
	}
	c.link = nil
	for id, seq := range pos {
		c.termPos[id] = seq
	}
	if c.closed {
		c.mu.Unlock()
		return
	}
	if c.cfg.Clock.Now().Sub(l.since) >= stableAfter {
		c.attempt = 0
	}
	c.lastErr = "connection lost"
	if cause != nil {
		c.lastErr += ": " + redact.Secrets(cause.Error())
	}
	if !c.inUseLocked() {
		c.state = StateIdle
		c.mu.Unlock()
		c.emit()
		return
	}
	c.state, c.supervising = StateReconnecting, true
	c.wg.Add(1)
	c.mu.Unlock()
	c.emit()
	go c.supervise()
}

// supervise reconnects with backoff while sessions use the machine. It is the
// only attempt running while c.supervising is set.
func (c *Conn) supervise() {
	defer c.wg.Done()
	for {
		c.mu.Lock()
		d := c.jittered(backoff(c.attempt))
		c.retryIn = d
		c.mu.Unlock()
		c.emit()

		timer := c.cfg.Clock.After(d)
	wait:
		for {
			select {
			case <-timer:
				break wait
			case <-c.poke:
				if !c.keepGoing() {
					return
				}
			case <-c.ctx.Done():
				return
			}
		}
		if !c.keepGoing() {
			return
		}
		c.mu.Lock()
		c.retryIn = 0
		c.mu.Unlock()
		err := c.connectOnce(StateReconnecting, true)
		if err == nil || errors.Is(err, ErrClosed) {
			return
		}
		c.mu.Lock()
		c.attempt++
		c.mu.Unlock()
	}
}

// keepGoing says whether the supervisor should still try; when no session
// uses the machine any more it ends the supervision.
func (c *Conn) keepGoing() bool {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return false
	}
	if c.inUseLocked() {
		c.mu.Unlock()
		return true
	}
	c.supervising, c.retryIn = false, 0
	if c.state == StateReconnecting {
		c.state = StateIdle
	}
	c.mu.Unlock()
	c.emit()
	return false
}

// jittered spreads d over ±20 %.
func (c *Conn) jittered(d time.Duration) time.Duration {
	r := min(max(c.cfg.Jitter(), 0), 1)
	return time.Duration(float64(d) * (0.8 + 0.4*r))
}

// handlers wraps the caller's handlers to keep track of what is in use and
// how far the chats got.
func (c *Conn) handlers() client.Handlers {
	h := c.cfg.Handlers
	return client.Handlers{
		Terminal: trackedTerminal{c, h.Terminal},
		Agent: func(ev as.Event) {
			c.mu.Lock()
			if ev.Seq > c.chatSeq[ev.SessionID] {
				c.chatSeq[ev.SessionID] = ev.Seq
			}
			c.mu.Unlock()
			if h.Agent != nil {
				h.Agent(ev)
			}
		},
		OnReset: h.OnReset,
	}
}

// trackedTerminal forwards terminal events and notices exits: a terminal
// that ended no longer needs the connection.
type trackedTerminal struct {
	c *Conn
	h terminal.Events
}

func (t trackedTerminal) Output(id string, data []byte) {
	if t.h != nil {
		t.h.Output(id, data)
	}
}

func (t trackedTerminal) Exited(id string, code int) {
	t.c.mu.Lock()
	delete(t.c.terms, id)
	t.c.mu.Unlock()
	t.c.wake()
	if t.h != nil {
		t.h.Exited(id, code)
	}
}

func (t trackedTerminal) Title(id string, info terminal.Info) {
	if t.h != nil {
		t.h.Title(id, info)
	}
}

func (t trackedTerminal) AgentState(id, state string) {
	if t.h != nil {
		t.h.AgentState(id, state)
	}
}
