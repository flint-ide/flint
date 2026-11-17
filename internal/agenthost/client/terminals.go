package client

import (
	"flint/internal/agenthost"
	"flint/internal/terminal"
)

// terminals implements sessions.Terminals over the connection.
type terminals struct{ c *Client }

func (t terminals) Start(id string, o terminal.Options) error {
	err := t.c.callT(agenthost.MethodTermStart, agenthost.TermStartParams{
		ID: id, Cwd: o.Cwd, Cols: o.Cols, Rows: o.Rows, Shell: o.Shell, Args: o.Args, Env: o.Env,
	}, nil)
	if err != nil {
		return err
	}
	t.c.mu.Lock()
	t.c.terms[id] = struct{}{}
	t.c.mu.Unlock()
	return t.c.run(func() {
		// Output may already have created the state; only a left-over of a
		// finished shell under the same id is replaced (onTermData sees that
		// too, when the new output comes first).
		if st := t.c.tstate[id]; st == nil || st.exited {
			t.c.tstate[id] = &termState{}
		}
	})
}

func (t terminals) Write(id string, data []byte) error {
	// The host takes at most MaxWrite per call; a bigger paste goes in pieces.
	for {
		n := min(len(data), agenthost.MaxWrite)
		if err := t.c.callT(agenthost.MethodTermWrite, agenthost.TermWriteParams{ID: id, Data: data[:n]}, nil); err != nil {
			return err
		}
		if data = data[n:]; len(data) == 0 {
			return nil
		}
	}
}

func (t terminals) Resize(id string, cols, rows int) error {
	return t.c.callT(agenthost.MethodTermResize, agenthost.TermResizeParams{ID: id, Cols: cols, Rows: rows}, nil)
}

func (t terminals) Cwd(id string) (string, error) {
	var res agenthost.TermCwdResult
	err := t.c.callT(agenthost.MethodTermCwd, agenthost.IDParams{ID: id}, &res)
	return res.Cwd, err
}

func (t terminals) Resume(id string) (terminal.Resume, error) {
	var res terminal.Resume
	err := t.c.callT(agenthost.MethodTermResume, agenthost.IDParams{ID: id}, &res)
	return res, err
}

// Close hangs up the shell. Like the in-process manager, the output and the
// exit that follow are still reported.
func (t terminals) Close(id string) {
	_ = t.c.callT(agenthost.MethodTermClose, agenthost.IDParams{ID: id}, nil)
	t.c.mu.Lock()
	delete(t.c.terms, id)
	t.c.mu.Unlock()
	_ = t.c.run(func() {
		if st := t.c.tstate[id]; st != nil {
			st.closing = true
			t.c.forgetIfDone(id)
		}
	})
}

// CloseAll closes the terminals this client started or attached; other
// windows' terminals on the same host are theirs to close.
func (t terminals) CloseAll() {
	t.c.mu.Lock()
	ids := make([]string, 0, len(t.c.terms))
	for id := range t.c.terms {
		ids = append(ids, id)
	}
	t.c.mu.Unlock()
	for _, id := range ids {
		t.Close(id)
	}
}
