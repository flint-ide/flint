package workflows

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"flint/internal/agenttask"
	"flint/internal/feature"
)

// A script that changed what later steps run (RunResult.Changed) stops the
// run: the step fails, nothing is sent back for rework, no chat starts.
func TestScriptThatChangedProtectedPathsStopsTheRun(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	f.allowUnsandboxed()
	// It even "failed", which would send the work back to Plan.
	f.h.SetRunResult("test", feature.RunResult{ID: "run-1", Code: 1, Changed: []string{".claude/settings.json", "CLAUDE.md"}}, nil)
	v := f.start("check")
	f.finishFirst(v.ID, "RESULT: DONE")
	r := f.waitFor(v.ID, "the stop", func(r Run) bool { return r.Over() })
	if r.State != StateStopped || r.Current != 1 || r.Records[1].State != StepFailed || r.Records[0].State != StepDone {
		t.Fatalf("run %s at %d, records %+v", r.State, r.Current, r.Records)
	}
	want := "Stopped: the script changed .claude/settings.json, CLAUDE.md, which later steps would run: the run stopped"
	if last := r.Log[len(r.Log)-1].Text; last != want {
		t.Errorf("log %q", last)
	}
	f.e.wg.Wait()
	if n := len(f.h.StepChats()); n != 0 {
		t.Errorf("%d chats started after the script changed them", n)
	}
}

// A review step's chat is a reviewer's (StepChatOptions.Reviewer); an
// agent step's is not.
func TestReviewStepChatIsAReviewer(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	f.finishChat(v.ID, 1, "RESULT: DONE")
	f.chatOf(v.ID, 2)
	calls := f.h.StepChats()
	if len(calls) != 2 || calls[0].Reviewer || !calls[1].Reviewer {
		t.Fatalf("step chats %+v", calls)
	}
}

// The first step's result is read from the chat Flint reported for the
// task's start, never from another chat open in the worktree; without a
// reported chat the step waits for the user.
func TestFirstStepReadsTheReportedChat(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.taskChat(v.ID, "the-step-chat")
	f.h.SetChatOutput("the-step-chat", "RESULT: BLOCKED")
	// A chat the agent opened later, whose last line forges a pass.
	f.h.SetTaskChat(v.Task, "forged")
	f.h.SetChatOutput("forged", "RESULT: DONE")
	f.h.UpdateAgentTask(v.Task, func(t *feature.AgentTask) { t.State = agenttask.Finished })
	f.e.kickTasks()
	r := f.waitFor(v.ID, "the first step's end", func(r Run) bool { return !byTask(r) })
	if r.State != StateFailed || r.Error != reasonBlocked {
		t.Fatalf("first step: %s %q", r.State, r.Error)
	}

	g := newFixture(t)
	w := g.start("feature")
	g.h.UpdateAgentTask(w.Task, func(t *feature.AgentTask) { t.State = agenttask.Finished })
	g.e.kickTasks()
	r = g.waitFor(w.ID, "the first step's end", func(r Run) bool { return !byTask(r) })
	if r.State != StateFailed || r.Error != reasonNoChat {
		t.Fatalf("without a reported chat: %s %q", r.State, r.Error)
	}
}

