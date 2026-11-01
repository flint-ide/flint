package main

import (
	"slices"

	"flint/internal/agent"
	as "flint/internal/agentsession"
	"flint/internal/agentsession/acpdriver"
	"flint/internal/agentsession/claude"
	"flint/internal/agentsession/codex"
	"flint/internal/settings"
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

// syncCustomAgents registers one ACP driver per custom agent in next and
// removes the ones only prev had; an unchanged agent keeps its driver.
// Sessions already running keep the driver that started them (Manager).
// untrusted folders refuse every custom agent (acpdriver.ErrRestricted).
func syncCustomAgents(m *as.Manager, prev, next []settings.CustomAgent, untrusted func(cwd string) bool) {
	for _, p := range prev {
		if !slices.ContainsFunc(next, func(c settings.CustomAgent) bool { return c.ID == p.ID }) {
			m.Unregister(p.ID)
		}
	}
	for _, c := range next {
		i := slices.IndexFunc(prev, func(p settings.CustomAgent) bool { return p.ID == c.ID })
		if i >= 0 && prev[i].Equal(c) {
			continue
		}
		m.Register(c.ID, acpdriver.New(customAgentOptions(c, untrusted)))
	}
}

// customAgentOptions starts custom agent c over ACP.
func customAgentOptions(c settings.CustomAgent, untrusted func(cwd string) bool) acpdriver.Options {
	return acpdriver.Options{Name: c.ID, Command: c.Command, Args: slices.Clone(c.Args), Untrusted: untrusted}
}

// installedAgents lists the agents that can start: the native CLIs found
// on the login PATH (or at their configured path) in preference order, then
// the custom agents whose command resolves, in settings order. lookPath is
// exec.LookPath once the login shell's PATH was imported.
func installedAgents(s settings.Settings, lookPath func(string) (string, error)) []string {
	out := []string{}
	for _, k := range agent.Available(s.AgentConfigs()) {
		out = append(out, string(k))
	}
	for _, c := range s.CustomAgents {
		if _, err := lookPath(c.Command); err == nil {
			out = append(out, c.ID)
		}
	}
	return out
}

// isNative reports one of the agent CLIs Flint drives directly.
func isNative(name string) bool { return agent.Valid(agent.Kind(name)) }
