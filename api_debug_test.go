package main

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"flint/internal/dap"
)

type sentEvents struct {
	mu  sync.Mutex
	all []DebugEvent
}

func (s *sentEvents) add(ev DebugEvent) {
	s.mu.Lock()
	s.all = append(s.all, ev)
	s.mu.Unlock()
}

func (s *sentEvents) list() []DebugEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DebugEvent(nil), s.all...)
}

func newTestDebugEvents() (*debugEvents, *sentEvents) {
	sent := &sentEvents{}
	return &debugEvents{send: sent.add, seq: map[string]uint64{}, pending: map[string]*pendingOutput{}}, sent
}

func outputEvent(category, text string) dap.Event {
	body, _ := json.Marshal(dap.OutputEvent{Category: category, Output: text})
	return dap.Event{Name: "output", Body: body}
}

func TestDebugOutputIsMergedAndKeptBeforeLaterEvents(t *testing.T) {
	e, sent := newTestDebugEvents()
	for range 1000 {
		e.Event("s", outputEvent("stdout", "line\n"))
	}
	e.Event("s", dap.Event{Name: "stopped", Body: json.RawMessage(`{"reason":"breakpoint"}`)})

	evs := sent.list()
	last := evs[len(evs)-1]
	if last.Name != "stopped" || len(evs) > 10 {
		t.Fatalf("got %d events ending in %q, want a few merged outputs then stopped", len(evs), last.Name)
	}
	lines := 0
	for i, ev := range evs[:len(evs)-1] {
		var o dap.OutputEvent
		_ = json.Unmarshal(ev.Body, &o)
		lines += strings.Count(o.Output, "line\n")
		if ev.Seq != uint64(i+1) {
			t.Fatalf("event %d has seq %d", i, ev.Seq)
		}
	}
	if lines != 1000 {
		t.Fatalf("merged output has %d lines, want 1000", lines)
	}
}

func TestDebugOutputFlushesAfterDelayAndOnCategoryChange(t *testing.T) {
	e, sent := newTestDebugEvents()
	e.Event("s", outputEvent("stdout", "a"))
	e.Event("s", outputEvent("stderr", "b"))
	deadline := time.Now().Add(time.Second)
	for len(sent.list()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if evs := sent.list(); len(evs) != 2 {
		t.Fatalf("events = %v, want stdout then stderr", evs)
	}
}

func TestDebugEventsForgetEndedSessions(t *testing.T) {
	e, _ := newTestDebugEvents()
	e.Event("a", outputEvent("stdout", "x"))
	e.Ended("a")
	e.output("b", "console", "build failed")
	e.finish("b")
	if len(e.seq) != 0 || len(e.pending) != 0 {
		t.Fatalf("left behind: seq=%v pending=%v", e.seq, e.pending)
	}
}
