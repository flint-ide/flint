package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"flint/internal/mcpserver"
	"flint/internal/mcptools"
)

// Orchestration lets an agent chat start and direct other agents in the same
// project (internal/mcptools/orchestrate.go). It does not ask before every
// call: the user answers one native dialog, which the web view cannot answer,
// the first time a chat uses an orchestration tool, and that yes is an
// orchGrant held in memory. The rules around it are constants, not settings,
// because they protect the user:
//
//   - the grant covers one project folder, the one the chat works in;
//   - there is none in Restricted Mode (an untrusted folder), and it stops
//     working if the folder loses its trust;
//   - it dies with the chat, and is never written to disk;
//   - children inherit it one level deeper, and a chat at the depth limit
//     cannot start agents;
//   - a "no" is remembered for the chat's life, so an agent cannot wear the
//     user down with the same question.

// orchGrant is what the user allowed one chat.
type orchGrant struct {
	// root is the project folder the chat may orchestrate in.
	root string
	// maxChildren and maxDepth are the limits shown in the dialog (the
	// settings at that moment); a later, lower setting still applies.
	maxChildren, maxDepth int
}

// orchGrants holds the grants and refusals of the open chats. The zero value
// is ready to use.
type orchGrants struct {
	mu       sync.Mutex
	grants   map[string]orchGrant
	declined map[string]struct{}
	asking   map[string]chan struct{} // chat → closed when its dialog is answered
}

func (g *orchGrants) get(chat string) (orchGrant, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	grant, ok := g.grants[chat]
	return grant, ok
}

// inherit gives child the grant of parent, if it has one.
func (g *orchGrants) inherit(parent, child string) {
	if parent == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if grant, ok := g.grants[parent]; ok {
		if g.grants == nil {
			g.grants = map[string]orchGrant{}
		}
		g.grants[child] = grant
	}
}

// forget drops everything about a chat that ended; a dialog still open for it
// records nothing when it is answered.
func (g *orchGrants) forget(chat string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.grants, chat)
	delete(g.declined, chat)
	delete(g.asking, chat)
}

// obtain returns whether chat holds a grant, showing ask (the dialog) at most
// once for the chat's whole life: concurrent calls wait for the one dialog,
// and a refusal is final. The answer is recorded even when this call gave up
// waiting (ctx), so a late yes is not asked for again.
func (g *orchGrants) obtain(ctx context.Context, chat string, want orchGrant, ask func() bool) (bool, error) {
	for {
		g.mu.Lock()
		if _, ok := g.grants[chat]; ok {
			g.mu.Unlock()
			return true, nil
		}
		if _, no := g.declined[chat]; no {
			g.mu.Unlock()
			return false, nil
		}
		wait, busy := g.asking[chat]
		if !busy {
			wait = make(chan struct{})
			if g.asking == nil {
				g.asking = map[string]chan struct{}{}
			}
			g.asking[chat] = wait
			go g.askOnce(chat, want, wait, ask)
		}
		g.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return false, errors.New("the user did not answer Flint's question in time, so nothing was started")
		}
	}
}

func (g *orchGrants) askOnce(chat string, want orchGrant, done chan struct{}, ask func() bool) {
	yes := ask()
	g.mu.Lock()
	defer g.mu.Unlock()
	defer close(done)
	if g.asking[chat] != done {
		return // the chat ended meanwhile
	}
	delete(g.asking, chat)
	if yes {
		if g.grants == nil {
			g.grants = map[string]orchGrant{}
		}
		g.grants[chat] = want
		return
	}
	if g.declined == nil {
		g.declined = map[string]struct{}{}
	}
	g.declined[chat] = struct{}{}
}

// orchGranted reports whether chat may use the orchestration tools right now:
// it holds a grant, orchestration is still switched on, and its folder is
// still trusted.
func (a *App) orchGranted(chat string) bool {
	if chat == "" || !a.currentSettings().Orchestration.Enabled {
		return false
	}
	grant, ok := a.orchGrants.get(chat)
	return ok && a.isTrusted(grant.root)
}

// orchGate is ActionGate.Granted: the agent's own permission request for a
// tool is allowed under a grant, for the orchestration tools and nothing else.
func (a *App) orchGate(sessionID, tool string) bool {
	return mcptools.IsOrchestrationTool(tool) && a.orchGranted(sessionID)
}

// approveOrchestration is approveAction for a tool a grant covers: nil under
// a grant, otherwise the grant dialog.
func (a *App) approveOrchestration(ctx context.Context, call mcpserver.Approval) error {
	if call.Granted(ctx) {
		return nil
	}
	chat := mcpserver.CallerFrom(ctx)
	node, known := a.orch.Get(chat)
	switch {
	case chat == "" || !known:
		return errors.New("orchestration is only available to a chat that Flint started")
	case !a.isTrusted(call.Root):
		return errors.New("orchestration is off in Restricted Mode; trust this folder first")
	}
	cfg := a.currentSettings().Orchestration
	if !cfg.Enabled {
		return errors.New("orchestration is switched off in Settings")
	}
	want := orchGrant{root: call.Root, maxChildren: cfg.MaxChildren, maxDepth: cfg.MaxDepth}
	ok, err := a.orchGrants.obtain(ctx, chat, want, func() bool {
		return a.askUser("Let this agent direct other agents?", orchQuestion(node.Agent, want), "Allow")
	})
	switch {
	case err != nil:
		return err
	case !ok:
		return mcpserver.ErrNotApproved
	}
	return nil
}

// orchQuestion is the grant dialog's text: who, where, how much, and that the
// messages reach the other chats.
func orchQuestion(agent string, g orchGrant) string {
	if agent == "" {
		agent = "agent"
	}
	return fmt.Sprintf("Let this %s chat in %s start and direct up to %d agents (depth %d)?\n\n"+
		"It can start agents in this project, send them messages and read their output. "+
		"Messages it sends appear in their chats. The permission lasts until this chat is closed, and covers this project only.",
		agent, g.root, g.maxChildren, g.maxDepth)
}
