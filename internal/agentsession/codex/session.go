package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"flint/internal/agentsession"
)

const (
	// turnStartTimeout bounds the turn/start round trip (not the turn).
	turnStartTimeout = 30 * time.Second
	// cancelGrace is how long Prompt waits for turn/completed after its ctx
	// is cancelled before giving up on the server.
	cancelGrace = 10 * time.Second
)

var errTurnActive = errors.New("codex: a turn is already in progress")

// session is one Codex thread served by its own app-server process.
type session struct {
	cwd    string
	sink   agentsession.Sink
	events *eventQueue
	sid    atomic.Pointer[string] // thread id, once known

	mu      sync.Mutex
	proc    *process
	turn    *turn
	items   map[string]*itemState
	pending map[string]*pendingRequest
	usage   tokenBreakdown // latest cumulative thread total
	exited  bool
	exitErr error
}

// turn is the state of the running prompt turn.
type turn struct {
	id              string
	done            chan struct{}
	stop            string
	err             error
	ended           bool
	cancelRequested bool
	interruptSent   bool
	usageBase       tokenBreakdown
	usageSeen       bool
	lastError       string
	lastTextItem    string // agent message item that last produced text
}

func newSession(cwd string, sink agentsession.Sink) *session {
	s := &session{
		cwd:     cwd,
		sink:    sink,
		events:  newEventQueue(),
		items:   map[string]*itemState{},
		pending: map[string]*pendingRequest{},
	}
	empty := ""
	s.sid.Store(&empty)
	return s
}

func (s *session) threadID() string { return *s.sid.Load() }

func (s *session) conn() *conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == nil {
		return nil
	}
	return s.proc.conn
}

// emit queues an event for in-order delivery to the sink.
func (s *session) emit(ev agentsession.Event) {
	ev.SessionID = s.threadID()
	s.events.push(func() { s.sink(ev) })
}

// barrier closes ch once every event queued before it has been delivered.
func (s *session) barrier(ch chan struct{}) {
	if !s.events.push(func() { close(ch) }) {
		close(ch)
	}
}

// handshake runs initialize / initialized and then thread/start, or
// thread/resume when resumeID names a stored thread.
func (s *session) handshake(ctx context.Context, opts Options, resumeID string) (agentsession.SessionInfo, error) {
	c := s.conn()
	init := initializeParams{
		ClientInfo:   clientInfo{Name: orDefault(opts.ClientName, "flint"), Title: "Flint", Version: orDefault(opts.ClientVersion, "0.0.0")},
		Capabilities: initializeCapabilities{OptOutNotificationMethods: noisyNotifications},
	}
	if err := c.call(ctx, methodInitialize, init, nil); err != nil {
		return agentsession.SessionInfo{}, err
	}
	if err := c.notify(methodInitialized, nil); err != nil {
		return agentsession.SessionInfo{}, err
	}
	approval := orDefault(opts.ApprovalPolicy, DefaultApprovalPolicy)
	sandbox := orDefault(opts.Sandbox, DefaultSandbox)
	method := methodThreadStart
	var params any = threadStartParams{Cwd: s.cwd, Model: opts.Model, ApprovalPolicy: approval, Sandbox: sandbox}
	if resumeID != "" {
		method = methodThreadResume
		params = threadResumeParams{ThreadID: resumeID, Cwd: s.cwd, Model: opts.Model, ApprovalPolicy: approval, Sandbox: sandbox, ExcludeTurns: true}
	}
	var res threadStartResponse
	if err := c.call(ctx, method, params, &res); err != nil {
		return agentsession.SessionInfo{}, err
	}
	if res.Thread.ID == "" {
		return agentsession.SessionInfo{}, fmt.Errorf("codex: %s returned no thread id", method)
	}
	id := res.Thread.ID
	s.sid.Store(&id)
	return agentsession.SessionInfo{ID: id, Agent: "codex", Model: res.Model, Cwd: s.cwd}, nil
}

// prompt runs one turn and blocks until it ends.
func (s *session) prompt(ctx context.Context, text string, atts []agentsession.Attachment) (string, error) {
	t, err := s.beginTurn()
	if err != nil {
		return "", err
	}
	params := turnStartParams{ThreadID: s.threadID(), Input: buildInput(text, atts)}
	var res turnStartResponse
	callCtx, cancel := context.WithTimeout(context.Background(), turnStartTimeout)
	err = s.conn().call(callCtx, methodTurnStart, params, &res)
	cancel()
	if err != nil {
		s.finishTurn(t, agentsession.StopError, err, err.Error())
		<-t.done
		return t.stop, t.err
	}
	s.setTurnID(t, res.Turn.ID)
	select {
	case <-t.done:
	case <-ctx.Done():
		s.cancel()
		select {
		case <-t.done:
		case <-time.After(cancelGrace):
			s.finishTurn(t, agentsession.StopCancelled, nil, "")
			<-t.done
		}
	}
	return t.stop, t.err
}

func (s *session) beginTurn() (*turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.exited && s.exitErr != nil:
		return nil, s.exitErr
	case s.exited || s.proc == nil:
		return nil, ErrClosed
	case s.turn != nil:
		return nil, errTurnActive
	}
	t := &turn{done: make(chan struct{}), usageBase: s.usage}
	s.turn = t
	s.items = map[string]*itemState{}
	return t, nil
}

// setTurnID records the turn id and sends a deferred interrupt if Cancel
// ran before the id was known.
func (s *session) setTurnID(t *turn, id string) {
	s.mu.Lock()
	if t.id == "" {
		t.id = id
	}
	send := t.cancelRequested && !t.interruptSent && !t.ended && t.id != ""
	if send {
		t.interruptSent = true
	}
	s.mu.Unlock()
	if send {
		s.interrupt(t.id)
	}
}

