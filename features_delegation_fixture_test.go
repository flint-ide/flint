package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/feature"
	"flint/internal/features/projects"
	"flint/internal/features/workflows"
	"flint/internal/mcpserver"
	"flint/internal/runner"
)

// The tests in features_delegation_test.go run the two REAL feature modules,
// Projects and Workflows, together on the REAL featureHost: an agent chat
// calls delegate_project_item and the workflow tools through Flint's MCP
// server (a real token for the chat's folder and id, the approval question
// answered by a.ask), real git repositories, and the fake agent of the other
// root tests. This file is their fixture.

// dlgEvents records the chats Flint opens and wakes whoever waits when any
// event is emitted (tasks:changed, workflows:changed, projects:changed, ...),
// so a test waits for a condition instead of sleeping.
type dlgEvents struct {
	mu     sync.Mutex
	wake   chan struct{}
	opened []ChatOpened
}

func newDlgEvents() *dlgEvents { return &dlgEvents{wake: make(chan struct{})} }

func (e *dlgEvents) hook(name string, data ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if name == eventChatOpened {
		e.opened = append(e.opened, data[0].(ChatOpened))
	}
	close(e.wake)
	e.wake = make(chan struct{})
}

func (e *dlgEvents) chats() []ChatOpened {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]ChatOpened(nil), e.opened...)
}

// until waits for cond, which is looked at again after every event; the
// timeout only bounds a test that would hang.
func (e *dlgEvents) until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()
	for {
		e.mu.Lock()
		wake := e.wake
		e.mu.Unlock()
		if cond() {
			return
		}
		select {
		case <-wake:
		case <-timeout.C:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// The workflows the repositories offer: an agent step and an approval step;
// and one with a script step.
const (
	dlgShip  = "---\ndescription: Fix, then approve\n---\n\n## Fix\n\nFix the bug.\n\n## Approve\ntype: gate\n"
	dlgCheck = "## Plan\n\nPlan it.\n\n## Test\ntype: script\nrun: test\n"
)

const (
	apiRemote = "git@github.com:acme/api.git"
	webRemote = "git@github.com:acme/web.git"
)

// dlgItem is an item file of the project.
func dlgItem(title string, fields ...string) string {
	lines := append([]string{"title: " + title, "status: todo", "agent: fake"}, fields...)
	return "---\n" + strings.Join(lines, "\n") + "\n---\nLimit it.\n"
}

// dlgFixture is an App with two open, trusted repositories (api, web: git
// repositories with the project's remote URLs), the project folder next to
// them, both modules started on the real host, Flint's MCP server on with
// the delegate and workflow tools switched on, and the user's answer to
// every approval question under the test's control.
type dlgFixture struct {
	*taskFixture
	pr        *projects.API
	wf        *workflows.API
	ev        *dlgEvents
	web, plan string
	url       string

	mu     sync.Mutex
	asked  []string
	answer bool
	tokens map[string]string
}

// newDlgFixture builds it; limit is agentTasks.maxParallel (0: none).
func newDlgFixture(t *testing.T, limit int, items map[string]string) *dlgFixture {
	t.Helper()
	if err := registerFeatures(); err != nil {
		t.Fatal(err)
	}
	f := &dlgFixture{taskFixture: queueFixture(t, limit), tokens: map[string]string{}, answer: true}
	a := f.app
	gitT(t, f.repo, "remote", "add", "origin", apiRemote)
	f.web = f.addRepoT("web", webRemote)
	for _, repo := range []string{f.repo, f.web} {
		commitT(t, repo, filepath.Join(".flint", "workflows", "ship.md"), dlgShip, "add ship")
		commitT(t, repo, filepath.Join(".flint", "workflows", "check.md"), dlgCheck, "add check")
	}

	a.runEvents = newRunEvents(a)
	a.runs = runner.NewManager(a.runEvents)
	a.mcp, a.mcpSources = newMCP(a)
	t.Cleanup(func() {
		a.runs.CloseAll()
		_ = a.mcp.Close()
	})
	a.ask = func(_, message, _ string) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.asked = append(f.asked, message)
		return f.answer
	}
	s := a.currentSettings()
	s.MCPServer.Enabled = true
	s.MCPServer.Actions.Delegate = true
	s.MCPServer.Actions.Workflows = true
	s.MCPServer.Actions.Projects = true
	a.setSettingsForTest(s)

	f.ev = newDlgEvents()
	a.emitHook = f.ev.hook
	h := &endingHost{featureHost: featureHost{a}}
	a.features = feature.New([]feature.Module{projects.Module, workflows.Module}, h)
	a.features.Start(t.Context())
	t.Cleanup(func() {
		h.mu.Lock()
		h.ended = true
		h.mu.Unlock()
		for _, c := range a.features.Closers() {
			c()
		}
	})
	for _, svc := range a.features.Services() {
		switch api := svc.Instance().(type) {
		case *projects.API:
			f.pr = api
		case *workflows.API:
			f.wf = api
		}
	}
	if f.pr == nil || f.wf == nil {
		t.Fatal("a module has no service")
	}
	f.plan = planT(t, a, f.pr, filepath.Dir(f.repo),
		[][2]string{{"api", apiRemote}, {"web", webRemote}}, items, "Acme is a shop.\n")
	return f
}

// setAnswer is the user's answer to the next approval questions.
func (f *dlgFixture) setAnswer(yes bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answer = yes
}

func (f *dlgFixture) questions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// token is an MCP token of the chat chat working in root, as the server
// issues one to an agent session (the chat id is what CallerFrom answers).
func (f *dlgFixture) token(t *testing.T, chat, root string) string {
	t.Helper()
	key := chat + "\x00" + root
	f.mu.Lock()
	defer f.mu.Unlock()
	if tok, ok := f.tokens[key]; ok {
		return tok
	}
	url, tok, _, err := f.app.mcp.Endpoint(root, func() string { return chat })
	if err != nil {
		t.Fatal(err)
	}
	f.url, f.tokens[key] = url, tok
	return tok
}

// call calls a tool through the MCP server as chat in root: the text of the
// answer and whether the call failed.
func (f *dlgFixture) call(t *testing.T, chat, root, name, args string) (string, bool) {
	t.Helper()
	token := f.token(t, chat, root)
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args)
	status, reply := mcpPost(t, f.url, token, body)
	var r struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(reply), &r); err != nil || status != http.StatusOK {
		t.Fatalf("%s: %d %s (%v)", name, status, reply, err)
	}
	if r.Error != nil {
		return r.Error.Message, true
	}
	if len(r.Result.Content) != 1 {
		t.Fatalf("%s: %s", name, reply)
	}
	return r.Result.Content[0].Text, r.Result.IsError
}

