package projects

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"flint/internal/feature"
)

// fakeRuns plays the Workflows feature for the fake host: it plans a run for
// every request that names a workflow it knows, binds it to its task, and
// answers TaskRun with a state the test sets.
type fakeRuns struct {
	mu      sync.Mutex
	known   []string
	runs    map[string]feature.TaskRun // by task
	asked   []feature.NewAgentTask
	dropped int
}

func (f *fixture) withRuns(known ...string) *fakeRuns {
	r := &fakeRuns{known: known, runs: map[string]feature.TaskRun{}}
	f.h.SetTaskWorkflow(r.plan)
	f.h.SetTaskRuns(r.run)
	return r
}

func (r *fakeRuns) plan(_ context.Context, req feature.NewAgentTask) (feature.PlannedWorkflow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, req)
	if !slices.Contains(r.known, req.Workflow) {
		return feature.PlannedWorkflow{}, errors.New(`there is no workflow named "` + req.Workflow + `" here`)
	}
	return feature.PlannedWorkflow{
		Agent: "backend", // the first step's
		Bind: func(taskID string) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.runs[taskID] = feature.TaskRun{ID: "r-" + taskID, Workflow: req.Workflow, State: "running"}
			return nil
		},
		Drop: func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.dropped++
		},
	}, nil
}

func (r *fakeRuns) run(taskID string) (feature.TaskRun, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[taskID]
	return run, ok
}

func (r *fakeRuns) set(taskID, state string) { r.setAt(taskID, state, false) }

// setAt sets the run's state and whether its current step is the last.
func (r *fakeRuns) setAt(taskID, state string, last bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runs[taskID]
	run.State, run.AtLastStep = state, last
	r.runs[taskID] = run
}

func TestParseItemWorkflow(t *testing.T) {
	c := Config{Repositories: []RepositoryDecl{{Name: "api", URL: "git@github.com:acme/api.git"}, {Name: "web", URL: "git@github.com:acme/web.git"}}}
	for _, tc := range []struct {
		name, extra, want, err string
	}{
		{"none", "", "", ""},
		{"empty", "workflow:", "", ""},
		{"named", "workflow: bug-fix", "bug-fix", ""},
		{"with several repos, one run each", "repos: api, web\nworkflow: bug-fix", "bug-fix", ""},
		{"a path", "workflow: ../x", "", `workflow "../x": use the name of a workflow`},
		{"spaces", "workflow: bug fix", "", `workflow "bug fix"`},
		{"upper case", "workflow: Bug-Fix", "", `workflow "Bug-Fix": use the name of a workflow (lowercase letters, digits and dashes`},
		{"underscore", "workflow: bug_fix", "", `workflow "bug_fix"`},
		{"too long", "workflow: " + strings.Repeat("a", 65), "", "use the name of a workflow"},
		{"digits first", "workflow: 2fast", "2fast", ""},
		{"with together", "repos: api, web\ntogether: true\nworkflow: bug-fix", "", "a workflow runs in one repository: remove together or workflow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it, err := ParseItem("x", []byte(item("X", "todo", tc.extra)), c)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("ParseItem = %+v, %v; want %q", it, err, tc.err)
				}
				return
			}
			if err != nil || it.Workflow != tc.want {
				t.Fatalf("ParseItem = %+v, %v", it, err)
			}
		})
	}
}

func TestBuildItemFileWithWorkflow(t *testing.T) {
	c := Config{Repositories: []RepositoryDecl{{Name: "api", URL: "git@github.com:acme/api.git"}}}
	data, err := BuildItemFile(ItemSpec{Title: "Fix it", Text: "Fix.", Repos: []string{"api"}, Workflow: "bug-fix"}, "fix-it", c)
	if err != nil || string(data) != "---\ntitle: Fix it\nstatus: todo\npriority: normal\nrepos: api\nworkflow: bug-fix\n---\nFix.\n" {
		t.Fatalf("BuildItemFile = %q, %v", data, err)
	}
	for _, bad := range []ItemSpec{
		{Title: "X", Workflow: "../x"},
		{Title: "X", Workflow: "a\nstatus: done"},
		{Title: "X", Workflow: "Bug-Fix"},
		{Title: "X", Workflow: "bug_fix"},
	} {
		if _, err := BuildItemFile(bad, "x", c); err == nil {
			t.Errorf("BuildItemFile(%+v) accepted", bad)
		}
	}
	// The status line is rewritten byte for byte, the workflow line kept.
	file := "---\r\ntitle: Fix it\r\nworkflow:  bug-fix\r\nstatus: todo\r\n---\r\nFix.\r\n"
	out, err := SetStatus([]byte(file), StatusDoing)
	if err != nil || string(out) != strings.Replace(file, "status: todo", "status: doing", 1) {
		t.Fatalf("SetStatus = %q, %v", out, err)
	}
}

