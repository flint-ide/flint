package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"flint/internal/feature"
	"flint/internal/mcpserver"
	"flint/internal/mcptools"
)

func (f *fixture) allowDelegate() {
	s := f.h.Settings()
	s.MCPServer.Enabled = true
	s.MCPServer.Actions.Delegate = true
	f.h.SetSettings(s)
}

// planDelegateAs plans one delegate_project_item call as chat in root.
func (f *fixture) planDelegateAs(chat, root, args string) (mcpserver.Planned, error) {
	return f.api.planDelegate(mcpserver.WithCaller(f.t.Context(), chat), root, json.RawMessage(args))
}

// delegateAs plans and runs one call (the user said yes).
func (f *fixture) delegateAs(chat, root, args string) (string, error) {
	p, err := f.planDelegateAs(chat, root, args)
	if err != nil {
		return "", err
	}
	return p.Run(mcpserver.WithCaller(f.t.Context(), chat))
}

func toolNames(tools []mcpserver.Tool) []string {
	var out []string
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

func TestDelegateToolIsOptInAndActs(t *testing.T) {
	f := newFixture(t, nil)
	// "Add project items" does not switch it on.
	f.allowAdd()
	if got := toolNames(f.api.agentTools()); slices.Contains(got, DelegateToolName) {
		t.Fatalf("offered with actions.delegate off: %v", got)
	}
	f.allowDelegate()
	var tool mcpserver.Tool
	for _, tl := range f.api.agentTools() {
		if tl.Name == DelegateToolName {
			tool = tl
		}
	}
	if !tool.Acts || tool.Plan == nil || tool.Call != nil || tool.Granted != nil || !slices.Contains(mcptools.ActionNames, tool.Name) {
		t.Fatalf("tool %+v", tool)
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema["additionalProperties"] != false {
		t.Fatalf("schema %s: %v", tool.InputSchema, err)
	}
	s := f.h.Settings()
	s.MCPServer.Enabled = false
	f.h.SetSettings(s)
	if got := toolNames(f.api.agentTools()); slices.Contains(got, DelegateToolName) {
		t.Fatalf("offered with the server off: %v", got)
	}
}

// The summary the user approves: the project, the item, its agent or
// workflow, each repository with its folder, together, and the text's
// first 600 characters. Plan makes nothing.
func TestDelegateToolSummary(t *testing.T) {
	long := strings.Repeat("Limit it. ", 100)
	f := newFixture(t, map[string]string{
		"rl":   "---\ntitle: Rate limit\nagent: backend\nrepos: api, web\n---\n" + long,
		"flow": item("Flow", "todo", "repos: api", "workflow: bug-fix"),
		"both": item("Both", "todo", "repos: api, web", "together: true"),
		"here": item("Here", "todo"),
	})
	web := f.withWeb(true)
	f.open()
	runs := f.withRuns("bug-fix")
	p, err := f.planDelegateAs("chat-1", f.root, `{"item":"rl"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`Delegate the item rl ("Rate limit") of the project "Acme" (` + f.plan + `)`, "Agent: backend\n",
		"- api: " + f.root + "\n", "- web: " + web + "\n", "Together: no, each repository gets its own agent.",
		"The item's status becomes doing.", "Text:\nLimit it.", " […]",
	} {
		if !strings.Contains(p.Summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, p.Summary)
		}
	}
	if strings.Count(p.Summary, "Limit it.") != 60 {
		t.Errorf("the text is not cut at 600 characters:\n%s", p.Summary)
	}
	if p, err := f.planDelegateAs("chat-1", f.root, `{"item":"both"}`); err != nil || !strings.Contains(p.Summary, "Together: one agent works in all of them, in one chat.") {
		t.Errorf("together: %v\n%s", err, p.Summary)
	}
	p, err = f.planDelegateAs("chat-1", f.root, `{"project":"Acme","item":"flow"}`)
	if err != nil || !strings.Contains(p.Summary, "Workflow: bug-fix (a run takes each agent task; steps that name no agent: claude). Approval steps wait for you; the agent cannot approve them.") {
		t.Errorf("workflow: %v\n%s", err, p.Summary)
	}
	if p, err := f.planDelegateAs("chat-1", f.root, `{"project":"`+f.plan+`","item":"here"}`); err != nil ||
		!strings.Contains(p.Summary, "Folder: a new agent task (branch and worktree) from "+f.root+" (the item names no repository: this chat's folder)") {
		t.Errorf("no repository: %v\n%s", err, p.Summary)
	}
	if n := len(f.h.Requested()); n != 0 || f.read("rl") != "---\ntitle: Rate limit\nagent: backend\nrepos: api, web\n---\n"+long {
		t.Fatalf("Plan made %d calls or wrote the item", n)
	}
	// The workflow was checked as the agent's (refused, not asked, when it
	// has script steps the user has not allowed), and nothing stays planned.
	if len(runs.asked) != 1 || runs.asked[0].DelegatedBy == "" || runs.dropped != 1 {
		t.Errorf("workflow checks %+v, dropped %d", runs.asked, runs.dropped)
	}
}

// A successful delegation across two repositories makes the same tasks
// and runs as the click, linked to the item, and says who delegated them.
func TestDelegateToolDelegatesLikeTheClick(t *testing.T) {
	items := map[string]string{"x": item("X", "todo", "repos: api, web", "workflow: bug-fix")}
	click := newFixture(t, items)
	click.withWeb(true)
	click.open()
	click.withRuns("bug-fix")
	clicked := click.delegate("x")

	f := newFixture(t, items)
	web := f.withWeb(true)
	f.open()
	runs := f.withRuns("bug-fix")
	out, err := f.delegateAs("chat-7", f.root, `{"item":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	by := "an agent in " + f.root + " (chat chat-7)"
	reqs := f.h.Requested()
	if len(reqs) != 1 || len(reqs[0]) != 2 || reqs[0][0].Dir != f.root || reqs[0][1].Dir != web {
		t.Fatalf("requests %+v", reqs)
	}
	want := click.h.Requested()[0]
	for i, r := range reqs[0] {
		o, ok := parseOrigin(r.Origin)
		w, _ := parseOrigin(want[i].Origin)
		if !ok || o.By != by || r.DelegatedBy != by || o.Item != "x" || o.Repo != w.Repo ||
			r.Description != want[i].Description || r.Agent != want[i].Agent || r.Workflow != want[i].Workflow {
			t.Errorf("request %d: %+v, the click's %+v", i, r, want[i])
		}
	}
	// Checked once for each repository before the question, planned once
	// for each after the yes, always as the agent's.
	if len(runs.asked) != 4 || slices.ContainsFunc(runs.asked, func(r feature.NewAgentTask) bool { return r.DelegatedBy != by }) || runs.dropped != 2 {
		t.Errorf("runs planned %+v, dropped %d", runs.asked, runs.dropped)
	}
	ls := f.links("x")
	if len(ls) != 2 || ls[0].Repo != "api" || ls[1].Repo != "web" || ls[0].Workflow != "bug-fix" || ls[0].RunID == "" || len(clicked.Tasks) != 2 {
		t.Errorf("links %+v", ls)
	}
	if !strings.Contains(f.read("x"), "status: doing") {
		t.Errorf("status:\n%s", f.read("x"))
	}
	for _, want := range []string{"Delegated the item x (X): its status is doing.", "in api (working): workflow run r-", `of "bug-fix" (running)`, "wait_for_workflow_run"} {
		if !strings.Contains(out, want) {
			t.Errorf("answer lacks %q:\n%s", want, out)
		}
	}
	// get_project shows the item's runs as they are now.
	runs.set(ls[0].Task, "waiting")
	if desc := f.api.describe("", ""); !strings.Contains(desc, "runs: bug-fix · waiting for the user's approval (run r-"+ls[0].Task+", api); bug-fix · running (run r-"+ls[1].Task+", web)") {
		t.Errorf("get_project:\n%s", desc)
	}
}

func TestDelegateToolRefusals(t *testing.T) {
	f := newFixture(t, map[string]string{
		"ready":   item("Ready", "todo", "repos: api"),
		"dep":     item("Dep", "todo"),
		"waits":   item("Waits", "todo", "repos: api", "after: dep"),
		"web":     item("Web", "todo", "repos: web"),
		"scripts": item("Scripts", "todo", "repos: api", "workflow: check"),
	})
	f.open()
	f.withRuns("check")
	other := newProject(t, filepath.Join(f.base, "other"), map[string]string{"o": item("O", "todo", "repos: api")})
	write(t, filepath.Join(other, ConfigFile), `{"name":"Other","repositories":[{"name":"api","url":"git@github.com:acme/elsewhere.git"}]}`)
	f.h.SetTrusted(other)
	if _, err := f.api.Open(other); err != nil {
		t.Fatal(err)
	}
	untrustedPlan := newProject(t, filepath.Join(f.base, "untrusted-plan"), map[string]string{"u": item("U", "todo", "repos: api")})
	if _, err := f.api.Open(untrustedPlan); err != nil {
		t.Fatal(err)
	}
	f.allowDelegate()

	// An untrusted checkout of web, in the workspace.
	web := filepath.Join(f.base, "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	f.h.SetRepository(web, "git@github.com:acme/web.git")
	f.h.SetRoots(f.root, web)

	// A chat of an agent task (an item's agent, a workflow step's first chat).
	f.h.SetAgentTask(feature.AgentTask{ID: "t1", Worktree: f.root + "-t1"})
	if _, err := f.h.CreateAgentTasks(t.Context(), []feature.NewAgentTask{{Dir: f.root, Description: "d", Agent: "claude", Origin: "workflow:abc"}}); err != nil {
		t.Fatal(err)
	}
	var stepTask string
	for id, tk := range f.h.AllAgentTasks() {
		if f.h.Origin(id) == "workflow:abc" {
			stepTask = tk.ID
		}
	}
	f.h.SetChat("step-chat", stepTask)
	// A worktree of an agent task.
	wt := filepath.Join(f.base, "api-wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	f.h.SetRepository(wt, "git@github.com:acme/api.git")
	f.h.SetTaskWorktree(wt)
	f.h.SetTrusted(wt)
	// A trusted folder in no project.
	lone := filepath.Join(f.base, "lone")
	if err := os.MkdirAll(lone, 0o755); err != nil {
		t.Fatal(err)
	}
	f.h.SetRepository(lone, "git@github.com:acme/lone.git")
	f.h.SetTrusted(lone)

	for _, tc := range []struct {
		name, chat, root, args, want string
	}{
		{"unknown argument", "c", f.root, `{"item":"ready","now":true}`, "invalid arguments"},
		{"no item", "c", f.root, `{}`, "item is required"},
		{"bad item id", "c", f.root, `{"item":"../x"}`, "invalid item id"},
		{"unknown item", "c", f.root, `{"item":"nope"}`, `no project this chat's folder is in has an item "nope"`},
		{"unknown item of a project", "c", f.root, `{"project":"Acme","item":"nope"}`, "items/nope.md"},
		{"not ready", "c", f.root, `{"item":"waits"}`, "the item waits is not ready: it waits for dep"},
		{"untrusted project", "c", f.root, `{"project":"` + untrustedPlan + `","item":"u"}`, "an agent cannot delegate its items"},
		{"untrusted project by name", "c", f.root, `{"project":"Acme 2","item":"u"}`, "no open project"},
		{"untrusted checkout", "c", f.root, `{"item":"web"}`, "web (" + web + ") is in Restricted Mode"},
		{"a step's chat", "step-chat", f.root, `{"item":"ready"}`, "an agent that was handed work cannot hand out work"},
		{"a task's worktree", "c", wt, `{"item":"ready"}`, "an agent that was handed work cannot hand out work"},
		{"an untrusted folder", "c", web, `{"item":"ready"}`, "Restricted Mode"},
		{"out of scope", "c", lone, `{"project":"Acme","item":"ready"}`, `this chat's folder is not in the project "Acme"`},
		{"out of scope by folder", "c", f.root, `{"project":"` + other + `","item":"o"}`, `this chat's folder is not in the project`},
		{"unsandboxed scripts", "c", f.root, `{"item":"scripts"}`, "run this workflow once themselves"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "untrusted project by name" {
				write(t, filepath.Join(untrustedPlan, ConfigFile), `{"name":"Acme 2","repositories":[{"name":"api","url":"git@github.com:acme/api.git"}]}`)
			}
			if tc.name == "unsandboxed scripts" {
				f.h.SetTaskWorkflowCheck(func(_ context.Context, req feature.NewAgentTask) error {
					if req.DelegatedBy == "" {
						return errors.New("not the agent's")
					}
					return errors.New(`the workflow "check" has script steps that would run without a sandbox: ask the user to run this workflow once themselves in Flint`)
				})
				defer f.h.SetTaskWorkflowCheck(nil)
			}
			_, err := f.delegateAs(tc.chat, tc.root, tc.args)
			wantErr(t, err, tc.want)
		})
	}
	if n := len(f.h.Requested()); n != 1 { // the step task above
		t.Fatalf("%d CreateAgentTasks calls", n)
	}
	// The project's trust taken back between the question and the yes.
	p, err := f.planDelegateAs("c", f.root, `{"item":"ready"}`)
	if err != nil {
		t.Fatal(err)
	}
	f.h.SetUntrusted(f.plan)
	_, err = p.Run(mcpserver.WithCaller(t.Context(), "c"))
	wantErr(t, err, "no project this chat's folder is in")
	f.h.SetTrusted(f.plan)
	if n := len(f.h.Requested()); n != 1 {
		t.Fatalf("%d CreateAgentTasks calls", n)
	}
}

// At most 10 delegations per chat; a refused call is not counted, and a
// chat at its limit is never forgotten.
func TestDelegateToolLimit(t *testing.T) {
	items := map[string]string{}
	for i := range maxDelegationsPerChat + 1 {
		items[fmt.Sprintf("i%d", i)] = item(fmt.Sprintf("I%d", i), "todo", "repos: api")
	}
	items["waits"] = item("Waits", "todo", "repos: api", "after: i0")
	f := newFixture(t, items)
	f.open()
	if _, err := f.delegateAs("c", f.root, `{"item":"waits"}`); err == nil {
		t.Fatal("a waiting item was delegated")
	}
	f.h.SetCreateError(errors.New("disk full"))
	if _, err := f.delegateAs("c", f.root, `{"item":"i0"}`); err == nil {
		t.Fatal("a failed creation succeeded")
	}
	f.h.SetCreateError(nil)
	for i := range maxDelegationsPerChat {
		if _, err := f.delegateAs("c", f.root, fmt.Sprintf(`{"item":"i%d"}`, i)); err != nil {
			t.Fatalf("delegation %d: %v", i, err)
		}
	}
	last := fmt.Sprintf(`{"item":"i%d"}`, maxDelegationsPerChat)
	_, err := f.delegateAs("c", f.root, last)
	wantErr(t, err, "this chat delegated 10 items, the most one chat may")
	// Another chat has its own count.
	if _, err := f.delegateAs("d", f.root, last); err != nil {
		t.Fatal(err)
	}
	// Filling the counters never forgets the chat at its limit.
	f.api.mu.Lock()
	for i := len(f.api.delegated); i < maxDelegateCounters; i++ {
		f.api.delegated[fmt.Sprintf("x%d", i)] = 1
	}
	if !roomForDelegator(f.api.delegated) || f.api.delegated["c"] != maxDelegationsPerChat {
		t.Errorf("room: c = %d", f.api.delegated["c"])
	}
	f.api.mu.Unlock()
	full := map[string]int{}
	for i := range maxDelegateCounters {
		full[fmt.Sprint(i)] = maxDelegationsPerChat
	}
	if roomForDelegator(full) || len(full) != maxDelegateCounters {
		t.Error("a chat at its limit was forgotten")
	}
}

// The coordinator's projects: the project folder in the chat's folder, or
// a repository of the project checked out there; never for a worktree, and
// never an untrusted project.
func TestFolderProjects(t *testing.T) {
	f := newFixture(t, map[string]string{"x": item("X", "todo", "repos: api")})
	f.open()
	res := f.delegate("x")
	got := f.api.folderProjects(t.Context(), f.root)
	if !slices.Equal(got.Names, []string{"Acme"}) || !slices.Equal(got.Tasks, []string{res.Tasks[0].ID}) {
		t.Errorf("by repository: %+v", got)
	}
	if got := f.api.folderProjects(t.Context(), f.base); !slices.Equal(got.Names, []string{"Acme"}) {
		t.Errorf("the project inside the folder: %+v", got)
	}
	lone := filepath.Join(f.base, "lone")
	f.h.SetRepository(lone, "git@github.com:acme/lone.git")
	if got := f.api.folderProjects(t.Context(), lone); len(got.Names)+len(got.Tasks) != 0 {
		t.Errorf("a folder in no project: %+v", got)
	}
	wt := filepath.Join(f.base, "api-wt")
	f.h.SetRepository(wt, "git@github.com:acme/api.git")
	f.h.SetTaskWorktree(wt)
	if got := f.api.folderProjects(t.Context(), wt); len(got.Names)+len(got.Tasks) != 0 {
		t.Errorf("a worktree: %+v", got)
	}
	f.h.SetUntrusted(f.plan)
	if got := f.api.folderProjects(t.Context(), f.root); len(got.Names)+len(got.Tasks) != 0 {
		t.Errorf("an untrusted project: %+v", got)
	}
}
