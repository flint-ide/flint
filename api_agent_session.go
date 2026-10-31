package main

import (
	"context"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/config"
)

const (
	eventAgentEvent   = "agent:event"
	agentStartTimeout = 90 * time.Second // first start may authenticate / warm caches
)

// AgentChatAPI runs conversational agent sessions (Claude Code, Codex, ACP agents)
// rendered in Flint's agent panel.
type AgentChatAPI struct {
	app *App
}

// newAgentManager builds the session manager with every installed driver.
func newAgentManager(a *App) *as.Manager {
	m := as.NewManager(func(e as.Event) { a.emit(eventAgentEvent, e) })
	registerDrivers(m, func(cwd string) bool { return !a.isTrusted(cwd) }, a.agentConfig)
	if p, ok := a.agentPolicy(); ok {
		m.SetPolicy(p)
	}
	return m
}

// Agents lists the agents available for chat sessions.
func (c *AgentChatAPI) Agents() []string {
	return installedAgents(c.app.agents.Agents(), c.app.currentSettings().AgentConfigs())
}

// Start opens a conversation with agent in cwd.
func (c *AgentChatAPI) Start(agent, cwd string) (as.SessionInfo, error) {
	ctx, cancel := context.WithTimeout(c.app.ctx, agentStartTimeout)
	defer cancel()
	return c.app.agents.Start(ctx, agent, cwd)
}

// Resume reopens a conversation from an earlier run (after a restart). The
// returned ID may differ from sessionID if the agent forked it.
func (c *AgentChatAPI) Resume(agent, cwd, sessionID string) (as.SessionInfo, error) {
	ctx, cancel := context.WithTimeout(c.app.ctx, agentStartTimeout)
	defer cancel()
	return c.app.agents.Resume(ctx, agent, cwd, sessionID)
}

// Prompt sends a message; the reply streams as "agent:event" events.
func (c *AgentChatAPI) Prompt(sessionID, text string, attachments []as.Attachment) error {
	return c.app.agents.Prompt(sessionID, text, attachments)
}

// Cancel interrupts the running turn.
func (c *AgentChatAPI) Cancel(sessionID string) error { return c.app.agents.Cancel(sessionID) }

// Respond answers a permission request.
func (c *AgentChatAPI) Respond(sessionID, requestID, optionID string) error {
	return c.app.agents.Respond(sessionID, requestID, optionID)
}

// Stop ends a conversation and its agent process.
func (c *AgentChatAPI) Stop(sessionID string) error { return c.app.agents.Stop(sessionID) }

// Policy returns which permission requests are answered automatically.
func (c *AgentChatAPI) Policy() as.Policy { return c.app.agents.Policy() }

// SetPolicy changes and remembers the approval policy.
func (c *AgentChatAPI) SetPolicy(p as.Policy) error {
	c.app.agents.SetPolicy(p)
	return c.app.saveAgentPolicy(p)
}

func (a *App) agentPolicy() (as.Policy, bool) {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.state.AgentPolicy == nil {
		return as.Policy{}, false
	}
	return *a.state.AgentPolicy, true
}

func (a *App) saveAgentPolicy(p as.Policy) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	next := a.state
	next.AgentPolicy = &p
	if err := config.SaveJSON(a.statePath(), next); err != nil {
		return err
	}
	a.state = next
	return nil
}
