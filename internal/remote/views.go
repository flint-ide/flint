package remote

import (
	"context"

	as "flint/internal/agentsession"
	"flint/internal/sessions"
	"flint/internal/terminal"
)

var (
	_ sessions.Terminals = terminals{}
	_ sessions.Agents    = agents{}
)

// terminals is the sessions.Terminals of a Conn.
type terminals struct{ c *Conn }

// Start connects first when needed. The terminal counts as in use from the
// moment the host has it, so a drop right after is recovered.
func (t terminals) Start(id string, opts terminal.Options) error {
	cl, err := t.c.acquire(t.c.ctx)
	if err != nil {
		return err
	}
	t.c.mu.Lock()
	t.c.terms[id] = struct{}{}
	delete(t.c.termPos, id) // a new shell under an old id starts from 0
	delete(t.c.pendingClose, id)
	t.c.mu.Unlock()
	if err := cl.Terminals().Start(id, opts); err != nil {
		t.c.mu.Lock()
		delete(t.c.terms, id)
		t.c.mu.Unlock()
		t.c.wake()
		return err
	}
	return nil
}

func (t terminals) Write(id string, data []byte) error {
	cl, err := t.c.live()
	if err != nil {
		return err
	}
	return cl.Terminals().Write(id, data)
}

func (t terminals) Resize(id string, cols, rows int) error {
	cl, err := t.c.live()
	if err != nil {
		return err
	}
	return cl.Terminals().Resize(id, cols, rows)
}

func (t terminals) Cwd(id string) (string, error) {
	cl, err := t.c.live()
	if err != nil {
		return "", err
	}
	return cl.Terminals().Cwd(id)
}

func (t terminals) Resume(id string) (terminal.Resume, error) {
	cl, err := t.c.live()
	if err != nil {
		return terminal.Resume{}, err
	}
	return cl.Terminals().Resume(id)
}

// Close hangs the shell up. When the connection is down the shell is closed
// on the remote at the next connection.
func (t terminals) Close(id string) {
	t.c.mu.Lock()
	delete(t.c.terms, id)
	delete(t.c.termPos, id)
	var cl = t.c.link
	if cl == nil {
		t.c.pendingClose[id] = struct{}{}
	}
	t.c.mu.Unlock()
	t.c.wake()
	if cl != nil {
		cl.cl.Terminals().Close(id)
	}
}

func (t terminals) CloseAll() {
	t.c.mu.Lock()
	ids := keys(t.c.terms)
	t.c.mu.Unlock()
	for _, id := range ids {
		t.Close(id)
	}
}

// agents is the sessions.Agents of a Conn.
type agents struct{ c *Conn }

func (a agents) Agents() []string {
	cl, err := a.c.live()
	if err != nil {
		return nil
	}
	return cl.Agents().Agents()
}

func (a agents) Start(ctx context.Context, agent, cwd string) (as.SessionInfo, error) {
	cl, err := a.c.acquire(ctx)
	if err != nil {
		return as.SessionInfo{}, err
	}
	info, err := cl.Agents().Start(ctx, agent, cwd)
	if err == nil {
		a.opened(info.ID)
	}
	return info, err
}

func (a agents) Resume(ctx context.Context, agent, cwd, sessionID string) (as.SessionInfo, error) {
	cl, err := a.c.acquire(ctx)
	if err != nil {
		return as.SessionInfo{}, err
	}
	info, err := cl.Agents().Resume(ctx, agent, cwd, sessionID)
	if err == nil {
		a.opened(info.ID)
	}
	return info, err
}

func (a agents) opened(id string) {
	a.c.mu.Lock()
	a.c.chats[id] = struct{}{}
	delete(a.c.pendingStop, id)
	a.c.mu.Unlock()
}

func (a agents) Prompt(id, text string, attachments []as.Attachment) error {
	cl, err := a.c.live()
	if err != nil {
		return err
	}
	return cl.Agents().Prompt(id, text, attachments)
}

func (a agents) Busy(id string) bool {
	cl, err := a.c.live()
	return err == nil && cl.Agents().Busy(id)
}

func (a agents) Cancel(id string) error {
	cl, err := a.c.live()
	if err != nil {
		return err
	}
	return cl.Agents().Cancel(id)
}

func (a agents) Respond(id, requestID, optionID string) error {
	cl, err := a.c.live()
	if err != nil {
		return err
	}
	return cl.Agents().Respond(id, requestID, optionID)
}

// Stop ends the chat. When the connection is down it is stopped on the remote
// at the next connection.
func (a agents) Stop(id string) error {
	a.c.mu.Lock()
	delete(a.c.chats, id)
	delete(a.c.chatSeq, id)
	l := a.c.link
	if l == nil {
		a.c.pendingStop[id] = struct{}{}
	}
	a.c.mu.Unlock()
	a.c.wake()
	if l == nil {
		return nil
	}
	return l.cl.Agents().Stop(id)
}

// Policy is the policy of the remote host, or the last one set here when it
// cannot be asked.
func (a agents) Policy() as.Policy {
	if cl, err := a.c.live(); err == nil {
		return cl.Agents().Policy()
	}
	a.c.mu.Lock()
	defer a.c.mu.Unlock()
	if a.c.policySet {
		return a.c.policy
	}
	return as.DefaultPolicy()
}

// SetPolicy applies now when connected and at every later connection.
func (a agents) SetPolicy(p as.Policy) {
	a.c.mu.Lock()
	a.c.policy, a.c.policySet = p, true
	a.c.mu.Unlock()
	if cl, err := a.c.live(); err == nil {
		cl.Agents().SetPolicy(p)
	}
}

// Close stops the chats started through this Conn.
func (a agents) Close() error {
	a.c.mu.Lock()
	ids := keys(a.c.chats)
	a.c.mu.Unlock()
	var first error
	for _, id := range ids {
		if err := a.Stop(id); err != nil && first == nil {
			first = err
		}
	}
	return first
}
