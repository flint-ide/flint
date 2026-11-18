package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/mcptools"
	"flint/internal/settings"
)

// gitProject turns dir into a repository on main with one commit, for tests
// that need worktrees.
func gitProject(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	gitT(t, dir, "init", "-q", "-b", "main")
	gitT(t, dir, "config", "user.name", "T")
	gitT(t, dir, "config", "user.email", "t@example.com")
	gitT(t, dir, "config", "commit.gpgsign", "false")
	commitT(t, dir, "a.txt", "one\n", "initial")
}

// worktreeApp is an orchestration app whose project is a git repository the
// window has open, with an agent that tasks can use.
func worktreeApp(t *testing.T) *orchApp {
	t.Helper()
	o := newOrchApp(t)
	gitProject(t, o.project)
	o.settings.CustomAgents = []settings.CustomAgent{{ID: "fake", Command: "sh"}}
	o.settings.AgentTasks.MaxParallel = 0
	o.openProject(o.project, nil)
	return o
}

// taskOfChat is the task whose worktree the chat works in.
func taskOfChat(t *testing.T, o *orchApp, chat string) agenttask.Task {
	t.Helper()
	n, ok := o.orch.Get(chat)
	if !ok {
		t.Fatalf("chat %s is not registered", chat)
	}
	_, task, ok := o.taskStore().Find(o.taskByFolder(n.Cwd))
	if !ok {
		t.Fatalf("no task has the worktree %s", n.Cwd)
	}
	return task
}

func TestStartAgentWithWorktreeRunsTheChildInATask(t *testing.T) {
	o := worktreeApp(t)
	id := o.start(t, `{"task":"add a retry button","agent":"fake","worktree":true}`)

	task := taskOfChat(t, o, id)
	if task.Branch == "" || task.Base != "main" || !strings.Contains(task.Prompt, "retry button") {
		t.Errorf("task = %+v", task)
	}
	if gitT(t, task.Worktree, "branch", "--show-current") != task.Branch {
		t.Error("the child's folder is not a worktree on the task's branch")
	}
	if !o.isTrusted(task.Worktree) {
		t.Error("the worktree of a trusted repository is not trusted")
	}
	if got := o.driver.waitPrompts(t, id, 1); !strings.Contains(got[0], "retry button") {
		t.Errorf("first prompt = %q", got)
	}
	var opened *ChatOpened
	o.chatApp.mu.Lock()
	for i := range o.chatApp.opened {
		if o.chatApp.opened[i].Info.ID == id {
			opened = &o.chatApp.opened[i]
		}
	}
	o.chatApp.mu.Unlock()
	if opened == nil || opened.TaskID != task.ID || opened.Parent != o.chat || !opened.Background {
		t.Errorf("chat:opened = %+v", opened)
	}
	// The slot held for the start is given back.
	o.queue.startMu.Lock()
	held := len(o.queue.starting)
	o.queue.startMu.Unlock()
	if held != 0 {
		t.Errorf("%d tasks still hold the slot reserved for a start", held)
	}
}

