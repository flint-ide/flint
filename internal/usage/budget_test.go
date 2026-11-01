package usage

import "testing"

func TestFireOncePerThresholdPerDay(t *testing.T) {
	s, c, path := newStore(t, "2026-11-01 10:00")
	limits := Limits{DailyUSD: 1, DailyTokens: 1000}
	must(t, s.Add(entry("c1", "/p", "claude", "opus", "", turn(100, 10, 0.6))))
	if got, _ := s.Fire(limits, "", nil); len(got) != 0 {
		t.Fatalf("under budget, got %+v", got)
	}
	must(t, s.Add(entry("c1", "/p", "claude", "opus", "", turn(100, 10, 0.6))))
	got, err := s.Fire(limits, "", nil)
	must(t, err)
	if len(got) != 1 || got[0].Scope != ScopeDay || got[0].Kind != KindUSD || got[0].Key != "2026-11-01" || !near(got[0].Spent, 1.2) {
		t.Fatalf("crossing the daily budget: %+v", got)
	}
	must(t, s.Add(entry("c1", "/p", "claude", "opus", "", turn(1000, 0, 0.1))))
	got, _ = s.Fire(limits, "", nil)
	if len(got) != 1 || got[0].Kind != KindTokens {
		t.Fatalf("the token budget is its own threshold, the money one does not repeat: %+v", got)
	}
	// A restart does not repeat them.
	reopened := openAt(t, path, c, 90)
	if got, _ := reopened.Fire(limits, "", nil); len(got) != 0 {
		t.Errorf("fired again after a restart: %+v", got)
	}
	// Raising the limit arms it again.
	if got, _ := reopened.Fire(Limits{DailyUSD: 1.2}, "", nil); len(got) != 1 {
		t.Errorf("a new threshold should fire: %+v", got)
	}
	// The next day starts over.
	c.t = day("2026-11-02 08:00")
	must(t, reopened.Add(entry("c1", "/p", "claude", "opus", "", turn(1, 1, 2))))
	if got, _ := reopened.Fire(limits, "", nil); len(got) != 1 || got[0].Key != "2026-11-02" {
		t.Errorf("next day: %+v", got)
	}
}

func TestTaskBudgetAndEstimates(t *testing.T) {
	s, c, _ := newStore(t, "2026-10-30 10:00")
	prices := Prices{"gpt-5": {In: 1, Out: 1}}
	limits := Limits{TaskUSD: 2}
	must(t, s.Add(entry("c1", "/p", "codex", "gpt-5", "t1", turn(1_500_000, 0, 0))))
	c.t = day("2026-11-01 10:00")
	must(t, s.Add(entry("c1", "/p", "codex", "gpt-5", "t1", turn(600_000, 0, 0))))
	must(t, s.Add(entry("c9", "/p", "codex", "gpt-5", "t2", turn(5_000_000, 0, 0))))
	if got, _ := s.Fire(limits, "t1", nil); len(got) != 0 {
		t.Errorf("without a price, codex tokens cost nothing known: %+v", got)
	}
	got, _ := s.Fire(limits, "t1", prices)
	if len(got) != 1 || got[0].Scope != ScopeTask || got[0].Key != "t1" || !got[0].Estimated || !near(got[0].Spent, 2.1) {
		t.Errorf("task budget over days, from estimates: %+v", got)
	}
}

func TestOverrideUnblocks(t *testing.T) {
	s, _, _ := newStore(t, "2026-11-01 10:00")
	limits := Limits{DailyTokens: 10, TaskTokens: 10}
	must(t, s.Add(entry("c1", "/p", "claude", "opus", "t1", turn(20, 0, 0))))
	if b := s.Blocked(limits, "t1", nil); len(b) != 2 {
		t.Fatalf("blocked = %+v", b)
	}
	must(t, s.Override("t1"))
	if b := s.Blocked(limits, "t1", nil); len(b) != 0 {
		t.Errorf("after override: %+v", b)
	}
	if o := s.Over(limits, "t1", nil); len(o) != 2 || !o[0].Overridden {
		t.Errorf("still over, marked overridden: %+v", o)
	}
	if b := s.Blocked(limits, "t2", nil); len(b) != 0 {
		t.Errorf("another task in the overridden day: %+v", b)
	}
}
