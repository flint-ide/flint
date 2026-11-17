package main

import (
	"flint/internal/agent"
	as "flint/internal/agentsession"
)

// registerHostDrivers gives the background session host (hostmode) the same
// native drivers the window registers, through the same registerDrivers. What
// the window feeds them from the App is replaced by seams that fail closed
// until the host can read it itself:
//
//   - trust: hostUntrusted treats every folder as untrusted, so project agent
//     settings and the user's extra arguments never load in the host. The host
//     will read the trusted folders from state.json (and after
//     host.trustChanged) in step 3.9.
//   - agent settings (CLI path, model, arguments): hostAgentConfig is the empty
//     configuration, so the CLI is looked up on the PATH. Settings sync
//     (host.settingsChanged) replaces it in step 3.9.
//   - Flint's MCP server: none is offered (a nil provider); it moves into the
//     host in step 3.8.
//   - the action gate and custom ACP agents need the window and settings; the
//     host registers neither yet.
func registerHostDrivers(m *as.Manager) {
	registerDrivers(m, hostUntrusted, hostAgentConfig, nil)
}

func hostUntrusted(string) bool { return true }

func hostAgentConfig(agent.Kind) agent.Config { return agent.Config{} }