// listed reports whether the server lists the tool for chat in root.
func (f *dlgFixture) listed(t *testing.T, chat, root, name string) bool {
	t.Helper()
	token := f.token(t, chat, root)
	status, body := mcpPost(t, f.url, token, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if status != http.StatusOK {
		t.Fatalf("tools/list: %d %s", status, body)
	}
	return strings.Contains(body, `"`+name+`"`)
}

// agentTool is a tool of the feature set by name.
func (f *dlgFixture) agentTool(t *testing.T, name string) mcpserver.Tool {
	t.Helper()
	for _, tl := range f.app.features.AgentTools() {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("the tool %s is not offered", name)
	return mcpserver.Tool{}
}

// callDirect makes one call of a tool as the server does, without HTTP:
// Plan, then (the user said yes) Run.
func (f *dlgFixture) callDirect(t *testing.T, chat, root, name, args string) (string, error) {
	t.Helper()
	ctx := mcpserver.WithCaller(t.Context(), chat)
	tl := f.agentTool(t, name)
	if !tl.Acts {
		return tl.Call(ctx, root, json.RawMessage(args))
	}
	p, err := tl.Plan(ctx, root, json.RawMessage(args))
	if err != nil {
		return "", err
	}
	return p.Run(ctx)
}

// delegate delegates item as the coordinator chat "coord" in the api
// repository, the user saying yes, and returns the answer.
func (f *dlgFixture) delegate(t *testing.T, item string) string {
	t.Helper()
	f.setAnswer(true)
	out, failed := f.call(t, "coord", f.repo, projects.DelegateToolName, `{"item":"`+item+`"}`)
	if failed {
		t.Fatalf("delegate %s: %s", item, out)
	}
	return out
}

// status is the item's status in its file.
func (f *dlgFixture) status(t *testing.T, id string) string {
	t.Helper()
	data, _, err := projects.ReadItemFile(f.plan, id)
	if err != nil {
		t.Fatal(err)
	}
	c, err := projects.LoadConfig(f.plan)
	if err != nil {
		t.Fatal(err)
	}
	it, err := projects.ParseItem(id, data, c)
	if err != nil {
		t.Fatal(err)
	}
	return it.Status
}

// runIn is the newest run of the workflow runs whose checkout is dir.
func (f *dlgFixture) runIn(dir string) (workflows.RunView, bool) {
	for _, v := range f.wf.Runs() { // newest first
		if v.Dir == dir {
			return v, true
		}
	}
	return workflows.RunView{}, false
}

// runState is the state of the run in dir ("" when there is none).
func (f *dlgFixture) runState(dir string) string {
	v, _ := f.runIn(dir)
	return v.State
}

// firstChat is the chat Flint opened for the task (waited for).
func (f *dlgFixture) firstChat(t *testing.T, taskID string) string {
	t.Helper()
	var id string
	f.ev.until(t, "the chat of task "+taskID, func() bool {
		for _, c := range f.ev.chats() {
			if c.TaskID == taskID {
				id = c.Info.ID
				return true
			}
		}
		return false
	})
	return id
}

// finish ends the turn of chat saying result (the agent's last line).
func (f *dlgFixture) finish(chat, result string) {
	// A turn can end only after its prompt reached the agent: a step's chat
	// is announced (chat:opened) before Flint sends the prompt, and a turn
	// end that arrives in between would be dropped, leaving the chat working.
	for i := 0; i < 500 && len(f.driver.got(chat)) == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	f.app.orch.Observe(as.Event{SessionID: chat, Kind: as.KindMessageChunk, Text: result})
	f.app.orch.Observe(as.Event{SessionID: chat, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
	f.app.tasks.Observe(as.Event{SessionID: chat, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
}

// taskOf is the agent task of the run in dir.
func (f *dlgFixture) taskOf(t *testing.T, dir string) string {
	t.Helper()
	v, ok := f.runIn(dir)
	if !ok || v.Task == "" {
		t.Fatalf("no run with a task in %s", dir)
	}
	return v.Task
}

// taskState is the state of the agent task id as the panel shows it.
func (f *dlgFixture) taskState(id string) string {
	return (featureHost{f.app}).AgentTasks(context.Background(), []string{id})[id].State
}

// worktreeOf is the worktree of the agent task id.
func (f *dlgFixture) worktreeOf(id string) string {
	return (featureHost{f.app}).AgentTasks(context.Background(), []string{id})[id].Worktree
}

// world is what a refused call must leave as it was.
type dlgWorld struct {
	tasks      int
	runs       int
	branches   map[string]string
	worktrees  map[string]string
	items      map[string]string
	worktreeFS map[string]bool
}

func (f *dlgFixture) world(t *testing.T) dlgWorld {
	t.Helper()
	w := dlgWorld{runs: len(f.wf.Runs()), branches: map[string]string{}, worktrees: map[string]string{}, items: map[string]string{}, worktreeFS: map[string]bool{}}
	for _, tasks := range f.app.taskStore() {
		w.tasks += len(tasks)
	}
	for _, repo := range []string{f.repo, f.web} {
		w.branches[repo] = gitT(t, repo, "branch", "--list")
		w.worktrees[repo] = gitT(t, repo, "worktree", "list", "--porcelain")
		_, err := os.Stat(repo + ".worktrees")
		w.worktreeFS[repo] = err == nil
	}
	entries, err := os.ReadDir(filepath.Join(f.plan, projects.ItemsDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(f.plan, projects.ItemsDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		w.items[e.Name()] = string(data)
	}
	return w
}

// same fails the test unless the world is as before.
func (w dlgWorld) same(t *testing.T, after dlgWorld) {
	t.Helper()
	if after.tasks != w.tasks || after.runs != w.runs {
		t.Errorf("tasks %d -> %d, runs %d -> %d", w.tasks, after.tasks, w.runs, after.runs)
	}
	for repo, b := range w.branches {
		if after.branches[repo] != b {
			t.Errorf("%s branches changed:\n%s\n->\n%s", repo, b, after.branches[repo])
		}
		if after.worktrees[repo] != w.worktrees[repo] || after.worktreeFS[repo] != w.worktreeFS[repo] {
			t.Errorf("%s worktrees changed:\n%s\n->\n%s", repo, w.worktrees[repo], after.worktrees[repo])
		}
	}
	for name, text := range w.items {
		if after.items[name] != text {
			t.Errorf("item %s changed:\n%s\n->\n%s", name, text, after.items[name])
		}
	}
}
