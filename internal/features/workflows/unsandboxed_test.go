package workflows

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"flint/internal/feature"
	"flint/internal/mcptools"
)

// consentFixture is a fixture whose checkout offers the workflow "check"
// (a script step running "test", which resolves to a command line with a
// token in it), with the sandbox off (the default).
func consentFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.sandboxOff() // the dialog applies only after the user switched the sandbox off
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	f.h.SetRunLaunch(f.root, "test", feature.RunLaunch{Name: "test", Cwd: ".", Commands: []feature.RunCommand{{
		Line: "npm run test -- --token ghp_abcdefghijklmnopqrstuvwxyz0123456789", Program: "npm",
		Args: []string{"run", "test", "--", "--token", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
	}}})
	return f
}

// startCheck starts "check" as Run Workflow… does.
func (f *fixture) startCheck(dir string) (RunView, error) {
	return f.api.Start(StartRequest{Dir: dir, Workflow: "check", Description: "Fix it"})
}

func TestUnsandboxedScriptsAskOnceAndRemember(t *testing.T) {
	f := consentFixture(t)

	// Cancel (the default, "" from the dialog): nothing is made.
	if _, err := f.startCheck(f.root); err == nil || err.Error() != errUnsandboxedDeclined.Error() {
		t.Fatalf("declined start: %v", err)
	}
	if len(f.h.Requested()) != 0 || len(f.e.list()) != 0 || f.h.State(stateUnsandboxed) != nil {
		t.Fatalf("a declined start made something: %d requests, %d runs, state %s", len(f.h.Requested()), len(f.e.list()), f.h.State(stateUnsandboxed))
	}
	asked := f.h.Asked()
	if len(asked) != 1 {
		t.Fatalf("asked %d times", len(asked))
	}
	for _, want := range []string{
		unsandboxedTitle + "\n",
		f.root,
		"- Test (test): npm run test -- --token [github token]",
		"These commands run on code an agent wrote, with your access to this computer. A sandbox limits them to the run's folder (Settings ▸ Workflows).",
	} {
		if !strings.Contains(asked[0], want) {
			t.Errorf("the question lacks %q:\n%s", want, asked[0])
		}
	}
	if strings.Contains(asked[0], "ghp_") {
		t.Errorf("the question shows a token:\n%s", asked[0])
	}

	// Run without a sandbox: it starts and the checkout is remembered.
	f.h.Choice = unsandboxedButton
	if _, err := f.startCheck(f.root); err != nil {
		t.Fatal(err)
	}
	if !f.api.unsandboxedOK(f.root) || len(f.h.Requested()) != 1 {
		t.Fatalf("accepted: ok %v, %d requests", f.api.unsandboxedOK(f.root), len(f.h.Requested()))
	}
	// Not asked again, even when the dialog would say no now.
	f.h.Choice = ""
	if _, err := f.startCheck(filepath.Join(f.root)); err != nil {
		t.Fatal(err)
	}
	if n := len(f.h.Asked()); n != 2 {
		t.Errorf("asked %d times in all, want 2", n)
	}
}

// Two starts at once in one folder ask one question.
func TestUnsandboxedOneQuestionAtATime(t *testing.T) {
	f := consentFixture(t)
	f.h.Choice = unsandboxedButton
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() { _, errs[i] = f.startCheck(f.root) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if n := len(f.h.Asked()); n != 1 {
		t.Errorf("asked %d times", n)
	}
}

// Nothing is asked when no script step would run unsandboxed: the sandbox
// is on (available: it confines them; unavailable: they fail closed), or
// the workflow has no script step.
func TestUnsandboxedNotAskedWhenSandboxedOrNoScripts(t *testing.T) {
	for _, tc := range []struct {
		name, workflow, sandbox string
		available               bool
	}{
		{"sandbox on", "check", SandboxOn, true},
		{"sandbox unavailable", "check", SandboxOn, false},
		{"no script step", "feature", SandboxOff, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := consentFixture(t)
			f.settings(base(func(s *Settings) { s.Sandbox = tc.sandbox }))
			f.h.SetScriptSandbox(feature.SandboxSupport{Available: tc.available})
			if _, err := f.api.Start(StartRequest{Dir: f.root, Workflow: tc.workflow, Description: "Fix it"}); err != nil {
				t.Fatal(err)
			}
			if len(f.h.Asked()) != 0 || f.h.State(stateUnsandboxed) != nil {
				t.Errorf("asked %q, state %s", f.h.Asked(), f.h.State(stateUnsandboxed))
			}
		})
	}
}

