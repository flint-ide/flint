//go:build darwin || linux

package workflows

import (
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/cli"
)

// gatedRuns makes one run waiting at its approval step and one failed.
func (f *cliFixture) gatedRuns() (gated, failed string) {
	f.t.Helper()
	writeFile(f.t, filepath.Join(ProjectDir(f.root), "gated.md"), "## Do\n\nDo it.\n\n## Approve\ntype: gate\n")
	g := f.start("gated")
	f.finishFirst(g.ID, "RESULT: DONE")
	f.waitFor(g.ID, "the approval step", func(r Run) bool { return r.State == StateWaiting })
	x := f.start("gated")
	f.finishFirst(x.ID, "RESULT: BLOCKED")
	f.waitFor(x.ID, "the failure", func(r Run) bool { return r.State == StateFailed })
	return g.ID, x.ID
}

// run approve and run continue change nothing unless the user says yes in
// Flint's dialog, whoever runs them: a person in a terminal included.
func TestCLIHumanDecisionsNeedTheDialog(t *testing.T) {
	callers := []cli.Caller{
		{PID: 7, Program: "zsh"},
		{Agent: true, What: "claude", PID: 7},
		{Detached: true, What: "it has no terminal", PID: 7},
		{Unknown: true, What: "?", PID: 7},
	}
	for _, caller := range callers {
		f := newCLIFixture(t)
		gated, failed := f.gatedRuns()
		f.caller.Store(&caller)
		f.decline.Store(true)
		for _, cmd := range [][]string{{"run", "approve", gated}, {"run", "continue", failed}} {
			_, errs, code := f.flint("", cmd...)
			if code != cli.ExitFailed || errs == "" {
				t.Errorf("%+v: flint %q = %d %q", caller, cmd, code, errs)
			}
			human := !caller.Agent && !caller.Unknown && !caller.Detached
			if human && errs != "flint: not approved in Flint\n" {
				t.Errorf("declined: %q", errs)
			}
		}
		if r, _ := f.e.get(gated); r.State != StateWaiting || r.Records[1].State == StepDone {
			t.Errorf("%+v: the approval step was passed: %s", caller, r.State)
		}
		if r, _ := f.e.get(failed); r.State != StateFailed || r.Records[0].Skipped {
			t.Errorf("%+v: the failed step was passed: %s", caller, r.State)
		}
		if want := int32(0); !(caller.Agent || caller.Unknown || caller.Detached) {
			want = 2
			if f.asked.Load() != want {
				t.Errorf("a person was asked %d times", f.asked.Load())
			}
		} else if f.asked.Load() != want {
			t.Errorf("%+v was asked: refused callers get no dialog", caller)
		}
	}
	// With the user's yes, they act.
	f := newCLIFixture(t)
	gated, failed := f.gatedRuns()
	if _, errs, code := f.flint("", "run", "approve", gated); code != 0 {
		t.Fatalf("approved: %d %s", code, errs)
	}
	if _, errs, code := f.flint("", "run", "continue", failed); code != 0 {
		t.Fatalf("continued: %d %s", code, errs)
	}
	if r, _ := f.e.get(gated); r.State != StateDone {
		t.Errorf("approved run: %s", r.State)
	}
}

// No command of the command line passes an approval step or a failed step
// without Flint's dialog: every command, with any argument a run takes, sent
// by a person whose dialog answers no.
func TestNoCLICommandPassesAStepWithoutTheDialog(t *testing.T) {
	f := newCLIFixture(t)
	gated, failed := f.gatedRuns()
	f.decline.Store(true)
	for _, c := range f.api.cliCommands() {
		if c.Human && c.Ask == nil {
			t.Errorf("%s: a human decision without a question", c.Path)
		}
		if c.Path == "run stop" || c.Path == "run remove" || c.Path == "run send-back" || c.Path == "run retry" {
			continue // they act on the run, but pass no step; checked elsewhere
		}
		for _, id := range []string{gated, failed} {
			args := append(strings.Fields(c.Path), id)
			_, _, _ = f.flint("", args...)
			_, _, _ = f.flint("", append(args, "--step", "Approve")...)
		}
	}
	if r, _ := f.e.get(gated); r.State != StateWaiting || r.Records[1].State == StepDone {
		t.Errorf("the approval step was passed: %s %+v", r.State, r.Log)
	}
	if r, _ := f.e.get(failed); r.State != StateFailed || r.Records[0].Skipped {
		t.Errorf("the failed step was passed: %s %+v", r.State, r.Log)
	}
	for _, path := range []string{"run approve", "run continue"} {
		found := false
		for _, c := range f.api.cliCommands() {
			found = found || c.Path == path && c.Human
		}
		if !found {
			t.Errorf("%s is not a human decision", path)
		}
	}
}
