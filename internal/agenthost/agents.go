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

// chatFor returns the record of id, creating it when there is room: at the
// limit a stopped chat nobody listens to makes way, and otherwise nil says no.
func (s *Server) chatFor(id string, create bool) *chat {
	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()
	if c := s.chatMap[id]; c != nil {
		return c
	}
	if !create {
		return nil
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
			return nil
		}
	}
	c := newChat()
	s.chatMap[id] = c
	return c
}

// emitAgent receives the agent manager's events, already numbered per chat.
func (s *Server) emitAgent(e as.Event) {
	if e.SessionID == "" {
		return
	}
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
	c := s.chatFor(e.SessionID, true)
	if c == nil {
		return
	}
	c.mu.Lock()
	c.ring.add(e.Seq, data)
	for cl := range c.subs {
		if !cl.push(NotifyAgentEvent, data) {
			delete(c.subs, cl)
		}
	}
	c.mu.Unlock()
}

// register makes cl a listener of the chat that just started.
func (s *Server) register(cl *client, id string) error {
	c := s.chatFor(id, true)
	if c == nil {
		return fmt.Errorf("the host keeps at most %d chats; stop some", MaxSessions)
	}
	c.mu.Lock()
	c.stopped = false
	c.subs[cl] = struct{}{}
	c.mu.Unlock()
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
	if c := s.chatFor(p.ID, false); c != nil {
		c.mu.Lock()
		c.stopped = true
		c.mu.Unlock()
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
	c := s.chatFor(p.ID, false)
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
type usageStore struct {
	mu    sync.Mutex
	max   int
	seq   uint64
	items []item
}

// reserve numbers the next usage event.
func (u *usageStore) reserve() uint64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.seq++
	return u.seq
}

// put stores the event's JSON under its number, dropping the oldest events
// over the cap.
func (u *usageStore) put(seq uint64, data []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.items = append(u.items, item{seq: seq, data: data})
	if over := len(u.items) - u.max; over > 0 {
		u.items = append(u.items[:0], u.items[over:]...)
	}
}

// ack forgets events up to and including upTo.
func (u *usageStore) ack(upTo uint64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	keep := 0
	for keep < len(u.items) && u.items[keep].seq <= upTo {
		keep++
	}
	u.items = append(u.items[:0], u.items[keep:]...)
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
	res := UsageBacklogResult{More: more, Events: make([]json.RawMessage, len(items))}
	for i, it := range items {
		res.Events[i] = it.data
	}
	return res, nil
}
