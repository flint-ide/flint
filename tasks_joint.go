package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"flint/internal/agent"
	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/git"
)

// Joint agent task groups (docs/design/projects.md, "One agent across
// repositories"): one agent that works in several repositories at once. Each
// repository gets an agent task of its own, made by createTasks like any
// other (branch, worktree, trust, merge, pull request, removal), and the tasks
// are linked as a joint group (agenttask.Task.Joint) with a lead. They share
// one chat: it runs in the lead's worktree and is given the other worktrees
// as additional folders (agentsession.WithFolders; Claude Code's --add-dir).
// The folders are derived from the chat's cwd whenever a chat starts or
// resumes there (withJointFolders), never taken from the UI. The tracker
// binds that chat to every task of the group (agenttask.Store.ChatTasks), the
// group holds one slot of agentTasks.maxParallel, and it leaves the queue as
// a unit (agenttask.NextQueued, dequeue, startTaskChat).

// jointRequest is one repository of a joint group: a folder of the workspace
// inside it, and the description of the work there.
type jointRequest struct {
	Dir         string
	Description string
}

// errJointEngine is wrapped by the error createJointTasks and openChat return
// for an agent whose engine cannot be given additional folders (Codex, a
// custom ACP agent); errJointUntrusted by the one for a repository or
// worktree that is not trusted (Restricted Mode).
var (
	errJointEngine    = errors.New("cannot work in several folders at once")
	errJointUntrusted = errors.New("a joint agent task works only in trusted folders")
)

// jointEngineError names the agent and the engine it runs on.
type jointEngineError struct{ agent, engine string }

func (e *jointEngineError) Error() string {
	if e.agent == e.engine && e.engine != string(agent.Codex) { // a custom ACP agent
		return fmt.Sprintf("%s %v: delegate per repository instead", e.agent, errJointEngine)
	}
	return fmt.Sprintf("%s runs on %s, which %v: delegate per repository instead", e.agent, e.engine, errJointEngine)
}

func (e *jointEngineError) Unwrap() error { return errJointEngine }

// jointEngine checks that agent name, which runs on engine (loadedAgents.route:
// a defined agent's resolved engine), can work in several folders: only
// Claude Code can (--add-dir). See docs/design/projects.md for Codex.
func jointEngine(name, engine string) error {
	if engine == string(agent.Claude) {
		return nil
	}
	return &jointEngineError{agent: name, engine: engine}
}

// createJointTasks makes one agent task per request, all for agent, as one
// joint group whose lead is the first request's task, all or nothing: when
// one cannot be made, the ones already made are removed again. Every
// request must be in another repository, each repository's checkout must be
// trusted (its worktree is then trusted, as createOne does for any task),
// and agent's engine must be able to work in several folders (jointEngine).
//
// The group takes one slot of agentTasks.maxParallel: when one is free the
// lead holds it (reserveStart, as createTasks does for a task whose chat
// starts next) and no task is queued; otherwise every task is queued and the
// queue starts the group later, as a unit. The tasks are returned lead first.
// The caller starts the chat with startJointTasks (nothing to do when the
// tasks are queued, which it also handles), or undoes the group with
// discardJointTasks. With hold, the tasks stay held (taskQueue.held) until
// then, so the queue never starts the group while the caller may still roll
// it back; without, they are let go at once, and a lead that holds a slot
// keeps it for startGrace.
func (a *App) createJointTasks(ctx context.Context, reqs []jointRequest, agentName string, hold bool) ([]agenttask.Task, error) {
	switch {
	case len(reqs) < 2:
		return nil, errors.New("a joint agent task needs at least two repositories")
	case len(reqs) > agenttask.MaxJoint:
		return nil, fmt.Errorf("a joint agent task works in at most %d repositories", agenttask.MaxJoint)
	}
	_, engine, err := a.loadDefinedAgents().route(agentName, "")
	if err != nil {
		return nil, err
	}
	if err := jointEngine(agentName, engine); err != nil {
		return nil, err
	}
	// Before anything is made: every repository once, and trusted.
	repos := map[string]string{}
	for _, req := range reqs {
		r, err := git.Open(ctx, req.Dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", req.Dir, err)
		}
		key := cleanFolder(r.CommonDir)
		if other, dup := repos[key]; dup {
			return nil, fmt.Errorf("%s and %s are the same repository: a joint agent task has one task per repository", other, req.Dir)
		}
		repos[key] = req.Dir
		if !a.isTrusted(r.Root) {
			return nil, fmt.Errorf("%s is not trusted: trust it first (%w)", r.Root, errJointUntrusted)
		}
	}
	type made struct {
		r    *git.Repo
		task agenttask.Task
	}
	var all []made
	undo := func() {
		uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gitTimeout)
		defer cancel()
		var tasks []agenttask.Task
		for _, m := range all {
			a.discardTasks(uctx, m.r, []agenttask.Task{m.task})
			tasks = append(tasks, m.task)
		}
		a.unhold(tasks)
		if len(all) > 0 {
			a.tasksChanged()
		}
	}
	member := &jointMember{id: agenttask.NewID(), lead: true}
	for _, req := range reqs {
		rctx, cancel := context.WithTimeout(ctx, gitRemoteTimeout) // checkout hooks may run
		tasks, r, err := a.createTasksWith(rctx, NewTaskGroup{Dir: req.Dir, Description: req.Description, Agents: []string{agentName}},
			createOptions{queueOK: true, hold: true, joint: member})
		cancel()
		if err != nil {
			undo()
			return nil, fmt.Errorf("%s: %w", req.Dir, err)
		}
		task := tasks[0]
		all = append(all, made{r, task})
		if !a.isTrusted(task.Worktree) {
			undo()
			return nil, fmt.Errorf("the worktree %s is not trusted (%w)", task.Worktree, errJointUntrusted)
		}
		if member.lead {
			member = &jointMember{id: member.id, queued: task.Queued}
		}
	}
	out := make([]agenttask.Task, len(all))
	for i, m := range all {
		out[i] = m.task
	}
	if !out[0].Queued {
		a.reserveStart(out[0].ID) // the start begins now: startGrace counts from here
	}
	if !hold {
		a.unhold(out)
	}
	return out, nil
}

