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
	"os"
	"sync"
	"sync/atomic"
	"time"

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
	// MaxReplayKiB). SetReplayKiB changes it for terminals started later.
	ReplayKiB int
	// OnSettingsChanged is called for host.settingsChanged, which a window
	// sends after it saved settings.json; the host mode reads the file again.
	// Nil makes the call a no-op that still succeeds.
	OnSettingsChanged func()
	// OnShutdown is called once, after host.shutdown stopped what it was
	// asked to and its reply was written; the host mode exits the process
	// from there. Several shutdown requests call it once.
	OnShutdown func(stopSessions bool)
	// HelloTimeout is how long a connection has to complete hello before it
	// is closed (DefaultHelloTimeout when zero).
	HelloTimeout time.Duration
	// AllowShell lets term.start choose the shell and its arguments. It is
	// for tests, which need a deterministic shell; a host run by Flint
	// leaves it off, so that the socket offers no more than the web view's
	// own terminal calls do.
	AllowShell bool
	// OnChange is called after the connections or the sessions changed (see
	// Load), from any goroutine and with no lock held; it must return
	// quickly. The host mode uses it to start and stop its idle timer.
	OnChange func()
}

// Server serves any number of clients.
type Server struct {
	cfg       Config
	tokenHash [sha256.Size]byte
	terms     sessions.Terminals
	agents    sessions.Agents

	mu       sync.Mutex
	clients  map[*client]struct{}
	unauthed int // connections that have not completed hello, at most MaxUnauthenticated

	exitOnce                    sync.Once
	stopAsked                   atomic.Bool // some host.shutdown asked to stop the sessions
	termsMu                     sync.Mutex
	termMap                     map[string]*termEntry // by the id on the wire: the current entry of each
	termByKey                   map[string]*termEntry // by the manager's key: every entry whose shell may still report
	termGen                     uint64
	replayKiB                   atomic.Int64 // ring size of terminals started from now on
	chatsMu                     sync.Mutex
	chatMap                     map[string]*chat
	starting                    int                   // agent.start and agent.resume calls in flight
	pending                     map[string][]as.Event // events of sessions that are not registered yet, held while starting > 0
	pendingEvents, pendingBytes int

	usage usageStore
}

// New creates a server and its managers.
func New(cfg Config) (*Server, error) {
	if len(cfg.Token) < 16 {
		return nil, errors.New("agenthost: token must be at least 16 bytes")
	}
	if cfg.HelloTimeout <= 0 {
		cfg.HelloTimeout = DefaultHelloTimeout
	}
	s := &Server{
		cfg: cfg, tokenHash: sha256.Sum256([]byte(cfg.Token)),
		clients: map[*client]struct{}{}, termMap: map[string]*termEntry{}, termByKey: map[string]*termEntry{},
		chatMap: map[string]*chat{}, pending: map[string][]as.Event{},
		usage: usageStore{max: MaxUsageBacklog, maxBytes: MaxUsageBacklogBytes},
	}
	s.SetReplayKiB(cfg.ReplayKiB)
	if cfg.Terminals != nil {
		s.terms = cfg.Terminals(termEvents{s})
	}
	if cfg.Agents != nil {
		s.agents = cfg.Agents(s.emitAgent)
	}
	return s, nil
}

// SetReplayKiB sets the replay ring of terminals started from now on (0 or
// less is DefaultReplayKiB; more than MaxReplayKiB is MaxReplayKiB). Running
// terminals keep the ring they have: resizing one would drop or fake history.
func (s *Server) SetReplayKiB(kib int) {
	if kib <= 0 {
		kib = DefaultReplayKiB
	}
	s.replayKiB.Store(int64(min(kib, MaxReplayKiB)))
}

// errClosedByHost closes connections that broke a rule.
var errClosedByHost = errors.New("closed by the host")

var (
	errHelloTimeout      = errors.New("no hello in time")
	errTooManyUnauthed   = errors.New("too many connections that have not said hello")
	errFirstMessageLarge = fmt.Errorf("a message of more than %d bytes before hello", maxFirstMessage)
	errBeforeHelloReply  = errors.New("a message was sent before the hello reply")
)

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
	MethodTermList:   (*Server).termList,

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

	MethodHostSettingsChanged: (*Server).settingsChanged,

	MethodUsageAck:     (*Server).usageAck,
	MethodUsageBacklog: (*Server).usageBacklog,
}

// settingsChanged lets the host mode re-read settings.json. The window only
// says that the file changed, never what is in it: the host reads the file
// itself, so a client cannot hand it values the window would refuse to save.
func (s *Server) settingsChanged(context.Context, *client, json.RawMessage) (any, error) {
	if s.cfg.OnSettingsChanged != nil {
		s.cfg.OnSettingsChanged()
	}
	return struct{}{}, nil
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
		AfterReply: func(method string) { s.afterReply(cl, method) },
	})
	go func() { // unblocks the reader and the writer when the connection is closed from our side
		<-cl.conn.Done()
		_ = rwc.Close()
	}()
	// A connection that has not said hello is held to a short leash: it is
	// counted apart (a flood of them must not keep real clients out), it has
	// a few seconds to authenticate, and its first line is read with a small
	// limit instead of the 4 MiB of an authenticated peer.
	s.mu.Lock()
	if s.unauthed >= MaxUnauthenticated {
		s.mu.Unlock()
		cl.conn.Close(errTooManyUnauthed)
		return errTooManyUnauthed
	}
	s.unauthed++
	cl.unauth = true
	s.clients[cl] = struct{}{}
	s.mu.Unlock()
	timer := time.AfterFunc(s.cfg.HelloTimeout, func() {
		if !cl.authed.Load() {
			cl.conn.Close(errHelloTimeout)
		}
	})
	defer timer.Stop()
	go cl.send()

	err := cl.conn.Serve(&helloGuard{r: rwc, cl: cl})
	cl.conn.Close(err) // already closed by Serve; makes the dependent goroutines end
	s.forget(cl)
	s.changed()
	return err
}

