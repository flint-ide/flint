package agentsession

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// open starts a chat with id on a fake agent of its own.
func open(t *testing.T, m *Manager, id string) {
	t.Helper()
	f := newFake("")
	f.id = id
	m.Register("fake-"+id, f)
	if _, err := m.Start(context.Background(), "fake-"+id, "/p"); err != nil {
		t.Fatal(err)
	}
}

func emitN(m *Manager, id string, n int, text string) {
	for range n {
		m.emit(Event{SessionID: id, Kind: KindMessageChunk, Text: text})
	}
}

func TestEventLogIsOffByDefault(t *testing.T) {
	m := NewManager(func(Event) {})
	open(t, m, "s1")
	emitN(m, "s1", 3, "x")
	events, gap := m.Replay("s1", 0)
	if len(events) != 0 {
		t.Fatalf("replay without a log = %d events", len(events))
	}
	if gap == nil || gap.From != 1 || gap.To != 3 {
		t.Fatalf("gap = %+v, want 1–3 (nothing kept)", gap)
	}
}

func TestEventLogReplaysAfterSeq(t *testing.T) {
	m := NewManager(func(Event) {})
	m.SetLogLimits(LogLimits{Events: 100, Bytes: 1 << 20})
	open(t, m, "s1")
	open(t, m, "s2")
	emitN(m, "s1", 5, "hello")
	emitN(m, "s2", 2, "other")
	events, gap := m.Replay("s1", 2)
	if gap != nil {
		t.Fatalf("unexpected gap %+v", gap)
	}
	if len(events) != 3 || events[0].Seq != 3 || events[2].Seq != 5 {
		t.Fatalf("replay = %+v", events)
	}
	if events, _ := m.Replay("s1", 5); len(events) != 0 {
		t.Fatalf("replay after the last seq = %d events", len(events))
	}
	if events, gap := m.Replay("nope", 0); len(events) != 0 || gap != nil {
		t.Fatalf("unknown session: %d events, gap %+v", len(events), gap)
	}
	// A client that claims to be ahead of the session gets nothing, no gap.
	if events, gap := m.Replay("s2", 99); len(events) != 0 || gap != nil {
		t.Fatalf("ahead: %d events, gap %+v", len(events), gap)
	}
}

func TestEventLogDropsOldestByCountAndReportsTheGap(t *testing.T) {
	m := NewManager(func(Event) {})
	m.SetLogLimits(LogLimits{Events: 3, Bytes: 1 << 20})
	open(t, m, "s1")
	emitN(m, "s1", 10, "x")
	events, gap := m.Replay("s1", 0)
	if len(events) != 3 || events[0].Seq != 8 {
		t.Fatalf("kept %+v", events)
	}
	if gap == nil || gap.SessionID != "s1" || gap.From != 1 || gap.To != 7 {
		t.Fatalf("gap = %+v, want 1–7", gap)
	}
	// Starting inside the kept window: no gap.
	if _, gap := m.Replay("s1", 7); gap != nil {
		t.Fatalf("gap from 7 = %+v", gap)
	}
	if _, gap := m.Replay("s1", 5); gap == nil || gap.From != 6 || gap.To != 7 {
		t.Fatalf("gap from 5 = %+v", gap)
	}
}

func TestEventLogDropsOldestByBytes(t *testing.T) {
	m := NewManager(func(Event) {})
	m.SetLogLimits(LogLimits{Events: 1000, Bytes: 10_000})
	open(t, m, "s1")
	big := strings.Repeat("a", 3000)
	emitN(m, "s1", 10, big)
	events, gap := m.Replay("s1", 0)
	if len(events) == 0 || len(events) > 3 {
		t.Fatalf("kept %d events of 3000 bytes in a 10000-byte log", len(events))
	}
	if gap == nil || gap.To != events[0].Seq-1 {
		t.Fatalf("gap = %+v", gap)
	}
	// One event bigger than the whole log is still kept (the newest always is).
	emitN(m, "s1", 1, strings.Repeat("b", 50_000))
	if events, _ := m.Replay("s1", 0); len(events) != 1 || events[0].Seq != 11 {
		t.Fatalf("after a huge event: %d events", len(events))
	}
}

