package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/mcptools"
	"flint/internal/orchestrate"
)

// orchestrator carries out the orchestration tools (mcptools.Orchestrator). The
// tools only check the shape of their arguments; what a calling chat may touch
// is decided here, from the registry and the chat's grant, on every call.
//
// Scope: a target must be a chat whose folder is inside the root of the
// caller's token. Chats in a task's worktree of the same repository are not in
// scope yet (that needs the task state, which is a later step of the design),
// so agents only direct agents working in the project folder itself.
type orchestrator struct {
	app *App
	// startMu serialises start_agent so two parallel calls cannot both pass the
	// child limit.
	startMu sync.Mutex
}

var _ mcptools.Orchestrator = (*orchestrator)(nil)

func (o *orchestrator) Granted(caller string) bool { return o.app.orchGranted(caller) }

func (o *orchestrator) Limits() mcptools.OrchLimits {
	c := o.app.currentSettings().Orchestration
	return mcptools.OrchLimits{MaxWait: time.Duration(c.MaxWaitSeconds) * time.Second, OutputLines: c.OutputLines}
}

// authorize returns the caller's node and grant when the chat may direct
// agents in root: it is a chat Flint knows, holds a grant for exactly this
// project, and the project is still trusted.
func (o *orchestrator) authorize(caller, root string) (orchestrate.Node, orchGrant, error) {
	node, known := o.app.orch.Get(caller)
	grant, granted := o.app.orchGrants.get(caller)
	switch {
	case caller == "" || !known:
		return node, grant, errors.New("Flint does not know the calling chat")
	case !granted || !o.app.orchGranted(caller):
		return node, grant, errors.New("this chat has no permission from the user to direct agents")
	case filepath.Clean(grant.root) != filepath.Clean(root):
		return node, grant, errors.New("the permission the user gave covers another project")
	}
	return node, grant, nil
}

// target finds the chat id and checks that it is in the caller's project.
func (o *orchestrator) target(caller, root, id string) (orchestrate.Node, error) {
	node, ok := o.app.orch.Get(id)
	if !ok {
		return node, fmt.Errorf("unknown agent %q (list_agents shows the ids)", id)
	}
	if !insideRoot(node.Cwd, root) {
		return node, fmt.Errorf("agent %q is not in this project", id)
	}
	return node, nil
}

// descendant is target for the tools that act on what the caller started.
func (o *orchestrator) descendant(caller, root, id string) (orchestrate.Node, error) {
	node, err := o.target(caller, root, id)
	if err != nil {
		return node, err
	}
	if !o.app.orch.Descendant(caller, id) {
		return node, fmt.Errorf("agent %q is not your descendant: you can only direct the agents you started, and theirs", id)
	}
	return node, nil
}

func (o *orchestrator) List(caller, root string) ([]mcptools.AgentInfo, error) {
	if _, _, err := o.authorize(caller, root); err != nil {
		return nil, err
	}
	out := []mcptools.AgentInfo{}
	for _, n := range o.app.orch.All() {
		if !insideRoot(n.Cwd, root) {
			continue
		}
		out = append(out, mcptools.AgentInfo{
			ID: n.ID, Agent: n.Agent, Role: n.Role, State: n.Live.State, Parent: n.Parent, Depth: n.Depth,
			Cwd: relCwd(n.Cwd, root), Kind: "chat", Self: n.ID == caller,
		})
	}
	return out, nil
}

func (o *orchestrator) Start(ctx context.Context, caller, root string, r mcptools.StartRequest) (string, error) {
	node, grant, err := o.authorize(caller, root)
	if err != nil {
		return "", err
	}
	cfg := o.app.currentSettings().Orchestration
	maxDepth, maxChildren := min(grant.maxDepth, cfg.MaxDepth), min(grant.maxChildren, cfg.MaxChildren)
	if node.Depth >= maxDepth {
		return "", fmt.Errorf("this agent is at the depth limit (%d level(s) of agents) and cannot start agents; do the work yourself", maxDepth)
	}
	agent, err := o.agentFor(r, node, root)
	if err != nil {
		return "", err
	}
	o.startMu.Lock()
	defer o.startMu.Unlock()
	if n := len(o.app.orch.Children(caller)); n >= maxChildren {
		return "", fmt.Errorf("you already have %d agents (the limit is %d); wait for one and stop it with stop_agent first", n, maxChildren)
	}
	origin := as.Origin{SessionID: caller, Agent: node.Agent, Role: node.Role}
	info, err := o.app.openChat(ctx, ChatRequest{
		Agent: agent, Cwd: root, Prompt: r.Task, Role: r.Role, Parent: caller, Origin: &origin,
	})
	if err != nil {
		if info.ID != "" {
			// Only the first prompt failed (the usage budget, say): the chat exists.
			return "", fmt.Errorf("agent %s was started, but %v", info.ID, err)
		}
		return "", err
	}
	return info.ID, nil
}

