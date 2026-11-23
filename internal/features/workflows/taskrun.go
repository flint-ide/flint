package workflows

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"flint/internal/feature"
)

// Who started a run (Run.Origin).
const (
	// OriginItem: a project item's Delegate (feature.NewAgentTask.Workflow).
	OriginItem = "item"
	// OriginAgent: an agent's start_workflow call the user approved.
	OriginAgent = "agent"
)

// runRequest is a run to start for an agent task: what Run Workflow…,
// a project item's Delegate and start_workflow ask for. Every start takes
// the same checks (checkRun).
type runRequest struct {
	dir, workflow, description, agent string
	// origin is Run.Origin ("" for the user); by replaces the log's
	// "Run started" with who started it ("" keeps it).
	origin, by string
	// consent is how the start gets the user's OK for script steps without
	// a sandbox (scriptConsent): asked by default.
	consent consentMode
}

// checkedRun is a runRequest that passed every check.
type checkedRun struct {
	req                    runRequest
	root, dir, desc, agent string
	workflow               Workflow
	steps                  []Step
	// checkout is the repository the run's task branches from (Run.Dir).
	checkout string
}

// firstAgent takes the task's first chat: the first step's agent, else the
// run's.
func (c checkedRun) firstAgent() string { return cmp.Or(c.steps[0].Agent, c.agent) }

// checkRun checks a start: a folder inside an open, trusted workspace
// folder that is in a git repository; a workflow offered there (a
// repository's own only when its workspace folder is trusted) that
// resolves; a task text; agents that can start. It changes nothing.
func (a *API) checkRun(req runRequest) (checkedRun, error) {
	root, dir, err := a.workspaceDir(req.dir)
	if err != nil {
		return checkedRun{}, err
	}
	if !a.host.Trusted(dir) {
		return checkedRun{}, fmt.Errorf("%s is in Restricted Mode: trust the folder to run workflows there", dir)
	}
	w, steps, err := a.startable(root, req.workflow)
	if err != nil {
		return checkedRun{}, err
	}
	desc, err := checkDescription(req.description)
	if err != nil {
		return checkedRun{}, err
	}
	agent, err := checkAgents(req.agent, steps, a.host.Agents())
	if err != nil {
		return checkedRun{}, err
	}
	rctx, rcancel := a.ctx(requestTimeout)
	repo, err := a.host.Repository(rctx, dir)
	rcancel()
	if err != nil {
		return checkedRun{}, fmt.Errorf("%s is not in a git repository: %w", dir, err)
	}
	return checkedRun{req: req, root: root, dir: dir, desc: desc, agent: agent, workflow: w, steps: steps, checkout: repo.Root}, nil
}

// planRun checks req and stores its run, the first step started, without
// a task: nobody sees it (list, persist) until bindRun gives it one. Flint
// may start the task's chat, and ask taskContext for the step's prompt,
// before CreateAgentTasks returns, so the run must exist by then.
func (a *API) planRun(req runRequest) (checkedRun, string, error) {
	c, err := a.checkRun(req)
	if err != nil {
		return checkedRun{}, "", err
	}
	// Before the run exists: a question may stay open for long.
	if err := a.scriptConsent(c); err != nil {
		return checkedRun{}, "", err
	}
	e := a.engine
	id := e.newID()
	for _, ok := e.get(id); ok; _, ok = e.get(id) {
		id = e.newID()
	}
	now := e.now()
	r := NewRun(id, c.workflow.Name, c.steps, c.desc, c.agent, c.checkout, now)
	r.Origin = req.origin
	if req.by != "" {
		r.Log[len(r.Log)-1].Text = req.by // NewRun's own, fresh slice
	}
	e.add(r.Started(now))
	return c, id, nil
}

// bindRun gives the planned run id its agent task.
func (a *API) bindRun(id, taskID string) (Run, error) {
	r, err := a.engine.change(id, func(r Run) (Run, error) {
		if r.Task != "" {
			return r, errors.New("the workflow run has its agent task already")
		}
		r.Task = taskID
		return r, nil
	})
	if err != nil {
		return Run{}, err
	}
	a.engine.kickTasks() // the task may have moved on while it was made
	return r, nil
}

