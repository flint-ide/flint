package workflows

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"sync"
	"time"

	"flint/internal/feature"
)

// EventChanged is emitted after a run was added, changed or removed; its
// data is the run's id. The UI reads the runs again (API.Runs).
const EventChanged = "workflows:changed"

// originPrefix starts the Origin of a run's agent task: "workflow:<run id>".
const originPrefix = "workflow:"

// runID is the form of a run's id (newRunID).
var runID = regexp.MustCompile(`^[0-9a-f]{16}$`)

var (
	errNoRun = errors.New("no such workflow run: it was removed")
	// errStale: the completion of a step start that is not the run's
	// current one (the run moved on, was stopped or removed).
	errStale = errors.New("the step was started again or the run moved on")
	// errIdle: nothing to start (advance).
	errIdle = errors.New("no step to start")
)

// engine takes the runs' steps through feature.Host. Every run lives in runs
// behind mu; every change goes through change, which applies a pure
// transition, saves, tells the UI and advances the run. The Host is never
// called with mu held.
type engine struct {
	host feature.Host
	// now is the clock (unix seconds); newID makes run ids; afterFunc arms
	// a timer (time.AfterFunc) and returns its stop. Tests replace them.
	now       func() int64
	newID     func() string
	afterFunc func(d time.Duration, f func()) (stop func() bool)

	mu sync.Mutex
	// runs are the runs, oldest first (as state.json keeps them).
	runs []Run
	// version counts the changes of runs, so a slower save of an older
	// snapshot never overwrites a newer one.
	version int
	// execs are the steps being taken now, by run id.
	execs map[string]execution
	// timers resume the runs held by a limit (workflows.resumeAfterMinutes),
	// by run id; closed: Close ran, no timer is armed again.
	timers map[string]resumeTimer
	closed bool
	// ctx is Start's context (nil before Start); the steps run under it.
	ctx context.Context
	// passing: a TasksChanged pass runs; again: another one was asked for.
	passing, again bool
	// wake is closed (and cleared) by every change of runs: waitRun's
	// waiters look again (nil while nobody waits).
	wake chan struct{}

	saveMu sync.Mutex
	saved  int

	// wg counts the goroutines the engine started (tests wait for them).
	wg sync.WaitGroup
	// changed, when set, runs after every applied change (tests).
	changed func()

	// chats are the chats each run's steps opened, by run id, so Stop ends
	// every one still open (a step's record keeps only its latest attempt's).
	// In memory only: chats do not outlive Flint.
	chats map[string][]string
}

// maxRunChats bounds the chats remembered per run (30 steps, a few
// attempts each); the oldest are forgotten first.
const maxRunChats = 256

// resumeTimer resumes a held run at until.
type resumeTimer struct {
	until int64
	stop  func() bool
}

// execution is the step a run is taking: its start (Run.Exec) and the
// cancel of the context it runs under.
type execution struct {
	exec   int
	cancel context.CancelFunc
}

func newEngine(h feature.Host) *engine {
	return &engine{
		host:  h,
		now:   func() int64 { return time.Now().Unix() },
		newID: newRunID,
		afterFunc: func(d time.Duration, f func()) func() bool {
			return time.AfterFunc(d, f).Stop
		},
		execs:  map[string]execution{},
		timers: map[string]resumeTimer{},
		chats:  map[string][]string{},
	}
}