func TestNextStatusFollowsTheRun(t *testing.T) {
	run := func(state string) Link {
		// The task finished a turn (a step): only the run says how it goes.
		return Link{Task: "t-" + state, State: "finished", Workflow: "bug-fix", RunID: "r", Run: state}
	}
	last := func(state string) Link {
		l := run(state)
		l.RunLast = true
		return l
	}
	plain := Link{Task: "p", State: "finished"}
	for _, tc := range []struct {
		name   string
		status string
		links  []Link
		want   string
		failed bool
	}{
		{"running", "doing", []Link{run("running")}, "doing", false},
		{"paused", "doing", []Link{run("paused")}, "doing", false},
		{"waiting at the last gate", "doing", []Link{last("waiting")}, "review", false},
		{"waiting at a gate with steps after it", "doing", []Link{run("waiting")}, "doing", false},
		{"waiting at a middle gate, one plain finished", "doing", []Link{run("waiting"), plain}, "doing", false},
		{"waiting at the last gate, one plain finished", "doing", []Link{last("waiting"), plain}, "review", false},
		{"running at the last step is still working", "doing", []Link{last("running")}, "doing", false},
		{"done", "doing", []Link{run("done")}, "review", false},
		{"failed", "doing", []Link{run("failed")}, "doing", true},
		{"stopped", "doing", []Link{run("stopped")}, "doing", true},
		{"one running, one plain finished", "doing", []Link{run("running"), plain}, "doing", false},
		{"done and merged", "review", []Link{{Task: "m", State: "idle", Merged: true, Workflow: "bug-fix", Run: "done"}}, "done", false},
		{"a failed run's task turn says finished", "doing", []Link{{Task: "f", State: "failed", Workflow: "bug-fix", Run: "waiting", RunLast: true}}, "review", false},
		{"removed after its pull request", "doing", []Link{{Task: "g", Gone: true, PullRequest: "https://x/pr/1", Workflow: "bug-fix", Run: "running"}}, "done", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextStatus(tc.status, tc.links); got != tc.want {
				t.Errorf("NextStatus = %s, want %s", got, tc.want)
			}
			if got := Failed(tc.links); got != tc.failed {
				t.Errorf("Failed = %v, want %v", got, tc.failed)
			}
		})
	}
}

func TestRefreshTakesTheRun(t *testing.T) {
	links := []Link{{Task: "a", State: "working"}}
	got, changed := Refresh(links, map[string]feature.AgentTask{"a": {ID: "a", State: "finished", Run: feature.TaskRun{ID: "r1", Workflow: "bug-fix", State: "waiting", AtLastStep: true}}})
	if !changed || got[0] != (Link{Task: "a", State: "finished", Workflow: "bug-fix", RunID: "r1", Run: "waiting", RunLast: true}) {
		t.Fatalf("Refresh = %+v, %v", got, changed)
	}
	// A run that was removed (its task stays) keeps the last state.
	again, changed := Refresh(got, map[string]feature.AgentTask{"a": {ID: "a", State: "finished"}})
	if changed || again[0].Run != "waiting" {
		t.Errorf("after the run was removed: %+v, %v", again, changed)
	}
}

func TestDelegateAnItemThatRunsAWorkflow(t *testing.T) {
	f := newFixture(t, map[string]string{"fx": item("Fix", "todo", "repos: api", "agent: claude", "workflow: bug-fix")})
	runs := f.withRuns("bug-fix")
	f.open()
	res := f.delegate("fx")
	reqs := f.h.Requested()
	if len(reqs) != 1 || reqs[0][0].Workflow != "bug-fix" || reqs[0][0].Agent != "claude" {
		t.Fatalf("requests %+v", reqs)
	}
	if len(runs.asked) != 1 || runs.asked[0].Dir != f.root || runs.asked[0].Description != "Fix\n\nDo Fix." {
		t.Fatalf("planned %+v", runs.asked)
	}
	task := res.Tasks[0]
	if res.Item.Workflow != "bug-fix" || task.Workflow != "bug-fix" || task.RunState != "running" || task.Run != "r-"+task.ID || task.Agent != "backend" {
		t.Fatalf("result %+v", res)
	}
	if ls := f.links("fx"); len(ls) != 1 || ls[0].Workflow != "bug-fix" || ls[0].Run != "running" {
		t.Fatalf("links %+v", ls)
	}
	// Every step ends a turn of the task: the item stays in doing while the
	// run runs, goes to review when the run waits at its gate.
	f.h.UpdateAgentTask(task.ID, func(t *feature.AgentTask) { t.State = "finished" })
	if v := f.item(f.load(), "fx"); v.Status != StatusDoing || v.Tasks[0].RunState != "running" {
		t.Fatalf("after a step: %+v", v)
	}
	runs.set(task.ID, "waiting") // an approval with steps after it
	if v := f.item(f.load(), "fx"); v.Status != StatusDoing || v.Tasks[0].RunState != "waiting" {
		t.Fatalf("at a middle gate: %+v", v)
	}
	runs.setAt(task.ID, "waiting", true)
	if v := f.item(f.load(), "fx"); v.Status != StatusReview || v.Failed || v.Tasks[0].RunState != "waiting" {
		t.Fatalf("at the gate: %+v", v)
	}
	// Moved back by the user, then the run fails: doing, marked.
	if _, err := f.api.SetStatus(f.plan, "fx", StatusDoing); err != nil {
		t.Fatal(err)
	}
	runs.set(task.ID, "failed")
	if v := f.item(f.load(), "fx"); v.Status != StatusDoing || !v.Failed {
		t.Fatalf("failed run: %+v", v)
	}
}

