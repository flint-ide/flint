package main

import (
	"fmt"
	"slices"

	"flint/internal/agent"
	"flint/internal/agentdefs"
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
// changes apply to new sessions. mcp offers Flint's own MCP server to each
// new session.
func registerDrivers(m *as.Manager, untrusted func(cwd string) bool, config func(agent.Kind) agent.Config, mcp as.MCPProvider) {
	for _, k := range agentdefs.Engines() {
		m.Register(string(k), nativeDriver(k, untrusted, func() agent.Config { return config(k) }, mcp, ""))
	}
}

// nativeDriver is the driver of native agent k. settings is read on every
// start; instructions are a defined agent's standing instructions ("" for none).
func nativeDriver(k agent.Kind, untrusted func(cwd string) bool, settings func() agent.Config, mcp as.MCPProvider, instructions string) as.Driver {
	if k == agent.Codex {
		return codex.New(codex.Options{Untrusted: untrusted, MCP: mcp, Settings: settings, Instructions: instructions})
	}
	return claude.New(claude.Options{Untrusted: untrusted, MCP: mcp, Settings: settings, Instructions: instructions})
}

// syncCustomAgents registers one ACP driver per custom agent in next and
// removes the ones only prev had; an unchanged agent keeps its driver.
// Sessions already running keep the driver that started them (Manager).
// untrusted folders refuse every custom agent (acpdriver.ErrRestricted); mcp
// offers Flint's MCP server to agents that speak HTTP MCP.
func syncCustomAgents(m *as.Manager, prev, next []settings.CustomAgent, untrusted func(cwd string) bool, mcp as.MCPProvider) {
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
		opts := customAgentOptions(c, untrusted)
		opts.MCP = mcp
		m.Register(c.ID, acpdriver.New(opts))
	}
}

// customAgentOptions starts custom agent c over ACP.
func customAgentOptions(c settings.CustomAgent, untrusted func(cwd string) bool) acpdriver.Options {
	return acpdriver.Options{Name: c.ID, Command: c.Command, Args: slices.Clone(c.Args), Untrusted: untrusted}
}

// installedAgents lists the agents that can start: the native CLIs found
// (natives, agent.Available: on the login PATH or at their configured path)
// in preference order, then the custom agents whose command resolves, in
// settings order, then the defined agents whose engine is installed (any
// native agent for one without a pinned engine), by name. lookPath is
// exec.LookPath once the login shell's PATH was imported.
func installedAgents(s settings.Settings, natives []agent.Kind, lookPath func(string) (string, error), defined []agentdefs.Definition) []string {
	out := []string{}
	for _, k := range natives {
		out = append(out, string(k))
	}
	for _, c := range s.CustomAgents {
		if _, err := lookPath(c.Command); err == nil {
			out = append(out, c.ID)
		}
	}
	for _, d := range defined {
		if (d.Engine == "" && len(natives) > 0) || slices.Contains(natives, agent.Kind(d.Engine)) {
			out = append(out, d.Name)
		}
	}
	return out
}

// syncDefinedAgents builds the drivers of the definitions in next: an
// unchanged definition keeps its driver from prev, so its chats share one
// driver per engine. Nothing is registered with the session manager by name:
// a chat starts on the driver of the load it comes from (Manager.StartWith),
// and a driver that runs sessions is kept by the manager until they end.
func syncDefinedAgents(prev []*definedAgent, next []agentdefs.Definition, newDriver func(agentdefs.Definition) *definedAgent) []*definedAgent {
	out := make([]*definedAgent, 0, len(next))
	for _, d := range next {
		if i := slices.IndexFunc(prev, func(p *definedAgent) bool { return p.def.Same(d) }); i >= 0 {
			out = append(out, prev[i])
			continue
		}
		out = append(out, newDriver(d))
	}
	return out
}

// definedAgent is a defined agent's drivers: one per engine, each carrying
// the definition's instructions (and its model, on the pinned engine). A new
// conversation runs on the engine resolve picks; a resumed one on the engine
// it started on.
type definedAgent struct {
	def     agentdefs.Definition
	engines map[agent.Kind]as.Driver
	resolve func(agentdefs.Definition) (agent.Kind, error)
}

// newDefinedAgent builds d's drivers: one per engine, created now but
// starting nothing until a session does. config is the engine's settings.
func newDefinedAgent(d agentdefs.Definition, untrusted func(cwd string) bool, config func(agent.Kind) agent.Config, mcp as.MCPProvider, resolve func(agentdefs.Definition) (agent.Kind, error)) *definedAgent {
	da := &definedAgent{def: d, engines: map[agent.Kind]as.Driver{}, resolve: resolve}
	for _, k := range agentdefs.Engines() {
		settings := func() agent.Config { return definedConfig(d, k, config(k)) }
		da.engines[k] = nativeDriver(k, untrusted, settings, mcp, d.Instructions)
	}
	return da
}

// route picks the engine's driver: engine is the one a resumed conversation
// started on, "" for a new conversation (resolved now).
func (d *definedAgent) route(engine string) (as.Driver, agent.Kind, error) {
	k := agent.Kind(engine)
	if engine == "" {
		var err error
		if k, err = d.resolve(d.def); err != nil {
			return nil, "", err
		}
	}
	drv := d.engines[k]
	if drv == nil {
		return nil, "", fmt.Errorf("%s: unknown engine %q", d.def.Name, engine)
	}
	return drv, k, nil
}

// isNative reports one of the agent CLIs Flint drives directly.
func isNative(name string) bool { return agent.Valid(agent.Kind(name)) }
