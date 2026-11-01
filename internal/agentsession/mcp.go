package agentsession

// MCPServerName is the name of Flint's own MCP server in an agent's
// configuration; its tools appear as mcp__flint__<tool> in Claude Code.
const MCPServerName = "flint"

// MCPTokenEnv is the environment variable CLI agents read the bearer token
// from, so it never appears on a command line (other users can list those).
const MCPTokenEnv = "FLINT_MCP_TOKEN"

// MCPServer is Flint's read-only MCP server (Streamable HTTP) as offered to
// the agent of one session.
type MCPServer struct {
	URL   string
	Token string // bearer token; it grants read access to one project
}

// MCPProvider returns the server for a session in cwd; ok is false when none
// is offered (switched off in Settings, Restricted Mode, or it failed to
// start). Drivers call it on every Start and Resume.
type MCPProvider func(cwd string) (server MCPServer, ok bool)

// Offer calls p, treating a nil provider as "none".
func (p MCPProvider) Offer(cwd string) (MCPServer, bool) {
	if p == nil {
		return MCPServer{}, false
	}
	s, ok := p(cwd)
	return s, ok && s.URL != "" && s.Token != ""
}
