// Package client talks to an agenthost server and implements
// sessions.Terminals and sessions.Agents on top of it, so the app can use a
// background host through the same interfaces as its in-process managers.
//
// Output, exits, titles and chat events reach the same callbacks the
// in-process managers call (terminal.Events and an event func). They are
// delivered one at a time, in order, from a goroutine of the client, never from
// the connection's reader: a callback may call the client again. Every
// notification carries a sequence number; when one goes missing (a slow
// consumer, a dropped message) the client re-attaches and the host replays
// what was lost from its buffer.
//
// When the connection closes, calls fail with ErrDisconnected instead of
// waiting; Done and Handlers.OnDisconnect say it happened.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"flint/internal/agenthost"
	as "flint/internal/agentsession"
	"flint/internal/jsonrpc"
	"flint/internal/sessions"
	"flint/internal/terminal"
)

// ErrDisconnected wraps the error of every call that fails because the
// connection to the host is closed.
var ErrDisconnected = errors.New("background host disconnected")

// ProtocolError is returned by New when the host speaks another protocol. The
// client is then good for Shutdown and Close only.
type ProtocolError struct {
	Host    int    // the host's protocol
	Version string // the host's build version
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("background host speaks protocol %d (version %s), this Flint speaks %d", e.Host, e.Version, agenthost.Protocol)
}

// Handlers receive what the host reports. All are optional.
type Handlers struct {
	// Terminal gets output, exits and title changes of terminals.
	Terminal terminal.Events
	// Agent gets chat events.
	Agent func(as.Event)
	// OnReset is told, before the replayed history, that the host no longer
	// had everything after what the client saw: kind is "terminal" (the
	// screen was cleared) or "chat" (events are missing).
	OnReset func(kind, id string)
	// OnDisconnect is called once when the connection closes, with the reason.
	OnDisconnect func(err error)
}

// Config creates a Client.
type Config struct {
	Token    string
	Version  string
	Handlers Handlers
	// HelloTimeout bounds the handshake (helloWait when zero). A connection
	// that has to be set up first, such as ssh logging in, needs longer than
	// a Unix socket does.
	HelloTimeout time.Duration
}

const (
	// callTimeout bounds calls that have no context of their own.
	callTimeout = 30 * time.Second
	helloWait   = 10 * time.Second
	// jobQueue is how many notifications wait for the delivery goroutine
	// before further ones are dropped and recovered by re-attaching.
	jobQueue = 1024
	// maxPages bounds one catch-up, a safety net against a host that never
	// stops saying "more".
	maxPages = 64
)

// Client is a connection to the host.
type Client struct {
	conn  *jsonrpc.Conn
	h     Handlers
	hello agenthost.HelloResult // what the host said at the handshake; set before New returns

	jobs      chan func()
	wake      chan struct{}
	resync    atomic.Bool
	delivered chan struct{} // closed when the delivery goroutine has ended

	mu     sync.Mutex
	terms  map[string]struct{} // terminals to close on CloseAll
	chats  map[string]struct{} // chats to stop on Agents().Close
	policy as.Policy           // last known, for when the host cannot be asked

	// Owned by the delivery goroutine.
	tstate map[string]*termState
	cstate map[string]*chatState
}

