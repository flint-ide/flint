package workflows

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"flint/internal/feature"
)

// start loads the runs. A stored run that does not validate (validRun) is
// dropped. A step that was running when Flint quit has failed (a chat wait
// or a script does not survive a restart), except a first step taken by the
// task: its end is read from the task, which survives. A run that would
// start a new step now, at launch and without the user, is paused instead
// (restarted): Resume goes on.
func (e *engine) start(ctx context.Context) {
	var runs []Run
	_ = e.host.LoadState(stateRuns, &runs)
	now := e.now()
	changed := false
	kept := make([]Run, 0, len(runs))
	for _, r := range runs {
		if validRun(r) != nil || slices.ContainsFunc(kept, func(k Run) bool { return k.ID == r.ID }) {
			changed = true // not a run this engine wrote
			continue
		}
		next := r
		if !byTask(r) {
			next = restarted(r.Interrupted(now), now)
		}
		changed = changed || next.State != r.State
		kept = append(kept, next)
	}
	e.mu.Lock()
	e.ctx = ctx
	e.runs = prune(append(kept, e.runs...))
	e.changedLocked()
	e.mu.Unlock()
	if changed {
		e.persist()
		e.host.Emit(EventChanged, "")
	}
	for _, r := range kept {
		e.syncTimer(r.ID) // a held run resumes at its stored time
	}
	e.kick()
}

// restartedText is the log line of a run paused at launch (restarted).
const restartedText = "Flint restarted: Resume to go on"

// restarted pauses a run that would start its next step at once (running,
// its current step pending): Flint was quit between two steps, and a step
// starts at launch only when the user says so.
func restarted(r Run, now int64) Run {
	if _, ok := r.Next(); !ok {
		return r
	}
	p, err := r.Pause(now)
	if err != nil {
		return r
	}
	p.Log[len(p.Log)-1].Text = restartedText // Pause's own, fresh entry
	return p
}

// close cancels the scripts, chat waits and resume timers; the chats
// themselves go on (quitting Flint does not stop an agent's turn). After it
// no step starts (change, track).
func (e *engine) close() {
	e.mu.Lock()
	execs, timers := e.execs, e.timers
	e.execs, e.timers, e.closed = map[string]execution{}, map[string]resumeTimer{}, true
	e.mu.Unlock()
	for _, x := range execs {
		x.cancel()
	}
	for _, t := range timers {
		t.stop()
	}
}

// taskContext answers Flint's start of a run's first chat with the first
// step's prompt; Flint appends the task's description after it. The run is
// found by the task's origin (a run Run Workflow… or start_workflow made:
// "workflow:<id>", asked before the run knows its task) or by the task
// (another feature's task a run takes, bound before its chat starts). Only
// a first step still taken by the task's own first chat answers.
func (e *engine) taskContext(_ context.Context, st feature.TaskStart) string {
	r, ok := e.startingRun(st)
	if !ok {
		return ""
	}
	return StepPrompt(r, false)
}

// taskChatStarted is the Instance.TaskChatStarted hook: Flint opened chat
// for the task start st. When st starts the first step of a run, the chat is
// that step's (its record), the one Stop ends and whose last lines are the
// step's result, before its first message is even sent.
func (e *engine) taskChatStarted(st feature.TaskStart, chat string) {
	r, ok := e.startingRun(st)
	if !ok {
		return
	}
	e.addChat(r.ID, chat)
	exec := r.Exec
	_, _ = e.change(r.ID, func(cur Run) (Run, error) {
		if !byTask(cur) || cur.Exec != exec || cur.Records[0].Chat != "" {
			return cur, errStale
		}
		return cur.WithChat(chat), nil
	})
	e.kickTasks()
}

// errRunOver refuses the start of the task of a run that is over or gone.
var errRunOver = errors.New("its workflow run was stopped")

// taskStartCheck is the Instance.TaskStartCheck hook: the chat of a run's
// task (by its origin, "workflow:<id>", or a run bound to the task) starts
// only while the run's first step waits for it. A run that is over, was
// removed or is unknown starts no agent, from the queue, Start now or after
// a restart.
func (e *engine) taskStartCheck(st feature.TaskStart) error {
	if id, ok := strings.CutPrefix(st.Origin, originPrefix); ok {
		r, found := e.get(id)
		if !runID.MatchString(id) || !found || r.Over() {
			return errRunOver
		}
		return nil
	}
	if st.TaskID == "" {
		return nil
	}
	if r, ok := e.taskRunLocked(st.TaskID); ok && r.Over() {
		return errRunOver
	}
	return nil
}

// taskRunLocked is the newest run whose task is taskID.
func (e *engine) taskRunLocked(taskID string) (Run, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.runs) - 1; i >= 0; i-- {
		if e.runs[i].Task == taskID {
			return e.runs[i], true
		}
	}
	return Run{}, false
}

// startingRun is the run whose first chat st starts.
func (e *engine) startingRun(st feature.TaskStart) (Run, bool) {
	if id, ok := strings.CutPrefix(st.Origin, originPrefix); ok {
		if !runID.MatchString(id) {
			return Run{}, false
		}
		r, ok := e.get(id)
		if !ok || !byTask(r) || st.TaskID != "" && r.Task != "" && r.Task != st.TaskID {
			return Run{}, false
		}
		return r, true
	}
	if st.TaskID == "" {
		return Run{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.runs) - 1; i >= 0; i-- {
		if r := e.runs[i]; r.Task == st.TaskID {
			return r, byTask(r)
		}
	}
	return Run{}, false
}

// syncTimer arms the timer that resumes run id when it is held with a
// time to resume at, and stops the one it no longer needs (resumed,
// stopped, removed, or Close ran).
func (e *engine) syncTimer(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var until int64
	if i := e.index(id); i >= 0 && !e.closed {
		if r := e.runs[i]; r.Hold != nil && r.State == StatePaused {
			until = r.Hold.Until
		}
	}
	t, ok := e.timers[id]
	if ok && t.until == until {
		return
	}
	if ok {
		t.stop()
		delete(e.timers, id)
	}
	if until == 0 {
		return
	}
	d := max(time.Duration(until-e.now())*time.Second, 0)
	e.timers[id] = resumeTimer{until: until, stop: e.afterFunc(d, func() { e.autoResume(id, until) })}
}

// autoResume resumes run id, if it is still held until then.
func (e *engine) autoResume(id string, until int64) {
	e.mu.Lock()
	if t, ok := e.timers[id]; ok && t.until == until {
		delete(e.timers, id)
	}
	e.mu.Unlock()
	now := e.now()
	_, _ = e.change(id, func(r Run) (Run, error) {
		if r.Hold == nil || r.Hold.Until != until || r.State != StatePaused {
			return r, errStale
		}
		return r.Resume(now)
	})
}
