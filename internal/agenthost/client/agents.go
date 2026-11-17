package client

import (
	"context"
	"errors"
	"fmt"

	"flint/internal/agenthost"
	as "flint/internal/agentsession"
	"flint/internal/jsonrpc"
)

// agents implements sessions.Agents over the connection.
type agents struct{ c *Client }

// Agents lists the host's agents; nil when the host cannot be asked.
func (a agents) Agents() []string {
	var res agenthost.AgentsResult
	if err := a.c.callT(agenthost.MethodAgentAgents, nil, &res); err != nil {
		return nil
	}
	return res.Agents
}

func (a agents) Start(ctx context.Context, agent, cwd string) (as.SessionInfo, error) {
	var info as.SessionInfo
	if err := a.c.call(ctx, agenthost.MethodAgentStart, agenthost.AgentStartParams{Agent: agent, Cwd: cwd}, &info); err != nil {
		return as.SessionInfo{}, err
	}
	return info, a.opened(info)
}

func (a agents) Resume(ctx context.Context, agent, cwd, sessionID string) (as.SessionInfo, error) {
	var info as.SessionInfo
	if err := a.c.call(ctx, agenthost.MethodAgentResume, agenthost.AgentResumeParams{Agent: agent, Cwd: cwd, Session: sessionID}, &info); err != nil {
		return as.SessionInfo{}, err
	}
	return info, a.opened(info)
}

// opened remembers the chat and catches up on what the host buffered while
// the call was in flight: the host subscribed this client when it opened, but
// events from before that are only in the ring.
func (a agents) opened(info as.SessionInfo) error {
	a.c.mu.Lock()
	a.c.chats[info.ID] = struct{}{}
	a.c.mu.Unlock()
	return a.c.run(func() {
		if a.c.cstate[info.ID] == nil {
			a.c.cstate[info.ID] = &chatState{}
		}
		a.c.syncChat(info.ID)
	})
}

// ErrPromptTooLarge is returned by Prompt for a prompt (with its attachments)
// whose encoding does not fit a message of the protocol: unlike terminal
// input, a prompt cannot be sent in pieces. Ordinary text, markup included,
// takes about its own size on the wire; only text full of control characters
// (six bytes each) comes near the limit.
var ErrPromptTooLarge = errors.New("the prompt is too large to send to the background host")

func (a agents) Prompt(id, text string, attachments []as.Attachment) error {
	params := agenthost.AgentPromptParams{ID: id, Text: text, Attachments: attachments}
	// The same encoding the connection uses, checked before sending so the
	// failure is this error and not a transport one.
	if data, err := jsonrpc.Marshal(params); err == nil && len(data) >= agenthost.MaxMessage-promptEnvelope {
		return fmt.Errorf("%w: %d bytes encoded, the limit is %d", ErrPromptTooLarge, len(data), agenthost.MaxMessage-promptEnvelope)
	}
	return a.c.callT(agenthost.MethodAgentPrompt, params, nil)
}

// promptEnvelope is what the request around a prompt's params takes (the
// jsonrpc fields, the method, the id).
const promptEnvelope = 1 << 10

// Busy is false when the host cannot be asked.
func (a agents) Busy(id string) bool {
	var res agenthost.BusyResult
	return a.c.callT(agenthost.MethodAgentBusy, agenthost.IDParams{ID: id}, &res) == nil && res.Busy
}

func (a agents) Cancel(id string) error {
	return a.c.callT(agenthost.MethodAgentCancel, agenthost.IDParams{ID: id}, nil)
}

func (a agents) Respond(id, requestID, optionID string) error {
	return a.c.callT(agenthost.MethodAgentRespond, agenthost.AgentRespondParams{ID: id, Request: requestID, Option: optionID}, nil)
}

func (a agents) Stop(id string) error {
	err := a.c.callT(agenthost.MethodAgentStop, agenthost.IDParams{ID: id}, nil)
	a.c.mu.Lock()
	delete(a.c.chats, id)
	a.c.mu.Unlock()
	return err
}

// Policy is the host's policy, or the last one known when it cannot be asked.
func (a agents) Policy() as.Policy {
	var p as.Policy
	if err := a.c.callT(agenthost.MethodAgentPolicy, nil, &p); err != nil {
		a.c.mu.Lock()
		defer a.c.mu.Unlock()
		return a.c.policy
	}
	a.c.mu.Lock()
	a.c.policy = p
	a.c.mu.Unlock()
	return p
}

// SetPolicy applies to the host; the interface has no error to return, a
// host that cannot be reached is reported through Done.
func (a agents) SetPolicy(p as.Policy) {
	a.c.mu.Lock()
	a.c.policy = p
	a.c.mu.Unlock()
	_ = a.c.callT(agenthost.MethodAgentSetPolicy, p, nil)
}

// Close stops the chats this client opened or attached. The connection stays:
// Client.Close detaches without stopping anything.
func (a agents) Close() error {
	a.c.mu.Lock()
	ids := make([]string, 0, len(a.c.chats))
	for id := range a.c.chats {
		ids = append(ids, id)
	}
	a.c.mu.Unlock()
	var errs []error
	for _, id := range ids {
		if err := a.Stop(id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
