package agenthost

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	as "flint/internal/agentsession"
	"flint/internal/jsonrpc"
)

// chat is the host's record of one agent conversation: the ring of its latest
// events (as the JSON sent to clients) and who listens.
type chat struct {
	mu      sync.Mutex
	ring    *ring
	subs    map[*client]struct{}
	stopped bool // the session ended; kept so a window can still read it, evicted first
}

func newChat() *chat {
	return &chat{ring: newRing(ChatReplayBytes, ChatReplayEvents), subs: map[*client]struct{}{}}
}

func (c *chat) unsubscribe(cl *client) {
	c.mu.Lock()
	delete(c.subs, cl)
	c.mu.Unlock()
}

// chatFor returns the record of id, or nil when the host has none. A record
// is made only by register, never by an event: an event of a session that
// never registered, or one that was evicted, would otherwise count toward the
// cap with nobody able to stop it.
func (s *Server) chatFor(id string) *chat {
	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()
	return s.chatMap[id]
}

// openChat returns the record of a chat that registers: the existing one, or
// a new one when there is room (at the limit a stopped chat nobody listens to
// makes way; otherwise nil says no). A new record is returned locked, with
// the events that were held for it while its start was in flight, so the
// caller can add them before any live event does.
func (s *Server) openChat(id string) (c *chat, held []as.Event, created bool) {
	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()
	if c := s.chatMap[id]; c != nil {
		return c, nil, false
	}
	if len(s.chatMap) >= MaxSessions {
		evicted := false
		for k, old := range s.chatMap {
			old.mu.Lock()
			free := old.stopped && len(old.subs) == 0
			old.mu.Unlock()
			if free {
				delete(s.chatMap, k)
				evicted = true
				break
			}
		}
		if !evicted {
			return nil, nil, false
		}
	}
	c = newChat()
	c.mu.Lock()
	s.chatMap[id] = c
	held = s.pending[id]
	s.dropPendingLocked(id)
	return c, held, true
}

// beginStart and endStart bracket an agent.start or agent.resume. While one
// is in flight, events of sessions that have no record yet are held rather
// than dropped: a driver may report the new session's first events before
// Start returns its id, and register adds them to the record it makes. When
// the last start ends what is still held belonged to nobody and is dropped.
func (s *Server) beginStart() {
	s.chatsMu.Lock()
	s.starting++
	s.chatsMu.Unlock()
}

func (s *Server) endStart() {
	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()
	if s.starting--; s.starting == 0 {
		s.pending = map[string][]as.Event{}
		s.pendingEvents, s.pendingBytes = 0, 0
	}
}

func (s *Server) dropPendingLocked(id string) {
	for _, e := range s.pending[id] {
		s.pendingEvents--
		s.pendingBytes -= eventSize(e)
	}
	delete(s.pending, id)
}

// eventSize is what a held event counts for: the text it carries and a flat
// allowance for the rest.
func eventSize(e as.Event) int { return len(e.Text) + 512 }

// route finds where an event goes: the record of its session, or nowhere. An
// event of a session that has no record yet is held while a start is in
// flight, within bounds, and dropped otherwise.
func (s *Server) route(e as.Event) *chat {
	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()
	if c := s.chatMap[e.SessionID]; c != nil {
		return c
	}
	size := eventSize(e)
	if s.starting > 0 && s.pendingEvents < MaxPendingEvents && s.pendingBytes+size <= maxPendingBytes {
		s.pending[e.SessionID] = append(s.pending[e.SessionID], e)
		s.pendingEvents++
		s.pendingBytes += size
	}
	return nil
}

// emitAgent receives the agent manager's events, already numbered per chat.
func (s *Server) emitAgent(e as.Event) {
	if e.SessionID == "" {
		return
	}
	c := s.route(e)
	if c == nil {
		return
	}
	c.mu.Lock()
	s.deliverLocked(c, e)
	c.mu.Unlock()
}