func TestEventLogLimitsChangeAndOff(t *testing.T) {
	m := NewManager(func(Event) {})
	m.SetLogLimits(LogLimits{Events: 100, Bytes: 1 << 20})
	open(t, m, "s1")
	emitN(m, "s1", 10, "x")
	m.SetLogLimits(LogLimits{Events: 4, Bytes: 1 << 20})
	if events, _ := m.Replay("s1", 0); len(events) != 4 {
		t.Fatalf("shrunk log keeps %d", len(events))
	}
	m.SetLogLimits(LogLimits{})
	if events, gap := m.Replay("s1", 0); len(events) != 0 || gap == nil || gap.To != 10 {
		t.Fatalf("log off: %d events, gap %+v", len(events), gap)
	}
	emitN(m, "s1", 1, "x")
	if events, _ := m.Replay("s1", 0); len(events) != 0 {
		t.Fatal("a switched-off log must not record")
	}
	// Switched on again: only what comes next is kept, the rest is a gap.
	m.SetLogLimits(LogLimits{Events: 100, Bytes: 1 << 20})
	emitN(m, "s1", 1, "x")
	if events, gap := m.Replay("s1", 0); len(events) != 1 || events[0].Seq != 12 || gap == nil || gap.To != 11 {
		t.Fatalf("on again: %+v, gap %+v", events, gap)
	}
}

func TestEventLogIsForgottenWhenTheSessionStops(t *testing.T) {
	m := NewManager(func(Event) {})
	m.SetLogLimits(LogLimits{Events: 100, Bytes: 1 << 20})
	fake := newFake("")
	m.Register("fake", fake)
	info, err := m.Start(context.Background(), "fake", "/p")
	if err != nil {
		t.Fatal(err)
	}
	emitN(m, info.ID, 2, "x")
	if events, _ := m.Replay(info.ID, 0); len(events) != 2 {
		t.Fatalf("kept %d", len(events))
	}
	if err := m.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	if events, gap := m.Replay(info.ID, 0); len(events) != 0 || gap != nil {
		t.Fatalf("stopped session: %d events, gap %+v", len(events), gap)
	}
}

func TestSessionsReportStateAndHookFires(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	var mu sync.Mutex
	changed := map[string]int{}
	m.SetStateHook(func(id string) {
		mu.Lock()
		changed[id]++
		mu.Unlock()
	})
	count := func(id string) int {
		mu.Lock()
		defer mu.Unlock()
		return changed[id]
	}
	fake := newFake(ToolExecute)
	m.Register("fake", fake)
	info, err := m.Start(context.Background(), "fake", "/p")
	if err != nil {
		t.Fatal(err)
	}
	if count("s1") == 0 {
		t.Fatal("no state change on start")
	}
	list := m.Sessions()
	if len(list) != 1 || list[0].ID != "s1" || list[0].Agent != "fake" || list[0].Cwd != "/p" || list[0].Busy || list[0].Pending != 0 {
		t.Fatalf("sessions = %+v", list)
	}
	if err := m.Prompt(info.ID, "run", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { s := m.Sessions(); return len(s) == 1 && s[0].Busy && s[0].Pending == 1 })
	m.emit(Event{SessionID: "s1", Kind: KindPermissionSettled, RequestID: "p1"})
	waitFor(t, func() bool { return m.Sessions()[0].Pending == 0 })
	before := count("s1")
	close(fake.release)
	waitFor(t, func() bool { return !m.Sessions()[0].Busy })
	waitFor(t, func() bool { return count("s1") > before })
	if s := m.Sessions()[0]; s.LastSeq == 0 {
		t.Fatalf("last seq = 0: %+v", s)
	}
	before = count("s1")
	_ = m.Stop("s1")
	if len(m.Sessions()) != 0 || count("s1") <= before {
		t.Fatal("stop must remove the session and fire the hook")
	}
}

func TestTurnEndClearsPendingRequests(t *testing.T) {
	m := NewManager(func(Event) {})
	fake := newFake("")
	m.Register("fake", fake)
	_, _ = m.Start(context.Background(), "fake", "/p")
	m.emit(Event{SessionID: "s1", Kind: KindPermission, Permission: &PermissionRequest{ID: "a"}})
	m.emit(Event{SessionID: "s1", Kind: KindPermission, Permission: &PermissionRequest{ID: "b"}})
	if p := m.Sessions()[0].Pending; p != 2 {
		t.Fatalf("pending = %d", p)
	}
	m.emit(Event{SessionID: "s1", Kind: KindTurnEnd, StopReason: StopCancelled})
	if p := m.Sessions()[0].Pending; p != 0 {
		t.Fatalf("pending after turn end = %d", p)
	}
}

func TestLateEventsOfAClosedChatKeepNothing(t *testing.T) {
	m := NewManager(func(Event) {})
	m.SetLogLimits(LogLimits{Events: 100, Bytes: 1 << 20})
	open(t, m, "s1")
	_ = m.Stop("s1")
	m.emit(Event{SessionID: "s1", Kind: KindTurnEnd, StopReason: StopCancelled})
	m.emit(Event{SessionID: "s1", Kind: KindPermission, Permission: &PermissionRequest{ID: "late"}})
	m.seqMu.Lock()
	defer m.seqMu.Unlock()
	if len(m.logs) != 0 || len(m.pending) != 0 {
		t.Fatalf("kept %d logs, %d pending sets for a closed chat", len(m.logs), len(m.pending))
	}
}
