package workflows

import (
	"context"
)

// advance starts the run's next step, if it has one to start now (change
// starts it).
func (e *engine) advance(id string) {
	_, _ = e.change(id, func(r Run) (Run, error) {
		if _, ok := r.Next(); !ok || r.Task == "" {
			return r, errIdle
		}
		return r, nil
	})
}

// launch takes the step change has just started in r: a chat or a script,
// in a goroutine. A gate waits for the user, and the first step's first
// start is the task's own first chat (taskContext).
func (e *engine) launch(r Run) {
	if r.State != StateRunning || !r.valid() {
		return
	}
	kind := r.Steps[r.Current].Kind
	if r.Current == 0 && r.Exec == 1 {
		return
	}
	ctx, ok := e.track(r)
	if !ok {
		return
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer e.untrack(r.ID, r.Exec)
		if kind == KindScript {
			e.takeScript(ctx, r)
		} else {
			e.takeChat(ctx, r)
		}
	}()
}

// track records the step r has just started, with a context Stop and Close
// cancel. It fails when the run moved on meanwhile (a Stop in between).
func (e *engine) track(r Run) (context.Context, bool) {
	base := e.baseCtx()
	e.mu.Lock()
	defer e.mu.Unlock()
	i := e.index(r.ID)
	if e.closed || i < 0 || e.runs[i].Over() || e.runs[i].Exec != r.Exec {
		return nil, false
	}
	if old, ok := e.execs[r.ID]; ok {
		old.cancel() // stale: the run started a step again
	}
	ctx, cancel := context.WithCancel(base)
	e.execs[r.ID] = execution{exec: r.Exec, cancel: cancel}
	return ctx, true
}

func (e *engine) untrack(id string, exec int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if x, ok := e.execs[id]; ok && x.exec == exec {
		x.cancel()
		delete(e.execs, id)
	}
}

// cancelExec cancels the step run id is taking.
func (e *engine) cancelExec(id string) {
	e.mu.Lock()
	x, ok := e.execs[id]
	delete(e.execs, id)
	e.mu.Unlock()
	if ok {
		x.cancel()
	}
}

// complete applies the end of a step start (exec) to the run, unless the
// run moved on since (or the start was already ended: a hold).
func (e *engine) complete(id string, exec int, fn func(r Run, p policy, now int64) Run) error {
	p, now := e.policy(), e.now()
	_, err := e.change(id, func(r Run) (Run, error) {
		if r.Over() || r.Exec != exec || !r.valid() || r.Records[r.Current].State != StepRunning {
			return r, errStale
		}
		return fn(r, p, now), nil
	})
	return err
}

// policy reads the settings and agents a step's end depends on.
func (e *engine) policy() policy {
	s := Section.Get(e.host.Settings())
	p := policy{maxAttempts: s.MaxAttempts, fallback: s.FallbackAgent, resumeAfter: int64(s.ResumeAfterMinutes) * 60}
	if p.fallback != "" {
		p.available = e.host.Agents().Available
	}
	return p
}

// current reports whether exec is still the run's current step start.
func (e *engine) current(id string, exec int) bool {
	r, ok := e.get(id)
	return ok && !r.Over() && r.Exec == exec
}

// stop stops the run and everything it started that still runs: its
// running script, and every chat its steps opened that is still open (the
// task's own first chat as Flint reported it, never a lookup of "the
// task's latest chat", which may be one the user or an agent opened).
func (e *engine) stop(id, reason string) error {
	var stopped Run
	now := e.now()
	r, err := e.change(id, func(r Run) (Run, error) { return r.Stop(reason, now) })
	if err != nil {
		return err
	}
	stopped = r
	e.cancelExec(id)
	for _, chat := range e.runChats(stopped) {
		_ = e.host.StopChat(chat)
	}
	return nil
}

// kick advances every run that is not over and looks at their tasks.
func (e *engine) kick() {
	for _, r := range e.list() {
		if !r.Over() {
			e.advance(r.ID)
		}
	}
	e.kickTasks()
}
