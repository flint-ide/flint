package main

import (
	"context"
	"errors"
	"fmt"

	as "flint/internal/agentsession"
	"flint/internal/orchestrate"
	"flint/internal/redact"
)

// eventChatOpened tells the UI that a chat exists which it did not ask for
// (or that its own request created): the payload is ChatOpened.
const eventChatOpened = "chat:opened"

// ChatRequest describes a chat to open from Go: by the UI (AgentChatAPI.Start),
// by an agent that starts another one, or by the task queue.
type ChatRequest struct {
	Agent string
	Cwd   string
	// Prompt, when set, is sent as the chat's first message through the same
	// path as AgentChatAPI.Prompt (usage gate, task tracking).
	Prompt string
	// Role names a role template whose body is put before Prompt ("" for none).
	Role string
	// TaskID is the worktree task the chat works on, for the UI's labels.
	TaskID string
	// Parent is the chat that started this one ("" when the user did).
	Parent string
	// Origin says who is sending Prompt when it is not the person at the
	// keyboard; the transcript shows the message as "from <agent>".
	Origin *as.Origin
	// wrapRegister, when set, runs the chat's registration (do) for the
	// caller, which may hold a lock around it: start_agent turns its reserved
	// slot into the registered child atomically.
	wrapRegister func(do func() error) error
}

// ChatOpened is the payload of "chat:opened". Background asks the UI not to
// take focus: someone else started the chat, the user is doing something else.
type ChatOpened struct {
	Info       as.SessionInfo `json:"info"`
	Parent     string         `json:"parent"`
	TaskID     string         `json:"taskId"`
	Role       string         `json:"role"`
	Background bool           `json:"background"`
}

// openChat is the one place a chat is opened. It starts the agent, binds the
// chat to its task and its usage record, registers it for orchestration,
// tells the UI, and sends the first prompt if there is one. When only the
// prompt fails (the usage budget is spent, say) the chat stays open and the
// returned info is valid next to the error, so a caller can still address it.
func (a *App) openChat(ctx context.Context, r ChatRequest) (as.SessionInfo, error) {
	// A definition added, changed or removed since: read now.
	return a.openChatWith(ctx, r, a.loadDefinedAgents())
}

// openChatWith is openChat with the agent definitions of loaded: a defined
// agent starts on the driver of that read, on the engine resolved once here,
// so another call's read in between can't change what this chat runs.
func (a *App) openChatWith(ctx context.Context, r ChatRequest, loaded loadedAgents) (as.SessionInfo, error) {
	// Before anything starts: a role that cannot be used must not leave an
	// agent process behind.
	r, drv, engine, err := a.applyRoleEngine(r, loaded)
	if err != nil {
		return as.SessionInfo{}, err
	}
	// A chat in the lead's worktree of a joint group works in every worktree
	// of the group: Flint derives the folders from the cwd, never from the
	// caller (tasks_joint.go).
	ctx, err = a.withJointFolders(ctx, r.Agent, engine, r.Cwd)
	if err != nil {
		return as.SessionInfo{}, err
	}
	info, err := a.startChat(ctx, drv, r.Agent, r.Cwd)
	if err != nil {
		return info, &agentStartError{err}
	}
	a.bindTaskChat(info, r.Cwd, r.Parent == "", "")
	a.usageChats.bind(info.ID, usageChat{chat: info.ID, agent: info.Agent, cwd: r.Cwd, model: info.Model})
	node := orchestrate.Node{ID: info.ID, Agent: info.Agent, Role: r.Role, Parent: r.Parent, Cwd: r.Cwd}
	register := func() error { return a.orch.Register(node) }
	if r.wrapRegister != nil {
		err = r.wrapRegister(register)
	} else {
		err = register()
	}
	if err != nil {
		a.forgetChat(info.ID)
		_ = a.agents.Stop(info.ID)
		return as.SessionInfo{}, err
	}
	a.orchGrants.inherit(r.Parent, info.ID) // the parent's grant, one level deeper
	a.emit(eventChatOpened, ChatOpened{
		Info: info, Parent: r.Parent, TaskID: r.TaskID, Role: r.Role,
		Background: r.Origin != nil || r.Parent != "",
	})
	if r.Prompt == "" {
		return info, nil
	}
	if err := a.promptChat(info.ID, r.Prompt, nil, false, r.Origin); err != nil {
		return info, fmt.Errorf("chat %s is open, but its first prompt failed: %w", info.ID, err)
	}
	return info, nil
}

// applyRole resolves r.Role (project roles only in a trusted folder: Restricted
// Mode) and puts its body before the prompt. A role that names an agent fixes
// it when the request has none and must fit it when it has one (roleFits).
// drv is the driver a defined agent starts on (loadedAgents.route), nil for
// any other agent.
func (a *App) applyRole(r ChatRequest, loaded loadedAgents) (ChatRequest, as.Driver, error) {
	r, drv, _, err := a.applyRoleEngine(r, loaded)
	return r, drv, err
}

// applyRoleEngine is applyRole that also returns the engine the chat runs
// on (loadedAgents.route: a defined agent's resolved engine, else the agent).
func (a *App) applyRoleEngine(r ChatRequest, loaded loadedAgents) (ChatRequest, as.Driver, string, error) {
	var role orchestrate.Role
	if r.Role != "" {
		var err error
		if role, err = orchestrate.ResolveRole(a.configDir, r.Cwd, a.isTrusted(r.Cwd), r.Role); err != nil {
			return r, nil, "", err
		}
		if r.Agent == "" {
			r.Agent = role.Agent
		}
	}
	drv, engine, err := loaded.route(r.Agent, "")
	if err != nil {
		return r, nil, "", err
	}
	if r.Role == "" {
		return r, drv, engine, nil
	}
	if !roleFits(role.Agent, r.Agent, engine) {
		return r, nil, "", fmt.Errorf("role %s is for %s, not %s", role.Name, role.Agent, describeAgent(r.Agent, engine))
	}
	r.Prompt = role.Apply(r.Prompt)
	return r, drv, engine, nil
}

