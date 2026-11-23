package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"flint/internal/features/projects"
	"flint/internal/features/workflows"
	"flint/internal/mcpserver"
	"flint/internal/mcptools"
	"flint/internal/settings"
)

// dlgItems are the items of the project the tests share.
func dlgItems() map[string]string {
	return map[string]string{
		"rl":      dlgItem("Rate limit", "repos: api, web", "workflow: ship"),
		"dep":     dlgItem("Dependency", "repos: api"),
		"waits":   dlgItem("Waits", "repos: api, web", "workflow: ship", "after: dep"),
		"scripty": dlgItem("Scripty", "repos: api", "workflow: check"),
	}
}

// An agent chat in the api repository delegates an item that names a
// workflow and two repositories, with the user's yes: two agent tasks (one
// per repository), each taken by a run of the workflow that says who
// delegated it, the item in doing, and each task's first message the
// project's block and then the workflow step's.
func TestAgentDelegatesAnItemWithAWorkflowAcrossTwoRepositories(t *testing.T) {
	f := newDlgFixture(t, 0, dlgItems())
	before := f.world(t)

	// Plan makes nothing and says what the user approves.
	tl := f.agentTool(t, projects.DelegateToolName)
	p, err := tl.Plan(mcpserver.WithCaller(context.Background(), "coord"), f.repo, json.RawMessage(`{"item":"rl"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`Delegate the item rl ("Rate limit") of the project "Acme" (` + f.plan + `)`,
		"Workflow: ship (a run takes each agent task; steps that name no agent: fake).",
		"- api: " + f.repo + "\n", "- web: " + f.web + "\n",
		"Together: no, each repository gets its own agent.", "The item's status becomes doing.",
	} {
		if !strings.Contains(p.Summary, want) {
			t.Errorf("the summary lacks %q:\n%s", want, p.Summary)
		}
	}
	before.same(t, f.world(t))
	if len(f.questions()) != 0 {
		t.Fatalf("Plan asked: %q", f.questions())
	}

	// Through the server: the user is asked with that summary, says yes.
	f.setAnswer(true)
	out, failed := f.call(t, "coord", f.repo, projects.DelegateToolName, `{"item":"rl"}`)
	if failed {
		t.Fatalf("delegate: %s", out)
	}
	asked := f.questions()
	if len(asked) != 1 || !strings.Contains(asked[0], "Delegate the item rl") ||
		!strings.Contains(asked[0], "Workflow: ship") || !strings.Contains(asked[0], f.repo) || !strings.Contains(asked[0], f.web) {
		t.Fatalf("the question:\n%q", asked)
	}
	f.waitStarts()
	for _, want := range []string{"Delegated the item rl (Rate limit): its status is doing.", "in api (", "in web (", `of "ship" (running)`, "wait_for_workflow_run"} {
		if !strings.Contains(out, want) {
			t.Errorf("the answer lacks %q:\n%s", want, out)
		}
	}

	// One task per repository, each in its own repository's worktrees, each
	// with a run that is the agent's and says who delegated it.
	by := "an agent in " + f.repo + " (chat coord)"
	runs := f.wf.Runs()
	if len(runs) != 2 {
		t.Fatalf("runs %+v", runs)
	}
	dirs := []string{}
	tasks := []string{}
	for _, r := range runs {
		dirs, tasks = append(dirs, r.Dir), append(tasks, r.Task)
		if r.Origin != workflows.OriginAgent || r.Workflow != "ship" || r.Task == "" {
			t.Errorf("run %+v", r)
		}
		if len(r.Log) == 0 || r.Log[0].Text != "Run started for a project item, delegated by "+by+", approved by you" {
			t.Errorf("the log of %s: %+v", r.ID, r.Log)
		}
		wt := f.worktreeOf(r.Task)
		if !within(wt, r.Dir+".worktrees") {
			t.Errorf("the worktree %s of the run in %s", wt, r.Dir)
		}
	}
	slices.Sort(dirs)
	if want := []string{f.repo, f.web}; !slices.Equal(dirs, want) || tasks[0] == tasks[1] {
		t.Errorf("run checkouts %v, tasks %v", dirs, tasks)
	}
	if got := f.status(t, "rl"); got != projects.StatusDoing {
		t.Errorf("the item is %q", got)
	}
	if n := len(f.app.taskStore()); n != 2 {
		t.Errorf("%d repositories have tasks", n)
	}

	// Each task's first chat: the project's block, then the step's, then
	// the item's text.
	for _, task := range tasks {
		chat := f.firstChat(t, task)
		prompt := f.driver.waitPrompts(t, chat, 1)[0]
		project := strings.Index(prompt, "===== Project context (from Flint) =====\n")
		step := strings.Index(prompt, "===== Workflow step (from Flint) =====\n")
		if project != 0 || step < 0 || project > step || strings.Count(prompt, "===== Project context") != 1 || strings.Count(prompt, "===== Workflow step") != 1 {
			t.Errorf("first message of %s (project block at %d, step block at %d):\n%s", task, project, step, prompt)
		}
		if !strings.Contains(prompt, `step 1 of 2, "Fix", of the workflow "ship"`) || !strings.Contains(prompt, "===== The task =====\n\nRate limit\n\nLimit it.") {
			t.Errorf("first message of %s:\n%s", task, prompt)
		}
	}
}

// Every refusal happens before anything exists: no task, no run, no
// branch, no worktree, and the item keeps its status.
func TestDelegateToolRefusalsCreateNothingOnTheRealHost(t *testing.T) {
	f := newDlgFixture(t, 0, dlgItems())
	root := f.repo
	// A task of the user's own, for the caller in its worktree.
	task := f.create("Something else")
	cases := []struct {
		name string
		// run makes the call (and any preparation); it returns the text of
		// the refusal, and whether the user was asked.
		run   func(t *testing.T) (text string, failed bool)
		want  string
		asked bool
	}{
		{"the user says no", func(t *testing.T) (string, bool) {
			f.setAnswer(false)
			defer f.setAnswer(true)
			return f.call(t, "coord", root, projects.DelegateToolName, `{"item":"rl"}`)
		}, mcpserver.ErrNotApproved.Error(), true},
		{"the item waits for another", func(t *testing.T) (string, bool) {
			return f.call(t, "coord", root, projects.DelegateToolName, `{"item":"waits"}`)
		}, "the item waits is not ready: it waits for dep", false},
		{"the project folder is untrusted", func(t *testing.T) (string, bool) {
			if err := f.app.setTrust(f.plan, false); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := f.app.setTrust(f.plan, true); err != nil {
					t.Fatal(err)
				}
			}()
			return f.call(t, "coord", root, projects.DelegateToolName, `{"item":"rl"}`)
		}, "no project this chat's folder is in has an item", false},
		{"the untrusted project is named", func(t *testing.T) (string, bool) {
			if err := f.app.setTrust(f.plan, false); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := f.app.setTrust(f.plan, true); err != nil {
					t.Fatal(err)
				}
			}()
			return f.call(t, "coord", root, projects.DelegateToolName, `{"project":"`+f.plan+`","item":"rl"}`)
		}, "an agent cannot delegate its items", false},
		{"the caller is in an agent task's worktree", func(t *testing.T) (string, bool) {
			return f.call(t, "helper", task.Worktree, projects.DelegateToolName, `{"item":"rl"}`)
		}, "an agent that was handed work cannot hand out work", false},
		{"the workflow has a script step and the checkout has no consent", func(t *testing.T) (string, bool) {
			// The consent applies once the user has switched the sandbox (on by default) off.
			off := func(sandbox string) {
				if _, err := f.app.updateSettings(func(s settings.Settings) settings.Settings {
					w := workflows.Section.Get(s)
					w.Sandbox = sandbox
					return workflows.Section.With(s, w)
				}); err != nil {
					t.Fatal(err)
				}
			}
			off(workflows.SandboxOff)
			defer off(workflows.SandboxOn)
			return f.call(t, "coord", root, projects.DelegateToolName, `{"item":"scripty"}`)
		}, "run this workflow once themselves", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, asked := f.world(t), len(f.questions())
			text, failed := tc.run(t)
			if !failed || !strings.Contains(text, tc.want) {
				t.Fatalf("answer %q (failed %v), want a refusal with %q", text, failed, tc.want)
			}
			if got := len(f.questions()) - asked; (got > 0) != tc.asked {
				t.Errorf("asked %d times, want asked = %v", got, tc.asked)
			}
			f.waitStarts()
			before.same(t, f.world(t))
		})
	}

	// Switched off: the tool is not even listed, and cannot be called.
	t.Run("the switch is off", func(t *testing.T) {
		if !f.listed(t, "coord", root, projects.DelegateToolName) {
			t.Fatal("the tool is not listed with the switch on")
		}
		before := f.world(t)
		s := f.app.currentSettings()
		s.MCPServer.Actions.Delegate = false
		f.app.setSettingsForTest(s)
		if f.listed(t, "coord", root, projects.DelegateToolName) {
			t.Error("the tool is listed with the switch off")
		}
		asked := len(f.questions())
		if text, failed := f.call(t, "coord", root, projects.DelegateToolName, `{"item":"rl"}`); !failed || !strings.Contains(text, "unknown tool") {
			t.Errorf("a switched-off tool answered: %q (failed %v)", text, failed)
		}
		if len(f.questions()) != asked {
			t.Error("asked about a tool that is off")
		}
		before.same(t, f.world(t))
		// The workflow tools follow their own switch.
		if !f.listed(t, "coord", root, mcptools.ToolWorkflowRuns) {
			t.Error("the workflow tools are not listed")
		}
	})

	// Nothing the refusals did touched the user's own task or the plain item.
	if _, err := os.Stat(task.Worktree); err != nil {
		t.Errorf("the user's task: %v", err)
	}
	if got := f.status(t, "rl"); got != projects.StatusTodo {
		t.Errorf("the item is %q", got)
	}
}
