package main

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	as "flint/internal/agentsession"
	"flint/internal/config"
)

const (
	maxEngineName     = 64 // an agent name (SessionInfo.Engine)
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
	m := as.NewManager(func(e as.Event) {
		a.emit(eventAgentEvent, e)
		a.observeTaskStart(e) // before the tracker moves the queue (tasks_queue.go)
		a.tasks.Observe(e)    // agent tasks follow their chats (tasks_state.go)
		a.observeChat(e)      // states and output for orchestration, notices (chat_notify.go)
		a.observeUsage(e)     // usage_state.go
	})
	m.SetActionGate(a.actionGate()) // Flint's tools that act always ask (api_mcp_actions.go)
	registerDrivers(m, a.untrusted, a.agentConfig, a.offerMCP)
	syncCustomAgents(m, nil, a.currentSettings().CustomAgents, a.untrusted, a.offerMCP)
	if p, ok := a.agentPolicy(); ok {
		m.SetPolicy(p)
	}
	return m
}

// Agents lists the agents available for chat sessions: the native CLIs found,
// then the custom ACP agents whose command resolves, then the defined agents
// whose engine is installed.
func (c *AgentChatAPI) Agents() []string {
	return c.app.availableAgents()
}

// Start opens a conversation with agent in cwd.
func (c *AgentChatAPI) Start(agent, cwd string) (as.SessionInfo, error) {
	ctx, cancel := context.WithTimeout(c.app.ctx, agentStartTimeout)
	defer cancel()
	// The UI adds the tab itself when this returns; the "chat:opened" event
	// openChat emits is then a no-op for it (store/agentChat.ts adopt).
	return c.app.openChat(ctx, ChatRequest{Agent: agent, Cwd: cwd})
}

// Resume reopens a conversation from an earlier run (after a restart). The
// returned ID may differ from sessionID if the agent forked it. engine is the
// SessionInfo.Engine the conversation started on ("" when not known): a
// defined agent resumes on it, whatever its default engine is now.
func (c *AgentChatAPI) Resume(agent, cwd, sessionID, engine string) (as.SessionInfo, error) {
	if len(engine) > maxEngineName || strings.ContainsFunc(engine, unicode.IsControl) {
		return as.SessionInfo{}, errors.New("invalid engine")
	}
	ctx, cancel := context.WithTimeout(c.app.ctx, agentStartTimeout)
	defer cancel()
	// A definition added, changed or removed since: read now, and resumed on
	// exactly what this read found.
	info, err := c.app.resumeChatWith(ctx, c.app.loadDefinedAgents(), agent, cwd, sessionID, engine)
	if err == nil {
		c.app.bindTaskChat(info, cwd, true, sessionID)
		c.app.usageChats.bind(info.ID, usageChat{chat: sessionID, agent: info.Agent, cwd: cwd, model: info.Model})
		c.app.adoptChat(info, cwd)
	}
	return info, err
}

// Prompt sends a message; the reply streams as "agent:event" events.
func (c *AgentChatAPI) Prompt(sessionID, text string, attachments []as.Attachment) error {
	return c.app.promptChat(sessionID, text, attachments, true, nil)
}

// Cancel interrupts the running turn.
func (c *AgentChatAPI) Cancel(sessionID string) error { return c.app.agents.Cancel(sessionID) }

// Respond answers a permission request.
func (c *AgentChatAPI) Respond(sessionID, requestID, optionID string) error {
	return c.app.agents.Respond(sessionID, requestID, optionID)
}

// Stop ends a conversation and its agent process, and the agents it started:
// they ran under its grant, which ends with it.
func (c *AgentChatAPI) Stop(sessionID string) error {
	return c.app.stopChatTree(sessionID)
}

// Policy returns which permission requests are answered automatically.
func (c *AgentChatAPI) Policy() as.Policy { return c.app.agents.Policy() }

// SetPolicy changes and remembers the approval policy.
func (c *AgentChatAPI) SetPolicy(p as.Policy) error {
	c.app.agents.SetPolicy(p)
	return c.app.saveAgentPolicy(p)
}

// untrusted reports a folder in Restricted Mode, for the agent drivers.
func (a *App) untrusted(cwd string) bool { return !a.isTrusted(cwd) }

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
