//go:build linux

package proctree

import "testing"

func TestParseStatPPID(t *testing.T) {
	for raw, want := range map[string]stat{
		"42 (bash) S 7 42 42 34816 1":  {ppid: 7, sid: 42, tty: 34816},
		"42 (a b) c)) R 1 1 1 0 1":     {ppid: 1, sid: 1, tty: 0},
		"42 (x) S 1234 1 99 34816 1 4": {ppid: 1234, sid: 99, tty: 34816},
	} {
		got, err := parseStat(raw)
		if err != nil || got != want {
			t.Errorf("parseStat(%q) = %+v, %v; want %+v", raw, got, err, want)
		}
	}
	if _, err := parseStat("garbage"); err == nil {
		t.Error("garbage parsed")
	}
	if _, err := parseStat("42 (x) S 1 2"); err == nil {
		t.Error("a short stat parsed")
	}
}
