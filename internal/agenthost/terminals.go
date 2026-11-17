package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"flint/internal/jsonrpc"
	"flint/internal/terminal"
)

// termEntry is the host's record of one terminal: its replay ring and who
// listens. It outlives the shell until the window closes the tab (term.close),
// so a window that attaches late still learns how the shell ended.
//
// The terminal manager knows the shell by key, not by the id on the wire: the
// key is the id until the id is reused while an older shell is still leaving
// (a shell that ignores SIGHUP), and then a distinct one, so that the old
// shell's output and exit cannot be taken for the new terminal's.
type termEntry struct {
	id  string
	key string

	// closed: term.close was called. The id is free to be started again, the
	// entry no longer counts as a session, and it goes once its shell is
	// reported gone. superseded: a newer entry took the id, so what this
	// shell still reports is of no interest to clients (on the wire it would
	// be indistinguishable from the new terminal's).
	closed     atomic.Bool
	superseded atomic.Bool

	mu     sync.Mutex
	ring   *ring
	seq    uint64
	subs   map[*client]struct{}
	cols   int
	rows   int
	exited bool
	code   int
	title  *TermInfo // the latest term.title, for attach
}

func (e *termEntry) subscribe(cl *client) {
	e.subs[cl] = struct{}{}
}

func (e *termEntry) unsubscribe(cl *client) {
	e.mu.Lock()
	delete(e.subs, cl)
	e.mu.Unlock()
}

// broadcast queues a notification for every subscriber; slow ones are dropped.
// Called with e.mu held.
func (e *termEntry) broadcast(method string, params json.RawMessage) {
	if e.superseded.Load() {
		return
	}
	for cl := range e.subs {
		if !cl.push(method, params) {
			delete(e.subs, cl)
		}
	}
}

func (e *termEntry) isExited() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exited
}

// term is the current entry of an id on the wire, closed or not.
func (s *Server) term(id string) *termEntry {
	s.termsMu.Lock()
	defer s.termsMu.Unlock()
	return s.termMap[id]
}

// liveTerm is the entry of id if it can still take calls.
func (s *Server) liveTerm(id string) *termEntry {
	if e := s.term(id); e != nil && !e.closed.Load() {
		return e
	}
	return nil
}

// byKey finds the entry a manager event is about.
func (s *Server) byKey(key string) *termEntry {
	s.termsMu.Lock()
	defer s.termsMu.Unlock()
	return s.termByKey[key]
}

// dropTerm forgets e: its key, and its id when e is still the id's current
// entry (the id may have been reused).
func (s *Server) dropTerm(e *termEntry) {
	s.termsMu.Lock()
	if s.termByKey[e.key] == e {
		delete(s.termByKey, e.key)
	}
	removed := s.termMap[e.id] == e
	if removed {
		delete(s.termMap, e.id)
	}
	s.termsMu.Unlock()
	if removed {
		s.changed()
	}
}

// termEvents is what the terminal manager reports to; it runs on the PTY pump,
// so everything here is quick and never waits for a client. Its ids are the
// entries' keys.
type termEvents struct{ s *Server }

func (ev termEvents) Output(key string, data []byte) {
	e := ev.s.byKey(key)
	if e == nil || e.superseded.Load() {
		return
	}
	for len(data) > 0 {
		n := min(len(data), maxChunk)
		piece := append([]byte(nil), data[:n]...)
		data = data[n:]
		e.mu.Lock()
		e.seq++
		e.ring.add(e.seq, piece)
		e.broadcast(NotifyTermData, notification(TermDataParams{ID: e.id, Seq: e.seq, Data: piece}))
		e.mu.Unlock()
	}
}

func (ev termEvents) Exited(key string, code int) {
	e := ev.s.byKey(key)
	if e == nil {
		return
	}
	e.mu.Lock()
	e.exited, e.code = true, code
	e.broadcast(NotifyTermExit, notification(TermExitParams{ID: e.id, Code: code, Seq: e.seq}))
	e.mu.Unlock()
	if e.closed.Load() || e.superseded.Load() {
		ev.s.dropTerm(e)
	}
	ev.s.changed() // an exited shell no longer keeps the host running
}

func (ev termEvents) Title(key string, info terminal.Info) {
	e := ev.s.byKey(key)
	if e == nil || e.superseded.Load() {
		return
	}
	e.mu.Lock()
	e.title = &TermInfo{Command: info.Command, Cwd: info.Cwd}
	e.broadcast(NotifyTermTitle, notification(TermTitleParams{ID: e.id, Command: info.Command, Cwd: info.Cwd}))
	e.mu.Unlock()
}

