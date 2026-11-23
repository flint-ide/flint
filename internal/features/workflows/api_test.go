package workflows

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"flint/internal/agenttask"
	"flint/internal/feature"
)

func TestStartRefusals(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *fixture)
		req   func(f *fixture) StartRequest
		want  string
	}{
		{"outside the workspace", nil, func(f *fixture) StartRequest {
			return StartRequest{Dir: filepath.Dir(f.root), Workflow: "feature", Description: "x"}
		}, "is not in an open workspace folder"},
		{"relative folder", nil, func(f *fixture) StartRequest {
			return StartRequest{Dir: "repo", Workflow: "feature", Description: "x"}
		}, "choose a workspace folder"},
		{"untrusted", func(f *fixture) { f.h.SetUntrusted(f.root) }, nil, "is in Restricted Mode: trust the folder to run workflows there"},
		{"unknown workflow", nil, func(f *fixture) StartRequest {
			return StartRequest{Dir: f.root, Workflow: "nope", Description: "x"}
		}, `there is no workflow named "nope" here`},
		{"invalid workflow name", nil, func(f *fixture) StartRequest {
			return StartRequest{Dir: f.root, Workflow: "../x", Description: "x"}
		}, "choose a workflow"},
		{"unresolvable", func(f *fixture) {
			writeFile(f.t, filepath.Join(ProjectDir(f.root), "outer.md"), "## Plan\n\nPlan it.\n\n## More\ntype: workflow\nworkflow: missing\n")
		}, func(f *fixture) StartRequest {
			return StartRequest{Dir: f.root, Workflow: "outer", Description: "x"}
		}, `the workflow "outer" cannot run: step "More": no workflow named "missing"`},
		{"a step's agent is missing", func(f *fixture) {
			writeFile(f.t, filepath.Join(ProjectDir(f.root), "planned.md"), "## Plan\nagent: planner\n\nPlan it.\n")
		}, func(f *fixture) StartRequest {
			return StartRequest{Dir: f.root, Workflow: "planned", Description: "x"}
		}, `the step "Plan" names the agent "planner", which is not available`},
		{"the run's agent is missing", nil, func(f *fixture) StartRequest {
			return StartRequest{Dir: f.root, Workflow: "feature", Description: "x", Agent: "gemini"}
		}, `the agent "gemini" is not available`},
		{"no agent at all", func(f *fixture) { f.h.SetAgents("") }, nil, "no agent can start"},
		{"empty description", nil, func(f *fixture) StartRequest {
			return StartRequest{Dir: f.root, Workflow: "feature", Description: " \n "}
		}, "describe the task"},
		{"too long", nil, func(f *fixture) StartRequest {
			return StartRequest{Dir: f.root, Workflow: "feature", Description: strings.Repeat("x", agenttask.MaxPrompt+1)}
		}, "the task is longer than"},
		{"hidden characters", nil, func(f *fixture) StartRequest {
			return StartRequest{Dir: f.root, Workflow: "feature", Description: "do it‮"}
		}, "the task contains invisible characters"},
		{"create fails", func(f *fixture) { f.h.SetCreateError(errors.New("git worktree add failed")) }, nil, "git worktree add failed"},
		{"detached head", func(f *fixture) { f.h.SetCreateError(&feature.DetachedHeadError{Dir: f.root}) }, nil,
			"is on a detached HEAD: check out a branch there, then run the workflow again"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			if c.setup != nil {
				c.setup(f)
			}
			req := StartRequest{Dir: f.root, Workflow: "feature", Description: "Add a flag"}
			if c.req != nil {
				req = c.req(f)
			}
			_, err := f.api.Start(req)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Start = %v, want %q", err, c.want)
			}
			f.e.mu.Lock()
			n := len(f.e.runs)
			f.e.mu.Unlock()
			if n != 0 || len(f.api.Runs()) != 0 {
				t.Errorf("a refused start left %d runs", n)
			}
		})
	}
}

