package main

import (
	"context"
	"errors"
	"fmt"

	"flint/internal/agenttask"
	"flint/internal/feature"
	"flint/internal/git"
)

// The agent tasks part of the feature Host (the Projects feature hands work to
// agents through it). It uses the code New Agent Task uses: createTasks for
// the branches, worktrees, queue and trust, startTaskChat for the chats, and
// TaskAPI.view for the states.

// maxFeatureTasks bounds the tasks one CreateAgentTasks call makes, and
// maxFeatureTaskIDs the tasks one AgentTasks call looks up.
const (
	maxFeatureTasks   = 50
	maxFeatureTaskIDs = 5000
)

// CreateAgentTasks makes one task per request, each as its own group of one,
// and when all of them exist starts the chats of those that got a slot, the
// way the queue starts a task (startTaskChat: the slot is released when the
// start is over, whatever the UI does). When one cannot be created, the
// tasks this call already made are removed again. Until then they are held
// (taskQueue.held): the queue starts none of them, so a rollback never
// removes a task whose chat is running, and the slots they hold do not run
// out while the other requests are created.
func (h featureHost) CreateAgentTasks(ctx context.Context, reqs []feature.NewAgentTask) ([]feature.AgentTask, error) {
	a := h.a
	if len(reqs) == 0 {
		return nil, errors.New("no agent task to create")
	}
	if len(reqs) > maxFeatureTasks {
		return nil, fmt.Errorf("at most %d agent tasks at once", maxFeatureTasks)
	}
	type group struct {
		r     *git.Repo
		tasks []agenttask.Task
	}
	var groups []group
	var all []agenttask.Task
	for i, req := range reqs {
		gctx, cancel := context.WithTimeout(ctx, gitRemoteTimeout) // checkout hooks may run
		made, r, err := a.createTasksWith(gctx, NewTaskGroup{Dir: req.Dir, Description: req.Description, Agents: []string{req.Agent}},
			createOptions{queueOK: true, hold: true, origin: req.Origin})
		cancel()
		if err != nil {
			uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gitTimeout)
			for _, g := range groups {
				a.discardTasks(uctx, g.r, g.tasks)
			}
			cancel()
			a.unhold(all)
			if len(groups) > 0 {
				a.tasksChanged()
			}
			if errors.Is(err, errDetachedHead) {
				return nil, &feature.DetachedHeadError{Dir: req.Dir}
			}
			return nil, fmt.Errorf("%s: %w", req.Dir, err)
		}
		groups = append(groups, group{r, made})
		all = append(all, made...)
		if a.queue.afterRequest != nil {
			a.queue.afterRequest(i + 1)
		}
	}
	for _, task := range all {
		if !task.Queued {
			a.reserveStart(task.ID) // their starts begin now: startGrace counts from here
		}
	}
	a.unhold(all)
	vctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	api := &TaskAPI{app: a}
	var out []feature.AgentTask
	for _, g := range groups {
		for _, task := range g.tasks {
			out = append(out, featureTask(api.view(vctx, g.r, task)))
			if task.Queued {
				continue
			}
			a.queue.starts.Add(1)
			go func() {
				defer a.queue.starts.Done()
				a.startTaskChat(task)
			}()
		}
	}
	a.startQueuedTasks() // queued ones it held back while slots came free
	return out, nil
}

// CreateJointAgentTasks makes one joint group (createJointTasks, held from
// the queue until it returns) and starts its one chat when the group got a
// slot (startJointTasks); a queued group is left to the queue. Its refusals
// keep their text and are matched by the feature errors: a detached HEAD as
// *feature.DetachedHeadError naming the request's folder, an engine that
// cannot work in several folders as feature.ErrJointEngine, a folder that is
// not trusted as feature.ErrJointUntrusted.
func (h featureHost) CreateJointAgentTasks(ctx context.Context, reqs []feature.NewAgentTask, agentName string) ([]feature.AgentTask, error) {
	a := h.a
	jreqs := make([]jointRequest, len(reqs))
	for i, req := range reqs {
		if req.Agent != "" && req.Agent != agentName {
			return nil, fmt.Errorf("%s: a joint agent task has one agent, %s, not %s", req.Dir, agentName, req.Agent)
		}
		jreqs[i] = jointRequest{Dir: req.Dir, Description: req.Description, Origin: req.Origin}
	}
	made, err := a.createJointTasks(ctx, jreqs, agentName, true)
	switch {
	case errors.Is(err, errDetachedHead):
		return nil, &feature.DetachedHeadError{Dir: detachedDir(ctx, reqs)}
	case errors.Is(err, errJointEngine):
		return nil, featureRefusal{err, feature.ErrJointEngine}
	case errors.Is(err, errJointUntrusted):
		return nil, featureRefusal{err, feature.ErrJointUntrusted}
	case err != nil:
		return nil, err
	}
	a.startJointTasks(made)
	vctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	api := &TaskAPI{app: a}
	out := make([]feature.AgentTask, len(made))
	for i, task := range made {
		if r, err := git.Open(vctx, task.Checkout); err == nil {
			out[i] = featureTask(api.view(vctx, r, task))
		} else {
			out[i] = storedFeatureTask(a, task)
		}
	}
	return out, nil
}

