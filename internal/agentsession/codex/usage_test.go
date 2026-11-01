package codex

import (
	"context"
	"testing"
	"time"

	"flint/internal/agentsession"
)

// A resumed thread's first usage total covers its whole history: the turn
// must count only what it used (the calls reported as "last" within it).
func TestResumedThreadCountsOnlyTheTurn(t *testing.T) {
	h := newHarness(t, "resume_usage.jsonl", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	info, err := h.d.Resume(ctx, h.cwd, "thr-old", h.rec.sink)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if stop, err := h.prompt(t, info.ID, "go on"); err != nil || stop != agentsession.StopEndTurn {
		t.Fatalf("turn: stop=%q err=%v", stop, err)
	}
	usage := h.rec.ofKind(agentsession.KindUsage)
	if len(usage) != 1 {
		t.Fatalf("usage events = %d", len(usage))
	}
	u := usage[0].Usage
	if u.InputTokens != 2500 || u.OutputTokens != 50 || u.CacheReadTokens != 2200 {
		t.Fatalf("turn usage = %+v, want the two calls of this turn only (2500 in incl. 2200 cached, 50 out)", u)
	}
	if u.ContextTokens != 1300 {
		t.Fatalf("context = %+v", u)
	}
}

func TestTurnUsageSplitsCachedInput(t *testing.T) {
	s := &session{usage: tokenBreakdown{InputTokens: 100, CachedInputTokens: 60, OutputTokens: 5}, usageKnown: true}
	tr := &turn{usageBase: s.usage, baseKnown: true}
	s.turn = tr
	s.onTokenUsage(tokenBreakdown{InputTokens: 300, CachedInputTokens: 200, CacheWriteInputTokens: 10, OutputTokens: 9},
		tokenBreakdown{InputTokens: 200, CachedInputTokens: 140, CacheWriteInputTokens: 10, OutputTokens: 4}, 1000)
	u := s.turnUsage(tr)
	if u.InputTokens != 200 || u.CacheReadTokens != 140 || u.CacheWriteTokens != 10 || u.OutputTokens != 4 {
		t.Fatalf("turn usage = %+v", u)
	}
}