// deliverLocked records an event in its chat and sends it to the chat's
// listeners. c.mu is held, which keeps events in order per chat.
func (s *Server) deliverLocked(c *chat, e as.Event) {
	msg := EventMsg{Event: e}
	if e.Kind == as.KindUsage {
		msg.UsageSeq = s.usage.reserve()
	}
	data, err := json.Marshal(msg)
	if err != nil || len(data) > maxEvent {
		// Same seq, so clients see no gap; the driver's detail is lost, which
		// beats a connection that cannot carry it.
		size := len(data)
		data, _ = json.Marshal(EventMsg{UsageSeq: msg.UsageSeq, Event: as.Event{
			SessionID: e.SessionID, Seq: e.Seq, Kind: as.KindError,
			Text: fmt.Sprintf("an event of %d bytes was too large to forward", size),
		}})
	}
	if msg.UsageSeq != 0 {
		s.usage.put(msg.UsageSeq, data)
	}
	c.ring.add(e.Seq, data)
	for cl := range c.subs {
		if !cl.push(NotifyAgentEvent, data) {
			delete(c.subs, cl)
		}
	}
}

// register makes cl a listener of the chat that just started, creating its
// record: the only place one is made.
func (s *Server) register(cl *client, id string) error {
	c, held, created := s.openChat(id)
	if c == nil {
		return fmt.Errorf("the host keeps at most %d chats; stop some", MaxSessions)
	}
	if !created {
		c.mu.Lock()
	}
	for _, e := range held {
		s.deliverLocked(c, e)
	}
	c.stopped = false
	c.subs[cl] = struct{}{}
	c.mu.Unlock()
	s.changed()
	return nil
}

// registered subscribes cl to a chat that just opened; when the host has no
// room to keep it, the chat is stopped rather than left running unseen.
func (s *Server) registered(cl *client, info as.SessionInfo) (any, error) {
	if err := s.register(cl, info.ID); err != nil {
		_ = s.agents.Stop(info.ID)
		return nil, err
	}
	return info, nil
}

func (s *Server) agentCall(raw json.RawMessage, p any) error {
	if err := decode(raw, p); err != nil {
		return err
	}
	if s.agents == nil {
		return unavailable("agents")
	}
	return nil
}

func (s *Server) agentAgents(_ context.Context, _ *client, _ json.RawMessage) (any, error) {
	if s.agents == nil {
		return nil, unavailable("agents")
	}
	return AgentsResult{Agents: s.agents.Agents()}, nil
}

