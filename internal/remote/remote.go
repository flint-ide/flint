// Package remote is Flint Remote's laptop side: a small HTTP API on a
// loopback address that a paired phone reaches through the user's own
// tunnel (ssh -L, tailscale serve). Phase 1 is read-only: the open agent
// chats, the agent tasks, and a WebSocket that replays a chat's recent events
// and then streams live ones (agent events, chat state, tasks changed).
//
// Remote access to Flint is remote access to the laptop, so the server
// trusts nothing but device tokens: it listens on loopback only, checks Host
// and Origin against DNS rebinding, caps bodies, limits each device's rate,
// compares token hashes in constant time, redacts what it sends
// (internal/redact) and writes one audit line per request (device,
// operation, status; never content). Only the standard library is used.
package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"flint/internal/agentsession"
)

// Security limits and protocol timing: constants, not settings.
const (
	maxBody           = 4 << 10 // bytes of one request body (pairing is the only one)
	maxStreamsDevice  = 4
	maxStreams        = 32
	maxSince          = 256 // sessions one stream request may name
	maxOutMessage     = 1 << 20
	pingInterval      = 30 * time.Second
	closeWait         = time.Second // for the client's close frame
	shutdownTimeout   = 2 * time.Second
	requestTimeout    = 30 * time.Second
	readHeaderTimeout = 5 * time.Second
)

// Chat is an open agent chat as a remote client lists it.
type Chat struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	// Project is the chat's folder (the home directory shown as ~).
	Project string `json:"project"`
	// State: idle, working or waiting (for an answer to a permission request).
	State              string `json:"state"`
	WaitingForApproval bool   `json:"waitingForApproval"`
	Pending            int    `json:"pending"`
	// ReadOnly: the folder is in Restricted Mode; the chat can be followed
	// but never answered from a phone.
	ReadOnly bool   `json:"readOnly"`
	LastSeq  uint64 `json:"lastSeq"`
}

// Chat states.
const (
	ChatIdle    = "idle"
	ChatWorking = "working"
	ChatWaiting = "waiting"
)

// ChatState derives a chat's state from its session.
func ChatState(s agentsession.SessionState) string {
	switch {
	case s.Pending > 0:
		return ChatWaiting
	case s.Busy:
		return ChatWorking
	}
	return ChatIdle
}

// Task is an agent task as a remote client lists it.
type Task struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Agent       string       `json:"agent"`
	Branch      string       `json:"branch"`
	Base        string       `json:"base"`
	State       string       `json:"state"`
	Error       string       `json:"error,omitempty"`
	Stale       bool         `json:"stale"`
	Ahead       int          `json:"ahead"`
	Merged      bool         `json:"merged"`
	PullRequest string       `json:"pullRequest,omitempty"`
	CreatedAt   int64        `json:"createdAt"`
	Changes     *DiffSummary `json:"changes,omitempty"`
}

// DiffSummary counts what a task changed.
type DiffSummary struct {
	Files     int  `json:"files"`
	Added     int  `json:"added"`
	Deleted   int  `json:"deleted"`
	Truncated bool `json:"truncated"`
}

// Options are what the server reads; every function must be safe for
// concurrent use.
type Options struct {
	Devices *Devices
	// Chats lists the open chats.
	Chats func() []Chat
	// Tasks lists the agent tasks of the open projects.
	Tasks func(ctx context.Context) ([]Task, error)
	// Replay returns a chat's kept events after a Seq (agentsession.Manager.Replay).
	Replay func(sessionID string, after uint64) ([]agentsession.Event, *agentsession.Gap)
	// Logf writes audit lines (Flint's log). Nil discards them.
	Logf func(format string, args ...any)
}

// Config is what the settings decide.
type Config struct {
	// Host is the address to bind; it must be a loopback IP.
	Host string
	// Port to bind; 0 lets the OS pick (tests).
	Port int
	// AllowedHosts are extra names for the Host and Origin checks.
	AllowedHosts []string
	// RequestsPerMinute is each device's budget.
	RequestsPerMinute int
}

// ErrNotLoopback refuses a bind address other than a loopback IP.
var ErrNotLoopback = errors.New("remote: the server only listens on a loopback address (use a tunnel to reach it)")

