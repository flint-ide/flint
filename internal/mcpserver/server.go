// Package mcpserver serves Flint's read-only context (diagnostics, open
// editors, terminal output, test results, cluster status) to the coding
// agents Flint starts, over the Model Context Protocol's Streamable HTTP
// transport: JSON-RPC 2.0 messages POSTed to one endpoint, answered with
// application/json (protocol 2025-06-18; no server-sent events, no
// sessions). Only the standard library is used.
//
// The server listens on 127.0.0.1 with a port the OS picks, starting on the
// first Endpoint call. Every project gets its own random bearer token, and a
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
	maxRoots        = 256      // projects with a token
	maxConcurrent   = 4        // tool calls running at once (they may start kubectl)
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

// Server is the MCP endpoint. It is safe for concurrent use.
type Server struct {
	opts  Options
	calls chan struct{} // semaphore of running tool calls

	ctx    context.Context // cancelled by Close: ends running tool calls
	cancel context.CancelFunc

	mu     sync.Mutex
	tokens map[[sha256.Size]byte]string // hash of a token → its project root
	byRoot map[string]string            // project root → token
	srv    *http.Server
	served chan struct{} // closed when Serve returned
	port   int
	closed bool
}

// New returns a server; nothing listens until Endpoint is called.
func New(opts Options) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		opts:   opts,
		calls:  make(chan struct{}, maxConcurrent),
		ctx:    ctx,
		cancel: cancel,
		tokens: map[[sha256.Size]byte]string{},
		byRoot: map[string]string{},
	}
}

// Endpoint returns the URL and the bearer token an agent working in root
// uses, starting the listener on first use. A project keeps its token until
// Close.
func (s *Server) Endpoint(root string) (url, token string, err error) {
	if !filepath.IsAbs(root) {
		return "", "", fmt.Errorf("mcpserver: root %q is not absolute", root)
	}
	root = filepath.Clean(root)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", "", ErrClosed
	}
	if err := s.listenLocked(); err != nil {
		return "", "", err
	}
	token, ok := s.byRoot[root]
	if !ok {
		if len(s.byRoot) >= maxRoots {
			return "", "", fmt.Errorf("mcpserver: more than %d projects", maxRoots)
		}
		if token, err = newToken(); err != nil {
			return "", "", err
		}
		s.byRoot[root] = token
		s.tokens[sha256.Sum256([]byte(token))] = root
	}
	return "http://127.0.0.1:" + strconv.Itoa(s.port) + endpointPath, token, nil
}

func (s *Server) listenLocked() error {
	if s.srv != nil {
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("mcpserver: listen: %w", err)
	}
	s.port = ln.Addr().(*net.TCPAddr).Port
	s.srv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      callTimeout + 10*time.Second,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return s.ctx },
	}
	s.served = make(chan struct{})
	go func(srv *http.Server, done chan struct{}) {
		defer close(done)
		_ = srv.Serve(ln)
	}(s.srv, s.served)
	return nil
}

// Close stops the listener, cancels running tool calls and forgets every
// token. It is safe to call more than once.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	srv, served := s.srv, s.served
	s.tokens, s.byRoot = map[[sha256.Size]byte]string{}, map[string]string{}
	s.mu.Unlock()
	s.cancel()
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		_ = srv.Close()
	}
	<-served
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
