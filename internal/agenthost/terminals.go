package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"flint/internal/jsonrpc"
	"flint/internal/terminal"
)

// termEntry is the host's record of one terminal: its replay ring and who
// listens. It outlives the shell until the window closes the tab (term.close),
// so a window that attaches late still learns how the shell ended.
type termEntry struct {
	mu      sync.Mutex
	ring    *ring
	seq     uint64
	subs    map[*client]struct{}
	cols    int
	rows    int
	exited  bool
	closing bool // term.close was called; the entry goes once the shell is reported gone
	code    int
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
	for cl := range e.subs {
		if !cl.push(method, params) {
			delete(e.subs, cl)
		}
	}
}

func (s *Server) term(id string) *termEntry {
	s.termsMu.Lock()
	defer s.termsMu.Unlock()
	return s.termMap[id]
}

// removeTerm deletes the entry if it is still e (the id may have been reused).
func (s *Server) removeTerm(id string, e *termEntry) {
	s.termsMu.Lock()
	if s.termMap[id] == e {
		delete(s.termMap, id)
	}
	s.termsMu.Unlock()
}

// termEvents is what the terminal manager reports to; it runs on the PTY pump,
// so everything here is quick and never waits for a client.
type termEvents struct{ s *Server }

func (ev termEvents) Output(id string, data []byte) {
	e := ev.s.term(id)
	if e == nil {
		return
	}
	for len(data) > 0 {
		n := min(len(data), maxChunk)
		piece := append([]byte(nil), data[:n]...)
		data = data[n:]
		e.mu.Lock()
		e.seq++
		e.ring.add(e.seq, piece)
		e.broadcast(NotifyTermData, notification(TermDataParams{ID: id, Seq: e.seq, Data: piece}))
		e.mu.Unlock()
	}
}

func (ev termEvents) Exited(id string, code int) {
	e := ev.s.term(id)
	if e == nil {
		return
	}
	e.mu.Lock()
	e.exited, e.code = true, code
	e.broadcast(NotifyTermExit, notification(TermExitParams{ID: id, Code: code, Seq: e.seq}))
	gone := e.closing
	e.mu.Unlock()
	if gone {
		ev.s.removeTerm(id, e)
	}
}

func (ev termEvents) Title(id string, info terminal.Info) {
	e := ev.s.term(id)
	if e == nil {
		return
	}
	e.mu.Lock()
	e.broadcast(NotifyTermTitle, notification(TermTitleParams{ID: id, Command: info.Command, Cwd: info.Cwd}))
	e.mu.Unlock()
}

func (s *Server) termStart(_ context.Context, cl *client, raw json.RawMessage) (any, error) {
	var p TermStartParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(p.validate()); err != nil {
		return nil, err
	}
	if s.terms == nil {
		return nil, unavailable("terminals")
	}
	e := &termEntry{
		ring: newRing(s.cfg.ReplayKiB<<10, 0), subs: map[*client]struct{}{cl: {}},
		cols: orDefault(p.Cols, 80), rows: orDefault(p.Rows, 24),
	}
	// The entry exists before the shell does: its first output may arrive
	// before Start returns.
	s.termsMu.Lock()
	old := s.termMap[p.ID]
	switch {
	case old != nil && !old.isExited():
		s.termsMu.Unlock()
		return nil, fmt.Errorf("terminal %q already running", p.ID)
	case old == nil && len(s.termMap) >= MaxSessions:
		s.termsMu.Unlock()
		return nil, fmt.Errorf("the host keeps at most %d terminals; close some", MaxSessions)
	}
	s.termMap[p.ID] = e
	s.termsMu.Unlock()

	if err := s.terms.Start(p.ID, p.Options()); err != nil {
		s.removeTerm(p.ID, e)
		return nil, err
	}
	return struct{}{}, nil
}

func (e *termEntry) isExited() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exited
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// termCall validates the id and the terminal part, for the plain forwards.
func (s *Server) termCall(raw json.RawMessage, p interface{ id() string }) error {
	if err := decode(raw, p); err != nil {
		return err
	}
	if err := invalid(checkID(p.id())); err != nil {
		return err
	}
	if s.terms == nil {
		return unavailable("terminals")
	}
	return nil
}

func (p *IDParams) id() string          { return p.ID }
func (p *TermWriteParams) id() string   { return p.ID }
func (p *TermResizeParams) id() string  { return p.ID }
func (p *AttachParams) id() string      { return p.ID }
func (p *AgentPromptParams) id() string { return p.ID }

func (s *Server) termWrite(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p TermWriteParams
	if err := s.termCall(raw, &p); err != nil {
		return nil, err
	}
	if len(p.Data) > MaxWrite {
		return nil, invalid(fmt.Errorf("write of %d bytes is over the limit of %d", len(p.Data), MaxWrite))
	}
	return struct{}{}, s.terms.Write(p.ID, p.Data)
}

func (s *Server) termResize(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p TermResizeParams
	if err := s.termCall(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(checkSize(p.Cols, p.Rows)); err != nil {
		return nil, err
	}
	if err := s.terms.Resize(p.ID, p.Cols, p.Rows); err != nil {
		return nil, err
	}
	if e := s.term(p.ID); e != nil {
		e.mu.Lock()
		e.cols, e.rows = orDefault(p.Cols, 80), orDefault(p.Rows, 24)
		e.mu.Unlock()
	}
	return struct{}{}, nil
}

func (s *Server) termCwd(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p IDParams
	if err := s.termCall(raw, &p); err != nil {
		return nil, err
	}
	cwd, err := s.terms.Cwd(p.ID)
	if err != nil {
		return nil, err
	}
	return TermCwdResult{Cwd: cwd}, nil
}

func (s *Server) termResume(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p IDParams
	if err := s.termCall(raw, &p); err != nil {
		return nil, err
	}
	r, err := s.terms.Resume(p.ID)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Server) termClose(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p IDParams
	if err := s.termCall(raw, &p); err != nil {
		return nil, err
	}
	// The entry stays until the shell is reported gone, so subscribers still
	// get the output and the exit that follow a hang-up, as they would from
	// an in-process manager.
	if e := s.term(p.ID); e != nil {
		e.mu.Lock()
		e.closing = true
		gone := e.exited
		e.mu.Unlock()
		if gone {
			s.removeTerm(p.ID, e)
		}
	}
	s.terms.Close(p.ID)
	return struct{}{}, nil
}

func (s *Server) termAttach(_ context.Context, cl *client, raw json.RawMessage) (any, error) {
	var p AttachParams
	if err := s.termCall(raw, &p); err != nil {
		return nil, err
	}
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
	cols, rows, running := e.cols, e.rows, !e.exited
	e.mu.Unlock()

	if reset && running {
		// Programs that draw the whole screen (editors, agents' TUIs) cannot
		// be rebuilt from the tail of their output; a size change makes them
		// redraw, and the redraw arrives as live output.
		_ = s.terms.Resize(p.ID, cols, rows+1)
		_ = s.terms.Resize(p.ID, cols, rows)
	}
	return res, nil
}