// detachedDir is the folder of the first request whose checkout is on a
// detached HEAD: createJointTasks makes the tasks in order and stops at the
// first that fails.
func detachedDir(ctx context.Context, reqs []feature.NewAgentTask) string {
	for _, req := range reqs {
		r, err := git.Open(ctx, req.Dir)
		if err != nil {
			continue
		}
		if st, err := r.Status(ctx); err == nil && st.Branch == "" {
			return req.Dir
		}
	}
	return reqs[0].Dir
}

// featureRefusal is an error of the app that is also a feature error (kind):
// its text stays the app's.
type featureRefusal struct{ err, kind error }

func (e featureRefusal) Error() string   { return e.err.Error() }
func (e featureRefusal) Unwrap() []error { return []error{e.err, e.kind} }

// AgentTasks looks the tasks up in the store and reads their state as the
// Agent Tasks panel does (merged from git, live state from the tracker).
func (h featureHost) AgentTasks(ctx context.Context, ids []string) map[string]feature.AgentTask {
	a := h.a
	out := map[string]feature.AgentTask{}
	if len(ids) > maxFeatureTaskIDs {
		ids = ids[:maxFeatureTaskIDs]
	}
	store := a.taskStore()
	repos := map[string]*git.Repo{}
	api := &TaskAPI{app: a}
	for _, id := range ids {
		if !agenttask.ValidID(id) {
			continue
		}
		_, task, ok := store.Find(id)
		if !ok {
			continue
		}
		r, seen := repos[task.Checkout]
		if !seen {
			r, _ = git.Open(ctx, task.Checkout)
			repos[task.Checkout] = r
		}
		if r == nil { // the checkout is gone: no git facts, the rest as known
			out[id] = storedFeatureTask(a, task)
			continue
		}
		out[id] = featureTask(api.view(ctx, r, task))
	}
	return out
}

func featureTask(v TaskView) feature.AgentTask {
	return feature.AgentTask{
		ID: v.ID, Title: v.Title, Agent: v.Agent, Branch: v.Branch, Base: v.Base, Worktree: v.Worktree,
		State: v.State, Error: v.Error, PullRequest: v.PullRequest, Merged: v.Merged, Stale: v.Stale,
		Joint: v.Joint, Lead: v.Lead,
	}
}

// storedFeatureTask is a task whose checkout can't be opened: no git facts,
// the rest as known.
func storedFeatureTask(a *App, task agenttask.Task) feature.AgentTask {
	live := a.tasks.State(task.ID, task.State)
	if live.State == agenttask.Failed && live.Error == "" {
		live.Error = task.Error
	}
	return feature.AgentTask{
		ID: task.ID, Title: task.Title, Agent: task.Agent, Branch: task.Branch, Base: task.Base,
		Worktree: task.Worktree, State: live.State, Error: live.Error, PullRequest: task.PullRequest,
		Stale: !isDir(task.Worktree), Joint: task.Joint, Lead: task.Lead,
	}
}

// Repository reads the checkout dir is in and its remotes.
func (h featureHost) Repository(ctx context.Context, dir string) (feature.Repository, error) {
	r, err := git.Open(ctx, dir)
	if err != nil {
		return feature.Repository{}, err
	}
	remotes, err := r.Remotes(ctx)
	if err != nil {
		return feature.Repository{}, err
	}
	out := feature.Repository{Root: r.Root, TaskWorktree: h.a.taskStore().ByWorktree(cleanFolder(r.Root)) != ""}
	for _, rm := range remotes {
		out.Remotes = append(out.Remotes, rm.URL)
	}
	return out, nil
}

// TaskOrigin is the origin of the task the chat works for: the tasks the
// tracker bound the chat to (by its folder), and for a joint group's chat
// its lead's.
func (h featureHost) TaskOrigin(chat string) string {
	if chat == "" {
		return ""
	}
	store := h.a.taskStore()
	for _, id := range h.a.tasks.Tasks(chat) {
		_, task, ok := store.Find(id)
		if !ok {
			continue
		}
		if task.Joint != "" {
			if members := store.Joint(task.Joint); len(members) > 0 {
				return members[0].Origin
			}
		}
		return task.Origin
	}
	return ""
}

// Agents are AgentAPI.Info's available agents and preferred one.
func (h featureHost) Agents() feature.Agents {
	info := (&AgentAPI{app: h.a}).Info()
	return feature.Agents{Available: info.Available, Default: info.Preferred}
}