func TestDelegateAWorkflowItemRefused(t *testing.T) {
	f := newFixture(t, map[string]string{
		"fx": item("Fix", "todo", "repos: api", "workflow: bug-fix"),
		"nx": item("Nope", "todo", "repos: api", "workflow: nope"),
	})
	f.open()
	// Workflows switched off: nothing is made, the item stays.
	_, err := f.api.Delegate(f.plan, "fx", f.root)
	wantErr(t, err, "workflows are switched off (Settings ▸ Extensions ▸ Built-in)")
	runs := f.withRuns("bug-fix")
	_, err = f.api.Delegate(f.plan, "nx", f.root)
	wantErr(t, err, `there is no workflow named "nope" here`)
	if len(f.h.AllAgentTasks()) != 0 || len(f.links("fx")) != 0 || len(f.links("nx")) != 0 || runs.dropped != 0 {
		t.Fatalf("tasks %v, dropped %d", f.h.AllAgentTasks(), runs.dropped)
	}
	if !strings.Contains(f.read("fx"), "status: todo") {
		t.Errorf("file %q", f.read("fx"))
	}
}

func TestUntrustedDelegateNamesTheWorkflow(t *testing.T) {
	f := newFixture(t, map[string]string{"fx": item("Fix", "todo", "repos: api", "workflow: bug-fix")})
	f.withRuns("bug-fix")
	f.open()
	f.h.SetUntrusted(f.plan)
	f.h.Answer = false
	_, err := f.api.Delegate(f.plan, "fx", f.root)
	wantErr(t, err, "Delegate was cancelled")
	asked := f.h.Asked()
	if len(asked) != 1 || !strings.Contains(asked[0], `The item runs the workflow "bug-fix"`) ||
		!strings.Contains(asked[0], "never from the project's folder") {
		t.Fatalf("asked %q", asked)
	}
	if len(f.h.Requested()) != 0 {
		t.Error("No still created tasks")
	}
}