func TestStartUsesTheFirstStepsAgentForTheTask(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(ProjectDir(f.root), "planned.md"), "## Plan\nagent: codex\n\nPlan it.\n\n## Do\n\nDo it.\n")
	v, err := f.api.Start(StartRequest{Dir: filepath.Join(f.root), Workflow: "planned", Description: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if req := f.h.Requested(); req[0][0].Agent != "codex" || v.Agent != "claude" || v.Dir != f.root {
		t.Errorf("task agent %q, run agent %q, dir %q", req[0][0].Agent, v.Agent, v.Dir)
	}
}

func TestListOffersProjectWorkflowsOnlyWhenTrusted(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	writeFile(t, filepath.Join(ProjectDir(f.root), "broken.md"), "no steps here\n")
	writeFile(t, filepath.Join(ProjectDir(f.root), "loop.md"), "## Plan\n\nx\n\n## Again\ntype: workflow\nworkflow: loop\n")
	writeFile(t, filepath.Join(PersonalDir(f.h.Dir), "mine.md"), "## Do\n\nDo it.\n")
	sub := filepath.Join(f.root, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := f.api.List(sub)
	if err != nil {
		t.Fatal(err)
	}
	names := func(l Listing) []string {
		var out []string
		for _, w := range l.Workflows {
			out = append(out, w.Name+":"+w.Source)
		}
		return out
	}
	if got := names(l); !slices.Equal(got, []string{"bug-fix:builtin", "check:project", "feature:builtin", "loop:project", "mine:personal"}) {
		t.Errorf("listed %v", got)
	}
	if l.Restricted || len(l.Problems) != 1 || !strings.HasPrefix(l.Problems[0], "broken.md:") {
		t.Errorf("restricted %v problems %v", l.Restricted, l.Problems)
	}
	loop := l.Workflows[slices.IndexFunc(l.Workflows, func(w WorkflowView) bool { return w.Name == "loop" })]
	if !strings.Contains(loop.Problem, "a cycle") || len(loop.Steps) != 0 {
		t.Errorf("loop = %+v", loop)
	}
	check := l.Workflows[1]
	if len(check.Steps) != 3 || check.Steps[1].Kind != "script" || check.Steps[1].Run != "test" || check.Steps[1].OnFail != "plan" {
		t.Errorf("check steps %+v", check.Steps)
	}

	f.h.SetUntrusted(f.root)
	l, err = f.api.List(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(l); !slices.Equal(got, []string{"bug-fix:builtin", "feature:builtin", "mine:personal"}) || !l.Restricted || l.Hidden != 3 {
		t.Errorf("in Restricted Mode: %v restricted %v hidden %d", got, l.Restricted, l.Hidden)
	}
	if _, err := f.api.List("/elsewhere"); err == nil {
		t.Error("a folder outside the workspace was listed")
	}
}

func TestListRefusesALinkOutOfTheWorkspace(t *testing.T) {
	f := newFixture(t)
	out := tempDir(t)
	link := filepath.Join(f.root, "out")
	if err := os.Symlink(out, link); err != nil {
		t.Fatal(err)
	}
	if _, err := f.api.List(link); err == nil || !strings.Contains(err.Error(), "leads out of its workspace folder") {
		t.Errorf("List through a link = %v", err)
	}
}

func TestNewWorkflow(t *testing.T) {
	f := newFixture(t)
	path, err := f.api.NewWorkflow(f.root, "deliver")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(ProjectDir(f.root), "deliver.md") {
		t.Errorf("path %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := Parse("deliver", data)
	if err != nil {
		t.Fatal(err)
	}
	steps, err := Resolve(w, Find(nil))
	if err != nil || len(steps) != 4 || steps[2].Kind != KindReview || steps[2].OnFail != "implement" || steps[3].Kind != KindGate {
		t.Errorf("starter steps %+v, %v", steps, err)
	}
	if _, err := f.api.NewWorkflow(f.root, "deliver"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("overwrote: %v", err)
	}
	if _, err := f.api.NewWorkflow(f.root, "Bad Name"); err == nil {
		t.Error("an invalid name was written")
	}
	f.h.SetUntrusted(f.root)
	if _, err := f.api.NewWorkflow(f.root, "other"); err == nil || !strings.Contains(err.Error(), "Restricted Mode") {
		t.Errorf("untrusted: %v", err)
	}
}

func TestNewWorkflowDoesNotFollowALinkOut(t *testing.T) {
	f := newFixture(t)
	out := tempDir(t)
	if err := os.Symlink(out, filepath.Join(f.root, ".flint")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.api.NewWorkflow(f.root, "x"); err == nil {
		t.Error("wrote through a link out of the folder")
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Errorf("wrote %v outside", entries)
	}
}

func TestRemoveOnlyARunThatIsOver(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	if err := f.api.Remove(v.ID); err == nil || err.Error() != "stop the run first" {
		t.Fatalf("Remove of a live run = %v", err)
	}
	if err := f.api.Stop(v.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.api.Remove(v.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.api.Runs()) != 0 || len(f.savedRuns()) != 0 {
		t.Error("the run was kept")
	}
	if _, ok := f.h.AllAgentTasks()[v.Task]; !ok {
		t.Error("the agent task was removed with the run")
	}
}

func TestActionsCheckTheRunID(t *testing.T) {
	f := newFixture(t)
	for _, id := range []string{"", "../x", "0123456789abcdef", strings.Repeat("a", 1000)} {
		for name, call := range map[string]func(string) error{
			"approve": f.api.Approve, "retry": f.api.Retry, "skip": f.api.Skip, "pause": f.api.Pause,
			"resume": f.api.Resume, "stop": f.api.Stop, "remove": f.api.Remove,
			"sendBack": func(id string) error { return f.api.SendBack(id, "plan", "") },
		} {
			if err := call(id); !errors.Is(err, errNoRun) {
				t.Errorf("%s(%q) = %v", name, id, err)
			}
		}
	}
}

func TestRunsShowsTheNewestFirstWithTaskState(t *testing.T) {
	f := newFixture(t)
	a := f.start("feature")
	f.clock.Add(10)
	b := f.start("bug-fix")
	f.h.UpdateAgentTask(b.Task, func(t *feature.AgentTask) { t.State = agenttask.Queued })
	runs := f.api.Runs()
	if len(runs) != 2 || runs[0].ID != b.ID || runs[1].ID != a.ID {
		t.Fatalf("runs %+v", runs)
	}
	if runs[0].TaskState != agenttask.Queued || !runs[0].Queued || runs[1].Queued {
		t.Errorf("task states %q %v / %q %v", runs[0].TaskState, runs[0].Queued, runs[1].TaskState, runs[1].Queued)
	}
	if !slices.Equal(runs[0].Actions, []string{ActionPause, ActionStop}) || runs[0].Hold != nil || runs[0].Log == nil {
		t.Errorf("view %+v", runs[0])
	}
}

// countingHost records which agent tasks Runs asks the host about.
type countingHost struct {
	feature.Host
	mu    sync.Mutex
	asked [][]string
}

func (h *countingHost) AgentTasks(ctx context.Context, ids []string) map[string]feature.AgentTask {
	h.mu.Lock()
	h.asked = append(h.asked, slices.Clone(ids))
	h.mu.Unlock()
	return h.Host.AgentTasks(ctx, ids)
}

func (h *countingHost) calls() [][]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.asked)
}

func TestRunsAsksTheHostOnlyForRunsThatAreNotOver(t *testing.T) {
	f := newFixture(t)
	over := f.start("feature")
	live := f.start("bug-fix")
	if err := f.api.Stop(over.ID); err != nil {
		t.Fatal(err)
	}
	f.h.UpdateAgentTask(live.Task, func(t *feature.AgentTask) { t.State = agenttask.Queued })
	host := &countingHost{Host: f.h}
	f.api.host = host

	runs := f.api.Runs()
	if len(runs) != 2 {
		t.Fatalf("runs %+v", runs)
	}
	calls := host.calls()
	if len(calls) != 1 || !slices.Equal(calls[0], []string{live.Task}) {
		t.Fatalf("host asked %v, want one lookup of the live run's task %s", calls, live.Task)
	}
	byID := map[string]RunView{runs[0].ID: runs[0], runs[1].ID: runs[1]}
	if v := byID[live.ID]; v.TaskState != agenttask.Queued || !v.Queued {
		t.Errorf("live run: taskState %q queued %v", v.TaskState, v.Queued)
	}
	if v := byID[over.ID]; v.State != StateStopped || v.TaskState != "" || v.Queued || !slices.Equal(v.Actions, []string{ActionRemove}) {
		t.Errorf("stopped run: %+v", v)
	}

	// Only runs that are over: no lookup at all.
	if err := f.api.Stop(live.ID); err != nil {
		t.Fatal(err)
	}
	before := len(host.calls())
	if runs := f.api.Runs(); len(runs) != 2 {
		t.Fatalf("runs %+v", runs)
	}
	if got := len(host.calls()); got != before {
		t.Errorf("a list of finished runs asked the host %d times", got-before)
	}
}