// Server is Flint Remote's HTTP server. Safe for concurrent use.
type Server struct {
	opts Options
	lim  *limiter
	hub  *hub

	mu      sync.Mutex
	cfg     Config
	hosts   map[string]bool // allowed Host / Origin names, lowercased
	srv     *http.Server    // nil while stopped
	served  chan struct{}
	ctx     context.Context // ends when the listener stops
	cancel  context.CancelFunc
	addr    string
	streams map[*stream]struct{}
	wg      sync.WaitGroup // running streams
	closed  bool
}

// New returns a stopped server.
func New(opts Options) *Server {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	return &Server{opts: opts, lim: newLimiter(120), hub: newHub(), streams: map[*stream]struct{}{}}
}

// Start listens with cfg, or applies cfg to the running server: a new
// address restarts the listener (open streams end), other changes apply in
// place.
func (s *Server) Start(cfg Config) error {
	ip := net.ParseIP(cfg.Host)
	if ip == nil || !ip.IsLoopback() {
		return ErrNotLoopback
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return fmt.Errorf("remote: invalid port %d", cfg.Port)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("remote: closed")
	}
	running, sameBind := s.srv != nil, s.cfg.Host == cfg.Host && s.cfg.Port == cfg.Port
	s.applyLocked(cfg)
	s.mu.Unlock()
	if running && sameBind {
		return nil
	}
	s.Stop()
	return s.listen(cfg)
}

func (s *Server) applyLocked(cfg Config) {
	s.cfg = cfg
	s.hosts = map[string]bool{"127.0.0.1": true, "localhost": true, "::1": true}
	for _, h := range cfg.AllowedHosts {
		s.hosts[normalizeHost(h)] = true
	}
	s.lim.setRate(max(1, cfg.RequestsPerMinute))
}

func (s *Server) listen(cfg Config) error {
	ln, err := net.Listen("tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return fmt.Errorf("remote: listen: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv := &http.Server{
		Handler:           s,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      requestTimeout,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ErrorLog:          nil,
	}
	served := make(chan struct{})
	s.mu.Lock()
	if s.closed || s.srv != nil {
		s.mu.Unlock()
		cancel()
		_ = ln.Close()
		return errors.New("remote: closed")
	}
	s.srv, s.served, s.ctx, s.cancel, s.addr = srv, served, ctx, cancel, ln.Addr().String()
	s.mu.Unlock()
	go func() {
		defer close(served)
		_ = srv.Serve(ln)
	}()
	s.opts.Logf("remote: listening on %s", ln.Addr())
	return nil
}

// Stop closes the listener and ends every request and stream; it returns
// when they have ended.
func (s *Server) Stop() {
	s.mu.Lock()
	srv, served, cancel := s.srv, s.served, s.cancel
	s.srv, s.served, s.cancel, s.addr = nil, nil, nil, ""
	s.mu.Unlock()
	if srv == nil {
		return
	}
	cancel() // streams see it and close with "going away"
	ctx, done := context.WithTimeout(context.Background(), shutdownTimeout)
	defer done()
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
	}
	<-served
	s.wg.Wait()
	s.opts.Logf("remote: stopped")
}

// Close stops the server for good.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.Stop()
	return nil
}

// Addr is the listening address ("" while stopped).
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Revoke forgets a device and ends its open streams at once.
func (s *Server) Revoke(id string) error {
	err := s.opts.Devices.Revoke(id)
	s.cutStreams(func(st *stream) bool { return st.device == id })
	s.lim.forget(id)
	return err
}

// RevokeAll forgets every device and ends every stream.
func (s *Server) RevokeAll() error {
	err := s.opts.Devices.RevokeAll()
	s.cutStreams(func(*stream) bool { return true })
	return err
}

func (s *Server) cutStreams(match func(*stream) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for st := range s.streams {
		if match(st) {
			st.revoke()
		}
	}
}

// Streams counts the open streams (tests, Settings).
func (s *Server) Streams() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams)
}

// AgentEvent hands an agent event to the open streams. It never blocks: a
// stream that can't keep up is closed and the client reconnects with since.
func (s *Server) AgentEvent(e agentsession.Event) { s.hub.publish(hubMsg{kind: msgAgent, event: e}) }

// TasksChanged tells the open streams that the task list changed.
func (s *Server) TasksChanged() { s.hub.publish(hubMsg{kind: msgTasks}) }

// ChatChanged tells the open streams that a chat opened, closed or changed
// state (agentsession.Manager's state hook).
func (s *Server) ChatChanged(id string) { s.hub.publish(hubMsg{kind: msgChat, id: id}) }
