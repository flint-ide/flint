package workflows

import (
	"strings"
	"testing"

	"flint/internal/feature"
)

func TestSandboxHint(t *testing.T) {
	cases := []struct {
		name      string
		sandboxed bool
		res       feature.RunResult
		want      bool
	}{
		{"sandboxed and failed", true, feature.RunResult{Code: 1}, true},
		{"sandboxed and passed", true, feature.RunResult{}, false},
		{"not sandboxed and failed", false, feature.RunResult{Code: 1}, false},
		{"sandboxed and stopped", true, feature.RunResult{Code: 1, Stopped: true}, false},
		{"sandboxed and did not start", true, feature.RunResult{Error: "no such program"}, false},
	}
	for _, c := range cases {
		got := sandboxHint(c.sandboxed, c.res)
		if (got != "") != c.want || (c.want && !strings.Contains(got, "Settings ▸ Workflows")) {
			t.Errorf("%s: hint %q", c.name, got)
		}
	}
}

func TestOutcomeHintPlacement(t *testing.T) {
	// The test step (script, on-fail: code) is started after plan and code.
	start := func() Run {
		r := newTestRun()
		for range 2 {
			r = start(t, r, 101).Passed("ok", 102)
		}
		return start(t, r, 103)
	}
	failed := scriptOutcome(feature.RunResult{Code: 2, Output: "boom"})
	failed.hint = sandboxHintText

	// The step's attempts are spent: the run fails and the user reads the hint after the reason.
	r := failed.apply(start(), policy{maxAttempts: 1}, 5)
	if r.State != StateFailed || r.Error != "it exited with code 2\n"+sandboxHintText {
		t.Errorf("failed run: %s %q", r.State, r.Error)
	}

	// Sent back automatically: the agent's feedback has no hint, the log has it.
	r = failed.apply(start(), policy{maxAttempts: 3}, 5)
	if r.State == StateFailed || strings.Contains(r.Feedback, "sandbox") {
		t.Errorf("sent back: %s feedback %q", r.State, r.Feedback)
	}
	if n := len(r.Log); n == 0 || r.Log[n-1].Text != sandboxHintText {
		t.Errorf("log %+v", r.Log)
	}
}
