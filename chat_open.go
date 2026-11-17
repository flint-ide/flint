package main

import (
	"context"
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
	info, err := a.agents.Start(ctx, r.Agent, r.Cwd)
	if err != nil {
		return info, err
	}
	a.tasks.Bind(info.ID, r.Cwd)
	a.usageChats.bind(info.ID, usageChat{chat: info.ID, agent: info.Agent, cwd: r.Cwd, model: info.Model})
	node := orchestrate.Node{ID: info.ID, Agent: info.Agent, Role: r.Role, Parent: r.Parent, Cwd: r.Cwd}
	if err := a.orch.Register(node); err != nil {
		a.forgetChat(info.ID)
		_ = a.agents.Stop(info.ID)
		return as.SessionInfo{}, err
	}
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
	a.orch.Remove(id)
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
// what the user typed. Nothing is announced when the chat is busy.
func (a *App) promptChat(id, text string, attachments []as.Attachment, fromWeb bool, origin *as.Origin) error {
	if err := a.usageGate(id); err != nil {
		return err
	}
	busy := a.agents.Busy(id)
	if !fromWeb {
		if busy {
			return as.ErrBusy // before announcing: a refused message must not appear
		}
		text = redact.Secrets(text)
		if an, ok := a.agents.(announcer); ok {
			if err := an.Announce(id, as.Event{Kind: as.KindUserMessage, Text: text, Origin: origin}); err != nil {
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
	return a.agents.Prompt(id, text, attachments)
}
