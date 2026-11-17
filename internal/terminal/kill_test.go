package terminal

import (
	"strings"
	"testing"
	"time"
)

// A shell that ignores SIGHUP survives Close; the manager kills its process
// group once the grace period has passed, so its id is not held forever.
func TestCloseKillsAShellThatIgnoresHangUp(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	m.SetKillGrace(300 * time.Millisecond)
	if err := m.Start("stubborn", Options{Shell: "/bin/sh", Args: []string{"-c", "trap '' HUP; echo ready; while :; do sleep 1; done"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(rec.output(), "ready") {
		if time.Now().After(deadline) {
			t.Fatalf("shell did not start; output %q", rec.output())
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	m.Close("stubborn")
	waitExit(t, rec)
	if took := time.Since(start); took < 250*time.Millisecond {
		t.Errorf("the shell ended after %v, before the grace period: SIGHUP alone should not have ended it", took)
	}
}

// The kill timer of a shell that left in time must not fire later at a
// process id that may belong to someone else by then.
func TestCloseDoesNotKillAShellThatLeavesInTime(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	m.SetKillGrace(time.Hour)
	if err := m.Start("t", Options{Shell: "/bin/sh"}); err != nil {
		t.Fatal(err)
	}
	m.Close("t")
	waitExit(t, rec)
}