func (s *session) interrupt(turnID string) {
	if c := s.conn(); c != nil {
		_ = c.fire(methodTurnInterrupt, turnInterruptParams{ThreadID: s.threadID(), TurnID: turnID})
	}
}

// cancel interrupts the running turn and withdraws its pending approvals.
func (s *session) cancel() {
	s.mu.Lock()
	t := s.turn
	if t == nil {
		s.mu.Unlock()
		return
	}
	t.cancelRequested = true
	pending := s.takePending()
	send := t.id != "" && !t.interruptSent
	if send {
		t.interruptSent = true
	}
	s.mu.Unlock()

	c := s.conn()
	for _, p := range pending {
		if c != nil {
			_ = c.reply(p.id, p.onCancel)
		}
		s.emit(agentsession.Event{Kind: agentsession.KindPermissionSettled, RequestID: p.key})
	}
	if send {
		s.interrupt(t.id)
	}
}

// finishTurn ends t exactly once: it withdraws pending approvals, closes
// open tool calls, reports usage and the stop reason, then releases Prompt.
func (s *session) finishTurn(t *turn, stop string, err error, errText string) {
	s.mu.Lock()
	if t.ended {
		s.mu.Unlock()
		return
	}
	t.ended, t.stop, t.err = true, stop, err
	if s.turn == t {
		s.turn = nil
	}
	pending := s.takePending()
	open := s.openToolCalls(stop)
	s.items = map[string]*itemState{}
	usage := s.turnUsage(t)
	if errText == t.lastError {
		errText = "" // already reported by an "error" notification
	}
	s.mu.Unlock()

	for _, p := range pending {
		s.emit(agentsession.Event{Kind: agentsession.KindPermissionSettled, RequestID: p.key})
	}
	for _, tc := range open {
		s.emit(agentsession.Event{Kind: agentsession.KindToolCallUpdate, ToolCall: tc})
	}
	if errText != "" {
		s.emit(agentsession.Event{Kind: agentsession.KindError, Text: errText})
	}
	if usage != nil {
		s.emit(agentsession.Event{Kind: agentsession.KindUsage, Usage: usage})
	}
	s.emit(agentsession.Event{Kind: agentsession.KindTurnEnd, StopReason: stop})
	s.barrier(t.done)
}

// openToolCalls returns final updates for announced, unfinished tool calls.
// Caller holds s.mu.
func (s *session) openToolCalls(stop string) []*agentsession.ToolCall {
	status := agentsession.StatusFailed
	if stop == agentsession.StopEndTurn {
		status = agentsession.StatusCompleted
	}
	var out []*agentsession.ToolCall
	for _, st := range s.items {
		if st.announce {
			out = append(out, &agentsession.ToolCall{ID: st.call.ID, Status: status})
		}
	}
	return out
}

// turnUsage is the token delta of the turn. Caller holds s.mu.
func (s *session) turnUsage(t *turn) *agentsession.Usage {
	if !t.usageSeen {
		return nil
	}
	return &agentsession.Usage{
		InputTokens:  s.usage.InputTokens - t.usageBase.InputTokens,
		OutputTokens: s.usage.OutputTokens - t.usageBase.OutputTokens,
	}
}

// takePending removes and returns every pending request. Caller holds s.mu.
func (s *session) takePending() []*pendingRequest {
	out := make([]*pendingRequest, 0, len(s.pending))
	for _, p := range s.pending {
		out = append(out, p)
	}
	s.pending = map[string]*pendingRequest{}
	return out
}

// onExit runs after the app-server exited and every message was dispatched.
func (s *session) onExit(err error) {
	s.mu.Lock()
	s.exited, s.exitErr = true, err
	t := s.turn
	s.mu.Unlock()
	closed := errors.Is(err, ErrClosed)
	switch {
	case t != nil && closed:
		s.finishTurn(t, agentsession.StopCancelled, nil, "")
	case t != nil:
		s.finishTurn(t, agentsession.StopError, err, err.Error())
	default:
		s.mu.Lock()
		pending := s.takePending()
		s.mu.Unlock()
		for _, p := range pending {
			s.emit(agentsession.Event{Kind: agentsession.KindPermissionSettled, RequestID: p.key})
		}
		if !closed {
			s.emit(agentsession.Event{Kind: agentsession.KindError, Text: err.Error()})
		}
	}
	s.events.close()
}

// close stops the app-server and waits until every event was delivered.
func (s *session) close() {
	s.mu.Lock()
	p := s.proc
	s.mu.Unlock()
	if p != nil {
		p.Close()
	} else {
		s.events.close()
	}
	<-s.events.done
}

// respond answers a pending approval with one of its option ids.
func (s *session) respond(requestID, optionID string) error {
	s.mu.Lock()
	p := s.pending[requestID]
	if p == nil {
		s.mu.Unlock()
		return fmt.Errorf("codex: no pending permission request %q", requestID)
	}
	result, ok := p.answer(optionID)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("codex: unknown option %q for request %q", optionID, requestID)
	}
	delete(s.pending, requestID)
	s.mu.Unlock()

	var err error
	if c := s.conn(); c != nil {
		err = c.reply(p.id, result)
	}
	s.emit(agentsession.Event{Kind: agentsession.KindPermissionSettled, RequestID: p.key})
	if err != nil {
		return fmt.Errorf("codex: answer request %q: %w", requestID, err)
	}
	return nil
}

// decode unmarshals params, reporting success.
func decode(params json.RawMessage, v any) bool {
	return len(params) > 0 && json.Unmarshal(params, v) == nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