func newRunID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// baseCtx is the context the engine's work runs under.
func (e *engine) baseCtx() context.Context {
	e.mu.Lock()
	ctx := e.ctx
	e.mu.Unlock()
	if ctx == nil {
		ctx = e.host.Context()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx
}

// byTask reports whether r's current step is its first one, started by
// Flint's own start of the agent task: its end is read from the task's
// state (tasksPass), not from a chat wait. Its chat is the one Flint
// reported when it opened it (taskChatStarted), "" until then.
func byTask(r Run) bool {
	return !r.Over() && r.Current == 0 && r.Exec == 1 && len(r.Records) > 0 &&
		r.Records[0].State == StepRunning
}

// ---- the runs

func (e *engine) index(id string) int {
	return slices.IndexFunc(e.runs, func(r Run) bool { return r.ID == id })
}

// get returns the run with this id.
func (e *engine) get(id string) (Run, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if i := e.index(id); i >= 0 {
		return e.runs[i], true
	}
	return Run{}, false
}

// list returns the runs that have an agent task (a run being created has
// none yet), oldest first.
func (e *engine) list() []Run {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.listLocked()
}

func (e *engine) listLocked() []Run {
	out := make([]Run, 0, len(e.runs))
	for _, r := range e.runs {
		if r.Task != "" {
			out = append(out, r)
		}
	}
	return out
}

// change applies fn to the run with this id. When fn returns an error
// nothing changes. Else, when the run has a step to start now (Next) and
// an agent task, the step is started in the same change (Started), so a
// transition is saved and told to the UI once, never with a false
// "running" between a passed step and a gate; then the step is taken
// (launch). After Close nothing is started.
func (e *engine) change(id string, fn func(Run) (Run, error)) (Run, error) {
	now := e.now()
	e.mu.Lock()
	i := e.index(id)
	if i < 0 {
		e.mu.Unlock()
		return Run{}, errNoRun
	}
	prev := e.runs[i]
	next, err := fn(prev)
	if err != nil {
		e.mu.Unlock()
		return Run{}, err
	}
	started := false
	if _, ok := next.Next(); ok && next.Task != "" && !e.closed {
		next, started = next.Started(now), true
	}
	runs := slices.Clone(e.runs)
	runs[i] = next
	e.runs = runs
	e.changedLocked()
	e.mu.Unlock()
	if next.Task != "" && next.State != prev.State {
		// What the run's task means to others (a project item's status,
		// TaskRun) changed.
		e.host.TasksChanged()
	}
	e.changedRuns(id)
	if started {
		e.launch(next)
	}
	return next, nil
}

// changedLocked counts a change of runs and wakes the waiters. Callers hold
// mu.
func (e *engine) changedLocked() {
	e.version++
	if e.wake != nil {
		close(e.wake)
		e.wake = nil
	}
}

// waitRun blocks until cond holds for run id (at once when it does now),
// the run is removed (errNoRun) or ctx ends (the run as it is, and ctx's
// error). No polling: every change wakes it.
func (e *engine) waitRun(ctx context.Context, id string, cond func(Run) bool) (Run, error) {
	for {
		e.mu.Lock()
		i := e.index(id)
		if i < 0 {
			e.mu.Unlock()
			return Run{}, errNoRun
		}
		r := e.runs[i]
		if cond(r) {
			e.mu.Unlock()
			return r, nil
		}
		if e.wake == nil {
			e.wake = make(chan struct{})
		}
		wake := e.wake
		e.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return r, ctx.Err()
		}
	}
}

// add stores a new run (pruning the oldest) without saving it: it is saved
// once it has its agent task (setTask).
func (e *engine) add(r Run) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runs = prune(append(slices.Clone(e.runs), r))
	e.changedLocked()
}

// forget drops a run that is over.
func (e *engine) forget(id string) error {
	e.mu.Lock()
	i := e.index(id)
	switch {
	case i < 0:
		e.mu.Unlock()
		return errNoRun
	case !e.runs[i].Over():
		e.mu.Unlock()
		return errors.New("stop the run first")
	}
	delete(e.chats, id)
	e.runs = slices.Delete(slices.Clone(e.runs), i, i+1)
	e.changedLocked()
	e.mu.Unlock()
	e.changedRuns(id)
	return nil
}

// drop forgets a run whose agent task could not be made: one never shown,
// or one bound to a task that a failed CreateAgentTasks took back.
func (e *engine) drop(id string) {
	e.mu.Lock()
	i := e.index(id)
	if i < 0 {
		e.mu.Unlock()
		return
	}
	shown := e.runs[i].Task != ""
	delete(e.chats, id)
	e.runs = slices.Delete(slices.Clone(e.runs), i, i+1)
	e.changedLocked()
	e.mu.Unlock()
	if shown {
		e.changedRuns(id)
	}
}

// addChat remembers a chat a step of run id opened (Stop ends it).
func (e *engine) addChat(id, chat string) {
	if chat == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.index(id) < 0 || slices.Contains(e.chats[id], chat) {
		return
	}
	list := append(slices.Clone(e.chats[id]), chat)
	if len(list) > maxRunChats {
		list = list[len(list)-maxRunChats:]
	}
	e.chats[id] = list
}

// runChats are the chats run r's steps opened: those its records name and
// those remembered (addChat), each once.
func (e *engine) runChats(r Run) []string {
	e.mu.Lock()
	out := slices.Clone(e.chats[r.ID])
	e.mu.Unlock()
	for _, rec := range r.Records {
		if rec.Chat != "" && !slices.Contains(out, rec.Chat) {
			out = append(out, rec.Chat)
		}
	}
	return out
}

// changedRuns saves the runs and tells the UI that run id changed.
func (e *engine) changedRuns(id string) {
	e.syncTimer(id)
	e.persist()
	e.host.Emit(EventChanged, id)
	if e.changed != nil {
		e.changed()
	}
}

// persist saves the runs that have an agent task, unless a newer snapshot
// was saved meanwhile.
func (e *engine) persist() {
	e.saveMu.Lock()
	defer e.saveMu.Unlock()
	e.mu.Lock()
	version, runs := e.version, prune(e.listLocked())
	e.mu.Unlock()
	if version <= e.saved {
		return
	}
	if err := e.host.SaveState(stateRuns, runs); err == nil {
		e.saved = version
	}
}
