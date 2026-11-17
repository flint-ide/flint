package agenthost

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	as "flint/internal/agentsession"
)

func startChat(t *testing.T, p *peer) as.SessionInfo {
	t.Helper()
	var info as.SessionInfo
	if err := p.call(t, MethodAgentStart, AgentStartParams{Agent: "fake", Cwd: "/w"}, &info); err != nil {
		t.Fatal(err)
	}
	return info
}

// resumeChat registers a chat under a chosen id, as the host does for a
// conversation that is reopened.
func resumeChat(t *testing.T, p *peer, id string) {
	t.Helper()
	if err := p.call(t, MethodAgentResume, AgentResumeParams{Agent: "fake", Cwd: "/w", Session: id}, nil); err != nil {
		t.Fatal(err)
	}
}

// registerQuietly registers a chat and lets its listener go, for tests that
// emit more than a listener's queue holds.
func registerQuietly(t *testing.T, r *rig, id string) {
	t.Helper()
	before := r.srv.Clients()
	p := r.connect(t)
	resumeChat(t, p, id)
	p.conn.Close(nil)
	_ = p.raw.Close()
	waitFor(t, "the registering client to be forgotten", func() bool { return r.srv.Clients() == before })
}

func TestAgentCallsAreForwarded(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	info := startChat(t, p)
	if info.ID != "chat-1" || info.Cwd != "/w" {
		t.Errorf("info = %+v", info)
	}
	var busy BusyResult
	if err := p.call(t, MethodAgentBusy, IDParams{ID: info.ID}, &busy); err != nil || !busy.Busy {
		t.Errorf("busy = %+v, %v", busy, err)
	}
	want := as.Policy{AutoApproveAll: true}
	if err := p.call(t, MethodAgentSetPolicy, want, nil); err != nil {
		t.Fatal(err)
	}
	var got as.Policy
	if err := p.call(t, MethodAgentPolicy, nil, &got); err != nil || got != want {
		t.Errorf("policy = %+v, %v", got, err)
	}
	var list AgentsResult
	if err := p.call(t, MethodAgentAgents, nil, &list); err != nil || len(list.Agents) != 1 {
		t.Errorf("agents = %+v, %v", list, err)
	}
	if err := p.call(t, MethodAgentStop, IDParams{ID: info.ID}, nil); err != nil || len(r.agents.stops) != 1 {
		t.Errorf("stop: %v %v", err, r.agents.stops)
	}
}

