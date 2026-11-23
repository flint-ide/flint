package workflows

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flint/internal/feature"
)

// itemRequest is a project item's request for a task that a workflow takes,
// as Projects makes it (its own origin, not the run's).
func (f *fixture) itemRequest(workflow string) feature.NewAgentTask {
	return feature.NewAgentTask{Dir: f.root, Description: "Rate limit\n\nLimit it.", Agent: "claude",
		Origin: `projects:{"project":"/p","item":"rl"}`, Workflow: workflow}
}

func TestTaskWorkflowPlansBindsAndAnswersTheFirstChat(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(ProjectDir(f.root), "planned.md"), "## Plan\nagent: codex\n\nPlan it.\n\n## Approve\ntype: gate\n")
	f.h.SetTaskWorkflow(f.api.taskWorkflow)
	f.h.SetTaskRuns(f.e.taskRun)
	tasks, err := f.h.CreateAgentTasks(t.Context(), []feature.NewAgentTask{f.itemRequest("planned")})
	if err != nil {
		t.Fatal(err)
	}
	task := tasks[0]
	if task.Agent != "codex" || task.Run.Workflow != "planned" || task.Run.State != StateRunning || task.Run.ID == "" {
		t.Fatalf("task %+v", task)
	}
	r, ok := f.e.get(task.Run.ID)
	if !ok || r.Task != task.ID || r.Origin != OriginItem || r.Agent != "claude" || r.Log[0].Text != "Run started for a project item" {
		t.Fatalf("run %+v", r)
	}
	// Flint starts the task's chat: the run is found by the task, whatever
	// the origin; another task, or no task id, gets nothing.
	st := feature.TaskStart{Origin: f.itemRequest("").Origin, TaskID: task.ID}
	if text := f.e.taskContext(t.Context(), st); !strings.HasSuffix(text, "===== The task =====") || !strings.Contains(text, `step 1 of 2, "Plan"`) {
		t.Errorf("context %q", text)
	}
	for _, other := range []feature.TaskStart{{Origin: st.Origin, TaskID: "0000000000000099"}, {Origin: st.Origin}, {Origin: "workflow:" + r.ID, TaskID: "0000000000000099"}} {
		if text := f.e.taskContext(t.Context(), other); text != "" {
			t.Errorf("%+v answered %q", other, text)
		}
	}
	// Only the first start of the first step.
	f.finishFirst(r.ID, "RESULT: DONE")
	if text := f.e.taskContext(t.Context(), st); text != "" {
		t.Errorf("after the first step: %q", text)
	}
	f.waitFor(r.ID, "the gate", func(r Run) bool { return r.State == StateWaiting })
	if run, ok := f.e.taskRun(task.ID); !ok || run.State != StateWaiting {
		t.Errorf("TaskRun = %+v, %v", run, ok)
	}
}

func TestTaskWorkflowSharesStartsChecksAndDrops(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *fixture)
		req   func(f *fixture) feature.NewAgentTask
		want  string
	}{
		{"untrusted", func(f *fixture) { f.h.SetUntrusted(f.root) }, nil, "is in Restricted Mode: trust the folder to run workflows there"},
		{"unknown workflow", nil, func(f *fixture) feature.NewAgentTask { return f.itemRequest("nope") }, `there is no workflow named "nope" here`},
		{"agent missing", nil, func(f *fixture) feature.NewAgentTask {
			r := f.itemRequest("feature")
			r.Agent = "gemini"
			return r
		}, `the agent "gemini" is not available`},
		{"outside the workspace", nil, func(f *fixture) feature.NewAgentTask {
			r := f.itemRequest("feature")
			r.Dir = filepath.Dir(f.root)
			return r
		}, "is not in an open workspace folder"},
		{"create fails", func(f *fixture) { f.h.SetCreateError(errors.New("git worktree add failed")) }, nil, "git worktree add failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.h.SetTaskWorkflow(f.api.taskWorkflow)
			if c.setup != nil {
				c.setup(f)
			}
			req := f.itemRequest("feature")
			if c.req != nil {
				req = c.req(f)
			}
			// A plain task after it: all or nothing.
			_, err := f.h.CreateAgentTasks(t.Context(), []feature.NewAgentTask{req, {Dir: f.root, Description: "x", Agent: "claude"}})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("CreateAgentTasks = %v, want %q", err, c.want)
			}
			f.e.mu.Lock()
			n := len(f.e.runs)
			f.e.mu.Unlock()
			if n != 0 || len(f.h.AllAgentTasks()) != 0 {
				t.Errorf("left %d runs, %d tasks", n, len(f.h.AllAgentTasks()))
			}
		})
	}
}

func TestRunStateChangesTellTheTasks(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	before := f.h.TasksChangedCount()
	f.finishFirst(v.ID, "RESULT: DONE") // still running: step 2 starts
	f.finishChat(v.ID, 1, "RESULT: DONE")
	if n := f.h.TasksChangedCount(); n != before {
		t.Errorf("running → running told the tasks %d times", n-before)
	}
	f.finishChat(v.ID, 2, "RESULT: PASS")
	f.waitFor(v.ID, "the gate", func(r Run) bool { return r.State == StateWaiting })
	if n := f.h.TasksChangedCount(); n != before+1 {
		t.Errorf("running → waiting told the tasks %d times", n-before)
	}
}

func TestWaitRunWakesOnChange(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	// Already settled: at once. Not settled: until a change, or ctx's end.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := f.e.waitRun(ctx, v.ID, settled); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitRun on a running run = %v", err)
	}
	done := make(chan Run, 1)
	go func() {
		r, err := f.e.waitRun(t.Context(), v.ID, settled)
		if err != nil {
			t.Error(err)
		}
		done <- r
	}()
	if err := f.api.Pause(v.ID); err != nil {
		t.Fatal(err)
	}
	f.finishFirst(v.ID, "RESULT: DONE")
	select {
	case r := <-done:
		if r.State != StatePaused {
			t.Errorf("woke at %s", r.State)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("waitRun did not wake")
	}
	if _, err := f.e.waitRun(t.Context(), "0000000000000000", settled); !errors.Is(err, errNoRun) {
		t.Errorf("an unknown run: %v", err)
	}
}
