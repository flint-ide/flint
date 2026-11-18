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
	find := func(cwd string) []string {
		switch cwd {
		case "/wt/a":
			return []string{"A"}
		case "/wt/lead": // a joint group's lead: its chat works for all three
			return []string{"L", "M", "N"}
		case "/wt/m":
			return []string{"M"}
		}
		return nil
	}
	return NewTracker(find, c.add), c
}

func TestTrackerJointSessionCountsForEveryTask(t *testing.T) {
	tr, c := newTestTracker()
	tr.Bind("j", "/wt/lead")
	tr.Prompted("j")
	if got := c.get(); !equal(got, []string{"L:working", "M:working", "N:working"}) {
		t.Fatalf("changes = %v", got)
	}
	for _, id := range []string{"L", "M", "N"} {
		if got := tr.State(id, "").State; got != Working {
			t.Errorf("%s = %s", id, got)
		}
		if got := tr.Sessions(id); !equal(got, []string{"j"}) {
			t.Errorf("Sessions(%s) = %v", id, got)
		}
	}
	if got := tr.Active(); !equal(got, []string{"L", "M", "N"}) {
		t.Errorf("Active = %v", got)
	}
	if got := tr.Tasks("j"); !equal(got, []string{"L", "M", "N"}) {
		t.Errorf("Tasks = %v", got)
	}
	// A chat of its own in a member's worktree counts for that member only.
	tr.Bind("own", "/wt/m")
	tr.Observe(as.Event{SessionID: "j", Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
	if got := c.get()[3:]; !equal(got, []string{"L:finished", "M:finished", "N:finished"}) {
		t.Fatalf("after the turn = %v", got)
	}
	tr.Prompted("own")
	if tr.State("M", "").State != Working || tr.State("L", "").State != Finished {
		t.Error("the member's own chat changed the others")
	}
	// A removed member is dropped from the joint session, which still counts
	// for the others.
	tr.Forget("M")
	if got := tr.Tasks("j"); !equal(got, []string{"L", "N"}) {
		t.Errorf("after Forget: %v", got)
	}
	if len(tr.Sessions("M")) != 0 {
		t.Error("the removed member still has sessions")
	}
	tr.Unbind("j")
	if got := c.get(); !equal(got[len(got)-2:], []string{"L:idle", "N:idle"}) {
		t.Errorf("after closing the chat: %v", got)
	}
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