func TestAgentEventsReachTheStarterAndCanBeReplayed(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	info := startChat(t, p)
	r.agents.emit(as.Event{SessionID: info.ID, Seq: 1, Kind: as.KindMessageChunk, Text: "hi"})
	r.agents.emit(as.Event{SessionID: info.ID, Seq: 2, Kind: as.KindTurnEnd})

	n := p.next(t)
	var e as.Event
	if n.method != NotifyAgentEvent || json.Unmarshal(n.params, &e) != nil || e.Seq != 1 || e.Text != "hi" {
		t.Fatalf("notification = %s %s", n.method, n.params)
	}
	later := r.connect(t)
	var res AgentAttachResult
	if err := later.call(t, MethodAgentAttach, AttachParams{ID: info.ID, AfterSeq: 1}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Reset || len(res.Events) != 1 || res.Last != 2 {
		t.Fatalf("attach = %+v", res)
	}
	r.agents.emit(as.Event{SessionID: info.ID, Seq: 3, Kind: as.KindTurnEnd})
	if n := later.next(t); n.method != NotifyAgentEvent {
		t.Errorf("the attached client got %s", n.method)
	}
	if err := later.call(t, MethodAgentAttach, AttachParams{ID: "unknown"}, nil); err == nil {
		t.Error("attach to an unknown chat succeeded")
	}
}

func TestChatRingKeepsTheLatest2000Events(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	registerQuietly(t, r, "c")
	for i := 1; i <= 2500; i++ {
		r.agents.emit(as.Event{SessionID: "c", Seq: uint64(i), Kind: as.KindMessageChunk, Text: "x"})
	}
	var res AgentAttachResult
	if err := p.call(t, MethodAgentAttach, AttachParams{ID: "c", AfterSeq: 10}, &res); err != nil {
		t.Fatal(err)
	}
	var first as.Event
	_ = json.Unmarshal(res.Events[0], &first)
	if !res.Reset || len(res.Events) != ChatReplayEvents || first.Seq != 501 || res.Last != 2500 {
		t.Errorf("attach: reset=%v events=%d first=%d last=%d", res.Reset, len(res.Events), first.Seq, res.Last)
	}
}

func TestChatRingIsBoundedInBytes(t *testing.T) {
	r := newRig(t, nil)
	resumeChat(t, r.connect(t), "c")
	text := strings.Repeat("x", 1<<20)
	for i := 1; i <= 20; i++ {
		r.agents.emit(as.Event{SessionID: "c", Seq: uint64(i), Kind: as.KindMessageChunk, Text: text})
	}
	c := r.srv.chatFor("c")
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ring.bytes > ChatReplayBytes || len(c.ring.items) >= 20 {
		t.Errorf("ring holds %d events, %d bytes", len(c.ring.items), c.ring.bytes)
	}
}

func TestOversizedEventIsReplacedKeepingItsSeq(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	resumeChat(t, p, "c")
	r.agents.emit(as.Event{SessionID: "c", Seq: 1, Kind: as.KindToolCallUpdate, Text: strings.Repeat("x", maxEvent+1)})
	var res AgentAttachResult
	if err := p.call(t, MethodAgentAttach, AttachParams{ID: "c"}, &res); err != nil || len(res.Events) != 1 {
		t.Fatalf("attach = %+v, %v", res, err)
	}
	var e as.Event
	_ = json.Unmarshal(res.Events[0], &e)
	if e.Seq != 1 || e.Kind != as.KindError {
		t.Errorf("event = %+v", e)
	}
}

func TestEventsWithoutASessionAreDropped(t *testing.T) {
	r := newRig(t, nil)
	r.agents.emit(as.Event{Seq: 1, Kind: as.KindError})
	if len(r.srv.chatMap) != 0 {
		t.Error("an event without a session created a chat")
	}
}

// A record exists only for a session that registered: an event cannot make
// one, or it would count toward the cap without anyone able to stop it.
func TestEventsForAnUnknownSessionAreDropped(t *testing.T) {
	r := newRig(t, nil)
	for i := range MaxSessions + 10 {
		r.agents.emit(as.Event{SessionID: fmt.Sprint("ghost", i), Seq: 1, Kind: as.KindError})
	}
	r.agents.emit(as.Event{SessionID: "ghost", Seq: 1, Kind: as.KindUsage, Usage: &as.Usage{}})
	if got := len(r.srv.chatMap); got != 0 {
		t.Errorf("%d chats were made by events", got)
	}
	if backlog, _ := r.srv.usage.after(0, pageBudget); len(backlog) != 0 {
		t.Error("a usage event of an unknown session was kept")
	}
}

func TestLateEventAfterEvictionDoesNotRecreateTheChat(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	info := startChat(t, p)
	if err := p.call(t, MethodAgentStop, IDParams{ID: info.ID}, nil); err != nil {
		t.Fatal(err)
	}
	p.conn.Close(nil)
	_ = p.raw.Close()
	waitFor(t, "the client to be forgotten", func() bool { return r.srv.Clients() == 0 })
	// Fill the table so that the stopped chat is evicted for a new one.
	q := r.connect(t)
	for i := 1; i < MaxSessions; i++ {
		resumeChat(t, q, fmt.Sprint("c", i))
	}
	resumeChat(t, q, "evictor")
	if r.srv.chatFor(info.ID) != nil {
		t.Fatal("the stopped chat was not evicted")
	}
	r.agents.emit(as.Event{SessionID: info.ID, Seq: 99, Kind: as.KindMessageChunk, Text: "late"})
	if r.srv.chatFor(info.ID) != nil || len(r.srv.chatMap) != MaxSessions {
		t.Errorf("a late event recreated the chat (%d records)", len(r.srv.chatMap))
	}
}

// A driver may report the new session's events before Start returns; they are
// held for the call and belong to the record that register makes.
func TestEventsEmittedWhileAChatStartsAreNotLost(t *testing.T) {
	r := newRig(t, nil)
	r.agents.beforeStart = func(info as.SessionInfo) {
		r.agents.emit(as.Event{SessionID: info.ID, Seq: 1, Kind: as.KindMessageChunk, Text: "early"})
		r.agents.emit(as.Event{SessionID: info.ID, Seq: 2, Kind: as.KindUsage, Usage: &as.Usage{}})
	}
	p := r.connect(t)
	info := startChat(t, p)
	var res AgentAttachResult
	if err := p.call(t, MethodAgentAttach, AttachParams{ID: info.ID}, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 2 || res.Last != 2 {
		t.Fatalf("attach = %d events, last %d; want the two early ones", len(res.Events), res.Last)
	}
	if backlog, _ := r.srv.usage.after(0, pageBudget); len(backlog) != 1 {
		t.Errorf("%d usage events kept, want the early one", len(backlog))
	}
}

func TestEventsHeldDuringAStartAreDroppedWhenItEnds(t *testing.T) {
	r := newRig(t, nil)
	r.agents.beforeStart = func(info as.SessionInfo) {
		r.agents.emit(as.Event{SessionID: "someone-else", Seq: 1, Kind: as.KindError})
	}
	p := r.connect(t)
	startChat(t, p)
	r.agents.beforeStart = nil
	if r.srv.chatFor("someone-else") != nil {
		t.Error("an event for another id made a record")
	}
	r.srv.chatsMu.Lock()
	held := len(r.srv.pending)
	r.srv.chatsMu.Unlock()
	if held != 0 {
		t.Errorf("%d ids still held after the start ended", held)
	}
	// Later registration of that id must not resurrect the dropped event.
	resumeChat(t, p, "someone-else")
	var res AgentAttachResult
	if err := p.call(t, MethodAgentAttach, AttachParams{ID: "someone-else"}, &res); err != nil || len(res.Events) != 0 {
		t.Errorf("attach = %+v, %v", res, err)
	}
}

func TestHeldEventsAreBounded(t *testing.T) {
	r := newRig(t, nil)
	r.agents.beforeStart = func(as.SessionInfo) {
		for i := range 3 * MaxPendingEvents {
			r.agents.emit(as.Event{SessionID: fmt.Sprint("x", i%50), Seq: uint64(i + 1), Kind: as.KindMessageChunk, Text: "x"})
		}
	}
	p := r.connect(t)
	startChat(t, p)
	r.srv.chatsMu.Lock()
	defer r.srv.chatsMu.Unlock()
	if len(r.srv.pending) != 0 || r.srv.pendingEvents != 0 || r.srv.pendingBytes != 0 {
		t.Errorf("pending = %d ids, %d events, %d bytes after the start", len(r.srv.pending), r.srv.pendingEvents, r.srv.pendingBytes)
	}
}

func TestChatCountIsBounded(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	for i := range MaxSessions {
		resumeChat(t, p, fmt.Sprint("c", i))
	}
	if got := len(r.srv.chatMap); got != MaxSessions {
		t.Fatalf("%d chats kept, want %d", got, MaxSessions)
	}
	// One more is refused, and its process stopped rather than left unseen.
	if err := p.call(t, MethodAgentResume, AgentResumeParams{Agent: "fake", Cwd: "/w", Session: "over"}, nil); err == nil {
		t.Error("a chat over the limit was kept")
	}
	if r.agents.stops[len(r.agents.stops)-1] != "over" {
		t.Errorf("stops = %v", r.agents.stops)
	}
	// A stopped chat that nobody follows makes room for a new one.
	if err := p.call(t, MethodAgentStop, IDParams{ID: "c0"}, nil); err != nil {
		t.Fatal(err)
	}
	p.conn.Close(nil)
	_ = p.raw.Close()
	waitFor(t, "the client to be forgotten", func() bool { return r.srv.Clients() == 0 })
	resumeChat(t, r.connect(t), "new")
	if r.srv.chatFor("new") == nil || r.srv.chatFor("c0") != nil {
		t.Error("the stopped chat was not evicted for the new one")
	}
}

func TestUsageEventsAreKeptUntilAcknowledged(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	registerQuietly(t, r, "c")
	const sent = MaxUsageBacklog + 50
	for i := 1; i <= sent; i++ {
		r.agents.emit(as.Event{SessionID: "c", Seq: uint64(i), Kind: as.KindUsage, Usage: &as.Usage{InputTokens: i}})
	}
	backlog := func(after uint64) (seqs []uint64, pages int) {
		for {
			var res UsageBacklogResult
			if err := p.call(t, MethodUsageBacklog, UsageBacklogParams{After: after}, &res); err != nil {
				t.Fatal(err)
			}
			pages++
			for _, raw := range res.Events {
				var m EventMsg
				if err := json.Unmarshal(raw, &m); err != nil {
					t.Fatal(err)
				}
				seqs = append(seqs, m.UsageSeq)
				after = m.UsageSeq
			}
			if !res.More {
				return seqs, pages
			}
		}
	}
	seqs, pages := backlog(0)
	if len(seqs) != MaxUsageBacklog || seqs[0] != 51 || seqs[len(seqs)-1] != sent {
		t.Fatalf("backlog has %d events from %d to %d, want the newest %d", len(seqs), seqs[0], seqs[len(seqs)-1], MaxUsageBacklog)
	}
	if pages < 2 {
		t.Errorf("%d events came in %d page", len(seqs), pages)
	}
	if err := p.call(t, MethodUsageAck, UsageAckParams{UpTo: sent - 10}, nil); err != nil {
		t.Fatal(err)
	}
	if seqs, _ := backlog(0); len(seqs) != 10 || seqs[0] != sent-9 {
		t.Errorf("after the ack %d events remain (from %d)", len(seqs), seqs[0])
	}
}

func TestUsageBacklogIsBoundedInBytesAndReportsWhatItDropped(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	registerQuietly(t, r, "c")
	text := strings.Repeat("x", 1<<20)
	const sent = 12
	for i := 1; i <= sent; i++ {
		r.agents.emit(as.Event{SessionID: "c", Seq: uint64(i), Kind: as.KindUsage, Usage: &as.Usage{}, Text: text})
	}
	var kept int
	var dropped uint64
	var after uint64
	for {
		var res UsageBacklogResult
		if err := p.call(t, MethodUsageBacklog, UsageBacklogParams{After: after}, &res); err != nil {
			t.Fatal(err)
		}
		dropped = res.Dropped
		for _, raw := range res.Events {
			kept++
			var m EventMsg
			_ = json.Unmarshal(raw, &m)
			after = m.UsageSeq
		}
		if !res.More {
			break
		}
	}
	if kept*len(text) > MaxUsageBacklogBytes || kept < 6 {
		t.Errorf("%d events of %d bytes kept; the cap is %d bytes", kept, len(text), MaxUsageBacklogBytes)
	}
	if dropped != uint64(sent-kept) {
		t.Errorf("dropped = %d, want %d", dropped, sent-kept)
	}
}

func TestUsageBacklogReportsEventsDroppedByTheCountCap(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	registerQuietly(t, r, "c")
	for i := 1; i <= MaxUsageBacklog+7; i++ {
		r.agents.emit(as.Event{SessionID: "c", Seq: uint64(i), Kind: as.KindUsage, Usage: &as.Usage{}})
	}
	var res UsageBacklogResult
	if err := p.call(t, MethodUsageBacklog, UsageBacklogParams{After: MaxUsageBacklog + 6}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Dropped != 7 {
		t.Errorf("dropped = %d, want 7", res.Dropped)
	}
}

// usage.ack is global: it is the host's backlog, not one window's.
func TestUsageAckFromOneClientClearsTheBacklogForAll(t *testing.T) {
	r := newRig(t, nil)
	a, b := r.connect(t), r.connect(t)
	resumeChat(t, a, "c")
	r.agents.emit(as.Event{SessionID: "c", Seq: 1, Kind: as.KindUsage, Usage: &as.Usage{}})
	if err := a.call(t, MethodUsageAck, UsageAckParams{UpTo: 1}, nil); err != nil {
		t.Fatal(err)
	}
	var res UsageBacklogResult
	if err := b.call(t, MethodUsageBacklog, UsageBacklogParams{}, &res); err != nil || len(res.Events) != 0 {
		t.Errorf("backlog after another window's ack = %+v, %v", res, err)
	}
}

func TestUsageEventsCarryTheirNumberInTheChatStream(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
	info := startChat(t, p)
	r.agents.emit(as.Event{SessionID: info.ID, Seq: 1, Kind: as.KindUsage, Usage: &as.Usage{}})
	var m EventMsg
	if n := p.next(t); json.Unmarshal(n.params, &m) != nil || m.UsageSeq != 1 || m.Kind != as.KindUsage {
		t.Errorf("event = %+v", m)
	}
}