// roleFits reports whether a role limited to roleAgent ("" for any) may run
// in agent name, which runs on engine. A role for a native agent (claude,
// codex) is for that program, so it fits a defined agent that runs on it; a
// role naming a defined or custom agent fits that agent only.
func roleFits(roleAgent, name, engine string) bool {
	return roleAgent == "" || roleAgent == name || isNative(roleAgent) && roleAgent == engine
}

// describeAgent names an agent with its engine when that is another name (a
// defined agent's).
func describeAgent(name, engine string) string {
	if engine == "" || engine == name {
		return name
	}
	return name + " (" + engine + ")"
}

// adoptChat registers a chat reopened by Resume: the UI already has its tab.
func (a *App) adoptChat(info as.SessionInfo, cwd string) {
	if err := a.orch.Register(orchestrate.Node{ID: info.ID, Agent: info.Agent, Cwd: cwd}); err != nil {
		a.logf("orchestrate: %v", err)
	}
}

// forgetChat drops everything Flint keeps about a chat that ended.
func (a *App) forgetChat(id string) {
	a.tasks.Unbind(id)
	a.usageChats.unbind(id)
	// The grant goes before the registry entry: a child registered in between
	// finds its parent still there but inherits nothing, never a grant at a
	// depth the registry no longer knows.
	a.orchGrants.forget(id) // a grant dies with its chat
	a.orch.Remove(id)
	a.notices.Drop(id) // and what its children had to tell it
	a.noticeProblems.Delete(id)
}

// stopChatTree ends a chat and every agent it started, deepest first, so
// closing a chat that holds a grant really ends the run: an agent left behind
// would spend the user's budget unseen, and its grant died with the chat. A
// second pass takes a child that was registered while the first one ran.
func (a *App) stopChatTree(id string) error {
	var errs []error
	done := map[string]bool{}
	for pass := 0; pass < 2; pass++ {
		for _, victim := range a.chatSubtree(id) {
			if done[victim] {
				continue
			}
			done[victim] = true
			a.forgetChat(victim)
			if err := a.agents.Stop(victim); err != nil {
				errs = append(errs, fmt.Errorf("stop %s: %w", victim, err))
			}
		}
	}
	return errors.Join(errs...)
}

// chatSubtree lists id and its descendants, children before their parents.
// The links are read from the children's Parent field, so it still finds the
// children of a chat already removed from the registry.
func (a *App) chatSubtree(id string) []string {
	var out []string
	var walk func(string, int)
	walk = func(n string, depth int) {
		if depth > 16 { // the registry bounds depth far lower; this ends a corrupt cycle
			return
		}
		for _, c := range a.orch.Children(n) {
			walk(c.ID, depth+1)
		}
		out = append(out, n)
	}
	walk(id, 0)
	return out
}

// announcer is what a session manager offers to put a message in a chat's
// event stream (agentsession.Manager.Announce).
type announcer interface {
	Announce(sessionID string, e as.Event) error
}

// promptChat is the one path a prompt takes to an agent, whoever wrote it:
// the usage budget is checked first, then the turn is tracked. A prompt that
// did not come from the web view (fromWeb false) is redacted, because it may
// carry what an agent read, and is announced as a "user_message" event so the
// target's transcript shows it, with its origin; the web view already shows
// what the user typed. Nothing is announced when the chat is busy. A prompt
// the user typed resets, and one an agent sent counts against, the budget of
// turns agents may start in each other (orchGrants.chargeTurn).
func (a *App) promptChat(id, text string, attachments []as.Attachment, fromWeb bool, origin *as.Origin) error {
	if err := a.usageGate(id); err != nil {
		return err
	}
	busy := a.agents.Busy(id)
	charged := false
	if fromWeb {
		a.orchGrants.resetTurns(id) // the user wrote: agents get their turns back
	} else {
		if busy {
			return as.ErrBusy // before announcing: a refused message must not appear
		}
		// A turn an agent (or Flint's notice) starts counts against the budget
		// of the group of chats the target belongs to; past it, nothing starts.
		if err := a.orchGrants.chargeTurn(id, a.currentSettings().Orchestration.MaxAutoTurns); err != nil {
			return err
		}
		charged = true
		text = redact.Secrets(text)
		if an, ok := a.agents.(announcer); ok {
			if err := an.Announce(id, as.Event{Kind: as.KindUserMessage, Text: text, Origin: origin}); err != nil {
				a.orchGrants.refundTurn(id)
				return err
			}
		}
	}
	a.usageChats.prompted(id, text)
	// Before the turn starts: an early failure's events must come after it.
	if !busy {
		a.tasks.Prompted(id)
		a.orch.Prompted(id)
	}
	err := a.agents.Prompt(id, text, attachments)
	if err != nil && charged {
		a.orchGrants.refundTurn(id)
	}
	return err
}

// agentStartError is openChat's error when the agent itself did not start
// (its program is missing, it failed its handshake, it is unknown): nothing
// about the chat that was asked for.
type agentStartError struct{ err error }

func (e *agentStartError) Error() string { return e.err.Error() }
func (e *agentStartError) Unwrap() error { return e.err }