func TestWorkflowInToolsAndPlan(t *testing.T) {
	f := newFixture(t, map[string]string{"fx": item("Fix", "todo", "repos: api", "workflow: bug-fix")})
	f.open()
	if out := f.api.describe("", ""); !strings.Contains(out, "; workflow: bug-fix (a workflow run takes each of its tasks)") {
		t.Errorf("get_project:\n%s", out)
	}
	f.allowAdd()
	out, err := f.add(`{"project":"Acme","title":"Second fix","text":"Fix more.","repos":["api"],"workflow":"bug-fix"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "second-fix") || !strings.Contains(f.read("second-fix"), "\nworkflow: bug-fix\n") {
		t.Errorf("added %q:\n%s", out, f.read("second-fix"))
	}
	if _, err := f.add(`{"project":"Acme","title":"Third","text":"","repos":["api"],"workflow":"../x"}`); err == nil {
		t.Error("a bad workflow name was accepted")
	}
	p, err := f.api.planAdd(t.Context(), f.root, []byte(`{"project":"Acme","title":"Fourth","text":"","workflow":"bug-fix"}`))
	if err != nil || !strings.Contains(p.Summary, "Workflow: bug-fix") {
		t.Errorf("summary %q, %v", p.Summary, err)
	}
	pp, err := f.api.PlanPrompt(f.plan, "Ship it")
	if err != nil || !strings.Contains(pp.Prompt, "- workflow (optional):") {
		t.Errorf("plan prompt: %v\n%s", err, pp.Prompt)
	}
}

func TestNewItemWithWorkflow(t *testing.T) {
	f := newFixture(t, nil)
	f.open()
	it, err := f.api.NewItemWith(f.plan, NewItemRequest{Title: "Fix the login", Workflow: "bug-fix"})
	if err != nil || it.Workflow != "bug-fix" {
		t.Fatalf("NewItemWith = %+v, %v", it, err)
	}
	file := f.read(it.ID)
	if !strings.Contains(file, "\nworkflow: bug-fix\n") || strings.Contains(file, "# workflow:") {
		t.Errorf("file:\n%s", file)
	}
	// No workflow: the commented example, like NewItem.
	plain, err := f.api.NewItemWith(f.plan, NewItemRequest{Title: "Plain one"})
	if err != nil || plain.Workflow != "" || !strings.Contains(f.read(plain.ID), "# workflow: bug-fix") {
		t.Fatalf("without a workflow: %+v, %v", plain, err)
	}
	// NewItem stays a thin wrapper.
	old, err := f.api.NewItem(f.plan, "Old way")
	if err != nil || old.Workflow != "" {
		t.Fatalf("NewItem = %+v, %v", old, err)
	}
	for _, bad := range []string{"Bug-Fix", "bug_fix", "../x", "a\nstatus: done", "-x", strings.Repeat("a", 65)} {
		if _, err := f.api.NewItemWith(f.plan, NewItemRequest{Title: "Bad " + bad[:1], Workflow: bad}); err == nil {
			t.Errorf("workflow %q accepted", bad)
		}
	}
	for _, id := range []string{"bad-b", "bad-.", "bad-a", "bad--"} {
		if _, err := os.Lstat(ItemFile(f.root, id)); err == nil {
			t.Errorf("item %s was written for a refused workflow", id)
		}
	}
}

func TestNextStatusAfterTakesReviewBack(t *testing.T) {
	link := func(state string, last bool) Link {
		return Link{Task: "t", State: "finished", Workflow: "bug-fix", RunID: "r", Run: state, RunLast: last}
	}
	plain := Link{Task: "p", State: "finished"}
	for _, tc := range []struct {
		name        string
		status      string
		prev, links []Link
		want        string
	}{
		{"sent back from the last approval", "review", []Link{link("waiting", true)}, []Link{link("running", false)}, "doing"},
		{"sent back, another task finished", "review", []Link{link("waiting", true), plain}, []Link{link("running", false), plain}, "doing"},
		{"the user moved it to review while the run ran", "review", []Link{link("running", false)}, []Link{link("running", true)}, "review"},
		{"the user moved it to review at a middle approval", "review", []Link{link("waiting", false)}, []Link{link("running", false)}, "review"},
		{"still waiting at the last approval", "review", []Link{link("waiting", true)}, []Link{link("waiting", true)}, "review"},
		{"failed again stays in review", "review", []Link{link("waiting", true)}, []Link{link("failed", false)}, "review"},
		{"done", "review", []Link{link("waiting", true)}, []Link{link("done", true)}, "review"},
		{"merged", "review", []Link{link("done", true)}, []Link{{Task: "t", Merged: true, Workflow: "bug-fix", Run: "running"}}, "done"},
		{"doing stays doing", "doing", []Link{link("waiting", true)}, []Link{link("running", false)}, "doing"},
		{"back at the last approval", "doing", []Link{link("running", true)}, []Link{link("waiting", true)}, "review"},
		{"a plain task is not taken back", "review", []Link{plain}, []Link{{Task: "p", State: "working"}}, "review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextStatusAfter(tc.status, tc.prev, tc.links); got != tc.want {
				t.Errorf("NextStatusAfter = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestItemGoesBackToDoingWhenTheRunWorksAgain(t *testing.T) {
	f := newFixture(t, map[string]string{"fx": item("Fix", "todo", "repos: api", "agent: claude", "workflow: bug-fix")})
	runs := f.withRuns("bug-fix")
	f.open()
	task := f.delegate("fx").Tasks[0]
	f.h.UpdateAgentTask(task.ID, func(t *feature.AgentTask) { t.State = "finished" })
	runs.setAt(task.ID, "waiting", true)
	if v := f.item(f.load(), "fx"); v.Status != StatusReview {
		t.Fatalf("at the last approval: %+v", v)
	}
	// Sent back: the agents work again, the board follows.
	runs.setAt(task.ID, "running", false)
	if v := f.item(f.load(), "fx"); v.Status != StatusDoing || v.Tasks[0].RunState != "running" {
		t.Fatalf("after the send-back: %+v", v)
	}
	runs.setAt(task.ID, "waiting", true)
	if v := f.item(f.load(), "fx"); v.Status != StatusReview {
		t.Fatalf("at the last approval again: %+v", v)
	}
	// The user moves it to doing and back to review by hand while the run
	// works: a later change of the tasks does not undo the user's choice.
	runs.setAt(task.ID, "running", false)
	f.item(f.load(), "fx")
	if _, err := f.api.SetStatus(f.plan, "fx", StatusReview); err != nil {
		t.Fatal(err)
	}
	runs.setAt(task.ID, "paused", false)
	runs.setAt(task.ID, "running", false)
	if v := f.item(f.load(), "fx"); v.Status != StatusReview {
		t.Fatalf("the user's move was undone: %+v", v)
	}
}
