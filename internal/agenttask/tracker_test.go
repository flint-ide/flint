package agenttask

import (
	"sync"
	"testing"

	as "flint/internal/agentsession"
)

type changes struct {
	mu   sync.Mutex
	list []string
}

func (c *changes) add(task string, l Live) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, task+":"+l.State)
}

func (c *changes) get() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.list...)
}

func newTestTracker() (*Tracker, *changes) {
	c := &changes{}
	find := func(cwd string) string {
		if cwd == "/wt/a" {
			return "A"
		}
		return ""
	}
	return NewTracker(find, c.add), c
}

func TestTrackerFollowsBoundSessions(t *testing.T) {
	tr, c := newTestTracker()
	tr.Bind("s1", "/wt/a")
	tr.Bind("other", "/elsewhere") // not a task: ignored
	tr.Prompted("s1")
	for range 50 { // streaming doesn't report a change per token
		tr.Observe(as.Event{SessionID: "s1", Kind: as.KindMessageChunk, Text: "x"})
	}
	tr.Observe(as.Event{SessionID: "other", Kind: as.KindPermission})
	tr.Observe(as.Event{SessionID: "s1", Kind: as.KindPermission})
	tr.Observe(as.Event{SessionID: "s1", Kind: as.KindPermissionSettled})
	tr.Observe(as.Event{SessionID: "s1", Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})

	want := []string{"A:working", "A:waiting", "A:working", "A:finished"}
	if got := c.get(); !equal(got, want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}
	if got := tr.State("A", "").State; got != Finished {
		t.Errorf("State = %s", got)
	}
	if got := tr.Sessions("A"); !equal(got, []string{"s1"}) {
		t.Errorf("Sessions = %v", got)
	}
}

func TestTrackerUnbind(t *testing.T) {
	tr, c := newTestTracker()
	tr.Bind("s1", "/wt/a")
	tr.Prompted("s1")
	tr.Unbind("s1")
	if got := c.get(); !equal(got, []string{"A:working", "A:idle"}) {
		t.Fatalf("changes = %v", got)
	}
	// Events of a closed chat no longer count.
	tr.Observe(as.Event{SessionID: "s1", Kind: as.KindPermission})
	if got := tr.State("A", Finished).State; got != Finished {
		t.Errorf("after unbind the remembered state shows: %s", got)
	}
	if len(tr.Sessions("A")) != 0 {
		t.Error("session still listed")
	}
}

func TestTrackerTwoChats(t *testing.T) {
	tr, _ := newTestTracker()
	tr.Bind("s1", "/wt/a")
	tr.Bind("s2", "/wt/a")
	tr.Prompted("s1")
	tr.Observe(as.Event{SessionID: "s1", Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
	tr.Prompted("s2")
	if got := tr.State("A", "").State; got != Working {
		t.Errorf("one chat finished, one working: %s", got)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
