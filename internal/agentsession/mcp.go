package agentsession

import (
	"context"
	"sync"
)

// MCPServerName is the name of Flint's own MCP server in an agent's
// configuration; its tools appear as mcp__flint__<tool> in Claude Code. A
// user's own server with this name is told apart by the session: only a
// session that was given Flint's server treats "flint" tools as Flint's.
const MCPServerName = "flint"

// MCPTokenEnv is the environment variable CLI agents read the bearer token
// from, so it never appears on a command line (other users can list those).
// Claude Code expands ${FLINT_MCP_TOKEN} in an inline --mcp-config header
// (checked with Claude Code 2.1.314); Codex reads it via bearer_token_env_var.
const MCPTokenEnv = "FLINT_MCP_TOKEN"

// MCPReadTools are the tools of Flint's server that only show the project's
// own state (problems, the editor, test results). With the reads policy they
// run without asking; the others (terminal output, cluster status) can show
// anything the user saw, so they always ask. The tools that act (run_tests,
// run_configuration, ...) are never in this list: see actions.go.
var MCPReadTools = []string{"get_diagnostics", "get_editor_state", "get_test_results"}

// MCPServer is Flint's read-only MCP server (Streamable HTTP) as offered to
// the agent of one session.
type MCPServer struct {
	URL   string
	Token string // bearer token; it grants read access to one project
	// Release revokes Token; drivers call it (through Revoke) when the
	// session's agent process ends, or when the session never started.
	Release func()
}

// Revoke ends the server's token; safe on a nil server and more than once.
func (s *MCPServer) Revoke() {
	if s != nil && s.Release != nil {
		s.Release()
	}
}

// MCPProvider returns the server for a new session in cwd, with a token for
// that session only; ok is false when none is offered (switched off in
// Settings, Restricted Mode, or it failed to start). Drivers call it on every
// Start and Resume with the context they were started with (it may carry a
// CallerSlot, see WithCaller), and must Revoke what they got.
type MCPProvider func(ctx context.Context, cwd string) (server MCPServer, ok bool)

// Offer calls p, treating a nil provider as "none".
func (p MCPProvider) Offer(ctx context.Context, cwd string) (MCPServer, bool) {
	if p == nil {
		return MCPServer{}, false
	}
	s, ok := p(ctx, cwd)
	if ok && (s.URL == "" || s.Token == "") {
		s.Revoke()
		return MCPServer{}, false
	}
	return s, ok
}

// CallerSlot carries the id of a chat that is still starting. A token is
// issued while the driver starts the agent, before the agent has told Flint
// its session id; the slot lets that token resolve to the id once Manager
// fills it in, so a tool call can tell which chat is calling.
type CallerSlot struct {
	mu sync.Mutex
	id string
}

// Set records the session id; Manager calls it once Start or Resume returned.
func (s *CallerSlot) Set(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.id = id
	s.mu.Unlock()
}

// ID returns the session id, or "" while it is not known yet (and on a nil
// slot, so callers need no check).
func (s *CallerSlot) ID() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

type callerKey struct{}

// WithCaller returns ctx carrying slot for an MCPProvider to find.
func WithCaller(ctx context.Context, slot *CallerSlot) context.Context {
	return context.WithValue(ctx, callerKey{}, slot)
}

// CallerSlotFrom returns the slot WithCaller stored in ctx, or nil.
func CallerSlotFrom(ctx context.Context) *CallerSlot {
	slot, _ := ctx.Value(callerKey{}).(*CallerSlot)
	return slot
}
