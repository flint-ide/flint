package workflows

import (
	"fmt"
	"testing"
)

func TestPrune(t *testing.T) {
	mk := func(id string, state string, updated int64) Run {
		return Run{ID: id, State: state, Updated: updated}
	}
	ids := func(runs []Run) string {
		s := ""
		for _, r := range runs {
			s += r.ID + " "
		}
		return s
	}
	t.Run("under the bound nothing goes", func(t *testing.T) {
		runs := []Run{mk("a", StateDone, 1), mk("b", StateRunning, 2)}
		if got := prune(runs); len(got) != 2 {
			t.Errorf("prune = %s", ids(got))
		}
	})
	t.Run("the oldest runs that are over go first, order kept", func(t *testing.T) {
		var runs []Run
		for i := range MaxRuns - 1 {
			runs = append(runs, mk(fmt.Sprintf("live%d", i), StateRunning, int64(1000+i)))
		}
		runs = append(runs, mk("old-done", StateDone, 5), mk("new-done", StateStopped, 900), mk("mid-done", StateDone, 50))
		got := prune(runs)
		if len(got) != MaxRuns {
			t.Fatalf("%d runs", len(got))
		}
		for _, r := range got {
			if r.ID == "old-done" || r.ID == "mid-done" {
				t.Errorf("%s was kept", r.ID)
			}
		}
		if got[len(got)-1].ID != "new-done" || got[0].ID != "live0" {
			t.Errorf("order changed: first %s, last %s", got[0].ID, got[len(got)-1].ID)
		}
	})
	t.Run("a run that is not over goes only when nothing else is left", func(t *testing.T) {
		var runs []Run
		for i := range MaxRuns + 3 {
			runs = append(runs, mk(fmt.Sprintf("r%d", i), StateWaiting, int64(100-i)))
		}
		runs[7].State = StateDone // the only run that is over: it goes first
		got := prune(runs)
		if len(got) != MaxRuns {
			t.Fatalf("%d runs", len(got))
		}
		kept := map[string]bool{}
		for _, r := range got {
			kept[r.ID] = true
		}
		// 3 to drop: r7 (over), then the two oldest waiting ones (the last two).
		for _, id := range []string{"r7", fmt.Sprintf("r%d", MaxRuns+2), fmt.Sprintf("r%d", MaxRuns+1)} {
			if kept[id] {
				t.Errorf("%s was kept", id)
			}
		}
	})
	t.Run("the argument is not changed", func(t *testing.T) {
		var runs []Run
		for i := range MaxRuns + 5 {
			runs = append(runs, mk(fmt.Sprintf("r%d", i), StateDone, int64(i)))
		}
		first := runs[0].ID
		got := prune(runs)
		if len(runs) != MaxRuns+5 || runs[0].ID != first || got[0].ID != "r5" {
			t.Errorf("runs = %d, first %s, got[0] %s", len(runs), runs[0].ID, got[0].ID)
		}
	})
}

func TestStateKey(t *testing.T) {
	if stateRuns != "workflowRuns" || MaxRuns != 100 {
		t.Error("the state key or its bound changed")
	}
}