// agentFor picks the agent to start: the request's, else the role's, else the
// caller's own.
func (o *orchestrator) agentFor(r mcptools.StartRequest, caller orchestrate.Node, root string) (string, error) {
	if r.Agent != "" {
		return r.Agent, nil
	}
	if r.Role != "" {
		role, err := orchestrate.ResolveRole(o.app.configDir, root, o.app.isTrusted(root), r.Role)
		if err != nil {
			return "", err
		}
		if role.Agent != "" {
			return role.Agent, nil
		}
	}
	return caller.Agent, nil
}

func (o *orchestrator) Send(caller, root, id, text string) error {
	node, _, err := o.authorize(caller, root)
	if err != nil {
		return err
	}
	if _, err := o.descendant(caller, root, id); err != nil {
		return err
	}
	return o.app.sendToChat(id, text, as.Origin{SessionID: caller, Agent: node.Agent, Role: node.Role})
}

func (o *orchestrator) Wait(ctx context.Context, caller, root, id string, until []string, timeout time.Duration) (mcptools.WaitResult, error) {
	if _, _, err := o.authorize(caller, root); err != nil {
		return mcptools.WaitResult{}, err
	}
	if _, err := o.target(caller, root, id); err != nil {
		return mcptools.WaitResult{}, err
	}
	// A deadline of our own, so that running out of time is an answer and the
	// call being cancelled (the agent gave up, Flint is closing) is an error.
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	live, err := o.app.orch.Wait(wctx, id, until)
	switch {
	case err == nil:
		return mcptools.WaitResult{State: live.State, Error: live.Error}, nil
	case errors.Is(err, orchestrate.ErrRemoved):
		return mcptools.WaitResult{}, fmt.Errorf("agent %q was closed while you waited", id)
	case ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded):
		n, _ := o.app.orch.Get(id)
		return mcptools.WaitResult{State: n.Live.State, Error: n.Live.Error, TimedOut: true}, nil
	}
	return mcptools.WaitResult{}, err
}

func (o *orchestrator) Output(caller, root, id string, lines int) (string, error) {
	if _, _, err := o.authorize(caller, root); err != nil {
		return "", err
	}
	if _, err := o.target(caller, root, id); err != nil {
		return "", err
	}
	return o.app.orch.Output(id, lines), nil
}

// Stop ends the chat and everything below it, deepest first: a chat left
// running without its parent would keep spending the user's budget unseen.
func (o *orchestrator) Stop(caller, root, id string) error {
	if _, _, err := o.authorize(caller, root); err != nil {
		return err
	}
	if _, err := o.descendant(caller, root, id); err != nil {
		return err
	}
	var errs []error
	for _, victim := range o.subtree(id) {
		o.app.forgetChat(victim)
		if err := o.app.agents.Stop(victim); err != nil {
			errs = append(errs, fmt.Errorf("stop %s: %w", victim, err))
		}
	}
	return errors.Join(errs...)
}

// subtree lists id and its descendants, children before their parents.
func (o *orchestrator) subtree(id string) []string {
	var out []string
	var walk func(string, int)
	walk = func(n string, depth int) {
		if depth > 16 { // the registry bounds depth far lower; this ends a corrupt cycle
			return
		}
		for _, c := range o.app.orch.Children(n) {
			walk(c.ID, depth+1)
		}
		out = append(out, n)
	}
	walk(id, 0)
	return out
}

// insideRoot reports whether a chat's folder is root or below it, with
// symlinks resolved on both sides so a link cannot move a chat out of or into
// the project.
func insideRoot(cwd, root string) bool {
	if cwd == "" || root == "" {
		return false
	}
	c, r := resolveDir(cwd), resolveDir(root)
	return c == r || strings.HasPrefix(c, strings.TrimSuffix(r, string(filepath.Separator))+string(filepath.Separator))
}

func resolveDir(p string) string {
	p = filepath.Clean(p)
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
}

// relCwd shows a chat's folder relative to the project ("." for the root).
func relCwd(cwd, root string) string {
	if r, err := filepath.Rel(resolveDir(root), resolveDir(cwd)); err == nil {
		return r
	}
	return cwd
}
