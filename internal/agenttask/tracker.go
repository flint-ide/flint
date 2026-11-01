package agenttask

import (
	"slices"
	"sync"

	as "flint/internal/agentsession"
)

// Tracker follows the chat sessions that run in task worktrees. Sessions are
// bound when they start or resume in a task's folder, so it needs no help
// from the UI, and every event is one map lookup for chats of no task.
// changed is called (outside the lock) only when a task's combined state or
// error changes, never per streamed token.
type Tracker struct {
	find    func(cwd string) string // task id of a session folder, "" for none
	changed func(taskID string, l Live)

	mu       sync.Mutex
	sessions map[string]string // session id → task id
	lives    map[string]Live   // session id → its state
}

// NewTracker creates a tracker; find maps a session's folder to a task id.
func NewTracker(find func(cwd string) string, changed func(taskID string, l Live)) *Tracker {
	return &Tracker{find: find, changed: changed, sessions: map[string]string{}, lives: map[string]Live{}}
}

// Bind starts following a session that opened in cwd, when cwd is a task's.
func (t *Tracker) Bind(sessionID, cwd string) {
	if t == nil {
		return
	}
	task := t.find(cwd)
	if task == "" || sessionID == "" {
		return
	}
	t.update(sessionID, task, func(l Live) Live {
		if l.State == "" {
			l.State = Idle
		}
		return l
	})
}

// Prompted marks a session's turn as started (the user sent a message).
func (t *Tracker) Prompted(sessionID string) { t.update(sessionID, "", Prompted) }

// Observe folds a session event in.
func (t *Tracker) Observe(e as.Event) {
	t.update(e.SessionID, "", func(l Live) Live { return Next(l, e) })
}

// Unbind stops following a session (its chat was closed).
func (t *Tracker) Unbind(sessionID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	task, ok := t.sessions[sessionID]
	if !ok {
		t.mu.Unlock()
		return
	}
	before := t.combined(task)
	delete(t.sessions, sessionID)
	delete(t.lives, sessionID)
	after := t.combined(task)
	t.mu.Unlock()
	if after != before {
		t.changed(task, after)
	}
}

// Forget drops every session of a removed task.
func (t *Tracker) Forget(taskID string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for s, task := range t.sessions {
		if task == taskID {
			delete(t.sessions, s)
			delete(t.lives, s)
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

// Sessions lists the chat sessions bound to a task.
func (t *Tracker) Sessions(taskID string) []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for s, task := range t.sessions {
		if task == taskID {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

// update applies fn to a bound session's state; bind (a task id) binds the
// session first. Sessions of no task return at once.
func (t *Tracker) update(sessionID, bind string, fn func(Live) Live) {
	if t == nil {
		return
	}
	t.mu.Lock()
	task, bound := t.sessions[sessionID]
	if bind != "" {
		task, bound = bind, true
	}
	if !bound {
		t.mu.Unlock()
		return
	}
	before := t.combined(task)
	t.sessions[sessionID] = task
	t.lives[sessionID] = fn(t.lives[sessionID])
	after := t.combined(task)
	t.mu.Unlock()
	if after != before {
		t.changed(task, after)
	}
}

// combined is the task's state from its open chats; called with t.mu held.
// With none it is idle here: the remembered state is the caller's to add.
func (t *Tracker) combined(taskID string) Live {
	return Combine(t.chats(taskID), "")
}

func (t *Tracker) chats(taskID string) []Live {
	var out []Live
	for s, task := range t.sessions {
		if task == taskID {
			out = append(out, t.lives[s])
		}
	}
	return out
}
