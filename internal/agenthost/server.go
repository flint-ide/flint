// Package agenthost is the server side of Flint's background session host: it
// runs terminals and agent chats and serves them to windows over JSON-RPC
// (docs/design/agent-runtime.md, "Phase 3"), so they survive closing a window.
//
// The server is transport-agnostic: Serve takes any io.ReadWriteCloser. The
// socket, its permissions, the peer-credential check and the process around it
// belong to the host mode that uses this package, which is why the package
// must not import Wails (a test checks the import graph).
//
// Output is kept in bounded replay buffers per terminal and per chat, so a
// window that attaches later, or one that lost a message, catches up from a
// sequence number. A client that cannot keep up is disconnected instead of
// slowing the PTY pumps or the other clients; it reconnects and re-attaches.
package agenthost

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	as "flint/internal/agentsession"
	"flint/internal/jsonrpc"
	"flint/internal/sessions"
	"flint/internal/terminal"
)

// Config creates a Server.
type Config struct {
	// Token is what clients must present in hello (at least 16 bytes; the
	// host mode reads it from a 0600 file). It is never logged or returned.
	Token string
	// Version is the host's build version, reported in hello.
	Version string
	// Terminals builds the terminal manager, handed the events the server
	// forwards to clients. Agents does the same for chat events. They are
	// factories because the managers take their event sink at construction;
	// tests pass fakes. A nil factory leaves that part of the protocol
	// answering "not available".
	Terminals func(terminal.Events) sessions.Terminals
	Agents    func(emit func(as.Event)) sessions.Agents
	// ReplayKiB is the byte ring per terminal (DefaultReplayKiB; at most
	// MaxReplayKiB).
	ReplayKiB int
	// OnShutdown is called after host.shutdown stopped what it was asked to;
	// the host mode exits the process from there.
	OnShutdown func(stopSessions bool)
}

// Server serves any number of clients.
type Server struct {
	cfg       Config
	tokenHash [sha256.Size]byte
	terms     sessions.Terminals
	agents    sessions.Agents

	mu      sync.Mutex
	clients map[*client]struct{}

	termsMu sync.Mutex
	termMap map[string]*termEntry

	chatsMu sync.Mutex
	chatMap map[string]*chat

	usage usageStore
}

// New creates a server and its managers.
func New(cfg Config) (*Server, error) {
	if len(cfg.Token) < 16 {
		return nil, errors.New("agenthost: token must be at least 16 bytes")
	}
	if cfg.ReplayKiB <= 0 {
		cfg.ReplayKiB = DefaultReplayKiB
	}
	cfg.ReplayKiB = min(cfg.ReplayKiB, MaxReplayKiB)
	s := &Server{
		cfg: cfg, tokenHash: sha256.Sum256([]byte(cfg.Token)),
		clients: map[*client]struct{}{}, termMap: map[string]*termEntry{}, chatMap: map[string]*chat{},
		usage: usageStore{max: MaxUsageBacklog},
	}
	if cfg.Terminals != nil {
		s.terms = cfg.Terminals(termEvents{s})
	}
	if cfg.Agents != nil {
		s.agents = cfg.Agents(s.emitAgent)
	}
	return s, nil
}

// errClosedByHost closes connections that broke a rule.
var errClosedByHost = errors.New("closed by the host")

// handler answers one method. cl is the connection that called.
type handler func(s *Server, ctx context.Context, cl *client, params json.RawMessage) (any, error)

// methods is the protocol's method table: supporting a new method is one entry
// here and its handler.
var methods = map[string]handler{
	MethodTermStart:  (*Server).termStart,
	MethodTermWrite:  (*Server).termWrite,
	MethodTermResize: (*Server).termResize,
	MethodTermCwd:    (*Server).termCwd,
	MethodTermResume: (*Server).termResume,
	MethodTermClose:  (*Server).termClose,
	MethodTermAttach: (*Server).termAttach,

	MethodAgentAgents:    (*Server).agentAgents,
	MethodAgentStart:     (*Server).agentStart,
	MethodAgentResume:    (*Server).agentResume,
	MethodAgentPrompt:    (*Server).agentPrompt,
	MethodAgentBusy:      (*Server).agentBusy,
	MethodAgentCancel:    (*Server).agentCancel,
	MethodAgentRespond:   (*Server).agentRespond,
	MethodAgentStop:      (*Server).agentStop,
	MethodAgentPolicy:    (*Server).agentPolicy,
	MethodAgentSetPolicy: (*Server).agentSetPolicy,
	MethodAgentAttach:    (*Server).agentAttach,

	MethodUsageAck:     (*Server).usageAck,
	MethodUsageBacklog: (*Server).usageBacklog,
}

// afterIncompatibleHello lists what a client of another protocol may still do.
var afterIncompatibleHello = map[string]bool{MethodHostShutdown: true}

// Serve serves one client until the connection ends and returns why (io.EOF
// when the client left). It closes rwc. It may be called concurrently for any
// number of connections.
func (s *Server) Serve(rwc io.ReadWriteCloser) error {
	cl := &client{out: make(chan outMsg, maxQueueMsgs)}
	cl.conn = jsonrpc.New(rwc, jsonrpc.Options{
		MaxMessage:    MaxMessage,
		MaxConcurrent: 64, // agent.start can take seconds; several may be pending
		OnRequest: func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			return s.handle(ctx, cl, method, params)
		},
	})
	go func() { // unblocks the reader and the writer when the connection is closed from our side
		<-cl.conn.Done()
		_ = rwc.Close()
	}()
	s.mu.Lock()
	s.clients[cl] = struct{}{}
	s.mu.Unlock()
	go cl.send()

	err := cl.conn.Serve(rwc)
	cl.conn.Close(err) // already closed by Serve; makes the dependent goroutines end
	s.forget(cl)
	return err
}

