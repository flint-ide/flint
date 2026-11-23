package workflows

import (
	"context"
	"time"

	"flint/internal/agenttask"
	"flint/internal/feature"
)

// tasksTimeout bounds one look at the runs' agent tasks (git calls).
const tasksTimeout = time.Minute

// kickTasks asks for a look at the runs' agent tasks (TasksChanged). At most
// one pass runs; asks that come while it runs make one more.
func (e *engine) kickTasks() {
	e.mu.Lock()
	if e.ctx == nil { // Start makes the first pass
		e.mu.Unlock()
		return
	}
	if e.passing {
		e.again = true
		e.mu.Unlock()
		return
	}
	e.passing = true
	e.wg.Add(1)
	e.mu.Unlock()
	go func() {
		defer e.wg.Done()
		for {
			e.tasksPass()
			e.mu.Lock()
			if !e.again {
				e.passing = false
				e.mu.Unlock()
				return
			}
			e.again = false
			e.mu.Unlock()
		}
	}()
}

// tasksPass reads the agent tasks of the runs that are not over: a removed
// task stops its run, and a first step taken by the task ends with the
// task's turn.
func (e *engine) tasksPass() {
	var live []Run
	for _, r := range e.list() {
		if !r.Over() {
			live = append(live, r)
		}
	}
	if len(live) == 0 {
		return
	}
	ids := make([]string, len(live))
	for i, r := range live {
		ids[i] = r.Task
	}
	ctx, cancel := context.WithTimeout(e.baseCtx(), tasksTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return
	}
	tasks := e.host.AgentTasks(ctx, ids)
	for _, r := range live {
		t, ok := tasks[r.Task]
		switch {
		case !ok:
			if e.current(r.ID, r.Exec) {
				_ = e.stop(r.ID, reasonRemoved)
			}
		case byTask(r):
			e.firstStep(r, t)
		}
	}
}

// firstStep follows the first step of r, taken by its task's own chat, from
// the state of the task t.
func (e *engine) firstStep(r Run, t feature.AgentTask) {
	// r was read before the tasks were looked up (git work, off the lock):
	// the chat Flint opened for the task may have been recorded since, so
	// the run is read again and a run that moved on meanwhile is left alone.
	cur, ok := e.get(r.ID)
	if !ok || !byTask(cur) || cur.Exec != r.Exec {
		return
	}
	r = cur
	switch t.State {
	case agenttask.Working, agenttask.Waiting:
		if !r.Records[0].Seen {
			_ = e.completeByTask(r, func(r Run, _ policy, _ int64) Run { return r.WithSeen() })
		}
	case agenttask.Finished:
		// The chat Flint opened for the task (taskChatStarted), never "the
		// task's latest chat": the agent or the user may have opened another
		// one in the worktree, whose last lines would be read as the result.
		chat := r.Records[0].Chat
		if chat == "" {
			_ = e.completeByTask(r, failed(reasonNoChat, "", false).apply)
			return
		}
		out := e.host.ChatOutput(chat, outputLines)
		o := chatOutcome(r.Steps[0].Kind, feature.ChatResult{State: agenttask.Finished}, out)
		_ = e.completeByTask(r, o.apply)
	case agenttask.Failed, agenttask.Interrupted:
		reason := t.Error
		switch {
		case reason != "":
		case t.State == agenttask.Failed:
			reason = "its agent task failed"
		default:
			reason = "its agent task was interrupted"
		}
		_ = e.completeByTask(r, failure(reason, t.Error, "").apply)
	case agenttask.Idle:
		if r.Records[0].Seen {
			_ = e.completeByTask(r, failed(reasonCanceled, "", false).apply)
		}
	}
	// queued, or idle before it was seen working: its chat has not run yet.
}

// completeByTask is complete for a first step still taken by its task.
func (e *engine) completeByTask(r Run, fn func(Run, policy, int64) Run) error {
	p, now := e.policy(), e.now()
	_, err := e.change(r.ID, func(cur Run) (Run, error) {
		if !byTask(cur) || cur.Exec != r.Exec {
			return cur, errStale
		}
		return fn(cur, p, now), nil
	})
	return err
}
