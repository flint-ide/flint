package agentsession

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
// Start and Resume, and must Revoke what they got.
type MCPProvider func(cwd string) (server MCPServer, ok bool)

// Offer calls p, treating a nil provider as "none".
func (p MCPProvider) Offer(cwd string) (MCPServer, bool) {
	if p == nil {
		return MCPServer{}, false
	}
	s, ok := p(cwd)
	if ok && (s.URL == "" || s.Token == "") {
		s.Revoke()
		return MCPServer{}, false
	}
	return s, ok
}
