//go:build darwin || linux

package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"flint/internal/cli"
	"flint/internal/cli/clisock"
)

// cliFixture serves the feature's commands on a socket, the way Flint does,
// and runs them through the flint client's own code.
type cliFixture struct {
	*fixture
	paths clisock.Paths
	agent atomic.Bool // the next callers are agents
	// caller, when set, is the next callers' classification (agent wins);
	// decline makes Flint's dialog answer no; asked counts the dialogs.
	caller  atomic.Pointer[cli.Caller]
	decline atomic.Bool
	asked   atomic.Int32
}

func newCLIFixture(t *testing.T) *cliFixture {
	t.Helper()
	f := &cliFixture{fixture: newFixture(t)}
	reg, err := cli.NewRegistry(f.api.cliCommands())
	if err != nil {
		t.Fatal(err)
	}
	d, err := os.MkdirTemp("/tmp", "fwf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	f.paths = clisock.PathsIn(filepath.Join(d, "run"))
	srv, err := clisock.Start(clisock.Options{Paths: f.paths, Registry: reg, Classify: func(pid int) cli.Caller {
		if f.agent.Load() {
			return cli.Caller{Agent: true, What: "claude", PID: pid}
		}
		if c := f.caller.Load(); c != nil {
			return *c
		}
		return cli.Caller{PID: pid, Program: "zsh"}
	}, Confirm: func(context.Context, cli.Question) bool {
		f.asked.Add(1)
		return !f.decline.Load()
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return f
}

// flint runs a command line in the workspace folder.
func (f *cliFixture) flint(stdin string, args ...string) (stdout, stderr string, code int) {
	f.t.Helper()
	var out, errb bytes.Buffer
	code = clisock.RunIn(f.t.Context(), f.paths, args, f.root, strings.NewReader(stdin), &out, &errb)
	return out.String(), errb.String(), code
}

func (f *cliFixture) runJSON(args ...string) RunView {
	f.t.Helper()
	out, errs, code := f.flint("", append(args, "--json")...)
	if code != 0 {
		f.t.Fatalf("flint %q = %d %s", args, code, errs)
	}
	var v RunView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		f.t.Fatalf("flint %q: %v\n%s", args, err, out)
	}
	return v
}

func TestCLIRunToDone(t *testing.T) {
	f := newCLIFixture(t)

	out, _, code := f.flint("", "workflow", "list")
	if code != 0 || !strings.Contains(out, "feature") || !strings.HasPrefix(out, "NAME") {
		t.Fatalf("workflow list = %d\n%s", code, out)
	}
	out, _, _ = f.flint("", "workflow", "list", "--json")
	var l Listing
	if err := json.Unmarshal([]byte(out), &l); err != nil || len(l.Workflows) == 0 {
		t.Fatalf("workflow list --json: %v\n%s", err, out)
	}
	if out, _, code := f.flint("", "workflow", "show", "feature"); code != 0 || !strings.Contains(out, "Approve") || !strings.Contains(out, "gate") {
		t.Fatalf("workflow show = %d\n%s", code, out)
	}

	// An agent may read, not start.
	f.agent.Store(true)
	if _, errs, code := f.flint("", "workflow", "run", "feature", "--task", "Add a --verbose flag"); code != 1 || !strings.Contains(errs, "run by an agent") {
		t.Fatalf("an agent's start = %d %q", code, errs)
	}
	if n := len(f.api.Runs()); n != 0 {
		t.Fatalf("an agent's start made %d runs", n)
	}
	f.agent.Store(false)

	// Usage errors exit 2 and start nothing.
	if _, errs, code := f.flint("", "workflow", "run", "feature"); code != cli.ExitUsage || !strings.Contains(errs, "--task or --task-file is required") {
		t.Fatalf("no task = %d %q", code, errs)
	}
	// The same refusals as Run Workflow…, in the same words.
	if _, errs, code := f.flint("x", "workflow", "run", "nope", "--task-file", "-"); code != 1 || errs != "flint: there is no workflow named \"nope\" here\n" {
		t.Fatalf("unknown workflow = %d %q", code, errs)
	}

	out, errs, code := f.flint("Add a --verbose flag\n", "workflow", "run", "feature", "--task-file", "-", "--json")
	if code != 0 {
		t.Fatalf("workflow run = %d %s", code, errs)
	}
	var v RunView
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.ID == "" || v.State != StateRunning || v.Description != "Add a --verbose flag" {
		t.Fatalf("workflow run --json: %v\n%s", err, out)
	}
	if out, _, code := f.flint("", "run", "list"); code != 0 || !strings.Contains(out, v.ID) || !strings.Contains(out, "Running") {
		t.Fatalf("run list = %d\n%s", code, out)
	}

	f.finishFirst(v.ID, "Wrote PLAN.md\nRESULT: DONE")
	f.finishChat(v.ID, 1, "Implemented.\nRESULT: DONE")
	f.finishChat(v.ID, 2, "Looks good.\nRESULT: PASS")

	// At the gate: wait returns at once with "needs you".
	out, _, code = f.flint("", "run", "wait", v.ID, "--json")
	var w waitResult
	if code != cli.ExitNeedsUser || json.Unmarshal([]byte(out), &w) != nil || w.TimedOut || w.Run.State != StateWaiting || !w.Run.NeedsUser {
		t.Fatalf("run wait at the gate = %d\n%s", code, out)
	}
	if out, _, _ := f.flint("", "run", "show", v.ID); !strings.Contains(out, "State:   Waiting for you") || !strings.Contains(out, "You can: approve, send-back, stop") {
		t.Fatalf("run show at the gate:\n%s", out)
	}

	// Approving and continuing past a failure are the user's alone.
	f.agent.Store(true)
	for _, cmd := range []string{"approve", "continue"} {
		_, errs, code := f.flint("", "run", cmd, v.ID)
		if code != 1 || errs != "flint: this command was run by an agent (claude). Agents change things through Flint's tools, which ask you first.\n" {
			t.Fatalf("run %s by an agent = %d %q", cmd, code, errs)
		}
	}
	if r, _ := f.e.get(v.ID); r.State != StateWaiting {
		t.Fatalf("an agent moved the run to %s", r.State)
	}
	f.agent.Store(false)

	if _, errs, code := f.flint("", "run", "continue", v.ID); code != 1 || !strings.Contains(errs, "flint: ") {
		t.Fatalf("continue at an approval = %d %q", code, errs)
	}
	after := f.runJSON("run", "approve", v.ID)
	if after.ID != v.ID {
		t.Fatalf("run approve --json = %+v", after)
	}
	f.waitFor(v.ID, "done", func(r Run) bool { return r.State == StateDone })
	out, _, code = f.flint("", "run", "wait", v.ID)
	if code != cli.ExitOK || !strings.Contains(out, "State:   Done") {
		t.Fatalf("run wait when done = %d\n%s", code, out)
	}
	if out, _, code := f.flint("", "run", "remove", v.ID); code != 0 || out != "Removed run "+v.ID+"\n" {
		t.Fatalf("run remove = %d %q", code, out)
	}
	if _, errs, code := f.flint("", "run", "show", v.ID); code != 1 || errs != "flint: no such workflow run: it was removed\n" {
		t.Fatalf("run show of a removed run = %d %q", code, errs)
	}
}

func TestCLIRunWaitTimesOut(t *testing.T) {
	f := newCLIFixture(t)
	v := f.start("feature")
	out, _, code := f.flint("", "run", "wait", v.ID, "--timeout", "1", "--json")
	var w waitResult
	if code != cli.ExitTimeout || json.Unmarshal([]byte(out), &w) != nil || !w.TimedOut || w.Run.State != StateRunning {
		t.Fatalf("run wait --timeout 1 = %d\n%s", code, out)
	}
	if _, errs, code := f.flint("", "run", "wait", v.ID, "--timeout", "0"); code != cli.ExitUsage || !strings.Contains(errs, "--timeout must be") {
		t.Fatalf("--timeout 0 = %d %q", code, errs)
	}
	// Steering by a person: pause, then resume.
	if p := f.runJSON("run", "pause", v.ID); !p.Paused {
		t.Fatalf("pause = %+v", p)
	}
	if out, _, code := f.flint("", "run", "resume", v.ID); code != 0 || !strings.Contains(out, "State:   Running") {
		t.Fatalf("resume = %d\n%s", code, out)
	}
	if _, errs, code := f.flint("", "run", "show", "../x"); code != 1 || !strings.Contains(errs, "no such workflow run") {
		t.Fatalf("a bad id = %d %q", code, errs)
	}
}

func TestCLINewAndCoordinate(t *testing.T) {
	f := newCLIFixture(t)
	out, _, code := f.flint("", "workflow", "new", "release")
	if code != 0 || out != "Wrote "+filepath.Join(ProjectDir(f.root), "release.md")+"\n" {
		t.Fatalf("workflow new = %d %q", code, out)
	}
	if _, errs, code := f.flint("", "workflow", "new", "release"); code != 1 || !strings.Contains(errs, "already exists") {
		t.Fatalf("again = %d %q", code, errs)
	}
	out, errs, code := f.flint("", "workflow", "coordinate", "--goal", "Ship the release")
	if code != 0 || !strings.Contains(out, "Ship the release") || !strings.Contains(errs, "Run and steer workflows") {
		t.Fatalf("coordinate = %d\n%s\n%s", code, out, errs)
	}
	outside := filepath.Dir(f.root)
	if _, errs, code := f.flint("", "workflow", "list", "--folder", outside); code != 1 || !strings.Contains(errs, "is not in an open workspace folder") {
		t.Fatalf("outside the workspace = %d %q", code, errs)
	}
	f.h.SetUntrusted(f.root)
	if _, errs, code := f.flint("", "workflow", "run", "feature", "--task", "x"); code != 1 || !strings.Contains(errs, "Restricted Mode") {
		t.Fatalf("untrusted = %d %q", code, errs)
	}
}

func TestRecheckCLI(t *testing.T) {
	f := newCLIFixture(t)
	v := f.start("feature")
	r := f.finishFirst(v.ID, "Question?\nRESULT: BLOCKED")
	if out, _, code := f.flint("", "run", "show", v.ID); code != 0 || !strings.Contains(out, "You can: send-back, check, retry, continue, stop") || !strings.Contains(out, "State:   Failed, needs you") {
		t.Fatalf("run show:\n%s", out)
	}
	if out, _, _ := f.flint("", "run", "show", v.ID); !strings.Contains(out, "Answer the agent in the step's chat in Flint, then: flint run check "+v.ID+" (or run retry, run continue)") {
		t.Fatalf("run show has no advice line:\n%s", out)
	}
	f.h.SetChatOutput(r.Records[0].Chat, "Answered.\nRESULT: DONE")
	if out, errs, code := f.flint("", "run", "check", v.ID); code != 0 {
		t.Fatalf("run check = %d\n%s\n%s", code, out, errs)
	}
	f.waitFor(v.ID, "the second step", func(r Run) bool { return r.Current == 1 })
	if _, errs, code := f.flint("", "run", "check", v.ID); code != 1 || !strings.Contains(errs, "failed at an agent or review step") {
		t.Fatalf("run check of a running run = %d %q", code, errs)
	}
}

func TestCLIWordsAndWait(t *testing.T) {
	// The run's states in the UI's words; ids stay in JSON.
	for _, tc := range []struct {
		v    RunView
		want string
	}{
		{RunView{State: StateRunning}, "Running"},
		{RunView{State: StateRunning, Paused: true}, "Running (pausing)"},
		{RunView{State: StateWaiting, NeedsUser: true}, "Waiting for you"},
		{RunView{State: StateFailed, NeedsUser: true}, "Failed, needs you"},
		{RunView{State: StatePaused}, "Paused"},
		{RunView{State: StatePaused, Hold: &Hold{Kind: LimitAgent, Until: 5}}, "Paused by a limit"},
		{RunView{State: StatePaused, Hold: &Hold{Kind: LimitBudget}, NeedsUser: true}, "Paused by a limit, needs you"},
		{RunView{State: StateDone}, "Done"},
		{RunView{State: StateStopped}, "Stopped"},
		{RunView{State: StateRunning, Queued: true}, "Queued"},
	} {
		if got := stateText(tc.v); got != tc.want {
			t.Errorf("stateText(%+v) = %q, want %q", tc.v, got, tc.want)
		}
	}
	for _, text := range []string{stepTable([]StepView{{Title: "A", Kind: "agent", State: StepDone, Attempts: 2, Skipped: true}}, true)} {
		if !strings.Contains(text, "Done (attempt 2) (continued anyway)") || strings.Contains(text, "skipped") {
			t.Errorf("step table:\n%s", text)
		}
	}
	// run wait ends at a pause of the user's or a limit that does not resume
	// by itself, not at a limit that resumes by itself.
	r := start(t, newTestRun(), 1)
	for name, tc := range map[string]struct {
		run  Run
		want bool
	}{
		"running":                   {r, false},
		"waiting":                   {Run{State: StateWaiting}, true},
		"failed":                    {Run{State: StateFailed}, true},
		"done":                      {Run{State: StateDone}, true},
		"paused by the user":        {Run{State: StatePaused, Paused: true}, true},
		"limit that resumes itself": {r.Held(LimitAgent, "q", 100, 2), false},
		"limit that waits for you":  {r.Held(LimitAgent, "q", 0, 2), true},
	} {
		if got := waitDone(tc.run); got != tc.want {
			t.Errorf("waitDone(%s) = %v, want %v", name, got, tc.want)
		}
	}
}

func TestCLINoteBound(t *testing.T) {
	f := newCLIFixture(t)
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: BLOCKED") // the run waits for the user
	// 4,001 characters, 8,002 bytes: the same refusal as the bound API's.
	long := strings.Repeat("ż", MaxNote+1)
	if _, errs, code := f.flint("", "run", "send-back", v.ID, "--step", "plan", "--note", long); code != 1 || !strings.Contains(errs, "the note is longer than 4000 characters") {
		t.Fatalf("a long note = %d %q", code, errs)
	}
	// 4,000 characters of two bytes each are accepted.
	if _, errs, code := f.flint("", "run", "send-back", v.ID, "--step", "plan", "--note", strings.Repeat("ż", MaxNote)); code != 0 {
		t.Fatalf("a note of %d characters = %d %q", MaxNote, code, errs)
	}
}
