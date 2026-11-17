package orchestrate

import (
	"testing"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
)

func TestObserveChangeReportsTheStateBeforeTheEvent(t *testing.T) {
	r := newWith(t, Node{ID: "p"}, Node{ID: "c", Parent: "p"})
	r.Prompted("c")
	n, prev, ok := r.ObserveChange(turnEnd("c", as.StopEndTurn))
	if !ok || prev != agenttask.Working || n.Live.State != agenttask.Finished || n.Parent != "p" || n.Depth != 1 {
		t.Fatalf("got %+v, prev %q, ok %v", n, prev, ok)
	}
	// An event that changes nothing reports the same state twice.
	n, prev, _ = r.ObserveChange(chunk("c", "text"))
	if prev != n.Live.State {
		t.Errorf("no change expected: %q -> %q", prev, n.Live.State)
	}
	if _, _, ok := r.ObserveChange(ev("nope", as.KindError)); ok {
		t.Error("an unknown chat reported a change")
	}
}
