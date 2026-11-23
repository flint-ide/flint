package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"flint/internal/feature"
	"flint/internal/features/projects"
	"flint/internal/features/workflows"
)

// Projects comes before Workflows in the list, so an item's task that a
// workflow takes starts with the project's block and then the step's
// (feature.Set.TaskContext joins in list order).
func TestFeatureListPutsProjectsBeforeWorkflows(t *testing.T) {
	ids := make([]string, len(features))
	for i, m := range features {
		ids[i] = m.ID
	}
	p, w := slices.Index(ids, projects.ID), slices.Index(ids, workflows.ID)
	if p < 0 || w < 0 || p > w {
		t.Fatalf("features in order %v", ids)
	}
}

// A project item that names a workflow: Delegate makes the task through
// CreateAgentTasks, which has the Workflows feature plan the run before the
// task exists and bind it before the chat starts, so the first message is
// the project's block, then the first step's block, then the item's text.
func TestDelegatedItemRunsItsWorkflow(t *testing.T) {
	if err := registerFeatures(); err != nil {
		t.Fatal(err)
	}
	f := queueFixture(t, 0)
	web := f.addRepoT("web", "git@github.com:acme/web.git")
	f.app.features = feature.New([]feature.Module{projects.Module, workflows.Module}, featureHost{f.app})
	var api *projects.API
	for _, s := range f.app.features.Services() {
		if p, ok := s.Instance().(*projects.API); ok {
			api = p
		}
	}
	writeT(t, filepath.Join(workflows.PersonalDir(f.app.configDir), "fix.md"),
		"---\ndescription: Fix, then approve\n---\n\n## Fix\n\nFix the bug.\n\n## Approve\ntype: gate\n")
	opened := chatsOpened(f.app)
	plan := planT(t, f.app, api, filepath.Dir(f.repo), [][2]string{{"web", "git@github.com:acme/web.git"}},
		map[string]string{"rl": "---\ntitle: Rate limit\nagent: fake\nrepos: web\nworkflow: fix\n---\nLimit it.\n"}, "Acme is a shop.\n")
	res, err := api.Delegate(plan, "rl", f.repo)
	if err != nil {
		t.Fatal(err)
	}
	f.waitStarts()
	if res.Item.Workflow != "fix" || len(res.Tasks) != 1 || res.Tasks[0].Workflow != "fix" || res.Tasks[0].RunState != workflows.StateRunning || res.Tasks[0].Run == "" {
		t.Fatalf("delegated %+v", res)
	}
	chats := opened()
	if len(chats) != 1 || chats[0].TaskID != res.Tasks[0].ID {
		t.Fatalf("chats %+v", chats)
	}
	prompt := f.driver.waitPrompts(t, chats[0].Info.ID, 1)[0]
	if !strings.HasPrefix(prompt, "===== Project context (from Flint) =====\n") ||
		!strings.Contains(prompt, "\n\n===== Your task =====\n\n===== Workflow step (from Flint) =====\n") ||
		!strings.Contains(prompt, `step 1 of 2, "Fix", of the workflow "fix"`) ||
		!strings.HasSuffix(prompt, "===== The task =====\n\nRate limit\n\nLimit it.") {
		t.Errorf("first message:\n%s", prompt)
	}
	got := (featureHost{f.app}).AgentTasks(context.Background(), []string{res.Tasks[0].ID})
	if r := got[res.Tasks[0].ID].Run; r.Workflow != "fix" || r.State != workflows.StateRunning {
		t.Errorf("AgentTasks' run = %+v", r)
	}
	if !within(got[res.Tasks[0].ID].Worktree, filepath.Dir(web)) {
		t.Errorf("task %+v", got)
	}
}

// planProbe is a feature that takes workflows: it refuses "bad" and records
// what it planned, bound and dropped.
type planProbe struct {
	mu                      sync.Mutex
	planned, bound, dropped []string
}

func (p *planProbe) module() feature.Module {
	return feature.Module{ID: "flows", New: func(feature.Host) feature.Instance {
		return feature.Instance{TaskWorkflow: func(_ context.Context, req feature.NewAgentTask) (feature.PlannedWorkflow, error) {
			if req.Workflow == "bad" {
				return feature.PlannedWorkflow{}, errors.New("there is no workflow named \"bad\" here")
			}
			p.mu.Lock()
			p.planned = append(p.planned, req.Workflow)
			p.mu.Unlock()
			name := req.Workflow
			return feature.PlannedWorkflow{
				Agent: "fake",
				Bind: func(taskID string) error {
					p.mu.Lock()
					defer p.mu.Unlock()
					p.bound = append(p.bound, name+":"+taskID)
					return nil
				},
				Drop: func() {
					p.mu.Lock()
					defer p.mu.Unlock()
					p.dropped = append(p.dropped, name)
				},
			}, nil
		}}
	}}
}

// Every request is planned before any task is made: a refused one, or no
// feature that takes workflows, creates nothing and drops what was planned.
func TestCreateAgentTasksPlansEveryWorkflowFirst(t *testing.T) {
	f := queueFixture(t, 0)
	h := featureHost{f.app}
	ctx := context.Background()
	count := func() int {
		n := 0
		for _, tasks := range f.app.taskStore() {
			n += len(tasks)
		}
		return n
	}

	f.app.features = feature.New(nil, h)
	_, err := h.CreateAgentTasks(ctx, []feature.NewAgentTask{{Dir: f.repo, Description: "x", Agent: "fake", Workflow: "fix"}})
	if !errors.Is(err, feature.ErrWorkflowsOff) || !strings.Contains(err.Error(), "workflows are switched off (Settings ▸ Extensions ▸ Built-in)") || count() != 0 {
		t.Fatalf("without workflows: %v, %d tasks", err, count())
	}

	p := &planProbe{}
	f.app.features = feature.New([]feature.Module{p.module()}, h)
	_, err = h.CreateAgentTasks(ctx, []feature.NewAgentTask{
		{Dir: f.repo, Description: "one", Agent: "codex", Workflow: "fix"},
		{Dir: f.repo, Description: "two", Agent: "fake", Workflow: "bad"},
	})
	if err == nil || !strings.Contains(err.Error(), `no workflow named "bad"`) || count() != 0 {
		t.Fatalf("a refused second request: %v, %d tasks", err, count())
	}
	if !slices.Equal(p.planned, []string{"fix"}) || !slices.Equal(p.dropped, []string{"fix"}) || len(p.bound) != 0 {
		t.Errorf("planned %v, dropped %v, bound %v", p.planned, p.dropped, p.bound)
	}

	made, err := h.CreateAgentTasks(ctx, []feature.NewAgentTask{
		{Dir: f.repo, Description: "plain", Agent: "fake"},
		{Dir: f.repo, Description: "flow", Agent: "codex", Workflow: "fix"},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.waitStarts()
	if len(made) != 2 || made[1].Agent != "fake" || !slices.Equal(p.bound, []string{"fix:" + made[1].ID}) {
		t.Errorf("made %+v, bound %v", made, p.bound)
	}
}

func TestJointAgentTasksRefuseAWorkflow(t *testing.T) {
	f := queueFixture(t, 0)
	web := f.addRepoT("web", "git@github.com:acme/web.git")
	_, err := featureHost{f.app}.CreateJointAgentTasks(context.Background(), []feature.NewAgentTask{
		{Dir: f.repo, Description: "x", Workflow: "fix"}, {Dir: web, Description: "x"},
	}, "claude")
	if !errors.Is(err, feature.ErrJointWorkflow) || len(f.app.taskStore()) != 0 {
		t.Fatalf("CreateJointAgentTasks = %v", err)
	}
}
