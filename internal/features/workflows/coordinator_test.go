package workflows

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"flint/internal/feature"
	"flint/internal/mcptools"
)

// repoFolder adds a trusted workspace folder that is its own checkout.
func (f *fixture) repoFolder(name string) string {
	f.t.Helper()
	dir := filepath.Join(filepath.Dir(f.root), name)
	writeFile(f.t, filepath.Join(dir, "x.txt"), "x")
	f.h.SetRoots(append(f.h.Roots(), dir)...)
	f.h.SetRepository(dir)
	f.h.SetTrusted(dir)
	return dir
}

// inProject makes the folder dir in a project whose items' tasks are these
// (Host.FolderProjects, which the Projects feature answers).
func (f *fixture) inProject(dir string, tasks ...string) {
	f.h.SetFolderProjects(func(_ context.Context, d string) feature.FolderProjects {
		if d != dir {
			return feature.FolderProjects{}
		}
		return feature.FolderProjects{Names: []string{"Acme"}, Tasks: slices.Clone(tasks)}
	})
}

// The coordinator in repository A lists, waits for and pauses the run of
// its project's item in repository B; it reaches no run of an unrelated
// folder, and a step's chat reaches nothing.
func TestCoordinatorReachesItsProjectsRuns(t *testing.T) {
	f := newFixture(t)
	f.allowTools(true)
	b := f.repoFolder("b")
	c := f.repoFolder("c")
	item, err := f.api.Start(StartRequest{Dir: b, Workflow: "feature", Description: "The item's run"})
	if err != nil {
		t.Fatal(err)
	}
	stepOf, err := f.api.Start(StartRequest{Dir: b, Workflow: "feature", Description: "Another item's run"})
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := f.api.Start(StartRequest{Dir: c, Workflow: "feature", Description: "Not the project's"})
	if err != nil {
		t.Fatal(err)
	}
	// Without the project, nothing of B is reached.
	if out, err := f.call("chat-1", f.root, mcptools.ToolWorkflowRuns, `{}`); err != nil || strings.Contains(out, item.ID) {
		t.Fatalf("before the project: %v\n%s", err, out)
	}
	f.inProject(f.root, item.Task, stepOf.Task)

	out, err := f.call("chat-1", f.root, mcptools.ToolWorkflowRuns, `{}`)
	if err != nil || !strings.Contains(out, "## Run "+item.ID) || !strings.Contains(out, "folder: "+b) || strings.Contains(out, unrelated.ID) {
		t.Fatalf("runs: %v\n%s", err, out)
	}
	out, err = f.call("chat-1", f.root, mcptools.ToolWorkflowWait, `{"run":"`+item.ID+`","timeoutSeconds":1}`)
	if err != nil || !strings.HasPrefix(out, "timedOut: true") || !strings.Contains(out, item.ID) {
		t.Fatalf("wait: %v\n%s", err, out)
	}
	if _, err := f.call("chat-1", f.root, mcptools.ToolWorkflowSteer, `{"run":"`+item.ID+`","action":"pause"}`); err != nil {
		t.Fatal(err)
	}
	if r, _ := f.e.get(item.ID); !r.Paused || !strings.HasPrefix(r.Log[len(r.Log)-1].Text, "Asked by an agent in "+f.root) {
		t.Errorf("after pause: %+v", r)
	}
	for _, name := range []string{mcptools.ToolWorkflowRuns, mcptools.ToolWorkflowWait, mcptools.ToolWorkflowSteer} {
		if _, err := f.call("chat-1", f.root, name, argsFor(name, unrelated.ID)); err == nil || !strings.Contains(err.Error(), "no workflow run") {
			t.Errorf("%s reached an unrelated run: %v", name, err)
		}
	}

	// A step of the project's run gets nothing, also not its own run.
	f.finishFirst(stepOf.ID, "RESULT: DONE")
	step := f.chatOf(stepOf.ID, 1)
	worktree := f.h.AllAgentTasks()[stepOf.Task].Worktree
	f.h.SetTrusted(worktree)
	for _, who := range []struct{ chat, root string }{{step, f.root}, {"helper", worktree}} {
		for _, name := range []string{mcptools.ToolWorkflowRuns, mcptools.ToolWorkflowWait, mcptools.ToolWorkflowSteer} {
			if _, err := f.call(who.chat, who.root, name, argsFor(name, stepOf.ID)); !errors.Is(err, errStepCaller) {
				t.Errorf("%s by a step (%s in %s): %v", name, who.chat, who.root, err)
			}
		}
	}

	// An agent task's worktree (handed-out work) does not coordinate: no
	// project scope there, even when the folder is in the project.
	wt := filepath.Join(filepath.Dir(f.root), "a-wt")
	writeFile(t, filepath.Join(wt, "x.txt"), "x")
	f.h.SetRepository(wt)
	f.h.SetTaskWorktree(wt)
	f.h.SetTrusted(wt)
	f.inProject(wt, item.Task)
	if out, err := f.call("chat-9", wt, mcptools.ToolWorkflowRuns, `{}`); err != nil || strings.Contains(out, item.ID) {
		t.Errorf("a worktree reached the project's run: %v\n%s", err, out)
	}
}