func TestParentReachesItsWorktreeChild(t *testing.T) {
	o := worktreeApp(t)
	id := o.start(t, `{"task":"work","agent":"fake","worktree":true}`)
	o.idle(t, id)

	var list struct {
		Agents []struct{ ID, Cwd, Kind string }
	}
	if err := json.Unmarshal([]byte(o.ok(t, "list_agents", `{}`)), &list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range list.Agents {
		if a.ID == id {
			found = true
			if a.Kind != "chat" || a.Cwd == "" {
				t.Errorf("listed as %+v", a)
			}
		}
	}
	if !found {
		t.Fatalf("list_agents does not show the worktree child: %+v", list.Agents)
	}
	o.ok(t, "send_prompt", `{"id":"`+id+`","text":"and tests please"}`)
	o.ok(t, "wait_for_agent", `{"id":"`+id+`","until":["idle","finished","failed"],"timeoutSeconds":5}`)
	o.ok(t, "read_agent_output", `{"id":"`+id+`"}`)
	o.idle(t, id)
	o.ok(t, "stop_agent", `{"id":"`+id+`"}`)
	if _, ok := o.orch.Get(id); ok {
		t.Error("the stopped child is still registered")
	}
}

func TestWorktreeChildIsUnreachableFromAnotherProject(t *testing.T) {
	o := worktreeApp(t)
	id := o.start(t, `{"task":"work","agent":"fake","worktree":true}`)
	o.idle(t, id)

	// Another project, also a repository, with its own chat and token.
	other := t.TempDir()
	gitProject(t, other)
	o.state.TrustedFolders = append(o.state.TrustedFolders, cleanFolder(other))
	o.openProject(other, nil)
	stranger, err := o.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: other})
	if err != nil {
		t.Fatal(err)
	}
	url, token, _, err := o.mcp.Endpoint(other, func() string { return stranger.ID })
	if err != nil {
		t.Fatal(err)
	}
	ownURL, ownToken := o.url, o.token
	o.url, o.token = url, token
	defer func() { o.url, o.token = ownURL, ownToken }()

	o.ok(t, "list_agents", `{}`) // the grant for the other project
	text := o.ok(t, "list_agents", `{}`)
	if strings.Contains(text, id) {
		t.Errorf("another project sees the worktree child: %s", text)
	}
	for _, call := range []struct{ tool, args string }{
		{"send_prompt", `{"id":"` + id + `","text":"hi"}`},
		{"wait_for_agent", `{"id":"` + id + `","until":["idle"],"timeoutSeconds":1}`},
		{"read_agent_output", `{"id":"` + id + `"}`},
		{"stop_agent", `{"id":"` + id + `"}`},
	} {
		o.fails(t, call.tool, call.args, "not in this folder")
	}
	if _, ok := o.orch.Get(id); !ok {
		t.Error("the child was stopped by a stranger")
	}
}

func TestWorktreeStartObeysLimitsAndLeavesNothingWhenRefused(t *testing.T) {
	o := worktreeApp(t)
	o.ok(t, "list_agents", `{}`)             // the grant records the limits of today (4 agents)
	o.settings.Orchestration.MaxChildren = 1 // a later, lower setting still applies
	first := o.start(t, `{"task":"one","agent":"fake","worktree":true}`)
	// The fake agent reports no events: end its turn for the task tracker.
	o.tasks.Observe(as.Event{SessionID: first, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
	before := len(o.taskStore().List(repoKeyOf(t, o.project)))

	o.fails(t, "start_agent", `{"task":"two","agent":"fake","worktree":true}`, "you already have 1 agents")
	if n := len(o.taskStore().List(repoKeyOf(t, o.project))); n != before {
		t.Errorf("a refused start left %d new task(s)", n-before)
	}

	// The limit of tasks working at once refuses; it does not queue.
	o.settings.Orchestration.MaxChildren = 4
	o.settings.AgentTasks.MaxParallel = 1
	hold := make(chan struct{})
	o.driver.mu.Lock()
	o.driver.hold = hold
	o.driver.mu.Unlock()
	t.Cleanup(func() { close(hold) })
	o.start(t, `{"task":"three","agent":"fake","worktree":true}`) // takes the one slot
	o.fails(t, "start_agent", `{"task":"four","agent":"fake","worktree":true}`, "limit of 1 tasks")
	for _, task := range o.taskStore().List(repoKeyOf(t, o.project)) {
		if task.Queued {
			t.Errorf("a refused worktree start was queued: %+v", task)
		}
	}

	// A branch that does not exist is refused before anything is made.
	o.settings.AgentTasks.MaxParallel = 0
	o.fails(t, "start_agent", `{"task":"five","agent":"fake","worktree":true,"base":"no-such-branch"}`, "not a local branch")
}

func TestWorktreeStartNeedsATrustedProject(t *testing.T) {
	// The MCP server itself is off for an untrusted project; this is the
	// check behind it, for the grant that outlives a lost trust.
	o := worktreeApp(t)
	o.state.TrustedFolders = nil
	orch := &orchestrator{app: o.App}
	_, err := orch.newWorktree(context.Background(), o.project, "fake", mcptools.StartRequest{Task: "x", Worktree: true})
	if err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("newWorktree in an untrusted project = %v", err)
	}
	if n := len(o.taskStore().List(repoKeyOf(t, o.project))); n != 0 {
		t.Errorf("%d task(s) were made", n)
	}
}