// The OK is per checkout: another repository asks again.
func TestUnsandboxedIsPerCheckout(t *testing.T) {
	f := consentFixture(t)
	other := filepath.Join(tempDir(t), "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(ProjectDir(other), "check.md"), scriptWorkflow)
	f.h.SetRoots(f.root, other)
	f.h.SetRepository(other)
	f.h.SetTrusted(other)
	f.allowUnsandboxed()
	if _, err := f.startCheck(f.root); err != nil {
		t.Fatal(err)
	}
	if _, err := f.startCheck(other); err == nil || len(f.h.Asked()) != 1 || !strings.Contains(f.h.Asked()[0], other) {
		t.Fatalf("the other checkout: %v, asked %q", err, f.h.Asked())
	}
	// A folder inside the accepted checkout is that checkout.
	sub := filepath.Join(f.root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := f.startCheck(sub); err != nil || len(f.h.Asked()) != 1 {
		t.Fatalf("a folder in the accepted checkout: %v, asked %d", err, len(f.h.Asked()))
	}
}

// An agent's start_workflow never asks: it is refused before the user is
// asked to approve the call, until the user ran such a workflow there.
func TestUnsandboxedAgentStartRefusedUntilTheUserSaidYes(t *testing.T) {
	f := consentFixture(t)
	f.allowTools(true)
	f.h.Choice = unsandboxedButton // would say yes: it must not be asked
	args := `{"workflow":"check","task":"Fix it"}`
	_, err := f.call("chat-1", f.root, mcptools.ToolWorkflowStart, args)
	if err == nil || !strings.Contains(err.Error(), "ask the user to run this workflow once themselves in Flint") {
		t.Fatalf("agent start before the user's OK: %v", err)
	}
	if len(f.h.Asked()) != 0 || len(f.h.Requested()) != 0 || f.api.unsandboxedOK(f.root) {
		t.Fatalf("asked %q, %d requests", f.h.Asked(), len(f.h.Requested()))
	}
	// The run step (after the user's yes to the call) refuses it too.
	if _, err := f.api.agentStart(t.Context(), f.root, runRequest{dir: f.root, workflow: "check", description: "x", origin: OriginAgent, consent: consentAgent}); err == nil {
		t.Fatal("agentStart started unsandboxed scripts the user never allowed")
	}
	// The user runs it once in Flint; then the agent may.
	if _, err := f.startCheck(f.root); err != nil {
		t.Fatal(err)
	}
	if _, err := f.call("chat-1", f.root, mcptools.ToolWorkflowStart, args); err != nil {
		t.Fatalf("agent start after the user's OK: %v", err)
	}
	if n := len(f.h.Asked()); n != 1 {
		t.Errorf("asked %d times", n)
	}
}

// A schedule never asks: it is skipped until the user said yes there.
func TestUnsandboxedScheduleSkippedUntilTheUserSaidYes(t *testing.T) {
	f := newSchedFixture(t)
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	f.h.SetRunLaunch(f.root, "test", feature.RunLaunch{Name: "test", Cwd: ".", Commands: []feature.RunCommand{{Line: "npm run test", Program: "npm", Args: []string{"run", "test"}}}})
	f.h.Choice = unsandboxedButton // would say yes: it must not be asked
	sc := nightly(f.root)
	sc.Workflow = "check"
	f.settings(base(func(s *Settings) { s.Sandbox, s.Schedules = SandboxOff, []Schedule{sc} })) // the sandbox switched off
	f.s.arm()
	f.s.sync()
	f.s.syncing.Wait()
	f.setNow(2026, 11, 22, 2, 30)
	f.fire(f.armed())
	v := f.view("nightly")
	if v.LastRun != "" || v.LastSkipped != "script steps here still need your OK to run without a sandbox: run this workflow once yourself, or switch the sandbox on" {
		t.Fatalf("before the user's OK: %+v", v)
	}
	if len(f.h.Asked()) != 0 || len(f.h.Requested()) != 0 {
		t.Fatalf("asked %q, %d requests", f.h.Asked(), len(f.h.Requested()))
	}
	f.allowUnsandboxed()
	f.setNow(2026, 11, 23, 2, 30)
	f.fire(f.armed())
	if v := f.view("nightly"); v.LastRun == "" || v.LastSkipped != "" {
		t.Fatalf("after the user's OK: %+v", v)
	}
}

// A project item's Delegate (TaskWorkflow) asks like Run Workflow…; no is
// a refusal before any run is planned.
func TestUnsandboxedDelegateAsks(t *testing.T) {
	f := consentFixture(t)
	req := feature.NewAgentTask{Dir: f.root, Workflow: "check", Description: "Fix it", Agent: "claude"}
	if _, err := f.api.taskWorkflow(t.Context(), req); err == nil || err.Error() != errUnsandboxedDeclined.Error() {
		t.Fatalf("declined: %v", err)
	}
	if len(f.h.Asked()) != 1 || len(f.e.list()) != 0 || len(f.e.runs) != 0 {
		t.Fatalf("asked %d, %d runs planned", len(f.h.Asked()), len(f.e.runs))
	}
	f.h.Choice = unsandboxedButton
	p, err := f.api.taskWorkflow(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	p.Drop()
	if !f.api.unsandboxedOK(f.root) {
		t.Error("the yes was not remembered")
	}
}

// flint workflow run asks in Flint like Run Workflow….
func TestUnsandboxedCLIRunAsks(t *testing.T) {
	f := newCLIFixture(t)
	f.sandboxOff()
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	_, errs, code := f.flint("", "workflow", "run", "check", "--task", "Fix it")
	if code == 0 || !strings.Contains(errs, "script steps need your OK to run without a sandbox") || len(f.h.Asked()) != 1 {
		t.Fatalf("declined: code %d %q, asked %d", code, errs, len(f.h.Asked()))
	}
	f.h.Choice = unsandboxedButton
	if _, errs, code := f.flint("", "workflow", "run", "check", "--task", "Fix it"); code != 0 {
		t.Fatalf("accepted: code %d %s", code, errs)
	}
	if !f.api.unsandboxedOK(f.root) {
		t.Error("the yes was not remembered")
	}
}

// The Listing says whether the folder's checkout was accepted, and
// ForgetUnsandboxed takes it back.
func TestForgetUnsandboxed(t *testing.T) {
	f := consentFixture(t)
	l, err := f.api.List(f.root)
	if err != nil || l.UnsandboxedOK {
		t.Fatalf("before: %v %v", l.UnsandboxedOK, err)
	}
	f.allowUnsandboxed()
	if l, _ := f.api.List(f.root); !l.UnsandboxedOK {
		t.Fatal("the listing does not show the OK")
	}
	for _, bad := range []string{"", "relative/path", "/" + strings.Repeat("a", maxPath)} {
		if err := f.api.ForgetUnsandboxed(bad); err == nil {
			t.Errorf("ForgetUnsandboxed(%q) accepted", bad)
		}
	}
	// A folder inside the checkout names the checkout.
	sub := filepath.Join(f.root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.api.ForgetUnsandboxed(sub); err != nil {
		t.Fatal(err)
	}
	if l, _ := f.api.List(f.root); l.UnsandboxedOK || f.api.unsandboxedOK(f.root) {
		t.Fatal("the OK is still there")
	}
	if _, err := f.startCheck(f.root); err == nil || len(f.h.Asked()) != 1 {
		t.Fatalf("not asked again: %v", err)
	}
	// A folder that is no longer open is forgotten by its path.
	gone := "/no/longer/open"
	f.api.rememberUnsandboxed(gone)
	if err := f.api.ForgetUnsandboxed(gone); err != nil || f.api.unsandboxedOK(gone) {
		t.Fatalf("forget a closed folder: %v", err)
	}
}

// The list keeps at most maxUnsandboxed folders, dropping the oldest, and
// drops what is not a clean absolute path when read.
func TestUnsandboxedStateIsBounded(t *testing.T) {
	f := newFixture(t)
	for i := range maxUnsandboxed + 5 {
		f.api.rememberUnsandboxed(fmt.Sprintf("/repo/%03d", i))
	}
	// Accepting one again makes it the newest.
	f.api.rememberUnsandboxed("/repo/010")
	list := f.api.loadUnsandboxed()
	if len(list) != maxUnsandboxed || list[len(list)-1] != "/repo/010" {
		t.Fatalf("%d folders, newest %q", len(list), list[len(list)-1])
	}
	for _, dropped := range []string{"/repo/000", "/repo/004"} {
		if f.api.unsandboxedOK(dropped) {
			t.Errorf("%s is kept", dropped)
		}
	}
	if !f.api.unsandboxedOK("/repo/005") || !f.api.unsandboxedOK("/repo/010") {
		t.Error("a recent folder was dropped")
	}
	if err := f.h.SaveState(stateUnsandboxed, []string{"relative", "/a/../b", "", "/ok", "/ok"}); err != nil {
		t.Fatal(err)
	}
	if got := f.api.loadUnsandboxed(); !slices.Equal(got, []string{"/ok"}) {
		t.Errorf("loaded %q", got)
	}
}
