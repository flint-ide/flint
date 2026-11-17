package orchestrate

import (
	"fmt"
	"strings"
	"testing"

	"flint/internal/agenttask"
)

func TestShouldNotifyOnlyForAChangeIntoAStateTheParentActsOn(t *testing.T) {
	for _, tc := range []struct {
		name        string
		prev, next  string
		noticeTurn  bool
		want        bool
		explanation string
	}{
		{"finished after work", agenttask.Working, agenttask.Finished, false, true, ""},
		{"failed after work", agenttask.Working, agenttask.Failed, false, true, ""},
		{"waits for approval", agenttask.Working, agenttask.Waiting, false, true, ""},
		{"first turn, from idle", agenttask.Idle, agenttask.Finished, false, true, ""},
		{"same state again is no change", agenttask.Finished, agenttask.Finished, false, false, ""},
		{"waiting again", agenttask.Waiting, agenttask.Waiting, false, false, ""},
		{"starting work", agenttask.Finished, agenttask.Working, false, false, ""},
		{"cancelled", agenttask.Working, agenttask.Idle, false, false, ""},
		{"approval answered", agenttask.Waiting, agenttask.Working, false, false, ""},
		{"empty states", "", "", false, false, ""},
		// A turn Flint started by delivering a notice must not set the next
		// notice off: that is how a parent and its child would answer each
		// other forever.
		{"finish of a turn a notice started", agenttask.Working, agenttask.Finished, true, false, ""},
		{"failure of a turn a notice started", agenttask.Working, agenttask.Failed, true, false, ""},
		{"approval during a turn a notice started still matters", agenttask.Working, agenttask.Waiting, true, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldNotify(tc.prev, tc.next, tc.noticeTurn); got != tc.want {
				t.Errorf("ShouldNotify(%q, %q, %v) = %v, want %v", tc.prev, tc.next, tc.noticeTurn, got, tc.want)
			}
		})
	}
}

func TestNoticeText(t *testing.T) {
	n := Notice{Child: "c2", Role: "reviewer", State: agenttask.Finished, Output: "looks fine\nship it"}
	if got, want := n.String(), "[Flint] child c2 (reviewer) is finished:\nlooks fine\nship it"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := (Notice{Child: "c3", State: agenttask.Failed}).String(); got != "[Flint] child c3 (no role) is failed:\n(no output)" {
		t.Errorf("no role, no output: %q", got)
	}
	big := Notice{Child: "c4", State: agenttask.Waiting, Output: strings.Repeat("é", MaxNoticeBytes)}.String()
	if len(big) > MaxNoticeBytes+200 || !strings.Contains(big, ":\n…") || !strings.Contains(big, "é") {
		t.Errorf("a notice must be bounded: %d bytes", len(big))
	}
	// Only the end of the output counts: it is where the agent says what it needs.
	long := Notice{Child: "c5", State: agenttask.Finished, Output: "start\n" + strings.Repeat("x", MaxNoticeBytes) + "\nthe end"}.String()
	if !strings.HasSuffix(long, "the end") || strings.Contains(long, "start") {
		t.Errorf("bounded notice keeps the wrong part: %.60q…", long)
	}
}

func ids(ns []Notice) string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Child+"="+n.State)
	}
	return strings.Join(out, " ")
}

func TestNoticesMergePerChildNewestLast(t *testing.T) {
	var q Notices
	q.Add("p", Notice{Child: "a", State: agenttask.Waiting})
	q.Add("p", Notice{Child: "b", State: agenttask.Finished})
	q.Add("p", Notice{Child: "a", State: agenttask.Failed}) // replaces a's, which moves to the end
	q.Add("other", Notice{Child: "z", State: agenttask.Failed})
	if got := ids(q.Take("p")); got != "b=finished a=failed" {
		t.Errorf("queue = %q", got)
	}
	if q.Pending("p") || !q.Pending("other") {
		t.Error("Take must empty only that parent's queue")
	}
	if got := q.Take("p"); len(got) != 0 {
		t.Errorf("taken twice: %v", got)
	}
}

func TestNoticesKeepTheNewestEight(t *testing.T) {
	var q Notices
	for i := 0; i < MaxQueuedNotices+3; i++ {
		q.Add("p", Notice{Child: fmt.Sprintf("c%d", i), State: agenttask.Finished})
	}
	got := q.Take("p")
	if len(got) != MaxQueuedNotices || got[0].Child != "c3" || got[len(got)-1].Child != "c10" {
		t.Errorf("kept %s", ids(got))
	}
	// A merge never costs another child its place.
	for i := 0; i < MaxQueuedNotices; i++ {
		q.Add("p", Notice{Child: fmt.Sprintf("c%d", i), State: agenttask.Waiting})
	}
	q.Add("p", Notice{Child: "c7", State: agenttask.Failed})
	if got := q.Take("p"); len(got) != MaxQueuedNotices {
		t.Errorf("merging changed the count: %s", ids(got))
	}
}

func TestNoticesDropAndRequeue(t *testing.T) {
	var q Notices
	q.Add("p", Notice{Child: "a", State: agenttask.Finished})
	q.Drop("p")
	if q.Pending("p") {
		t.Error("Drop left notices behind")
	}
	// A delivery that failed puts the notices back without overriding a newer
	// one for the same child.
	q.Add("p", Notice{Child: "a", State: agenttask.Finished})
	taken := q.Take("p")
	q.Add("p", Notice{Child: "a", State: agenttask.Failed})
	q.Requeue("p", taken)
	if got := ids(q.Take("p")); got != "a=failed" {
		t.Errorf("after requeue: %q", got)
	}
}

func TestNoticeTurnsAreRememberedUntilTheyEnd(t *testing.T) {
	var q Notices
	if q.EndTurn("p") {
		t.Error("a chat with no notice turn")
	}
	q.MarkTurn("p")
	if !q.EndTurn("p") {
		t.Error("the notice turn was forgotten")
	}
	if q.EndTurn("p") {
		t.Error("a turn ended twice")
	}
	q.MarkTurn("p")
	q.Drop("p") // the chat went away
	if q.EndTurn("p") {
		t.Error("Drop must forget the turn too")
	}
}

func TestComposeJoinsNoticesIntoOneMessage(t *testing.T) {
	got := Compose([]Notice{
		{Child: "a", Role: "x", State: agenttask.Finished, Output: "done"},
		{Child: "b", State: agenttask.Waiting, Output: "needs a yes"},
	})
	want := "[Flint] child a (x) is finished:\ndone\n\n[Flint] child b (no role) is waiting:\nneeds a yes"
	if got != want {
		t.Errorf("got %q", got)
	}
	if Compose(nil) != "" {
		t.Error("nothing to say")
	}
}