// checkStart is what term.start may ask for over the wire: no more than the
// web view's TerminalAPI.Start can (a working folder, a size and validated
// variables). The shell is the host's to choose, as it is there.
func (s *Server) checkStart(p TermStartParams) error {
	if err := p.validate(); err != nil {
		return err
	}
	if !s.cfg.AllowShell && (p.Shell != "" || len(p.Args) > 0) {
		return fmt.Errorf("a shell and its arguments cannot be chosen over the wire")
	}
	for _, entry := range p.Env {
		if err := terminal.CheckEnvEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func (ev termEvents) AgentState(key, state string) {
	e := ev.s.byKey(key)
	if e == nil || e.superseded.Load() {
		return
	}
	e.mu.Lock()
	e.broadcast(NotifyTermAgent, notification(TermAgentParams{ID: e.id, State: state}))
	e.mu.Unlock()
}

func (s *Server) termStart(_ context.Context, cl *client, raw json.RawMessage) (any, error) {
	var p TermStartParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(s.checkStart(p)); err != nil {
		return nil, err
	}
	if s.terms == nil {
		return nil, unavailable("terminals")
	}
	if p.Cwd == "" { // a remote window sends none: the shell starts in the host user's home
		if home, err := os.UserHomeDir(); err == nil {
			p.Cwd = home
		}
	}
	e := &termEntry{
		id: p.ID, ring: newRing(int(s.replayKiB.Load())<<10, 0), subs: map[*client]struct{}{cl: {}},
		cols: orDefault(p.Cols, 80), rows: orDefault(p.Rows, 24),
	}
	// The entry exists before the shell does: its first output may arrive
	// before Start returns.
	s.termsMu.Lock()
	if err := s.admitLocked(p.ID); err != nil {
		s.termsMu.Unlock()
		return nil, err
	}
	if old := s.termMap[p.ID]; old != nil {
		old.superseded.Store(true)
		if old.isExited() {
			delete(s.termByKey, old.key) // nothing more will come from it
		}
	}
	e.key = p.ID
	if s.termByKey[e.key] != nil { // an older shell of this id is still leaving
		s.termGen++
		e.key = fmt.Sprintf("%s\x00%d", p.ID, s.termGen) // ids have no control characters, so this cannot clash
	}
	s.termMap[p.ID] = e
	s.termByKey[e.key] = e
	s.termsMu.Unlock()
	s.changed()

	if err := s.terms.Start(e.key, p.Options()); err != nil {
		s.dropTerm(e)
		return nil, err
	}
	return struct{}{}, nil
}

// admitLocked decides whether id may start; termsMu is held. A running
// terminal under the id is refused; a closed one may be replaced at once.
func (s *Server) admitLocked(id string) error {
	old := s.termMap[id]
	if old != nil && !old.closed.Load() && !old.isExited() {
		return fmt.Errorf("terminal %q already running", id)
	}
	live, leaving := 0, 0
	for _, e := range s.termByKey {
		if e.closed.Load() {
			leaving++
		}
	}
	for _, e := range s.termMap {
		if !e.closed.Load() {
			live++
		}
	}
	if (old == nil || old.closed.Load()) && live >= MaxSessions {
		return fmt.Errorf("the host keeps at most %d terminals; close some", MaxSessions)
	}
	if old != nil && old.closed.Load() && leaving >= MaxSessions {
		return fmt.Errorf("%d closed terminals are still shutting down; try again in a moment", leaving)
	}
	return nil
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// termCall validates the id and finds the terminal that takes the call, for
// the plain forwards.
func (s *Server) termCall(raw json.RawMessage, p interface{ id() string }) (*termEntry, error) {
	if err := decode(raw, p); err != nil {
		return nil, err
	}
	if err := invalid(checkID(p.id())); err != nil {
		return nil, err
	}
	if s.terms == nil {
		return nil, unavailable("terminals")
	}
	e := s.liveTerm(p.id())
	if e == nil {
		return nil, fmt.Errorf("terminal %q not running", p.id())
	}
	return e, nil
}

func (p *IDParams) id() string          { return p.ID }
func (p *TermWriteParams) id() string   { return p.ID }
func (p *TermResizeParams) id() string  { return p.ID }
func (p *AttachParams) id() string      { return p.ID }
func (p *AgentPromptParams) id() string { return p.ID }

func (s *Server) termWrite(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p TermWriteParams
	e, err := s.termCall(raw, &p)
	if err != nil {
		return nil, err
	}
	if len(p.Data) > MaxWrite {
		return nil, invalid(fmt.Errorf("write of %d bytes is over the limit of %d", len(p.Data), MaxWrite))
	}
	return struct{}{}, s.terms.Write(e.key, p.Data)
}

func (s *Server) termResize(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p TermResizeParams
	e, err := s.termCall(raw, &p)
	if err != nil {
		return nil, err
	}
	if err := invalid(checkSize(p.Cols, p.Rows)); err != nil {
		return nil, err
	}
	if err := s.terms.Resize(e.key, p.Cols, p.Rows); err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.cols, e.rows = orDefault(p.Cols, 80), orDefault(p.Rows, 24)
	e.mu.Unlock()
	return struct{}{}, nil
}

func (s *Server) termCwd(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p IDParams
	e, err := s.termCall(raw, &p)
	if err != nil {
		return nil, err
	}
	cwd, err := s.terms.Cwd(e.key)
	if err != nil {
		return nil, err
	}
	return TermCwdResult{Cwd: cwd}, nil
}

func (s *Server) termResume(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p IDParams
	e, err := s.termCall(raw, &p)
	if err != nil {
		return nil, err
	}
	r, err := s.terms.Resume(e.key)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Server) termClose(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p IDParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(checkID(p.ID)); err != nil {
		return nil, err
	}
	if s.terms == nil {
		return nil, unavailable("terminals")
	}
	// Closing an unknown id, or one already closed, is not an error. Closed,
	// the id can be started again at once; the entry stays until the shell is
	// reported gone, so subscribers still get the output and the exit that
	// follow a hang-up, as they would from an in-process manager.
	e := s.liveTerm(p.ID)
	if e == nil {
		return struct{}{}, nil
	}
	e.mu.Lock()
	e.closed.Store(true)
	gone := e.exited
	e.mu.Unlock()
	if gone {
		s.dropTerm(e)
	}
	s.terms.Close(e.key)
	s.changed() // a closed terminal no longer keeps the host running
	return struct{}{}, nil
}

// termList answers what hello listed, as it is now: a window that restores its
// tabs asks after it connected, and an exited shell is worth telling apart
// from a running one.
func (s *Server) termList(context.Context, *client, json.RawMessage) (any, error) {
	if s.terms == nil {
		return nil, unavailable("terminals")
	}
	s.termsMu.Lock()
	entries := make([]*termEntry, 0, len(s.termMap))
	for _, e := range s.termMap {
		if !e.closed.Load() {
			entries = append(entries, e)
		}
	}
	s.termsMu.Unlock()
	out := make([]TermStatus, len(entries))
	for i, e := range entries {
		out[i] = TermStatus{ID: e.id, Exited: e.isExited()}
	}
	slices.SortFunc(out, func(a, b TermStatus) int { return strings.Compare(a.ID, b.ID) })
	return TermListResult{Terminals: out}, nil
}

func (s *Server) termAttach(_ context.Context, cl *client, raw json.RawMessage) (any, error) {
	var p AttachParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(checkID(p.ID)); err != nil {
		return nil, err
	}
	if s.terms == nil {
		return nil, unavailable("terminals")
	}
	// A closed terminal can still be attached until its shell is reported
	// gone: that is how a window learns how it ended.
	e := s.term(p.ID)
	if e == nil {
		return nil, jsonrpc.Errorf(jsonrpc.CodeInvalidParams, "terminal %q not running", p.ID)
	}
	// Snapshot and subscription happen under one lock, so output that comes
	// after the snapshot is queued for this client and none is lost between.
	e.mu.Lock()
	items, reset, more := e.ring.after(p.AfterSeq, pageBudget)
	e.subscribe(cl)
	res := TermAttachResult{Reset: reset, More: more, Last: e.ring.last(), Chunks: make([]Chunk, len(items))}
	for i, it := range items {
		res.Chunks[i] = Chunk{Seq: it.seq, Data: it.data}
	}
	if !more && e.exited {
		res.Exited, res.Code = true, e.code
	}
	if e.title != nil {
		title := *e.title
		res.Title = &title
	}
	cols, rows, running := e.cols, e.rows, !e.exited && !e.closed.Load()
	key := e.key
	e.mu.Unlock()

	if reset && running {
		// Programs that draw the whole screen (editors, agents' TUIs) cannot
		// be rebuilt from the tail of their output; a size change makes them
		// redraw, and the redraw arrives as live output.
		_ = s.terms.Resize(key, cols, rows+1)
		_ = s.terms.Resize(key, cols, rows)
	}
	return res, nil
}
