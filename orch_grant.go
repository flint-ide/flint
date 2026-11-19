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
//   - it dies with the chat, and is never written to disk; closing the chat
//     also stops every agent it started (stopChatTree), and a child's copy
//     works only while the grant of the chat that was asked still exists;
//   - children inherit it one level deeper, and a chat at the depth limit
//     cannot start agents;
//   - the turns agents start in each other without the user (send_prompt, a
//     child's first prompt, a delivered notice) are counted per grant and
//     stop at orchestration.maxAutoTurns until the user writes in the tree;
//   - a "no" is remembered for the chat's life, so an agent cannot wear the
//     user down with the same question.

// orchGrant is what the user allowed one chat.
type orchGrant struct {
	// root is the project folder the chat may orchestrate in.
	root string
	// maxChildren and maxDepth are the limits shown in the dialog (the
	// settings at that moment); a later, lower setting still applies.
	maxChildren, maxDepth int
	// from is the chat the user was asked about: the chat itself for the grant
	// the dialog made, and the same id in every copy a child inherits. A copy
	// works only while grants[from] exists, so the grant cannot outlive the
	// chat it was given to even when something fails to clean up after it.
	from string
}

// orchGrants holds the grants and refusals of the open chats. The zero value
// is ready to use.
type orchGrants struct {
	mu       sync.Mutex
	grants   map[string]orchGrant
	declined map[string]struct{}
	asking   map[string]chan struct{} // chat → closed when its dialog is answered
	// turns counts the agent-started turns of the tree of each granting chat
	// since the user last wrote in it (see chargeTurn).
	turns map[string]int
}

// errAutoTurns is the refusal of an agent-started turn past the budget.
var errAutoTurns = errors.New("agent-to-agent turn limit reached")

// autoTurnsError is what the agent is told: to stop, and to report.
func autoTurnsError(limit int) error {
	return fmt.Errorf("%w: agents have started %d turns in each other without the user writing in this group of chats. "+
		"Stop now: do not send more prompts or start more agents, and report to the user what is done and what remains. "+
		"The count starts again when the user sends a message in any of these chats", errAutoTurns, limit)
}

func (g *orchGrants) get(chat string) (orchGrant, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	grant, ok := g.grants[chat]
	return grant, ok
}

// live is get for a grant that may be used: the chat holds one and the chat
// that was asked still holds its own.
func (g *orchGrants) live(chat string) (orchGrant, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.liveLocked(chat)
}

func (g *orchGrants) liveLocked(chat string) (orchGrant, bool) {
	grant, ok := g.grants[chat]
	if !ok {
		return grant, false
	}
	_, rooted := g.grants[grant.from]
	return grant, rooted
}

// inherit gives child the grant of parent, if it has a live one.
func (g *orchGrants) inherit(parent, child string) {
	if parent == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if grant, ok := g.liveLocked(parent); ok {
		if g.grants == nil {
			g.grants = map[string]orchGrant{}
		}
		g.grants[child] = grant
	}
}

// chargeTurn counts one turn an agent (not the user) is about to start in
// chat, against the budget of the tree chat belongs to: the chats that hold a
// grant given through the same granting chat. It refuses past limit. A chat
// outside any grant is not counted.
func (g *orchGrants) chargeTurn(chat string, limit int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	grant, ok := g.liveLocked(chat)
	if !ok {
		return nil
	}
	if g.turns[grant.from] >= limit {
		return autoTurnsError(limit)
	}
	if g.turns == nil {
		g.turns = map[string]int{}
	}
	g.turns[grant.from]++
	return nil
}

// refundTurn gives back a turn that was charged but did not start.
func (g *orchGrants) refundTurn(chat string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if grant, ok := g.grants[chat]; ok && g.turns[grant.from] > 0 {
		g.turns[grant.from]--
	}
}

// resetTurns starts the budget of chat's tree again: the user wrote in it.
func (g *orchGrants) resetTurns(chat string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if grant, ok := g.grants[chat]; ok {
		delete(g.turns, grant.from)
	}
}

// turnsLeft reports whether chat's tree may still start a turn.
func (g *orchGrants) turnsLeft(chat string, limit int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	grant, ok := g.liveLocked(chat)
	return !ok || g.turns[grant.from] < limit
}

// forget drops everything about a chat that ended; a dialog still open for it
// records nothing when it is answered.
func (g *orchGrants) forget(chat string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.grants, chat)
	delete(g.declined, chat)
	delete(g.asking, chat)
	delete(g.turns, chat)
}

// obtain returns whether chat holds a grant, showing ask (the dialog) at most
// once for the chat's whole life: concurrent calls wait for the one dialog,
// and a refusal is final. The answer is recorded even when this call gave up
// waiting (ctx), so a late yes is not asked for again. It stops, asking
// nothing, once ctx is done or alive says the chat is gone: a chat closed
// while its dialog was open must not bring the dialog back. ask gets the
// context of the call that started it and reports whether it showed the
// dialog: one still queued behind another of Flint's questions when that
// call gave up is not shown and records nothing (a later call asks).
func (g *orchGrants) obtain(ctx context.Context, chat string, want orchGrant, alive func() bool, ask func(context.Context) (yes, shown bool)) (bool, error) {
	want.from = chat
	for {
		if err := ctx.Err(); err != nil {
			return false, errors.New("the user did not answer Flint's question in time, so nothing was started")
		}
		if !alive() {
			return false, errors.New("this chat was closed")
		}
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
			go g.askOnce(ctx, chat, want, wait, alive, ask)
		}
		g.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return false, errors.New("the user did not answer Flint's question in time, so nothing was started")
		}
	}
}

func (g *orchGrants) askOnce(ctx context.Context, chat string, want orchGrant, done chan struct{}, alive func() bool, ask func(context.Context) (bool, bool)) {
	yes, shown := ask(ctx)
	g.mu.Lock()
	defer g.mu.Unlock()
	defer close(done)
	if g.asking[chat] != done {
		return // the chat ended meanwhile
	}
	delete(g.asking, chat)
	if !shown {
		return // its caller gave up before the dialog's turn came
	}
	if yes {
		if !alive() {
			return // closed while the dialog was open: nothing may outlive the chat
		}
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
	grant, ok := a.orchGrants.live(chat)
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
	alive := func() bool { _, registered := a.orch.Get(chat); return registered }
	ok, err := a.orchGrants.obtain(ctx, chat, want, alive, func(ctx context.Context) (bool, bool) {
		return a.askUserCtx(ctx, "Let this agent direct other agents?", orchQuestion(node.Agent, want), "Allow")
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
		"It can start agents in this folder, send them messages and read their output. "+
		"Messages it sends appear in their chats. The permission lasts until this chat is closed, and covers this folder only.",
		agent, g.root, g.maxChildren, g.maxDepth)
}
