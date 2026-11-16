package settings

import "fmt"

// MCPServer configures Flint's own MCP server, which gives the agents Flint
// starts read-only access to what Flint knows and, when switched on, tools
// that act (internal/mcpserver).
type MCPServer struct {
	// Enabled offers the server to new agent sessions.
	Enabled bool     `json:"enabled"`
	Tools   MCPTools `json:"tools"`
	// Actions switches on the tools that act.
	Actions MCPActions `json:"actions"`
	// TerminalLines is the most lines of one terminal's output an agent gets.
	TerminalLines int `json:"terminalLines"`
	// TestWaitSeconds is how long run_tests waits for a test run to finish
	// before it answers with the results so far.
	TestWaitSeconds int `json:"testWaitSeconds"`
}

// MCPActions switches on the tools that do something in Flint for an agent,
// by group. All are off by default: such a tool isn't listed to agents until
// its group is on, and then the user is asked before every call.
type MCPActions struct {
	// Tests: run_tests (the test runner, like the gutter ▶).
	Tests bool `json:"tests"`
	// Run: run_configuration and get_run_output (run configurations).
	Run bool `json:"run"`
	// HTTP: send_http_request (named requests of .http files).
	HTTP bool `json:"http"`
	// SQL: run_sql_query (read-only database connections).
	SQL bool `json:"sql"`
}

// MCPTools switches the server's tools on and off. Diagnostics, the editor
// and test results show the project's own state and are on by default.
// Terminal output can show anything the user saw (credentials printed
// outside the project), so it is off by default and, when on, the agent asks
// before each call.
type MCPTools struct {
	Diagnostics bool `json:"diagnostics"`
	Editor      bool `json:"editor"`
	Terminals   bool `json:"terminals"`
	Tests       bool `json:"tests"`
}

// Bounds of MCPServer.TerminalLines.
const (
	MinTerminalLines     = 10
	MaxTerminalLines     = 2000
	defaultTerminalLines = 200
)

// Bounds of MCPServer.TestWaitSeconds.
const (
	MinTestWaitSeconds     = 10
	MaxTestWaitSeconds     = 540
	defaultTestWaitSeconds = 120
)

func defaultMCPServer() MCPServer {
	return MCPServer{
		Enabled:         true,
		Tools:           MCPTools{Diagnostics: true, Editor: true, Tests: true},
		TerminalLines:   defaultTerminalLines,
		TestWaitSeconds: defaultTestWaitSeconds,
	}
}

// normalizeMCPServer clamps the line count and the test wait (0 or missing
// is the default).
func normalizeMCPServer(m MCPServer) MCPServer {
	out := m
	switch {
	case m.TerminalLines == 0:
		out.TerminalLines = defaultTerminalLines
	default:
		out.TerminalLines = max(MinTerminalLines, min(MaxTerminalLines, m.TerminalLines))
	}
	switch {
	case m.TestWaitSeconds == 0:
		out.TestWaitSeconds = defaultTestWaitSeconds
	default:
		out.TestWaitSeconds = max(MinTestWaitSeconds, min(MaxTestWaitSeconds, m.TestWaitSeconds))
	}
	return out
}

func validateMCPServer(m MCPServer) error {
	if m.TerminalLines < MinTerminalLines || m.TerminalLines > MaxTerminalLines {
		return fmt.Errorf("MCP server terminal lines %d out of range %d–%d", m.TerminalLines, MinTerminalLines, MaxTerminalLines)
	}
	if m.TestWaitSeconds < MinTestWaitSeconds || m.TestWaitSeconds > MaxTestWaitSeconds {
		return fmt.Errorf("MCP server test wait %d s out of range %d–%d", m.TestWaitSeconds, MinTestWaitSeconds, MaxTestWaitSeconds)
	}
	return nil
}