// New runs the handshake over rwc and starts delivering. The client closes
// rwc when it is closed or the connection breaks. On a *ProtocolError the
// returned client is still usable for Shutdown and Close.
func New(rwc io.ReadWriteCloser, cfg Config) (*Client, error) {
	c := &Client{
		h: cfg.Handlers, jobs: make(chan func(), jobQueue), wake: make(chan struct{}, 1), delivered: make(chan struct{}),
		terms: map[string]struct{}{}, chats: map[string]struct{}{}, policy: as.DefaultPolicy(),
		tstate: map[string]*termState{}, cstate: map[string]*chatState{},
	}
	c.conn = jsonrpc.New(rwc, jsonrpc.Options{MaxMessage: agenthost.MaxMessage, OnNotify: c.onNotify})
	go func() { _ = c.conn.Serve(rwc) }()
	go func() {
		<-c.conn.Done()
		_ = rwc.Close()
		if c.h.OnDisconnect != nil {
			c.h.OnDisconnect(c.conn.Err())
		}
	}()
	go c.deliver()

	wait := cfg.HelloTimeout
	if wait <= 0 {
		wait = helloWait
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	var res agenthost.HelloResult
	err := c.call(ctx, agenthost.MethodHello, agenthost.HelloParams{
		Protocol: agenthost.Protocol, Version: cfg.Version, Token: cfg.Token,
	}, &res)
	c.hello = res
	switch {
	case err != nil:
		c.Close()
		return nil, err
	case !res.Compatible:
		return c, &ProtocolError{Host: res.Protocol, Version: res.Version}
	}
	return c, nil
}

// Hello is what the host reported at the handshake: its pid and version, and
// the ids of the terminals and chats it held then, so a newly attached window
// can find its sessions (AttachTerminal, AttachChat). It is a copy.
func (c *Client) Hello() agenthost.HelloResult {
	h := c.hello
	h.Terminals = append([]string{}, h.Terminals...)
	h.Chats = append([]string{}, h.Chats...)
	return h
}

// Terminals is the terminal side, a sessions.Terminals.
func (c *Client) Terminals() sessions.Terminals { return terminals{c} }

// Agents is the chat side, a sessions.Agents.
func (c *Client) Agents() sessions.Agents { return agents{c} }

// Done is closed when the connection is closed.
func (c *Client) Done() <-chan struct{} { return c.conn.Done() }

// Err is why the connection closed, nil while it is open.
func (c *Client) Err() error { return c.conn.Err() }

// Close disconnects. Sessions keep running on the host: this is the detach.
func (c *Client) Close() error {
	c.conn.Close(nil)
	return nil
}

// Shutdown asks the host to exit, stopping its sessions first when asked to.
func (c *Client) Shutdown(ctx context.Context, stopSessions bool) error {
	err := c.call(ctx, agenthost.MethodHostShutdown, agenthost.ShutdownParams{StopSessions: stopSessions}, nil)
	if errors.Is(err, ErrDisconnected) {
		// The host may close the connection while exiting, before its reply
		// is delivered: that is the shutdown, not a failure of it.
		return nil
	}
	return err
}

// AckUsage tells the host usage events up to upTo are accounted for.
func (c *Client) AckUsage(ctx context.Context, upTo uint64) error {
	return c.call(ctx, agenthost.MethodUsageAck, agenthost.UsageAckParams{UpTo: upTo}, nil)
}

// UsageEvent is a usage event the host kept: Seq is what to pass to AckUsage.
type UsageEvent struct {
	Seq   uint64
	Event as.Event
}

// UsageBacklog returns the usage events nobody acknowledged yet. They are
// not delivered to Handlers.Agent: what to do with them (they may duplicate
// events a chat replay brings) is up to the caller.
func (c *Client) UsageBacklog(ctx context.Context) ([]UsageEvent, error) {
	events, _, err := c.UsageBacklogDropped(ctx)
	return events, err
}

// UsageBacklogDropped is UsageBacklog and also says how many usage events the
// host ever had to discard because its backlog was full (events or bytes): the
// accounting is that much short. Acknowledgements (AckUsage) are the host's,
// not this window's: they clear the events for every window.
func (c *Client) UsageBacklogDropped(ctx context.Context) (events []UsageEvent, dropped uint64, err error) {
	var after uint64
	for range maxPages {
		var res agenthost.UsageBacklogResult
		if err := c.call(ctx, agenthost.MethodUsageBacklog, agenthost.UsageBacklogParams{After: after}, &res); err != nil {
			return events, dropped, err
		}
		dropped = res.Dropped
		for _, raw := range res.Events {
			var msg agenthost.EventMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				return events, dropped, fmt.Errorf("usage backlog: %w", err)
			}
			events = append(events, UsageEvent{Seq: msg.UsageSeq, Event: msg.Event})
			after = msg.UsageSeq
		}
		if !res.More {
			break
		}
	}
	return events, dropped, nil
}

// AttachTerminal starts listening to a terminal that was started by an
// earlier window: output after afterSeq (0 for everything the host still
// has) is delivered, then live output.
func (c *Client) AttachTerminal(id string, afterSeq uint64) error {
	c.mu.Lock()
	c.terms[id] = struct{}{}
	c.mu.Unlock()
	return c.run(func() {
		c.tstate[id] = &termState{last: afterSeq}
		c.syncTerm(id)
	})
}

// Positions waits until the connection has closed and delivery stopped, then
// reports the terminals this client started or attached (and did not close)
// with the seq of the last output it delivered for each: what a new
// connection passes to AttachTerminal so the window misses nothing and sees
// nothing twice. It blocks while the connection is open; call it once Done
// is closed (Handlers.OnDisconnect is a good place).
func (c *Client) Positions() map[string]uint64 {
	<-c.conn.Done()
	<-c.delivered // after this, the delivery goroutine's state is ours to read
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]uint64, len(c.terms))
	for id := range c.terms {
		var last uint64
		if st := c.tstate[id]; st != nil {
			last = st.last
		}
		out[id] = last
	}
	return out
}

// ListTerminals asks the host which terminals it holds now (Hello lists them
// as they were at the handshake).
func (c *Client) ListTerminals(ctx context.Context) ([]agenthost.TermStatus, error) {
	var res agenthost.TermListResult
	if err := c.call(ctx, agenthost.MethodTermList, struct{}{}, &res); err != nil {
		return nil, err
	}
	return res.Terminals, nil
}

// SettingsChanged tells the host that settings.json changed; it reads the
// file again itself.
func (c *Client) SettingsChanged(ctx context.Context) error {
	return c.call(ctx, agenthost.MethodHostSettingsChanged, struct{}{}, nil)
}

// AttachChat is AttachTerminal for a chat.
func (c *Client) AttachChat(id string, afterSeq uint64) error {
	c.mu.Lock()
	c.chats[id] = struct{}{}
	c.mu.Unlock()
	return c.run(func() {
		c.cstate[id] = &chatState{last: afterSeq}
		c.syncChat(id)
	})
}

// call is Conn.Call with a closed connection reported as ErrDisconnected.
func (c *Client) call(ctx context.Context, method string, params, result any) error {
	err := c.conn.Call(ctx, method, params, result)
	if err == nil {
		return nil
	}
	var rpcErr *jsonrpc.Error
	if errors.As(err, &rpcErr) {
		return err
	}
	select {
	case <-c.conn.Done():
		return fmt.Errorf("%w: %w", ErrDisconnected, c.conn.Err())
	default:
		return err
	}
}

// callT is call with the default timeout.
func (c *Client) callT(method string, params, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return c.call(ctx, method, params, result)
}
