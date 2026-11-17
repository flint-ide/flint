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
	"flint/internal/agenttask"
	"flint/internal/git"
	"flint/internal/mcptools"
	"flint/internal/orchestrate"
)

// orchestrator carries out the orchestration tools (mcptools.Orchestrator). The
// tools only check the shape of their arguments; what a calling chat may touch
// is decided here, from the registry and the chat's grant, on every call.
//
// Scope: a target must be a chat whose folder is inside the root of the
// caller's token, or inside the worktree of a task of the repository that root
// belongs to (what start_agent with worktree makes). The worktrees are read
// from the task state on every call, never guessed from a path.
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
		if _, isTerminal := o.terminal(id); isTerminal {
			return node, fmt.Errorf("%q is an agent running in a terminal tab: Flint only reports its state (list_agents); it cannot send it prompts, wait for it, read its output or stop it", id)
		}
		return node, fmt.Errorf("unknown agent %q (list_agents shows the ids)", id)
	}
	if !o.scopeOf(root).has(node.Cwd) {
		return node, fmt.Errorf("agent %q is not in this project", id)
	}
	return node, nil
}

// orchScope is where a call from one project may reach: the project folder
// and the worktrees of the tasks of its repository.
type orchScope struct {
	root      string
	worktrees []string
}

// scopeOf reads the task state for root's repository. A root that is not in a
// repository (or one git cannot open) reaches its own folder only.
func (o *orchestrator) scopeOf(root string) orchScope {
	s := orchScope{root: root}
	ctx, cancel := context.WithTimeout(o.app.ctx, gitTimeout)
	defer cancel()
	r, err := git.Open(ctx, root)
	if err != nil {
		return s
	}
	for _, t := range o.app.taskStore().List(cleanFolder(r.CommonDir)) {
		s.worktrees = append(s.worktrees, t.Worktree)
	}
	return s
}

// has reports whether cwd is in the project folder or in a task worktree.
func (s orchScope) has(cwd string) bool {
	if insideRoot(cwd, s.root) {
		return true
	}
	for _, w := range s.worktrees {
		if insideRoot(cwd, w) {
			return true
		}
	}
	return false
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

// selfOrDescendant is target for the tools that only read: the caller itself
// or an agent below it. The user's own chats, the caller's parent and its
// siblings are not reachable, whatever folder they are in.
func (o *orchestrator) selfOrDescendant(caller, root, id string) (orchestrate.Node, error) {
	node, err := o.target(caller, root, id)
	if err != nil {
		return node, err
	}
	if id != caller && !o.app.orch.Descendant(caller, id) {
		return node, fmt.Errorf("agent %q is not you or your descendant: you can only read the agents you started, and theirs", id)
	}
	return node, nil
}

// List shows the caller and its descendants. Agents in terminal tabs are left
// out: a tab has no parent link, so it cannot be attributed to the caller's
// subtree, and listing it would show other people's terminals.
func (o *orchestrator) List(caller, root string) ([]mcptools.AgentInfo, error) {
	if _, _, err := o.authorize(caller, root); err != nil {
		return nil, err
	}
	out := []mcptools.AgentInfo{}
	scope := o.scopeOf(root)
	for _, n := range o.app.orch.All() {
		if !scope.has(n.Cwd) || (n.ID != caller && !o.app.orch.Descendant(caller, n.ID)) {
			continue
		}
		out = append(out, mcptools.AgentInfo{
			ID: n.ID, Agent: n.Agent, Role: n.Role, State: n.Live.State, Parent: n.Parent, Depth: n.Depth,
			Cwd: relCwd(n.Cwd, root), Kind: "chat", Self: n.ID == caller,
		})
	}
	return out, nil
}

// terminal finds a terminal tab by id.
func (o *orchestrator) terminal(id string) (mcptools.TerminalInfo, bool) {
	if o.app.mcpSources == nil || o.app.mcpSources.Terminals == nil {
		return mcptools.TerminalInfo{}, false
	}
	return o.app.mcpSources.Terminals.Get(id)
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
	cwd, taskID := root, ""
	if r.Worktree {
		task, err := o.newWorktree(ctx, root, agent, r)
		if err != nil {
			return "", err
		}
		cwd, taskID = task.Worktree, task.ID
		// The slot the task holds while its chat starts: given back when the
		// chat's turn runs (taskChanged) or the start failed, either way here.
		defer func() {
			o.app.releaseStart(taskID)
			o.app.startQueuedTasks()
		}()
	}
	origin := as.Origin{SessionID: caller, Agent: node.Agent, Role: node.Role}
	info, err := o.app.openChat(ctx, ChatRequest{
		Agent: agent, Cwd: cwd, Prompt: r.Task, Role: r.Role, Parent: caller, Origin: &origin, TaskID: taskID,
	})
	if err != nil {
		if info.ID != "" {
			// Only the first prompt failed (the usage budget, say): the chat exists.
			return "", fmt.Errorf("agent %s was started, but %v", info.ID, err)
		}
		if taskID != "" {
			return "", fmt.Errorf("the worktree was made (task %s, see the Tasks panel) but the agent could not start: %w", taskID, err)
		}
		return "", err
	}
	return info.ID, nil
}

// newWorktree makes the branch and worktree a child works in: one task of the
// caller's repository, from base (default: the branch the project is on).
// Only a trusted project gets one (Restricted Mode starts nothing by itself),
// and a task beyond the limit of tasks working at once is refused, not
// queued: the caller asked for an agent now.
func (o *orchestrator) newWorktree(ctx context.Context, root, agent string, r mcptools.StartRequest) (agenttask.Task, error) {
	if !o.app.isTrusted(root) {
		return agenttask.Task{}, errors.New("this project is not trusted, so Flint starts no worktree tasks in it")
	}
	made, _, err := o.app.createTasks(ctx, NewTaskGroup{Dir: root, Description: r.Task, Base: r.Base, Agents: []string{agent}}, false)
	if err != nil {
		return agenttask.Task{}, err
	}
	return made[0], nil
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
	if _, err := o.selfOrDescendant(caller, root, id); err != nil {
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
	if _, err := o.selfOrDescendant(caller, root, id); err != nil {
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