// startRun starts a run with its own agent task (Run Workflow…,
// start_workflow): planRun, then the task, then bindRun.
func (a *API) startRun(req runRequest) (RunView, error) {
	c, id, err := a.planRun(req)
	if err != nil {
		return RunView{}, err
	}
	e := a.engine
	ctx, cancel := a.ctx(createTimeout)
	defer cancel()
	tasks, err := a.host.CreateAgentTasks(ctx, []feature.NewAgentTask{{
		Dir: c.dir, Description: c.desc, Agent: c.firstAgent(), Origin: originPrefix + id,
	}})
	if err == nil && len(tasks) != 1 {
		err = errors.New("no agent task was made")
	}
	if err != nil {
		e.drop(id)
		if dh := (*feature.DetachedHeadError)(nil); errors.As(err, &dh) {
			return RunView{}, fmt.Errorf("%s is on a detached HEAD: check out a branch there, then run the workflow again", dh.Dir)
		}
		return RunView{}, err
	}
	r, err := a.bindRun(id, tasks[0].ID)
	if err != nil {
		return RunView{}, err
	}
	return runView(r, tasks[0].State), nil
}

// taskWorkflow is the Instance.TaskWorkflow hook: another feature's agent
// task (a project item's) is taken by a run of req.Workflow, with every
// check of Run Workflow… for req.Dir. The run's agent is req.Agent; the
// task's is the first step's.
func (a *API) taskWorkflow(_ context.Context, req feature.NewAgentTask) (feature.PlannedWorkflow, error) {
	c, id, err := a.planRun(itemRequest(req))
	if err != nil {
		return feature.PlannedWorkflow{}, err
	}
	return feature.PlannedWorkflow{
		Agent: c.firstAgent(),
		Bind: func(taskID string) error {
			_, err := a.bindRun(id, taskID)
			return err
		},
		Drop: func() { a.engine.drop(id) },
	}, nil
}

// maxDelegatedBy bounds who delegated an item in a run's log (bytes).
const maxDelegatedBy = 300

// itemRequest is the run of a project item's task. An item an agent
// delegated with the user's yes to that call (NewAgentTask.DelegatedBy) is
// the agent's run: its origin is OriginAgent, its log says who delegated it,
// and script steps without a sandbox are refused, never asked, until the
// user allowed them in the checkout (consentAgent), as for start_workflow.
func itemRequest(req feature.NewAgentTask) runRequest {
	r := runRequest{
		dir: req.Dir, workflow: req.Workflow, description: req.Description, agent: req.Agent,
		origin: OriginItem, by: "Run started for a project item",
	}
	if by := oneLine(req.DelegatedBy); by != "" {
		r.origin, r.consent = OriginAgent, consentAgent
		r.by = "Run started for a project item, delegated by " + cut(by, maxDelegatedBy) + ", approved by you"
	}
	return r
}

// taskWorkflowCheck is the Instance.TaskWorkflowCheck hook: taskWorkflow's
// checks (checkRun, and for an agent's delegation the script consent,
// which then refuses rather than asks), planning nothing. A request the
// user made is not asked about scripts here: its start asks.
func (a *API) taskWorkflowCheck(_ context.Context, req feature.NewAgentTask) error {
	c, err := a.checkRun(itemRequest(req))
	if err != nil || c.req.consent == consentAsk {
		return err
	}
	return a.scriptConsent(c)
}

// taskRun is the Instance.TaskRun hook: the newest run whose task is
// taskID.
func (e *engine) taskRun(taskID string) (feature.TaskRun, bool) {
	if taskID == "" {
		return feature.TaskRun{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.runs) - 1; i >= 0; i-- {
		if r := e.runs[i]; r.Task == taskID {
			return feature.TaskRun{ID: r.ID, Workflow: r.Workflow, State: r.State, AtLastStep: r.Current >= len(r.Steps)-1}, true
		}
	}
	return feature.TaskRun{}, false
}