// Close disconnects every client. Sessions keep running.
func (s *Server) Close() {
	s.mu.Lock()
	all := make([]*client, 0, len(s.clients))
	for cl := range s.clients {
		all = append(all, cl)
	}
	s.mu.Unlock()
	for _, cl := range all {
		cl.conn.Close(errClosedByHost)
	}
}

// Clients is how many connections are open (authenticated or not).
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

// handle enforces the handshake, then runs the method.
func (s *Server) handle(ctx context.Context, cl *client, method string, params json.RawMessage) (any, error) {
	if method == MethodHello {
		return s.hello(cl, params)
	}
	// Anything before a successful hello closes the connection without an
	// answer: an unauthenticated peer learns nothing, not even which methods exist.
	if !cl.authed.Load() {
		cl.conn.Close(errors.New("call before hello"))
		return nil, errClosedByHost
	}
	if method == MethodHostShutdown {
		return s.shutdown(params)
	}
	if !cl.compatible.Load() && !afterIncompatibleHello[method] {
		return nil, jsonrpc.Errorf(jsonrpc.CodeInvalidRequest, "this host speaks protocol %d: only host.shutdown is available", Protocol)
	}
	h := methods[method]
	if h == nil {
		return nil, jsonrpc.Errorf(jsonrpc.CodeMethodNotFound, "method not found: %.64s", method)
	}
	return h(s, ctx, cl, params)
}

func (s *Server) hello(cl *client, raw json.RawMessage) (any, error) {
	var p HelloParams
	if err := decode(raw, &p); err != nil || cl.authed.Load() {
		cl.conn.Close(errors.New("bad hello"))
		return nil, errClosedByHost
	}
	// Compare digests so the comparison takes the same time for any token.
	got := sha256.Sum256([]byte(p.Token))
	if subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) != 1 {
		cl.conn.Close(errors.New("bad token"))
		return nil, errClosedByHost
	}
	cl.authed.Store(true)
	cl.compatible.Store(p.Protocol == Protocol)
	return HelloResult{Protocol: Protocol, Version: s.cfg.Version, Compatible: p.Protocol == Protocol}, nil
}

func (s *Server) shutdown(raw json.RawMessage) (any, error) {
	var p ShutdownParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.StopSessions {
		if s.terms != nil {
			s.terms.CloseAll()
		}
		if s.agents != nil {
			_ = s.agents.Close()
		}
	}
	if s.cfg.OnShutdown != nil {
		// After the reply left: the host exits from there.
		go s.cfg.OnShutdown(p.StopSessions)
	}
	return struct{}{}, nil
}

// forget removes a finished connection from every subscription.
func (s *Server) forget(cl *client) {
	s.mu.Lock()
	delete(s.clients, cl)
	s.mu.Unlock()
	s.termsMu.Lock()
	for _, e := range s.termMap {
		e.unsubscribe(cl)
	}
	s.termsMu.Unlock()
	s.chatsMu.Lock()
	for _, c := range s.chatMap {
		c.unsubscribe(cl)
	}
	s.chatsMu.Unlock()
}

// decode reads request params, mapping failures to "invalid params".
func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return jsonrpc.Errorf(jsonrpc.CodeInvalidParams, "missing params")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return jsonrpc.Errorf(jsonrpc.CodeInvalidParams, "invalid params: %v", err)
	}
	return nil
}

// invalid wraps a validation failure as "invalid params".
func invalid(err error) error {
	if err == nil {
		return nil
	}
	return jsonrpc.Errorf(jsonrpc.CodeInvalidParams, "%v", err)
}

// unavailable is the answer when a part of the host was not configured.
func unavailable(what string) error {
	return jsonrpc.Errorf(jsonrpc.CodeMethodNotFound, "%s are not available on this host", what)
}

// Per-client queue bounds: a client over either is too slow and is dropped.
const (
	maxQueueMsgs  = 1024
	maxQueueBytes = 8 << 20
)

type outMsg struct {
	method string
	params json.RawMessage
}

// client is one connection. Notifications go through a bounded queue and a
// goroutine of its own, so a slow client never blocks whoever produces them.
type client struct {
	conn       *jsonrpc.Conn
	out        chan outMsg
	queued     atomic.Int64
	authed     atomic.Bool
	compatible atomic.Bool
}

// push queues a notification without ever blocking; false means the client
// was too slow and its connection is being closed (it re-attaches on reconnect).
func (cl *client) push(method string, params json.RawMessage) bool {
	select {
	case <-cl.conn.Done():
		return false
	default:
	}
	if cl.queued.Add(int64(len(params))) > maxQueueBytes {
		cl.conn.Close(errors.New("client too slow"))
		return false
	}
	select {
	case cl.out <- outMsg{method, params}:
		return true
	default:
		cl.conn.Close(errors.New("client too slow"))
		return false
	}
}

// send delivers queued notifications in order.
func (cl *client) send() {
	for {
		select {
		case m := <-cl.out:
			cl.queued.Add(-int64(len(m.params)))
			if err := cl.conn.Notify(m.method, m.params); err != nil {
				cl.conn.Close(fmt.Errorf("notify: %w", err))
				return
			}
		case <-cl.conn.Done():
			return
		}
	}
}

// notification marshals a notification's params once for all its receivers.
func notification(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}
