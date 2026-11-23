package workflows

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"flint/internal/agenttask"
	"flint/internal/feature"
	"flint/internal/textsafe"
)

// Timeouts of the requests that call git.
const (
	requestTimeout = 30 * time.Second
	// createTimeout bounds making a run's agent task (a worktree, hooks).
	createTimeout = 5 * time.Minute
)

// maxArg bounds a short argument from the UI (an agent's name, a step).
const maxArg = 200

// API is the Workflows service. Every argument comes from the web view:
// run ids are checked against the known runs, folders against the open
// workspace folders, names against ValidName, and text is bounded.
type API struct {
	host   feature.Host
	engine *engine
	// sched starts the runs of workflows.schedules (schedule_timer.go).
	sched *scheduler

	// askMu asks one question about script steps without a sandbox at a
	// time; consentMu guards the accepted checkouts (unsandboxed.go).
	askMu, consentMu sync.Mutex

	mu sync.Mutex
	// started counts the runs each chat started with start_workflow
	// (maxStartsPerChat), by mcpserver.CallerFrom.
	started map[string]int
}

func newAPI(h feature.Host) *API {
	a := &API{host: h, engine: newEngine(h), started: map[string]int{}}
	a.sched = newScheduler(a)
	return a
}

func (a *API) ctx(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.engine.baseCtx(), timeout)
}

// StartRequest is what Run Workflow… asks for.
type StartRequest struct {
	// Dir is a folder inside an open workspace folder: the run's agent task
	// branches from its checkout.
	Dir string `json:"dir"`
	// Workflow is the workflow's name (WorkflowView.Name).
	Workflow string `json:"workflow"`
	// Description is the task: what the workflow should deliver.
	Description string `json:"description"`
	// Agent takes the steps that name none; "" is the default agent.
	Agent string `json:"agent"`
}

// Start starts a workflow run: its agent task is made and its first step
// is taken by the task's first chat (or queued with it).
func (a *API) Start(req StartRequest) (RunView, error) {
	return a.startRun(runRequest{dir: req.Dir, workflow: req.Workflow, description: req.Description, agent: req.Agent})
}

// checkDescription trims the task's text and checks it.
func checkDescription(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", errors.New("describe the task: what should the workflow deliver?")
	case len(s) > agenttask.MaxPrompt:
		return "", fmt.Errorf("the task is longer than %d bytes", agenttask.MaxPrompt)
	}
	if r, bad := textsafe.HiddenRune(s); bad {
		return "", fmt.Errorf("the task contains %s", textsafe.RuneKind(r))
	}
	return s, nil
}

// checkAgents picks the run's agent ("" = the default one) and checks that
// it and every agent a step names can start.
func checkAgents(agent string, steps []Step, agents feature.Agents) (string, error) {
	if len(agent) > maxArg {
		return "", errors.New("that agent is not available")
	}
	agent = cmp.Or(agent, agents.Default)
	if agent == "" {
		return "", errors.New("no agent can start: install Claude Code or Codex, or define an agent in Settings")
	}
	if !slices.Contains(agents.Available, agent) {
		return "", fmt.Errorf("the agent %q is not available", agent)
	}
	for _, s := range steps {
		if s.Agent != "" && !slices.Contains(agents.Available, s.Agent) {
			return "", fmt.Errorf("the step %q names the agent %q, which is not available", s.Title, s.Agent)
		}
	}
	return agent, nil
}

// Runs lists the runs, newest first. Only a run that is not over asks the
// host about its agent task (several git commands each): the task's state
// and whether it is queued matter while the run goes on. A run that is over
// shows no task state ("").
func (a *API) Runs() []RunView {
	runs := a.engine.list()
	var ids []string
	for _, r := range runs {
		if !r.Over() {
			ids = append(ids, r.Task)
		}
	}
	var tasks map[string]feature.AgentTask
	if len(ids) > 0 {
		ctx, cancel := a.ctx(requestTimeout)
		tasks = a.host.AgentTasks(ctx, ids)
		cancel()
	}
	out := make([]RunView, 0, len(runs))
	for _, r := range slices.Backward(runs) {
		out = append(out, runView(r, tasks[r.Task].State))
	}
	slices.SortStableFunc(out, func(x, y RunView) int { return cmp.Compare(y.Created, x.Created) })
	return out
}

// run checks a run id from the UI.
func (a *API) run(id string) (Run, error) {
	if !runID.MatchString(id) {
		return Run{}, errNoRun
	}
	r, ok := a.engine.get(id)
	if !ok || r.Task == "" {
		return Run{}, errNoRun
	}
	return r, nil
}

// act applies a transition the user asked for to the run id.
func (a *API) act(id string, fn func(r Run, now int64) (Run, error)) error {
	if _, err := a.run(id); err != nil {
		return err
	}
	now := a.engine.now()
	_, err := a.engine.change(id, func(r Run) (Run, error) { return fn(r, now) })
	return err
}

// Approve passes the gate the run waits at.
func (a *API) Approve(id string) error { return a.act(id, Run.Approve) }

// SendBack takes the steps from step (an id or a title) on again, with note
// for its agent.
func (a *API) SendBack(id, step, note string) error {
	if len(step) > maxArg {
		return errors.New("choose a step of the run")
	}
	return a.act(id, func(r Run, now int64) (Run, error) { return r.SendBack(step, note, now) })
}

// Retry takes the failed step again.
func (a *API) Retry(id string) error { return a.act(id, Run.Retry) }

// Recheck reads the end of the failed step's chat again ("Check again"): the
// user answered the agent there, so a result line may be there now. DONE or
// PASS passes the step and the run goes on; otherwise the run stays failed.
func (a *API) Recheck(id string) error {
	if _, err := a.run(id); err != nil {
		return err
	}
	return a.engine.recheck(id)
}

// Skip passes the failed step ("Continue anyway").
func (a *API) Skip(id string) error { return a.act(id, Run.Skip) }

// Pause holds the run before its next step.
func (a *API) Pause(id string) error { return a.act(id, Run.Pause) }

// Resume lets a paused run go on.
func (a *API) Resume(id string) error { return a.act(id, Run.Resume) }

// Stop ends the run and the chat or script of its running step. The agent
// task stays.
func (a *API) Stop(id string) error {
	if _, err := a.run(id); err != nil {
		return err
	}
	return a.engine.stop(id, "")
}

// Remove forgets a run that is over; its agent task stays.
func (a *API) Remove(id string) error {
	if _, err := a.run(id); err != nil {
		return err
	}
	return a.engine.forget(id)
}
