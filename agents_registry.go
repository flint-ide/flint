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
// untrusted marks folders whose project agent settings (and the user's extra
// arguments) must not load; config is read on every start, so settings
// changes apply to new sessions.
func registerDrivers(m *as.Manager, untrusted func(cwd string) bool, config func(agent.Kind) agent.Config) {
	m.Register(string(agent.Claude), claude.New(claude.Options{
		Untrusted: untrusted,
		Settings:  func() agent.Config { return config(agent.Claude) },
	}))
	m.Register(string(agent.Codex), codex.New(codex.Options{
		Untrusted: untrusted,
		Settings:  func() agent.Config { return config(agent.Codex) },
	}))
}

// installedAgents filters registered drivers to CLIs present on the login PATH
// (or at their configured path).
func installedAgents(registered []string, configs map[agent.Kind]agent.Config) []string {
	installed := agent.Available(configs)
	out := make([]string, 0, len(registered))
	for _, name := range registered {
		if slices.Contains(installed, agent.Kind(name)) {
			out = append(out, name)
		}
	}
	return out
}
