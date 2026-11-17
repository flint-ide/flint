package main

import (
	"context"
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
// are bounded (agentdefs limits). One call reads them once (loadDefinedAgents,
// agentView) and passes what it read along.

// loadedAgents is one read of the definitions: the set, and the driver of
// each definition (by name) to start its chats on. A chat starts on the
// driver of the read its own call made, never on whatever a later read left
// behind (openChatWith, resumeChatWith).
type loadedAgents struct {
	agentdefs.Set
	drivers map[string]*definedAgent
}

// loadDefinedAgents reads the definitions of the personal folder and of the
// workspace roots (project ones only when trusted) and builds their drivers.
// Running chats keep their driver.
func (a *App) loadDefinedAgents() loadedAgents {
	// One load at a time, from reading the files to keeping the drivers: two
	// overlapping loads could otherwise finish out of order and keep what the
	// older one read.
	a.definedMu.Lock()
	defer a.definedMu.Unlock()
	var roots []agentdefs.Root
	for _, r := range a.projectRoots() {
		roots = append(roots, agentdefs.Root{Path: r, Trusted: a.isTrusted(r)})
	}
	set := agentdefs.Load(agentdefs.PersonalDir(a.configDir), roots, customAgentID(a.currentSettings()))
	// Under settingsMu, as the custom agents are registered (updateSettings),
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
	out := loadedAgents{Set: set, drivers: map[string]*definedAgent{}}
	if a.agentMgr != nil {
		a.definedAgents = syncDefinedAgents(a.definedAgents, set.Agents, func(d agentdefs.Definition) *definedAgent {
			return newDefinedAgent(d, a.untrusted, a.agentConfig, a.offerMCP, a.resolveEngine)
		})
		for _, d := range a.definedAgents {
			out.drivers[d.def.Name] = d
		}
	}
	return out
}

// route is the driver a chat with agent name starts (engine "") or resumes
// (the engine it started on) on, and that engine, when name is a defined
// agent of this read; otherwise drv is nil and engine is name itself.
func (l loadedAgents) route(name, engine string) (drv as.Driver, k string, err error) {
	d := l.drivers[name]
	if d == nil {
		return nil, name, nil
	}
	drv, kind, err := d.route(engine)
	return drv, string(kind), err
}

// startChat starts a chat with agent name in cwd: on drv when it is a
// defined agent's engine driver (loadedAgents.route), else by name.
func (a *App) startChat(ctx context.Context, drv as.Driver, name, cwd string) (as.SessionInfo, error) {
	if drv != nil {
		return a.agentMgr.StartWith(ctx, name, cwd, drv)
	}
	return a.agents.Start(ctx, name, cwd)
}

// resumeChatWith reopens conversation sessionID of agent name with the
// definitions of loaded: a defined agent on engine, the one it started on
// ("" resolves it now).
func (a *App) resumeChatWith(ctx context.Context, loaded loadedAgents, name, cwd, sessionID, engine string) (as.SessionInfo, error) {
	drv, _, err := loaded.route(name, engine)
	if err != nil {
		return as.SessionInfo{}, err
	}
	if drv != nil {
		return a.agentMgr.ResumeWith(ctx, name, cwd, sessionID, drv)
	}
	return a.agents.Resume(ctx, name, cwd, sessionID)
}

// customAgentID reports the ids of s's custom agents.
func customAgentID(s settings.Settings) func(string) bool {
	return func(name string) bool {
		_, ok := s.CustomAgent(name)
		return ok
	}
}

// agentView is what one call knows about the agents, each read once: the
// settings, the definitions, the native CLIs installed and the default agent.
type agentView struct {
	settings  settings.Settings
	defined   loadedAgents
	natives   []agent.Kind
	preferred string
}

func (a *App) agentView() agentView {
	defined := a.loadDefinedAgents()
	s := a.currentSettings()
	return agentView{settings: s, defined: defined, natives: agent.Available(s.AgentConfigs()), preferred: a.preferredAgent()}
}

// available lists the agents that can start now (installedAgents).
func (v agentView) available() []string {
	return installedAgents(v.settings, v.natives, exec.LookPath, v.defined.Agents)
}

// resolveEngine picks the engine a new conversation of d runs on
// (agentdefs.ResolveEngine).
func (v agentView) resolveEngine(d agentdefs.Definition) (agent.Kind, error) {
	return agentdefs.ResolveEngine(d, v.preferred, v.natives)
}

// availableAgents lists the agents that can start now.
func (a *App) availableAgents() []string { return a.agentView().available() }

// resolveEngine picks the engine a new conversation of d runs on: its pinned
// one, else the default agent when that is native, else the first native
// agent installed (agentdefs.ResolveEngine). It reads settings and the PATH
// now, when a conversation starts.
func (a *App) resolveEngine(d agentdefs.Definition) (agent.Kind, error) {
	return agentdefs.ResolveEngine(d, a.preferredAgent(), agent.Available(a.currentSettings().AgentConfigs()))
}

// definedConfig is engine k's settings for d: d's model replaces the
// engine's (definedModel). c is read from settings when a conversation starts
// or resumes, so a tier resolves then, on the engine the conversation runs on.
func definedConfig(d agentdefs.Definition, k agent.Kind, c agent.Config) agent.Config {
	if m := definedModel(d, k, c); m != "" {
		c.Model = m
	}
	return c
}

// definedModel is the model d sets on engine k with settings c: a tier's
// model from c.Tiers (settings over the defaults), or d's model name on the
// engine d pins (model names belong to one engine). "" when d sets none
// there (no model, an unmapped tier): the engine's own model applies.
func definedModel(d agentdefs.Definition, k agent.Kind, c agent.Config) string {
	switch {
	case agent.IsTier(d.Model):
		return c.Tiers[d.Model]
	case d.Model != "" && agent.Kind(d.Engine) == k:
		return d.Model
	}
	return ""
}

// nativeRun is the native program behind an agent name: the engine, its
// settings (with a defined agent's model) and a defined agent's instructions.
type nativeRun struct {
	kind         agent.Kind
	cfg          agent.Config
	instructions string
}

// native is the native program behind agent name: the agent itself for
// claude and codex, the resolved engine of a defined agent. ok is false for a
// custom (ACP) agent or an unknown name. No defined agent's name is ever cast
// to agent.Kind.
func (v agentView) native(name string) (run nativeRun, ok bool, err error) {
	if isNative(name) {
		return nativeRun{kind: agent.Kind(name), cfg: v.settings.AgentConfig(name)}, true, nil
	}
	if _, custom := v.settings.CustomAgent(name); custom {
		return nativeRun{}, false, nil
	}
	d, found := v.defined.Find(name)
	if !found {
		return nativeRun{}, false, nil
	}
	k, err := v.resolveEngine(d)
	if err != nil {
		return nativeRun{}, false, err
	}
	return nativeRun{kind: k, cfg: definedConfig(d, k, v.settings.AgentConfig(string(k))), instructions: d.Instructions}, true, nil
}
