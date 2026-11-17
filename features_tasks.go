package main

import (
	"context"
	"errors"
	"fmt"

	"flint/internal/agenttask"
	"flint/internal/feature"
	"flint/internal/git"
)

// The agent tasks part of the feature Host (the Backlog feature hands work to
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
// tasks this call already made are removed again.
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
	for _, req := range reqs {
		gctx, cancel := context.WithTimeout(ctx, gitRemoteTimeout) // checkout hooks may run
		made, r, err := a.createTasks(gctx, NewTaskGroup{Dir: req.Dir, Description: req.Description, Agents: []string{req.Agent}}, true)
		cancel()
		if err != nil {
			uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gitTimeout)
			for _, g := range groups {
				a.discardTasks(uctx, g.r, g.tasks)
			}
			cancel()
			if len(groups) > 0 {
				a.tasksChanged()
			}
			return nil, fmt.Errorf("%s: %w", req.Dir, err)
		}
		groups = append(groups, group{r, made})
	}
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
	return out, nil
}

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
			live := a.tasks.State(task.ID, task.State)
			out[id] = feature.AgentTask{
				ID: task.ID, Title: task.Title, Agent: task.Agent, Branch: task.Branch, Base: task.Base,
				Worktree: task.Worktree, State: live.State, Error: live.Error, PullRequest: task.PullRequest,
				Stale: !isDir(task.Worktree),
			}
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

// Agents are AgentAPI.Info's available agents and preferred one.
func (h featureHost) Agents() feature.Agents {
	info := (&AgentAPI{app: h.a}).Info()
	return feature.Agents{Available: info.Available, Default: info.Preferred}
}
