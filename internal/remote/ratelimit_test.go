package remote

import (
	"testing"
	"time"
)

func TestLimiterRefillsAtTheRate(t *testing.T) {
	clk := &clock{t: time.Unix(0, 0)}
	l := newLimiter(60) // one per second, burst 10
	l.now = clk.now
	for i := range 10 {
		if !l.allow("d") {
			t.Fatalf("request %d of the burst refused", i+1)
		}
	}
	if l.allow("d") {
		t.Fatal("past the burst")
	}
	clk.add(time.Second)
	if !l.allow("d") || l.allow("d") {
		t.Fatal("one second should refill exactly one request")
	}
	clk.add(time.Hour)
	n := 0
	for l.allow("d") {
		n++
	}
	if n != 10 {
		t.Fatalf("refill capped at %d, want the burst (10)", n)
	}
	// Anonymous requests have their own fixed budget.
	for range anonBurst {
		if !l.allow(anonKey) {
			t.Fatal("anonymous burst refused")
		}
	}
	if l.allow(anonKey) {
		t.Fatal("past the anonymous burst")
	}
	l.forget("d")
	if !l.allow("d") {
		t.Fatal("a forgotten key starts with a full bucket")
	}
}
