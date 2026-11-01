// Package mcpserver serves Flint's read-only context (diagnostics, open
// editors, terminal output, test results, cluster status) to the coding
// agents Flint starts, over the Model Context Protocol's Streamable HTTP
// transport: JSON-RPC 2.0 messages POSTed to one endpoint, answered with
// application/json (protocol 2025-06-18; no server-sent events, no
// sessions). Only the standard library is used.
//
// The server listens on 127.0.0.1 with a port the OS picks, starting on the
// first Endpoint call; Stop closes it until the next one. Every agent session
// gets its own random bearer token, revoked when the session ends, and a
// request only ever sees the project its token belongs to. Requests are
// checked against DNS rebinding (Host, Origin), their bodies are capped, and
// every tool answer is redacted (internal/redact) and bounded.
package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Security limits: constants, not settings.
const (
	maxBody         = 1 << 20  // bytes of one request
	maxToolOutput   = 64 << 10 // bytes of one tool answer
	maxTokens       = 256      // live tokens (agent sessions)
	maxConcurrent   = 4        // tool calls running at once per project (they may start kubectl)
	callTimeout     = 30 * time.Second
	shutdownTimeout = 2 * time.Second
	endpointPath    = "/mcp"
)

// ErrClosed is returned by Endpoint after Close.
var ErrClosed = errors.New("mcpserver: closed")

// Tool is one read-only tool.
type Tool struct {
	Name        string
	Title       string
	Description string
	// InputSchema is a JSON Schema object; nil means "no arguments".
	InputSchema json.RawMessage
	// Call answers for the project at root (the caller's token decides it).
	// args are the raw "arguments" (an object, or null). An error is shown to
	// the agent as a failed tool call.
	Call func(ctx context.Context, root string, args json.RawMessage) (string, error)
}

// Options configure a Server.
type Options struct {
	// Tools returns the tools offered right now; it is read on every request,
	// so settings changes apply without restarting anything.
	Tools func() []Tool
	// Allow reports whether a project may use the server now (enabled in
	// Settings, trusted). Nil allows every project that has a token.
	Allow func(root string) bool
	// Version is serverInfo.version.
	Version string
	// Instructions are sent with initialize: how the agent should use the tools.
	Instructions string
}

type tokenHash = [sha256.Size]byte

// Server is the MCP endpoint. It is safe for concurrent use.
type Server struct {
	opts Options

	mu     sync.Mutex
	tokens map[tokenHash]string     // hash of a live token → its project root
	sems   map[string]chan struct{} // project root → its running tool calls
	srv    *http.Server             // nil while not listening
	served chan struct{}            // closed when Serve returned
	cancel context.CancelFunc       // ends the listener's requests
	port   int
	closed bool
}

// New returns a server; nothing listens until Endpoint is called.
func New(opts Options) *Server {
	return &Server{opts: opts, tokens: map[tokenHash]string{}, sems: map[string]chan struct{}{}}
}

// Endpoint starts the listener if needed and issues a new token for one
// agent session working in root. revoke ends the token (call it when the
// session ends); it is safe to call more than once.
func (s *Server) Endpoint(root string) (url, token string, revoke func(), err error) {
	if !filepath.IsAbs(root) {
		return "", "", nil, fmt.Errorf("mcpserver: root %q is not absolute", root)
	}
	root = filepath.Clean(root)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", "", nil, ErrClosed
	}
	if len(s.tokens) >= maxTokens {
		return "", "", nil, fmt.Errorf("mcpserver: more than %d agent sessions", maxTokens)
	}
	if err := s.listenLocked(); err != nil {
		return "", "", nil, err
	}
	if token, err = newToken(); err != nil {
		return "", "", nil, err
	}
	sum := sha256.Sum256([]byte(token))
	s.tokens[sum] = root
	var once sync.Once
	revoke = func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.tokens, sum)
			s.dropUnusedSemLocked(root)
		})
	}
	return "http://127.0.0.1:" + strconv.Itoa(s.port) + endpointPath, token, revoke, nil
}

// RevokeRoot ends every token of root (the project was closed).
func (s *Server) RevokeRoot(root string) {
	root = filepath.Clean(root)
	s.mu.Lock()
	defer s.mu.Unlock()
	for h, r := range s.tokens {
		if r == root {
			delete(s.tokens, h)
		}
	}
	s.dropUnusedSemLocked(root)
}

func (s *Server) dropUnusedSemLocked(root string) {
	for _, r := range s.tokens {
		if r == root {
			return
		}
	}
	delete(s.sems, root)
}

// Live reports how many tokens are valid (tests, diagnostics).
func (s *Server) Live() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tokens)
}

func (s *Server) listenLocked() error {
	if s.srv != nil {
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("mcpserver: listen: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.port, s.cancel = ln.Addr().(*net.TCPAddr).Port, cancel
	s.srv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      callTimeout + 10*time.Second,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	s.served = make(chan struct{})
	go func(srv *http.Server, done chan struct{}) {
		defer close(done)
		_ = srv.Serve(ln)
	}(s.srv, s.served)
	return nil
}

// Stop closes the listener, cancels running tool calls and revokes every
// token (the server was switched off). The next Endpoint listens again, on a
// new port.
func (s *Server) Stop() {
	s.mu.Lock()
	srv, served, cancel := s.srv, s.served, s.cancel
	s.srv, s.served, s.cancel, s.port = nil, nil, nil, 0
	s.tokens, s.sems = map[tokenHash]string{}, map[string]chan struct{}{}
	s.mu.Unlock()
	if srv == nil {
		return
	}
	cancel()
	ctx, done := context.WithTimeout(context.Background(), shutdownTimeout)
	defer done()
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
	}
	<-served
}

// Listening reports whether the listener is open.
func (s *Server) Listening() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.srv != nil
}

// Close stops the server for good. It is safe to call more than once.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.Stop()
	return nil
}

// rootFor returns the project of a bearer token.
func (s *Server) rootFor(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	sum := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	root, ok := s.tokens[sum]
	return root, ok
}

// sem returns the semaphore of root's running tool calls.
func (s *Server) sem(root string) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.sems[root]
	if c == nil {
		c = make(chan struct{}, maxConcurrent)
		s.sems[root] = c
	}
	return c
}

func (s *Server) listenPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

func (s *Server) tools() []Tool {
	if s.opts.Tools == nil {
		return nil
	}
	return s.opts.Tools()
}

// newToken returns 32 random bytes, base64url-encoded (43 characters).
func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("mcpserver: token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