func (s *Server) agentStart(ctx context.Context, cl *client, raw json.RawMessage) (any, error) {
	var p AgentStartParams
	if err := s.agentCall(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(firstErr(checkText("agent", p.Agent, MaxID), checkText("cwd", p.Cwd, MaxPath))); err != nil {
		return nil, err
	}
	s.beginStart()
	defer s.endStart()
	info, err := s.agents.Start(ctx, p.Agent, p.Cwd)
	if err != nil {
		return nil, err
	}
	return s.registered(cl, info)
}

func (s *Server) agentResume(ctx context.Context, cl *client, raw json.RawMessage) (any, error) {
	var p AgentResumeParams
	if err := s.agentCall(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(firstErr(checkText("agent", p.Agent, MaxID), checkText("cwd", p.Cwd, MaxPath), checkText("session", p.Session, MaxID))); err != nil {
		return nil, err
	}
	s.beginStart()
	defer s.endStart()
	info, err := s.agents.Resume(ctx, p.Agent, p.Cwd, p.Session)
	if err != nil {
		return nil, err
	}
	return s.registered(cl, info)
}

func (s *Server) agentPrompt(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p AgentPromptParams
	if err := s.agentCall(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(firstErr(checkID(p.ID), checkText("text", p.Text, MaxText))); err != nil {
		return nil, err
	}
	if len(p.Attachments) > MaxArgs {
		return nil, invalid(fmt.Errorf("more than %d attachments", MaxArgs))
	}
	return struct{}{}, s.agents.Prompt(p.ID, p.Text, p.Attachments)
}

func (s *Server) agentBusy(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p IDParams
	if err := s.agentCall(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(checkID(p.ID)); err != nil {
		return nil, err
	}
	return BusyResult{Busy: s.agents.Busy(p.ID)}, nil
}

func (s *Server) agentCancel(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p IDParams
	if err := s.agentCall(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(checkID(p.ID)); err != nil {
		return nil, err
	}
	return struct{}{}, s.agents.Cancel(p.ID)
}

func (s *Server) agentRespond(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p AgentRespondParams
	if err := s.agentCall(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(firstErr(checkID(p.ID), checkText("request", p.Request, MaxArg), checkText("option", p.Option, MaxArg))); err != nil {
		return nil, err
	}
	return struct{}{}, s.agents.Respond(p.ID, p.Request, p.Option)
}

func (s *Server) agentStop(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p IDParams
	if err := s.agentCall(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(checkID(p.ID)); err != nil {
		return nil, err
	}
	err := s.agents.Stop(p.ID)
	if c := s.chatFor(p.ID); c != nil {
		c.mu.Lock()
		c.stopped = true
		c.mu.Unlock()
		s.changed()
	}
	return struct{}{}, err
}

func (s *Server) agentPolicy(_ context.Context, _ *client, _ json.RawMessage) (any, error) {
	if s.agents == nil {
		return nil, unavailable("agents")
	}
	return s.agents.Policy(), nil
}

func (s *Server) agentSetPolicy(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p as.Policy
	if err := s.agentCall(raw, &p); err != nil {
		return nil, err
	}
	s.agents.SetPolicy(p)
	return struct{}{}, nil
}

func (s *Server) agentAttach(_ context.Context, cl *client, raw json.RawMessage) (any, error) {
	var p AttachParams
	if err := s.agentCall(raw, &p); err != nil {
		return nil, err
	}
	if err := invalid(checkID(p.ID)); err != nil {
		return nil, err
	}
	c := s.chatFor(p.ID)
	if c == nil {
		return nil, jsonrpc.Errorf(jsonrpc.CodeInvalidParams, "unknown session %q", p.ID)
	}
	c.mu.Lock() // snapshot and subscription together, as for terminals
	items, reset, more := c.ring.after(p.AfterSeq, pageBudget)
	c.subs[cl] = struct{}{}
	res := AgentAttachResult{Reset: reset, More: more, Last: c.ring.last(), Events: make([]json.RawMessage, len(items))}
	for i, it := range items {
		res.Events[i] = it.data
	}
	c.mu.Unlock()
	return res, nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// usageStore keeps usage events until a client acknowledges them, so cost
// accounting survives a window that was closed while agents kept working. It
// is separate from the chat rings because those forget old events.
//
// It is the host's, not a window's: usage.ack from any window clears events
// for all of them. Its size is bounded in events and in bytes; what the bounds
// push out is counted in dropped, so the loss is visible and not silent.
type usageStore struct {
	mu       sync.Mutex
	max      int
	maxBytes int
	seq      uint64
	items    []item
	bytes    int
	dropped  uint64
}

// reserve numbers the next usage event.
func (u *usageStore) reserve() uint64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.seq++
	return u.seq
}

// put stores the event's JSON under its number, dropping (and counting) the
// oldest events over either cap. The newest event is always kept.
func (u *usageStore) put(seq uint64, data []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.items = append(u.items, item{seq: seq, data: data})
	u.bytes += len(data)
	over, freed := 0, 0
	for len(u.items)-over > 1 && (len(u.items)-over > u.max || u.bytes-freed > u.maxBytes) {
		freed += len(u.items[over].data)
		over++
	}
	if over > 0 {
		u.items = append(u.items[:0], u.items[over:]...)
		u.bytes -= freed
		u.dropped += uint64(over)
	}
}

// ack forgets events up to and including upTo.
func (u *usageStore) ack(upTo uint64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	keep := 0
	for keep < len(u.items) && u.items[keep].seq <= upTo {
		u.bytes -= len(u.items[keep].data)
		keep++
	}
	u.items = append(u.items[:0], u.items[keep:]...)
}

// droppedCount is how many events the caps have pushed out so far.
func (u *usageStore) droppedCount() uint64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.dropped
}

// after returns unacknowledged events newer than seq, up to the page budget.
func (u *usageStore) after(seq uint64, budget int) (out []item, more bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	size := 0
	for _, it := range u.items {
		if it.seq <= seq {
			continue
		}
		if len(out) > 0 && size+len(it.data) > budget {
			return out, true
		}
		size += len(it.data)
		out = append(out, it)
	}
	return out, false
}

func (s *Server) usageAck(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p UsageAckParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	s.usage.ack(p.UpTo)
	return struct{}{}, nil
}

func (s *Server) usageBacklog(_ context.Context, _ *client, raw json.RawMessage) (any, error) {
	var p UsageBacklogParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	items, more := s.usage.after(p.After, pageBudget)
	res := UsageBacklogResult{More: more, Events: make([]json.RawMessage, len(items)), Dropped: s.usage.droppedCount()}
	for i, it := range items {
		res.Events[i] = it.data
	}
	return res, nil
}
