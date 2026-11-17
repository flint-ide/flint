package client

import (
	"encoding/json"
	"errors"
	"fmt"

	"flint/internal/agenthost"
	as "flint/internal/agentsession"
	"flint/internal/jsonrpc"
	"flint/internal/terminal"
)

// resetScreen (RIS) clears a terminal emulator completely: after a reset
// replay the old screen must not mix with the host's buffer.
var resetScreen = []byte("\x1bc")

type termState struct {
	last    uint64 // seq of the last chunk delivered
	exited  bool   // Exited was delivered
	closing bool   // Close was called; the state goes once the shell is reported gone
}

type chatState struct{ last uint64 }

// onNotify runs on the connection's reader: it only queues. When the queue is
// full the notification is dropped and everything is re-attached afterwards;
// the sequence numbers make that safe.
func (c *Client) onNotify(method string, params json.RawMessage) {
	job := c.notifyJob(method, params)
	if job == nil {
		return
	}
	select {
	case c.jobs <- job:
	default:
		c.resync.Store(true)
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
}

func (c *Client) notifyJob(method string, params json.RawMessage) func() {
	switch method {
	case agenthost.NotifyTermData:
		var p agenthost.TermDataParams
		if json.Unmarshal(params, &p) == nil {
			return func() { c.onTermData(p) }
		}
	case agenthost.NotifyTermExit:
		var p agenthost.TermExitParams
		if json.Unmarshal(params, &p) == nil {
			return func() { c.onTermExit(p) }
		}
	case agenthost.NotifyTermTitle:
		var p agenthost.TermTitleParams
		if json.Unmarshal(params, &p) == nil && c.h.Terminal != nil {
			return func() { c.h.Terminal.Title(p.ID, terminal.Info{Command: p.Command, Cwd: p.Cwd}) }
		}
	case agenthost.NotifyAgentEvent:
		var e as.Event
		if json.Unmarshal(params, &e) == nil {
			return func() { c.onAgentEvent(e) }
		}
	}
	return nil // unknown notifications of a newer host are ignored
}

// run queues work for the delivery goroutine, behind what is already queued.
// It never blocks the caller for long, so a callback may call the client.
func (c *Client) run(f func()) error {
	select {
	case <-c.conn.Done():
		return fmt.Errorf("%w: %w", ErrDisconnected, c.conn.Err())
	case c.jobs <- f:
	default:
		go func() {
			select {
			case c.jobs <- f:
			case <-c.conn.Done():
			}
		}()
	}
	return nil
}

// deliver is the one goroutine that calls the handlers and owns the state.
func (c *Client) deliver() {
	defer close(c.delivered)
	for {
		select {
		case f := <-c.jobs:
			f()
		case <-c.wake:
			if c.resync.Swap(false) {
				c.syncAll()
			}
		case <-c.conn.Done():
			return
		}
	}
}

func (c *Client) syncAll() {
	for id := range c.tstate {
		c.syncTerm(id)
	}
	for id := range c.cstate {
		c.syncChat(id)
	}
}

func (c *Client) output(id string, data []byte) {
	if c.h.Terminal != nil {
		c.h.Terminal.Output(id, data)
	}
}

func (c *Client) onTermData(p agenthost.TermDataParams) {
	st := c.tstate[p.ID]
	if st != nil && (st.exited || st.closing && st.last >= 1) && p.Seq == 1 {
		st = nil // the id was reused for a new shell (a closed one need not have exited)
	}
	if st == nil {
		st = &termState{}
		c.tstate[p.ID] = st
	}
	switch {
	case p.Seq <= st.last:
		// Already delivered: replayed by an attach that raced the live stream.
	case p.Seq == st.last+1:
		st.last = p.Seq
		c.output(p.ID, p.Data)
	default:
		c.syncTerm(p.ID)
	}
}

func (c *Client) onTermExit(p agenthost.TermExitParams) {
	st := c.tstate[p.ID]
	if st != nil && p.Seq > st.last {
		c.syncTerm(p.ID) // the output before the exit first
		st = c.tstate[p.ID]
	}
	if st != nil && st.exited {
		return // the catch-up already reported it
	}
	if st != nil {
		st.exited = true
	}
	if c.h.Terminal != nil {
		c.h.Terminal.Exited(p.ID, p.Code)
	}
	c.forgetIfDone(p.ID)
}

// forgetIfDone drops a terminal's state once its shell ended and its tab closed.
func (c *Client) forgetIfDone(id string) {
	if st := c.tstate[id]; st != nil && st.exited && st.closing {
		delete(c.tstate, id)
	}
}

// syncTerm asks the host for everything after the last chunk delivered and
// delivers it, page by page.
func (c *Client) syncTerm(id string) {
	st := c.tstate[id]
	if st == nil {
		return
	}
	for range maxPages {
		var res agenthost.TermAttachResult
		err := c.callT(agenthost.MethodTermAttach, agenthost.AttachParams{ID: id, AfterSeq: st.last}, &res)
		if err != nil {
			var rpcErr *jsonrpc.Error
			if errors.As(err, &rpcErr) {
				delete(c.tstate, id) // the host does not know it (any more)
			}
			return
		}
		if res.Reset {
			if st.last > 0 {
				c.output(id, resetScreen)
			}
			if c.h.OnReset != nil {
				c.h.OnReset("terminal", id)
			}
			st.last = res.Last
			if len(res.Chunks) > 0 {
				st.last = res.Chunks[0].Seq - 1
			}
		}
		for _, ch := range res.Chunks {
			if ch.Seq == st.last+1 {
				st.last = ch.Seq
				c.output(id, ch.Data)
			}
		}
		if res.More {
			continue
		}
		// Titles carry no sequence number: this is how a lost one is recovered.
		if res.Title != nil && c.h.Terminal != nil {
			c.h.Terminal.Title(id, terminal.Info{Command: res.Title.Command, Cwd: res.Title.Cwd})
		}
		if res.Exited && !st.exited {
			st.exited = true
			if c.h.Terminal != nil {
				c.h.Terminal.Exited(id, res.Code)
			}
			c.forgetIfDone(id)
		}
		return
	}
}

func (c *Client) onAgentEvent(e as.Event) {
	st := c.cstate[e.SessionID]
	if st == nil {
		st = &chatState{}
		c.cstate[e.SessionID] = st
	}
	switch {
	case e.Seq <= st.last:
	case e.Seq == st.last+1:
		st.last = e.Seq
		c.emit(e)
	default:
		c.syncChat(e.SessionID)
	}
}

func (c *Client) emit(e as.Event) {
	if c.h.Agent != nil {
		c.h.Agent(e)
	}
}

// syncChat is syncTerm for a chat.
func (c *Client) syncChat(id string) {
	st := c.cstate[id]
	if st == nil {
		return
	}
	for range maxPages {
		var res agenthost.AgentAttachResult
		err := c.callT(agenthost.MethodAgentAttach, agenthost.AttachParams{ID: id, AfterSeq: st.last}, &res)
		if err != nil {
			var rpcErr *jsonrpc.Error
			if errors.As(err, &rpcErr) {
				delete(c.cstate, id)
			}
			return
		}
		events := make([]as.Event, 0, len(res.Events))
		for _, raw := range res.Events {
			var e as.Event
			if json.Unmarshal(raw, &e) == nil {
				events = append(events, e)
			}
		}
		if res.Reset {
			if c.h.OnReset != nil {
				c.h.OnReset("chat", id)
			}
			st.last = res.Last
			if len(events) > 0 {
				st.last = events[0].Seq - 1
			}
		}
		for _, e := range events {
			if e.Seq == st.last+1 {
				st.last = e.Seq
				c.emit(e)
			}
		}
		if !res.More {
			return
		}
	}
}