// The task of a run that is over, removed or unknown does not start; the
// task of a run waiting for its first step does.
func TestTaskStartCheck(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	st := feature.TaskStart{Origin: originPrefix + v.ID, TaskID: v.Task}
	if err := f.e.taskStartCheck(st); err != nil {
		t.Fatalf("a live run's task: %v", err)
	}
	if err := f.api.Stop(v.ID); err != nil {
		t.Fatal(err)
	}
	for _, s := range []feature.TaskStart{
		st,
		{Origin: "project:x", TaskID: v.Task}, // a run bound to the task
		{Origin: originPrefix + "0123456789abcdef"},         // unknown
		{Origin: originPrefix + "../x", TaskID: "whatever"}, // malformed
	} {
		if err := f.e.taskStartCheck(s); err == nil || err.Error() != "its workflow run was stopped" {
			t.Errorf("%+v: %v", s, err)
		}
	}
	if err := f.e.taskStartCheck(feature.TaskStart{Origin: "project:x", TaskID: agenttask.NewID()}); err != nil {
		t.Errorf("a task no run takes: %v", err)
	}
	if err := f.api.Remove(v.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.e.taskStartCheck(st); err == nil {
		t.Error("the task of a removed run may start")
	}
}

// After Close nothing starts: a change that would start the next step
// leaves it pending, and no chat opens.
func TestNothingStartsAfterClose(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.e.close()
	f.finishFirst(v.ID, "RESULT: DONE")
	f.e.advance(v.ID)
	f.e.wg.Wait()
	r, _ := f.e.get(v.ID)
	if r.Current != 1 || r.Records[1].State != StepPending || len(f.h.StepChats()) != 0 {
		t.Fatalf("after close: current %d, %+v, %d chats", r.Current, r.Records[1], len(f.h.StepChats()))
	}
}

// One step's end that starts the next step is one saved state and one
// event: no state is ever saved or shown with a passed step's successor
// "running" before it started, nor with an approval step "running".
func TestOneEventPerTransition(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	f.allowUnsandboxed()
	f.h.SetRunResult("test", feature.RunResult{ID: "run-1"}, nil)
	var mu sync.Mutex
	var seen []Run
	signal := f.e.changed
	f.e.changed = func() {
		for _, r := range f.e.list() {
			mu.Lock()
			seen = append(seen, r)
			mu.Unlock()
		}
		signal()
	}
	v := f.start("check")
	f.finishFirst(v.ID, "RESULT: DONE")
	f.waitFor(v.ID, "the approval step", func(r Run) bool { return r.State == StateWaiting })
	f.e.wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	for _, r := range seen {
		if r.ID != v.ID || !r.valid() {
			continue
		}
		if r.State == StateRunning && r.Records[r.Current].State == StepPending {
			t.Errorf("a state with step %d running but not started: %+v", r.Current, r.Records)
		}
		if r.Steps[r.Current].Kind == KindGate && r.State == StateRunning {
			t.Errorf("the approval step shown running")
		}
	}
	r, _ := f.e.get(v.ID)
	for _, l := range r.Log {
		if strings.HasPrefix(l.Text, "Started Approve") {
			t.Errorf("the approval step was started: %q", l.Text)
		}
	}
}

// At launch a run between two steps is paused, not advanced; a stored run
// that does not validate is dropped.
func TestRestartPausesARunBetweenSteps(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	f.chatOf(v.ID, 1)
	f.e.close()
	f.e.wg.Wait()
	// Flint quit right after the plan passed: the next step is pending.
	runs := f.savedRuns()
	i := slices.IndexFunc(runs, func(r Run) bool { return r.ID == v.ID })
	runs[i].Records[1] = Record{State: StepPending}
	runs[i].Exec = 1
	bad := runs[i]
	bad.ID = "fedcba9876543210"
	bad.Steps = slices.Clone(bad.Steps)
	bad.Steps[1].Kind = "shell"
	evil := runs[i]
	evil.ID = "0123456789abcdef"
	evil.Steps = slices.Clone(evil.Steps)
	evil.Steps[1].Agent = "../../bin/sh"
	runs = append(runs, bad, evil)
	raw, _ := json.Marshal(runs)
	if err := f.h.SaveState(stateRuns, json.RawMessage(raw)); err != nil {
		t.Fatal(err)
	}
	f.restart()
	r, ok := f.e.get(v.ID)
	if !ok || r.State != StatePaused || !r.Paused || r.Log[len(r.Log)-1].Text != restartedText {
		t.Fatalf("after restart: %+v", r)
	}
	for _, id := range []string{bad.ID, evil.ID} {
		if _, ok := f.e.get(id); ok {
			t.Errorf("the invalid run %s was kept", id)
		}
	}
	f.e.wg.Wait()
	if n := len(f.h.StepChats()); n != 1 {
		t.Errorf("%d chats: a step started at launch", n)
	}
	if err := f.api.Resume(v.ID); err != nil {
		t.Fatal(err)
	}
	f.chatOf(v.ID, 1)
}

func TestValidRun(t *testing.T) {
	good := NewRun("0123456789abcdef", "feature", []Step{{ID: "plan", Title: "Plan", Kind: KindAgent, Text: "Plan it."}}, "Do it", "claude", "/repo", 1)
	good.Task = agenttask.NewID()
	if err := validRun(good); err != nil {
		t.Fatalf("a run this engine makes: %v", err)
	}
	for name, mutate := range map[string]func(r *Run){
		"kind":        func(r *Run) { r.Steps[0].Kind = KindWorkflow },
		"step id":     func(r *Run) { r.Steps[0].ID = "Plan It" },
		"agent":       func(r *Run) { r.Steps[0].Agent = "--yolo" },
		"run agent":   func(r *Run) { r.Agent = "a b" },
		"records":     func(r *Run) { r.Records = nil },
		"state":       func(r *Run) { r.State = "zombie" },
		"current":     func(r *Run) { r.Current = 5 },
		"text":        func(r *Run) { r.Steps[0].Text = strings.Repeat("x", MaxText+1) },
		"hidden":      func(r *Run) { r.Steps[0].Text = "a‮b" },
		"dir":         func(r *Run) { r.Dir = "repo" },
		"task":        func(r *Run) { r.Task = "x" },
		"record chat": func(r *Run) { r.Records[0].Chat = strings.Repeat("c", maxStoredChat+1) },
		"output":      func(r *Run) { r.Records[0].Output = strings.Repeat("o", MaxOutput+1) },
		"script":      func(r *Run) { r.Steps[0].Kind = KindScript; r.Steps[0].Run = "" },
	} {
		r := good
		r.Steps = slices.Clone(good.Steps)
		r.Records = slices.Clone(good.Records)
		mutate(&r)
		if validRun(r) == nil {
			t.Errorf("%s: an invalid run validated", name)
		}
	}
}