// helloGuard sits between the connection and the stream until hello is
// done. It bounds a line (an unauthenticated peer may not make the host
// buffer megabytes before it has proven anything) and enforces that nothing
// follows the hello message until its reply is out: bytes that arrive after
// the first message but before the host answered cannot come from a client
// that waited, so they end the connection, whatever the timing.
type helloGuard struct {
	r        io.Reader
	cl       *client
	line     int  // bytes of the current line
	content  bool // the current line has more than white space
	sawFirst bool // the first message ended
}

func (g *helloGuard) Read(p []byte) (int, error) {
	n, err := g.r.Read(p)
	if n == 0 || g.cl.authed.Load() {
		return n, err
	}
	if g.sawFirst {
		return 0, errBeforeHelloReply
	}
	for i, b := range p[:n] {
		if b == '\n' {
			if g.content {
				g.sawFirst = true
				if i+1 < n {
					return i + 1, errBeforeHelloReply // the first message is still delivered
				}
				return n, err
			}
			g.line = 0
			continue
		}
		if g.line++; g.line > maxFirstMessage {
			return 0, errFirstMessageLarge
		}
		if b != ' ' && b != '\t' && b != '\r' {
			g.content = true
		}
	}
	return n, err
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

// Clients is how many authenticated connections are open: the ones that keep
// the host running. A connection that has not said hello is not a client.
func (s *Server) Clients() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for cl := range s.clients {
		if cl.authed.Load() {
			n++
		}
	}
	return n
}

// Connections is how many connections are open, authenticated or not.
func (s *Server) Connections() int {
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
		return s.shutdown(cl, params)
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
	// Only one hello per connection ever gets this far: a second, even a
	// concurrent one, ends the connection.
	if err := decode(raw, &p); err != nil || !cl.helloing.CompareAndSwap(false, true) {
		cl.conn.Close(errors.New("bad hello"))
		return nil, errClosedByHost
	}
	// Compare digests so the comparison takes the same time for any token.
	got := sha256.Sum256([]byte(p.Token))
	if subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) != 1 {
		cl.conn.Close(errors.New("bad token"))
		return nil, errClosedByHost
	}
	// compatible first: whoever sees authed must also see the verdict.
	cl.compatible.Store(p.Protocol == Protocol)
	cl.authed.Store(true)
	s.authenticated(cl)
	terms, chats := s.held()
	return HelloResult{
		Protocol: Protocol, Version: s.cfg.Version, Compatible: p.Protocol == Protocol,
		Pid: os.Getpid(), Terminals: terms, Chats: chats,
	}, nil
}

// authenticated moves a connection out of the unauthenticated count and tells
// the host mode, whose idle timer counts clients.
func (s *Server) authenticated(cl *client) {
	s.releaseSlot(cl)
	s.changed()
}

// releaseSlot gives back the unauthenticated slot of cl, once.
func (s *Server) releaseSlot(cl *client) {
	s.mu.Lock()
	if cl.unauth {
		cl.unauth = false
		s.unauthed--
	}
	s.mu.Unlock()
}

// shutdown stops the sessions when asked to; the exit itself waits for the
// reply to be written (afterReply), so the requester sees an answer, not a
// connection that vanished.
func (s *Server) shutdown(cl *client, raw json.RawMessage) (any, error) {
	var p ShutdownParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.StopSessions {
		s.stopAsked.Store(true)
		if s.terms != nil {
			s.terms.CloseAll()
		}
		if s.agents != nil {
			_ = s.agents.Close()
		}
	}
	cl.exitAsked.Store(true)
	return struct{}{}, nil
}

// afterReply runs once a reply has been written. Whichever shutdown request
// gets here first calls the hook; the others find it called.
func (s *Server) afterReply(cl *client, method string) {
	if method != MethodHostShutdown || !cl.exitAsked.Load() || s.cfg.OnShutdown == nil {
		return
	}
	s.exitOnce.Do(func() { s.cfg.OnShutdown(s.stopAsked.Load()) })
}

// forget removes a finished connection from every subscription.
func (s *Server) forget(cl *client) {
	s.releaseSlot(cl)
	s.mu.Lock()
	delete(s.clients, cl)
	s.mu.Unlock()
	s.termsMu.Lock()
	for _, e := range s.termByKey { // includes closed entries still waiting for their shell
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
	helloing   atomic.Bool // a hello was accepted for processing
	authed     atomic.Bool
	compatible atomic.Bool // stored before authed
	exitAsked  atomic.Bool // the host.shutdown of this connection succeeded
	unauth     bool        // holds an unauthenticated slot; guarded by Server.mu
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
