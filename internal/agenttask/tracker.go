package agenttask

import (
	"slices"
	"sync"

	as "flint/internal/agentsession"
)

// Tracker follows the chat sessions that run in task worktrees. Sessions are
// bound when they start or resume in a task's folder, so it needs no help
// from the UI, and every event is one map lookup for chats of no task. A
// session is bound to every task its folder works for (Store.ChatTasks): one
// task, or all tasks of a joint group when it runs in the lead's worktree.
// changed is called (outside the lock) only when a task's combined state or
// error changes, never per streamed token, once for each task that changed.
type Tracker struct {
	find    func(cwd string) []string // task ids of a session folder, none for no task
	changed func(taskID string, l Live)

	mu       sync.Mutex
	sessions map[string][]string // session id → task ids
	lives    map[string]Live     // session id → its state
}

// NewTracker creates a tracker; find maps a session's folder to the ids of
// the tasks a chat there works for.
func NewTracker(find func(cwd string) []string, changed func(taskID string, l Live)) *Tracker {
	return &Tracker{find: find, changed: changed, sessions: map[string][]string{}, lives: map[string]Live{}}
}

// Bind starts following a session that opened in cwd, when cwd is a task's.
func (t *Tracker) Bind(sessionID, cwd string) {
	if t == nil {
		return
	}
	tasks := t.find(cwd)
	if len(tasks) == 0 || sessionID == "" {
		return
	}
	t.update(sessionID, tasks, func(l Live) Live {
		if l.State == "" {
			l.State = Idle
		}
		return l
	})
}

// Prompted marks a session's turn as started (the user sent a message).
func (t *Tracker) Prompted(sessionID string) { t.update(sessionID, nil, Prompted) }

// Observe folds a session event in.
func (t *Tracker) Observe(e as.Event) {
	t.update(e.SessionID, nil, func(l Live) Live { return Next(l, e) })
}

// Unbind stops following a session (its chat was closed).
func (t *Tracker) Unbind(sessionID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	tasks, ok := t.sessions[sessionID]
	if !ok {
		t.mu.Unlock()
		return
	}
	before := t.combinedAll(tasks)
	delete(t.sessions, sessionID)
	delete(t.lives, sessionID)
	notes := t.diff(tasks, before)
	t.mu.Unlock()
	t.notify(notes)
}

// Forget drops a removed task from every session: a session bound to it
// alone is no longer followed, one shared with other tasks (a joint group's
// chat) still counts for them.
func (t *Tracker) Forget(taskID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for s, tasks := range t.sessions {
		if !slices.Contains(tasks, taskID) {
			continue
		}
		rest := slices.DeleteFunc(slices.Clone(tasks), func(id string) bool { return id == taskID })
		if len(rest) == 0 {
			delete(t.sessions, s)
			delete(t.lives, s)
		} else {
			t.sessions[s] = rest
		}
	}
}

// State is a task's combined state; remembered is its settled state from
// before (shown when no chat of it is open).
func (t *Tracker) State(taskID, remembered string) Live {
	if t == nil {
		return Combine(nil, remembered)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return Combine(t.chats(taskID), remembered)
}

// Active lists the tasks with a turn running (working or waiting for an
// approval), sorted: the ones that hold a slot of the queue. Every task of
// a joint group whose chat runs is listed (the caller counts the group once).
func (t *Tracker) Active() []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, tasks := range t.sessions {
		for _, task := range tasks {
			if seen[task] {
				continue
			}
			seen[task] = true
			if Busy(t.combined(task).State) {
				out = append(out, task)
			}
		}
	}
	slices.Sort(out)
	return out
}

// Sessions lists the chat sessions bound to a task (a joint group's chat
// is bound to each of its tasks).
func (t *Tracker) Sessions(taskID string) []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for s, tasks := range t.sessions {
		if slices.Contains(tasks, taskID) {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

// Tasks lists the tasks a session is bound to (none when it is not followed).
func (t *Tracker) Tasks(sessionID string) []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.sessions[sessionID])
}

// update applies fn to a bound session's state; bind (task ids) binds the
// session first. Sessions of no task return at once.
func (t *Tracker) update(sessionID string, bind []string, fn func(Live) Live) {
	if t == nil {
		return
	}
	t.mu.Lock()
	tasks, bound := t.sessions[sessionID]
	if len(bind) > 0 {
		// Tasks bound before (a rebind) are compared too.
		tasks, bound = union(tasks, bind), true
	}
	if !bound {
		t.mu.Unlock()
		return
	}
	before := t.combinedAll(tasks)
	if len(bind) > 0 {
		t.sessions[sessionID] = slices.Clone(bind)
	}
	t.lives[sessionID] = fn(t.lives[sessionID])
	notes := t.diff(tasks, before)
	t.mu.Unlock()
	t.notify(notes)
}

type note struct {
	task string
	live Live
}

// combinedAll is combined for each of tasks; called with t.mu held.
func (t *Tracker) combinedAll(tasks []string) []Live {
	out := make([]Live, len(tasks))
	for i, task := range tasks {
		out[i] = t.combined(task)
	}
	return out
}

// diff lists the tasks whose combined state is no longer before; called
// with t.mu held.
func (t *Tracker) diff(tasks []string, before []Live) []note {
	var out []note
	for i, task := range tasks {
		if after := t.combined(task); after != before[i] {
			out = append(out, note{task, after})
		}
	}
	return out
}

// notify reports changes, outside the lock.
func (t *Tracker) notify(notes []note) {
	for _, n := range notes {
		t.changed(n.task, n.live)
	}
}

func union(a, b []string) []string {
	out := slices.Clone(a)
	for _, x := range b {
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// combined is the task's state from its open chats; called with t.mu held.
// With none it is idle here: the remembered state is the caller's to add.
func (t *Tracker) combined(taskID string) Live {
	return Combine(t.chats(taskID), "")
}

func (t *Tracker) chats(taskID string) []Live {
	var out []Live
	for s, tasks := range t.sessions {
		if slices.Contains(tasks, taskID) {
			out = append(out, t.lives[s])
		}
	}
	return out
}
