package settings

import "fmt"

// MCPServer configures Flint's own MCP server, which gives the agents Flint
// starts read-only access to what Flint knows (internal/mcpserver).
type MCPServer struct {
	// Enabled offers the server to new agent sessions.
	Enabled bool     `json:"enabled"`
	Tools   MCPTools `json:"tools"`
	// TerminalLines is the most lines of one terminal's output an agent gets.
	TerminalLines int `json:"terminalLines"`
}

// MCPTools switches the server's tools on and off.
type MCPTools struct {
	Diagnostics bool `json:"diagnostics"`
	Editor      bool `json:"editor"`
	Terminals   bool `json:"terminals"`
	Tests       bool `json:"tests"`
	Kubernetes  bool `json:"kubernetes"`
}

// Bounds of MCPServer.TerminalLines.
const (
	MinTerminalLines     = 10
	MaxTerminalLines     = 2000
	defaultTerminalLines = 200
)

func defaultMCPServer() MCPServer {
	return MCPServer{
		Enabled:       true,
		Tools:         MCPTools{Diagnostics: true, Editor: true, Terminals: true, Tests: true, Kubernetes: true},
		TerminalLines: defaultTerminalLines,
	}
}

// normalizeMCPServer clamps the line count (0 or missing is the default).
func normalizeMCPServer(m MCPServer) MCPServer {
	out := m
	switch {
	case m.TerminalLines == 0:
		out.TerminalLines = defaultTerminalLines
	default:
		out.TerminalLines = max(MinTerminalLines, min(MaxTerminalLines, m.TerminalLines))
	}
	return out
}

func validateMCPServer(m MCPServer) error {
	if m.TerminalLines < MinTerminalLines || m.TerminalLines > MaxTerminalLines {
		return fmt.Errorf("MCP server terminal lines %d out of range %d–%d", m.TerminalLines, MinTerminalLines, MaxTerminalLines)
	}
	return nil
}
