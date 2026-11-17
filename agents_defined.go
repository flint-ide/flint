package main

import (
	"fmt"
	"os/exec"
	"slices"

	"flint/internal/agent"
	"flint/internal/agentdefs"
	as "flint/internal/agentsession"
	"flint/internal/settings"
)

// Agent definitions (docs/design/agent-definitions.md) are files, so they are
// read again whenever they matter: when agents are listed and before a chat
// starts or resumes. A changed, added or removed file, a workspace folder
// opened or closed, or a folder trusted, therefore applies to the next
// conversation without a restart or a watcher. There are few files and they
// are bounded (agentdefs limits).

// loadDefinedAgents reads the definitions of the personal folder and of the
// workspace roots (project ones only when trusted) and registers them with
// the session manager. Running chats keep their driver.
func (a *App) loadDefinedAgents() agentdefs.Set {
	// One load at a time, from reading the files to registering them: two
	// overlapping loads could otherwise finish out of order and register what
	// the older one read, such as a definition of a folder untrusted since.
	a.definedMu.Lock()
	defer a.definedMu.Unlock()
	var roots []agentdefs.Root
	for _, r := range a.projectRoots() {
		roots = append(roots, agentdefs.Root{Path: r, Trusted: a.isTrusted(r)})
	}
	set := agentdefs.Load(agentdefs.PersonalDir(a.configDir), roots, customAgentID(a.currentSettings()))
	// Registered under settingsMu, as the custom agents are (updateSettings),
	// so a custom agent saved while the files were read keeps its name.
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	taken := customAgentID(a.settings)
	set.Agents = slices.DeleteFunc(set.Agents, func(d agentdefs.Definition) bool {
		if taken(d.Name) {
			set.Problems = append(set.Problems, fmt.Sprintf("%s: %s is a custom agent's id; choose another name", d.Path, d.Name))
			return true
		}
		return false
	})
	if a.agentMgr != nil {
		syncDefinedAgents(a.agentMgr, a.definedAgents, set.Agents, taken, func(d agentdefs.Definition) as.Driver {
			return newDefinedAgent(d, a.untrusted, a.agentConfig, a.offerMCP, a.resolveEngine)
		})
		a.definedAgents = set.Agents
	}
	return set
}

// customAgentID reports the ids of s's custom agents.
func customAgentID(s settings.Settings) func(string) bool {
	return func(name string) bool {
		_, ok := s.CustomAgent(name)
		return ok
	}
}

// availableAgents lists the agents that can start now (installedAgents).
func (a *App) availableAgents() []string {
	defined := a.loadDefinedAgents().Agents
	return installedAgents(a.currentSettings(), exec.LookPath, defined)
}

// resolveEngine picks the engine a new conversation of d runs on: its pinned
// one, else the default agent when that is native, else the first native
// agent installed (agentdefs.ResolveEngine).
func (a *App) resolveEngine(d agentdefs.Definition) (agent.Kind, error) {
	return agentdefs.ResolveEngine(d, a.preferredAgent(), agent.Available(a.currentSettings().AgentConfigs()))
}

// definedConfig is engine k's settings for d: d's model replaces the engine's
// on the engine d pins (model names belong to one engine).
func definedConfig(d agentdefs.Definition, k agent.Kind, c agent.Config) agent.Config {
	if d.Model != "" && agent.Kind(d.Engine) == k {
		c.Model = d.Model
	}
	return c
}

// agentEngine is the native program behind agent name and its settings: the
// agent itself for claude and codex, the resolved engine of a defined agent
// (with its model). ok is false for a custom (ACP) agent or an unknown name.
// No defined agent's name is ever cast to agent.Kind.
func (a *App) agentEngine(name string) (k agent.Kind, cfg agent.Config, ok bool, err error) {
	if isNative(name) {
		k = agent.Kind(name)
		return k, a.agentConfig(k), true, nil
	}
	if _, custom := a.currentSettings().CustomAgent(name); custom {
		return "", agent.Config{}, false, nil
	}
	d, found := a.loadDefinedAgents().Find(name)
	if !found {
		return "", agent.Config{}, false, nil
	}
	if k, err = a.resolveEngine(d); err != nil {
		return "", agent.Config{}, false, err
	}
	return k, definedConfig(d, k, a.agentConfig(k)), true, nil
}