// Through the project scope too, no tool approves a gate, continues past
// a failed step or checks it again.
func TestNoToolReachesApproveOrSkipAcrossProjects(t *testing.T) {
	f := newFixture(t)
	f.allowTools(true)
	b := f.repoFolder("b")
	writeFile(t, filepath.Join(ProjectDir(b), "gated.md"), "## Do\n\nDo it.\n\n## Approve\ntype: gate\n")
	start := func() RunView {
		v, err := f.api.Start(StartRequest{Dir: b, Workflow: "gated", Description: "An item"})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	gated, failed := start(), start()
	f.finishFirst(gated.ID, "RESULT: DONE")
	f.waitFor(gated.ID, "the gate", func(r Run) bool { return r.State == StateWaiting })
	f.finishFirst(failed.ID, "RESULT: BLOCKED")
	f.waitFor(failed.ID, "the failure", func(r Run) bool { return r.State == StateFailed })
	f.h.SetChatOutput("task-chat-"+failed.ID, "RESULT: DONE")
	f.inProject(f.root, gated.Task, failed.Task)
	if out, err := f.call("chat-1", f.root, mcptools.ToolWorkflowRuns, `{}`); err != nil || !strings.Contains(out, "Only the user can approve it") {
		t.Fatalf("the coordinator does not see the gate: %v\n%s", err, out)
	}
	for _, id := range []string{gated.ID, failed.ID} {
		for _, tl := range f.api.agentTools() {
			for _, act := range []string{"approve", "skip", "continue", "continue_anyway", "pass", "accept", "recheck", "check_again", "done", "resume"} {
				_, _ = f.call("chat-1", f.root, tl.Name, `{"run":"`+id+`","action":"`+act+`"}`)
			}
		}
	}
	if r, _ := f.e.get(gated.ID); r.State != StateWaiting || r.Records[1].State == StepDone {
		t.Errorf("the gate was passed: %s %+v", r.State, r.Records)
	}
	if r, _ := f.e.get(failed.ID); r.State != StateFailed || r.Records[0].Skipped {
		t.Errorf("the failed step was skipped: %s %+v", r.State, r.Records)
	}
}

// A project item an agent delegated with the user's yes is the agent's run:
// origin, log, and script steps without a sandbox refused rather than asked.
func TestDelegatedItemRun(t *testing.T) {
	f := newFixture(t)
	by := "an agent in /work/a (chat chat-7)"
	req := feature.NewAgentTask{Dir: f.root, Description: "Fix it", Agent: "claude", Workflow: "feature", Origin: `projects:{}`, DelegatedBy: by}
	if err := f.api.taskWorkflowCheck(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if len(f.api.Runs()) != 0 || len(f.e.runs) != 0 {
		t.Fatal("the check planned a run")
	}
	f.h.SetTaskWorkflow(f.api.taskWorkflow)
	tasks, err := f.h.CreateAgentTasks(t.Context(), []feature.NewAgentTask{req})
	if err != nil || len(tasks) != 1 {
		t.Fatal(err)
	}
	runs := f.api.Runs()
	if len(runs) != 1 {
		t.Fatalf("runs %+v", runs)
	}
	r, _ := f.e.get(runs[0].ID)
	if r.Origin != OriginAgent || r.Log[0].Text != "Run started for a project item, delegated by "+by+", approved by you" {
		t.Errorf("origin %q, log %+v", r.Origin, r.Log)
	}
	// A user's Delegate stays the item's.
	req.DelegatedBy = ""
	if _, err := f.h.CreateAgentTasks(t.Context(), []feature.NewAgentTask{req}); err != nil {
		t.Fatal(err)
	}
	if v := f.api.Runs(); len(v) != 2 || v[0].Origin == v[1].Origin {
		t.Errorf("origins %+v", v)
	}

	// Script steps without a sandbox (switched off): refused for the agent, never asked.
	f.sandboxOff()
	writeFile(t, filepath.Join(ProjectDir(f.root), "check.md"), scriptWorkflow)
	f.h.SetRunLaunch(f.root, "test", feature.RunLaunch{Name: "test", Commands: []feature.RunCommand{{Line: "go test ./..."}}})
	script := feature.NewAgentTask{Dir: f.root, Description: "Check it", Agent: "claude", Workflow: "check", DelegatedBy: by}
	for _, err := range []error{f.api.taskWorkflowCheck(t.Context(), script), func() error { _, err := f.api.taskWorkflow(t.Context(), script); return err }()} {
		if err == nil || !strings.Contains(err.Error(), "ask the user to run this workflow once themselves") {
			t.Errorf("unsandboxed scripts for an agent: %v", err)
		}
	}
	if len(f.h.Asked()) != 0 {
		t.Errorf("asked %v", f.h.Asked())
	}
	// The user's check does not ask either (its start will).
	script.DelegatedBy = ""
	if err := f.api.taskWorkflowCheck(t.Context(), script); err != nil || len(f.h.Asked()) != 0 {
		t.Errorf("the user's check: %v, asked %v", err, f.h.Asked())
	}
}

func TestCoordinatePromptForAProject(t *testing.T) {
	f := newFixture(t)
	p, err := f.api.CoordinatePrompt(f.root, "Ship the export feature")
	if err != nil || p.DelegateOn || strings.Contains(p.Prompt, mcptools.ToolProjectDelegate) {
		t.Fatalf("without a project: %v %+v", err, p)
	}
	f.inProject(f.root)
	p, err = f.api.CoordinatePrompt(f.root, "Ship the export feature")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`Flint project(s) "Acme"`, "get_project", "READY item", "delegate_project_item (project, item), one call at a time", "Flint asks me before each",
		"never ask to start it when it is ready", "only for work that is not an item", "wait_for_workflow_run", "Never try to approve", "as soon as I say so", `"Delegate project items"`} {
		if !strings.Contains(p.Prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p.Prompt)
		}
	}
	if p.DelegateOn || p.ToolOn {
		t.Error("switches on by default")
	}
	s := f.h.Settings()
	s.MCPServer.Enabled = true
	s.MCPServer.Actions.Delegate = true
	f.h.SetSettings(s)
	if p, _ := f.api.CoordinatePrompt(f.root, "x"); !p.DelegateOn || p.ToolOn {
		t.Errorf("delegateOn %v toolOn %v", p.DelegateOn, p.ToolOn)
	}
}
