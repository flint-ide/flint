package main

import (
	"slices"

	"flint/internal/agent"
	as "flint/internal/agentsession"
	"flint/internal/agentsession/claude"
	"flint/internal/agentsession/codex"
)

// registerDrivers adds every native agent driver Flint ships (no npm adapters).
// Drivers resolve their CLI lazily on Start, so registering costs nothing.
func registerDrivers(m *as.Manager) {
	m.Register(string(agent.Claude), claude.New(claude.Options{}))
	m.Register(string(agent.Codex), codex.New(codex.Options{}))
}

// installedAgents filters registered drivers to CLIs present on the login PATH.
func installedAgents(registered []string) []string {
	installed := agent.Available()
	out := make([]string, 0, len(registered))
	for _, name := range registered {
		if slices.Contains(installed, agent.Kind(name)) {
			out = append(out, name)
		}
	}
	return out
}
