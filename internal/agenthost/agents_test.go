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
	text := strings.Repeat("x", 1<<20)
	for i := 1; i <= 20; i++ {
		r.agents.emit(as.Event{SessionID: "c", Seq: uint64(i), Kind: as.KindMessageChunk, Text: text})
	}
	c := r.srv.chatFor("c", false)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ring.bytes > ChatReplayBytes || len(c.ring.items) >= 20 {
		t.Errorf("ring holds %d events, %d bytes", len(c.ring.items), c.ring.bytes)
	}
}

func TestOversizedEventIsReplacedKeepingItsSeq(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
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

func TestChatCountIsBounded(t *testing.T) {
	r := newRig(t, nil)
	for i := range MaxSessions + 10 {
		r.agents.emit(as.Event{SessionID: fmt.Sprint("c", i), Seq: 1, Kind: as.KindError})
	}
	if got := len(r.srv.chatMap); got != MaxSessions {
		t.Errorf("%d chats kept, want %d", got, MaxSessions)
	}
	// A stopped chat that nobody follows makes room for a new one.
	p := r.connect(t)
	if err := p.call(t, MethodAgentStop, IDParams{ID: "c0"}, nil); err != nil {
		t.Fatal(err)
	}
	r.agents.emit(as.Event{SessionID: "new", Seq: 1, Kind: as.KindError})
	if r.srv.chatFor("new", false) == nil || r.srv.chatFor("c0", false) != nil {
		t.Error("the stopped chat was not evicted for the new one")
	}
}

func TestUsageEventsAreKeptUntilAcknowledged(t *testing.T) {
	r := newRig(t, nil)
	p := r.connect(t)
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