// startJointTasks lets go of a joint group createJointTasks made (unhold) and
// starts its chat when the group got a slot: in its own goroutine, as the
// queue starts a task (startTaskChat: in the lead's worktree, with the
// group's description, the slot released when the start is over). A queued
// group is left to the queue, which is given a look.
func (a *App) startJointTasks(tasks []agenttask.Task) {
	a.unhold(tasks)
	if len(tasks) == 0 || tasks[0].Queued {
		a.startQueuedTasks()
		return
	}
	lead := tasks[0]
	a.queue.starts.Add(1)
	go func() {
		defer a.queue.starts.Done()
		a.startTaskChat(lead)
	}()
}

// discardJointTasks undoes a joint group createJointTasks made whose chat has
// not started (a caller that held it and must roll back): slots, worktrees,
// branches, trust and entries go, and the tasks are let go.
func (a *App) discardJointTasks(ctx context.Context, tasks []agenttask.Task) {
	for _, task := range tasks {
		r, err := git.Open(ctx, task.Checkout)
		if err != nil {
			a.logf("discard joint task %s: %v", task.ID, err)
			continue
		}
		a.discardTasks(ctx, r, []agenttask.Task{task})
	}
	a.unhold(tasks)
	if len(tasks) > 0 {
		a.tasksChanged()
	}
}

// jointFolders are the additional folders of a chat in cwd: the worktrees of
// the other tasks of the joint group whose lead's worktree cwd is, lead's
// order (agenttask.Store.Joint); none when cwd is not a lead's. They come from
// the task store only, and each must exist and be trusted, like cwd: a chat
// takes its settings from its cwd, so a trusted session must not write into
// a Restricted Mode folder, nor work in a group with a folder gone.
func (a *App) jointFolders(cwd string) ([]string, error) {
	store := a.taskStore()
	id := store.ByWorktree(cleanFolder(cwd))
	if id == "" {
		return nil, nil
	}
	_, lead, _ := store.Find(id)
	if lead.Joint == "" || !lead.Lead {
		return nil, nil
	}
	if !a.isTrusted(lead.Worktree) {
		return nil, fmt.Errorf("the worktree %s is not trusted (%w)", lead.Worktree, errJointUntrusted)
	}
	var out []string
	for _, m := range store.Joint(lead.Joint) {
		if m.ID == lead.ID {
			continue
		}
		switch {
		case !filepath.IsAbs(m.Worktree) || !isDir(m.Worktree):
			return nil, fmt.Errorf("the worktree of %q is gone (%s): remove that task to work on the others", m.Title, m.Worktree)
		case !a.isTrusted(m.Worktree):
			return nil, fmt.Errorf("the worktree %s is not trusted (%w)", m.Worktree, errJointUntrusted)
		}
		out = append(out, filepath.Clean(m.Worktree))
	}
	return out, nil
}

// withJointFolders returns ctx carrying the additional folders of a chat of
// agent name (running on engine) in cwd (jointFolders), refusing an engine
// that cannot be given folders. ctx as is when cwd is no joint lead's.
func (a *App) withJointFolders(ctx context.Context, name, engine, cwd string) (context.Context, error) {
	folders, err := a.jointFolders(cwd)
	if err != nil || len(folders) == 0 {
		return ctx, err
	}
	if err := jointEngine(name, engine); err != nil {
		return ctx, err
	}
	return as.WithFolders(ctx, folders), nil
}

// jointPrompt is the first message of a joint group's chat: the lead's
// description, then which folder is which repository (on which branch), the
// description of a repository whose differs, and that the change must be
// consistent across them.
func jointPrompt(members []agenttask.Task) string {
	if len(members) == 0 {
		return ""
	}
	lead := members[0]
	var b strings.Builder
	b.WriteString(lead.Prompt)
	fmt.Fprintf(&b, "\n\nThis task spans %d repositories. Each has its own worktree on its own branch, and you work in all of them:\n", len(members))
	for i, m := range members {
		here := ""
		if i == 0 {
			here = " (your working directory)"
		}
		fmt.Fprintf(&b, "- %s%s: repository %s, branch %s (from %s)\n", m.Worktree, here, filepath.Base(m.Checkout), m.Branch, m.Base)
		if m.Prompt != lead.Prompt {
			fmt.Fprintf(&b, "  What to do there:\n  %s\n", strings.ReplaceAll(m.Prompt, "\n", "\n  "))
		}
	}
	b.WriteString("Keep the change consistent across these repositories, and commit in each folder on its own branch.")
	return b.String()
}

// jointRemoval is what removing task means for its joint group: how many
// other tasks it has, and the chats task shares with them (bound to more
// than one task), which stop for all of them.
func (a *App) jointRemoval(task agenttask.Task) (others int, shared []string) {
	if task.Joint != "" {
		others = max(0, len(a.taskStore().Joint(task.Joint))-1)
	}
	for _, s := range a.tasks.Sessions(task.ID) {
		if len(a.tasks.Tasks(s)) > 1 {
			shared = append(shared, s)
		}
	}
	return others, shared
}
