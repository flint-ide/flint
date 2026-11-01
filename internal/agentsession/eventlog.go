package agentsession

// The Manager keeps a bounded log of each open session's latest events, so a
// client that connects mid-conversation (Flint Remote) can replay what it
// missed. The desktop UI keeps its own history and never reads it. The log is
// off (LogLimits{}) until something needs it; switching it on keeps only what
// comes next, and what was dropped or never kept is reported as a Gap.

// LogLimits bound one session's event log; the oldest events are dropped
// first. A zero Events or Bytes switches the log off. The newest event is
// always kept, however large.
type LogLimits struct {
	Events int
	Bytes  int
}

func (l LogLimits) on() bool { return l.Events > 0 && l.Bytes > 0 }

// Gap is a range of a session's events (From..To, inclusive) that Replay
// can't return any more.
type Gap struct {
	SessionID string `json:"sessionId"`
	From      uint64 `json:"from"`
	To        uint64 `json:"to"`
}

// SessionState is an open chat as a remote client lists it.
type SessionState struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	Cwd   string `json:"cwd"`
	// Busy: a turn is running.
	Busy bool `json:"busy"`
	// Pending counts permission requests waiting for an answer.
	Pending int `json:"pending"`
	// LastSeq is the Seq of the session's latest event (0 = none yet).
	LastSeq uint64 `json:"lastSeq"`
}

// sessionLog holds one session's kept events, oldest first.
type sessionLog struct {
	events []Event
	bytes  int
}

func (l *sessionLog) add(e Event, lim LogLimits) {
	l.events = append(l.events, e)
	l.bytes += eventSize(e)
	l.trim(lim)
}

// trim drops the oldest events until the log fits, keeping the newest one.
func (l *sessionLog) trim(lim LogLimits) {
	drop := 0
	for len(l.events)-drop > 1 && (len(l.events)-drop > lim.Events || l.bytes > lim.Bytes) {
		l.bytes -= eventSize(l.events[drop])
		drop++
	}
	if drop > 0 {
		clear(l.events[:drop]) // let the dropped events' text be collected
		l.events = l.events[drop:]
	}
}

// eventSize estimates the memory an event holds: its strings plus a fixed
// overhead. It never marshals, so recording stays cheap on the desktop path.
func eventSize(e Event) int {
	n := 128 + len(e.SessionID) + len(e.Text) + len(e.RequestID) + len(e.StopReason)
	if e.ToolCall != nil {
		n += toolCallSize(*e.ToolCall)
	}
	if e.Permission != nil {
		n += 64 + len(e.Permission.ID) + toolCallSize(e.Permission.ToolCall)
		for _, o := range e.Permission.Options {
			n += 48 + len(o.ID) + len(o.Name) + len(o.Kind)
		}
	}
	for _, p := range e.Plan {
		n += 48 + len(p.Content) + len(p.Status) + len(p.Priority)
	}
	return n
}

func toolCallSize(tc ToolCall) int {
	n := 96 + len(tc.ID) + len(tc.Title) + len(tc.Kind) + len(tc.Status) + len(tc.Input) + len(tc.Output)
	for _, p := range tc.Paths {
		n += 16 + len(p)
	}
	for _, d := range tc.Diffs {
		n += 48 + len(d.Path) + len(d.OldText) + len(d.NewText)
	}
	return n
}

// record updates the session's pending requests and log. Called from emit
// with seqMu held.
func (m *Manager) record(e Event) (stateChanged bool) {
	switch e.Kind {
	case KindPermission:
		if e.Permission != nil {
			if m.pending[e.SessionID] == nil {
				if !m.isOpen(e.SessionID) {
					break // late event of a closed chat: keep nothing for it
				}
				m.pending[e.SessionID] = map[string]bool{}
			}
			m.pending[e.SessionID][e.Permission.ID] = true
			stateChanged = true
		}
	case KindPermissionSettled:
		if m.pending[e.SessionID][e.RequestID] {
			delete(m.pending[e.SessionID], e.RequestID)
			stateChanged = true
		}
	case KindTurnEnd:
		if len(m.pending[e.SessionID]) > 0 {
			delete(m.pending, e.SessionID)
		}
		stateChanged = true
	}
	if m.logLimits.on() {
		l := m.logs[e.SessionID]
		if l == nil {
			if !m.isOpen(e.SessionID) {
				return stateChanged
			}
			l = &sessionLog{}
			m.logs[e.SessionID] = l
		}
		l.add(e, m.logLimits)
	}
	return stateChanged
}

// isOpen reports whether a session is open. Lock order: seqMu, then mu
// (nothing takes seqMu while holding mu).
func (m *Manager) isOpen(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[sessionID] != nil
}

// SetLogLimits changes the event log's bounds; LogLimits{} switches it off
// and frees what it kept.
func (m *Manager) SetLogLimits(lim LogLimits) {
	m.seqMu.Lock()
	defer m.seqMu.Unlock()
	m.logLimits = lim
	if !lim.on() {
		m.logs = map[string]*sessionLog{}
		return
	}
	for _, l := range m.logs {
		l.trim(lim)
	}
}

// Replay returns the kept events of an open session with Seq > after, and
// the range before them that is no longer kept (nil when none is missing).
// A session that isn't open has nothing to replay.
func (m *Manager) Replay(sessionID string, after uint64) ([]Event, *Gap) {
	m.mu.Lock()
	open := m.sessions[sessionID] != nil
	m.mu.Unlock()
	if !open {
		return nil, nil
	}
	m.seqMu.Lock()
	defer m.seqMu.Unlock()
	last := m.seq[sessionID]
	if after >= last {
		return nil, nil
	}
	var out []Event
	oldest := last + 1 // the first kept event's Seq
	if l := m.logs[sessionID]; l != nil && len(l.events) > 0 {
		oldest = l.events[0].Seq
		for _, e := range l.events {
			if e.Seq > after {
				out = append(out, e)
			}
		}
	}
	if after+1 < oldest {
		return out, &Gap{SessionID: sessionID, From: after + 1, To: oldest - 1}
	}
	return out, nil
}

// Sessions lists the open sessions with their state.
func (m *Manager) Sessions() []SessionState {
	m.mu.Lock()
	out := make([]SessionState, 0, len(m.sessions))
	for id, s := range m.sessions {
		out = append(out, SessionState{ID: id, Agent: s.agent, Cwd: s.cwd, Busy: s.cancel != nil})
	}
	m.mu.Unlock()
	m.seqMu.Lock()
	for i := range out {
		out[i].Pending = len(m.pending[out[i].ID])
		out[i].LastSeq = m.seq[out[i].ID]
	}
	m.seqMu.Unlock()
	return out
}

// SetStateHook sets fn to be told when a session opens, closes, starts or
// ends a turn, or gets or loses a pending permission request. fn must return
// quickly and must not call back into the Manager while it runs: it may be
// called with the Manager's locks held.
func (m *Manager) SetStateHook(fn func(sessionID string)) {
	m.hookMu.Lock()
	defer m.hookMu.Unlock()
	m.hook = fn
}

func (m *Manager) stateChanged(sessionID string) {
	m.hookMu.Lock()
	fn := m.hook
	m.hookMu.Unlock()
	if fn != nil {
		fn(sessionID)
	}
}

// forget drops what emit kept about a closed session (its log and pending
// requests); its sequence number stays, so a resumed chat continues it.
func (m *Manager) forget(sessionID string) {
	m.seqMu.Lock()
	defer m.seqMu.Unlock()
	delete(m.logs, sessionID)
	delete(m.pending, sessionID)
}
