package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"flint/internal/agentdefs"
	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/config"
	"flint/internal/settings"
)

// folderDriver is a fake Claude Code: it records the additional folders each
// chat was started or resumed with (agentsession.WithFolders) and hands
// every prompt to prompts; turns stay running until the test ends.
type folderDriver struct {
	mu      sync.Mutex
	next    int
	folders map[string][]string // session → its additional folders
	cwds    map[string]string
	stopped []string
	prompts chan string
	hold    chan struct{}
}

func newFolderDriver(t *testing.T) *folderDriver {
	d := &folderDriver{folders: map[string][]string{}, cwds: map[string]string{}, prompts: make(chan string, 16), hold: make(chan struct{})}
	t.Cleanup(func() { close(d.hold) })
	return d
}

func (d *folderDriver) record(ctx context.Context, id, cwd string) as.SessionInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.folders[id], d.cwds[id] = as.Folders(ctx), cwd
	return as.SessionInfo{ID: id, Cwd: cwd, Model: "m"}
}

func (d *folderDriver) Start(ctx context.Context, cwd string, _ as.Sink) (as.SessionInfo, error) {
	d.mu.Lock()
	d.next++
	id := fmt.Sprintf("j%d", d.next) // not c1…: the fake custom agent's ids
	d.mu.Unlock()
	return d.record(ctx, id, cwd), nil
}

func (d *folderDriver) Resume(ctx context.Context, cwd, id string, _ as.Sink) (as.SessionInfo, error) {
	return d.record(ctx, id, cwd), nil
}

func (d *folderDriver) Prompt(ctx context.Context, _, text string, _ []as.Attachment) (string, error) {
	d.prompts <- text
	select {
	case <-d.hold:
	case <-ctx.Done():
	}
	return as.StopEndTurn, nil
}

func (d *folderDriver) Cancel(string) error          { return nil }
func (d *folderDriver) Respond(_, _, _ string) error { return nil }
func (d *folderDriver) Close() error                 { return nil }
func (d *folderDriver) Stop(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = append(d.stopped, id)
	return nil
}

// chats lists the sessions started or resumed, by folder.
func (d *folderDriver) chats() map[string][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string][]string{}
	for id, cwd := range d.cwds {
		out[cwd] = append(out[cwd], id)
	}
	return out
}

func (d *folderDriver) foldersOf(id string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.folders[id]
}

// prompt waits for the next prompt the agent is handed (turns run in the
// background); a deadline only fails the test.
func (d *folderDriver) prompt(t *testing.T) string {
	t.Helper()
	select {
	case p := <-d.prompts:
		return p
	case <-time.After(10 * time.Second):
		t.Fatal("the agent was handed no prompt")
		return ""
	}
}

// jointFixture is a queue fixture (limit maxParallel) with n trusted
// repositories open in the window and "claude" installed: a fake Claude Code
// (folderDriver) behind the session manager.
type jointFixture struct {
	*taskFixture
	repos  []string
	claude *folderDriver
}

func initRepoT(t *testing.T, repo string) {
	t.Helper()
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "init", "-q", "-b", "main")
	gitT(t, repo, "config", "user.name", "T")
	gitT(t, repo, "config", "user.email", "t@example.com")
	gitT(t, repo, "config", "commit.gpgsign", "false")
	commitT(t, repo, "a.txt", "one\n", "initial")
}

func newJointFixture(t *testing.T, n, maxParallel int) *jointFixture {
	t.Helper()
	f := queueFixture(t, maxParallel)
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh to stand for claude")
	}
	f.app.settings.Agents["claude"] = settings.AgentSettings{Path: sh}
	d := newFolderDriver(t)
	f.app.agentMgr.Register("claude", d)
	j := &jointFixture{taskFixture: f, repos: []string{f.repo}, claude: d}
	for i := 1; i < n; i++ {
		repo := filepath.Join(filepath.Dir(f.repo), fmt.Sprintf("repo%d", i))
		initRepoT(t, repo)
		if err := f.app.addRoot(repo, nil); err != nil {
			t.Fatal(err)
		}
		if err := f.app.setTrust(repo, true); err != nil {
			t.Fatal(err)
		}
		j.repos = append(j.repos, repo)
	}
	return j
}

func (j *jointFixture) requests(desc string) []jointRequest {
	var out []jointRequest
	for _, r := range j.repos {
		out = append(out, jointRequest{Dir: r, Description: desc})
	}
	return out
}

// createJoint makes a joint group over every repository and starts it, as a
// Host method would.
func (j *jointFixture) createJoint(desc string) []agenttask.Task {
	j.t.Helper()
	made, err := j.app.createJointTasks(context.Background(), j.requests(desc), "claude", true)
	if err != nil {
		j.t.Fatal(err)
	}
	j.app.startJointTasks(made)
	j.waitStarts()
	return made
}

func worktrees(tasks []agenttask.Task) []string {
	var out []string
	for _, t := range tasks {
		out = append(out, t.Worktree)
	}
	return out
}

// jointStates is states over every repository's tasks.
func (j *jointFixture) jointStates(tasks ...agenttask.Task) string {
	j.t.Helper()
	j.waitStarts()
	list, err := j.api.ListAll(j.repos)
	if err != nil {
		j.t.Fatal(err)
	}
	var out []string
	for _, task := range tasks {
		state := "gone"
		for _, l := range list {
			if l.ID == task.ID {
				state = l.State
			}
		}
		out = append(out, state)
	}
	return strings.Join(out, " ")
}

func TestJointCreate(t *testing.T) {
	for _, n := range []int{2, 3} {
		t.Run(fmt.Sprintf("%d repositories", n), func(t *testing.T) {
			j := newJointFixture(t, n, 0)
			made := j.createJoint("Rename the API field")
			if len(made) != n {
				t.Fatalf("made %d tasks", len(made))
			}
			store := j.app.taskStore()
			for i, task := range made {
				_, saved, _ := store.Find(task.ID)
				if saved.Joint == "" || saved.Joint != made[0].Joint || saved.Lead != (i == 0) || saved.Group != "" || saved.Queued {
					t.Errorf("task %d = %+v", i, saved)
				}
				if gitT(t, task.Worktree, "branch", "--show-current") != task.Branch {
					t.Errorf("task %d: worktree not on its branch", i)
				}
				if !within(task.Worktree, filepath.Dir(j.repos[i])) || !j.app.isTrusted(task.Worktree) {
					t.Errorf("task %d: worktree %s (trusted %v)", i, task.Worktree, j.app.isTrusted(task.Worktree))
				}
			}
			// One chat, in the lead's worktree, with exactly the others' worktrees.
			chats := j.claude.chats()
			if len(chats) != 1 || len(chats[made[0].Worktree]) != 1 {
				t.Fatalf("chats = %v", chats)
			}
			if got, want := j.claude.foldersOf(chats[made[0].Worktree][0]), worktrees(made[1:]); !slices.Equal(got, want) {
				t.Errorf("folders = %v, want %v", got, want)
			}
			prompt := j.claude.prompt(t)
			for _, task := range made {
				if !strings.Contains(prompt, task.Worktree) || !strings.Contains(prompt, task.Branch) {
					t.Errorf("the prompt does not name %s on %s:\n%s", task.Worktree, task.Branch, prompt)
				}
			}
			if !strings.HasPrefix(prompt, "Rename the API field") || !strings.Contains(prompt, "consistent") {
				t.Errorf("prompt:\n%s", prompt)
			}
			// Every task follows the one chat, and the group holds one slot.
			if got := j.jointStates(made...); got != strings.TrimSpace(strings.Repeat("working ", n)) {
				t.Errorf("states = %s", got)
			}
			if a := j.app.activeTasks(); a != 1 {
				t.Errorf("active = %d, want 1", a)
			}
			list, _ := j.api.ListAll(j.repos)
			for _, v := range list {
				if v.Joint != made[0].Joint || v.Lead != (v.ID == made[0].ID) {
					t.Errorf("view = %+v", v)
				}
			}
		})
	}
}

func TestJointCreateIsAllOrNothing(t *testing.T) {
	j := newJointFixture(t, 3, 0)
	gitT(t, j.repos[1], "checkout", "-q", "--detach") // the second one cannot start a task
	_, err := j.app.createJointTasks(context.Background(), j.requests("Rename"), "claude", true)
	if !errors.Is(err, errDetachedHead) {
		t.Fatalf("err = %v", err)
	}
	if n := len(j.app.taskStore()); n != 0 {
		t.Errorf("tasks kept: %v", j.app.taskStore())
	}
	if out := gitT(t, j.repos[0], "worktree", "list"); strings.Count(out, "\n") != 0 {
		t.Errorf("worktrees kept:\n%s", out)
	}
	if out := gitT(t, j.repos[0], "branch", "--list", "agent/*"); out != "" {
		t.Errorf("branches kept: %s", out)
	}
	if a := j.app.activeTasks(); a != 0 {
		t.Errorf("a slot is still held: %d", a)
	}
	j.app.queue.startMu.Lock()
	held := len(j.app.queue.held)
	j.app.queue.startMu.Unlock()
	if held != 0 {
		t.Errorf("%d tasks still held", held)
	}
	if len(j.claude.chats()) != 0 {
		t.Error("a chat started")
	}
}

func TestJointCreateRefusals(t *testing.T) {
	j := newJointFixture(t, 2, 0)
	codex, _ := exec.LookPath("sh")
	j.app.settings.Agents["codex"] = settings.AgentSettings{Path: codex}
	untrusted := filepath.Join(filepath.Dir(j.repo), "untrusted")
	initRepoT(t, untrusted)
	if err := j.app.addRoot(untrusted, nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		reqs  []jointRequest
		agent string
		is    error
		want  string
	}{
		{"codex", j.requests("x"), "codex", errJointEngine, "codex runs on codex, which cannot work in several folders at once: delegate per repository instead"},
		{"a custom ACP agent", j.requests("x"), "fake", errJointEngine, "fake cannot work in several folders at once"},
		{"an untrusted repository", []jointRequest{{j.repos[0], "x"}, {untrusted, "x"}}, "claude", errJointUntrusted, "not trusted"},
		{"one repository", j.requests("x")[:1], "claude", nil, "at least two"},
		{"the same repository twice", []jointRequest{{j.repos[0], "x"}, {j.repos[0], "y"}}, "claude", nil, "same repository"},
	} {
		_, err := j.app.createJointTasks(context.Background(), c.reqs, c.agent, false)
		if err == nil || (c.is != nil && !errors.Is(err, c.is)) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
	if len(j.app.taskStore()) != 0 {
		t.Errorf("a refused group left tasks: %v", j.app.taskStore())
	}
}

func TestJointGroupTakesOneSlotAndQueuesAsAUnit(t *testing.T) {
	j := newJointFixture(t, 3, 2)
	a := j.create("first")
	j.work(a, "sa")
	// One slot is free: the group takes it, whatever its size.
	made := j.createJoint("Joint")
	if got := j.jointStates(made...); got != "working working working" {
		t.Fatalf("states = %s", got)
	}
	if b := j.create("second"); j.states(b) != "queued" {
		t.Fatalf("the group took more than one slot: %s", j.states(b))
	}

	// With no slot free, the whole group is queued, and nothing starts.
	k := newJointFixture(t, 2, 1)
	ka := k.create("first")
	k.work(ka, "sa")
	queued := k.createJoint("Joint")
	if got := k.jointStates(queued...); got != "queued queued" {
		t.Fatalf("states = %s", got)
	}
	for _, task := range queued {
		if _, saved, _ := k.app.taskStore().Find(task.ID); !saved.Queued {
			t.Errorf("%s is not queued in the store", task.ID)
		}
	}
	if len(k.claude.chats()) != 0 {
		t.Fatal("a queued group's chat started")
	}
	// A slot comes free: the group leaves the queue whole, with one chat.
	k.settle("sa")
	if got := k.jointStates(queued...); got != "working working" {
		t.Fatalf("after the slot came free: %s", got)
	}
	if chats := k.claude.chats(); len(chats) != 1 || len(chats[queued[0].Worktree]) != 1 {
		t.Fatalf("chats = %v", chats)
	}
	if !strings.Contains(k.claude.prompt(t), queued[1].Worktree) {
		t.Error("the queued group's prompt lacks the other worktree")
	}
	if a := k.app.activeTasks(); a != 1 {
		t.Errorf("active = %d", a)
	}
}

func TestJointStartNowOnAMember(t *testing.T) {
	j := newJointFixture(t, 3, 1)
	a := j.create("first")
	j.work(a, "sa")
	made := j.createJoint("Joint")
	// A worktree in Restricted Mode: the group stays queued.
	if err := j.app.untrustTask(made[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := j.api.StartNow(made[2].ID); !errors.Is(err, errJointUntrusted) {
		t.Fatalf("StartNow with an untrusted worktree = %v", err)
	}
	if err := j.app.setTrust(made[1].Worktree, true); err != nil {
		t.Fatal(err)
	}
	v, err := j.api.StartNow(made[2].ID) // a member, not the lead
	if err != nil {
		t.Fatal(err)
	}
	if v.ID != made[2].ID || v.Joint != made[0].Joint || v.State == agenttask.Queued {
		t.Errorf("view = %+v", v)
	}
	if got := j.jointStates(made...); got != "working working working" {
		t.Fatalf("states = %s", got)
	}
	chats := j.claude.chats()
	if len(chats) != 1 || len(chats[made[0].Worktree]) != 1 {
		t.Fatalf("chats = %v", chats)
	}
	if got := j.claude.foldersOf(chats[made[0].Worktree][0]); !slices.Equal(got, worktrees(made[1:])) {
		t.Errorf("folders = %v", got)
	}
	if _, err := j.api.StartNow(made[1].ID); err == nil || !strings.Contains(err.Error(), "not queued") {
		t.Errorf("starting the group twice = %v", err)
	}
}

func TestJointSettlesAndPersistsEveryTask(t *testing.T) {
	j := newJointFixture(t, 3, 0)
	made := j.createJoint("Joint")
	chat := j.claude.chats()[made[0].Worktree][0]
	j.app.tasks.Observe(as.Event{SessionID: chat, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
	if got := j.jointStates(made...); got != "finished finished finished" {
		t.Fatalf("states = %s", got)
	}
	var saved appState
	if err := config.LoadJSON(j.app.statePath(), &saved); err != nil {
		t.Fatal(err)
	}
	for _, task := range made {
		if _, s, _ := saved.AgentTasks.Find(task.ID); s.State != agenttask.Finished {
			t.Errorf("%s saved as %q", task.ID, s.State)
		}
	}
}

func TestJointChatFolders(t *testing.T) {
	j := newJointFixture(t, 3, 0)
	made := j.createJoint("Joint")
	chats := &AgentChatAPI{app: j.app}
	// A resumed chat in the lead's worktree gets the folders again, from Go.
	info, err := chats.Resume("claude", made[0].Worktree, "resumed", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := j.claude.foldersOf(info.ID); !slices.Equal(got, worktrees(made[1:])) {
		t.Errorf("resumed with %v", got)
	}
	if got := j.app.tasks.Tasks(info.ID); len(got) != 3 {
		t.Errorf("the resumed chat is bound to %v", got)
	}
	// A chat in a member's own worktree is that task's alone, without folders.
	info, err = chats.Start("claude", made[1].Worktree)
	if err != nil {
		t.Fatal(err)
	}
	if got := j.claude.foldersOf(info.ID); got != nil {
		t.Errorf("a member's chat got folders %v", got)
	}
	if got := j.app.tasks.Tasks(info.ID); !slices.Equal(got, []string{made[1].ID}) {
		t.Errorf("the member's chat is bound to %v", got)
	}
	// Restricted Mode for one worktree: the group's chat does not open.
	if err := j.app.untrustTask(made[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := chats.Resume("claude", made[0].Worktree, "again", ""); !errors.Is(err, errJointUntrusted) {
		t.Errorf("resume with an untrusted worktree = %v", err)
	}
	if err := j.app.setTrust(made[2].Worktree, true); err != nil {
		t.Fatal(err)
	}
	// Another engine in the lead's worktree is refused, not started alone.
	j.app.agentMgr.Register("codex", newChatDriver())
	if _, err := chats.Start("codex", made[0].Worktree); !errors.Is(err, errJointEngine) {
		t.Errorf("codex in the lead's worktree = %v", err)
	}
	// A worktree gone: the chat does not open without it.
	if err := os.RemoveAll(made[1].Worktree); err != nil {
		t.Fatal(err)
	}
	if _, err := chats.Start("claude", made[0].Worktree); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("with a worktree gone = %v", err)
	}
}

func TestJointRemoval(t *testing.T) {
	j := newJointFixture(t, 3, 0)
	made := j.createJoint("Joint")
	chat := j.claude.chats()[made[0].Worktree][0]

	// Removing a member while the shared chat runs: the UI is told, and the
	// chat stops once, for all.
	info, err := j.api.RemoveCheck(made[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if info.JointTasks != 2 || !info.StopsJointChat {
		t.Errorf("RemoveCheck = %+v", info)
	}
	if _, err := j.api.Remove(made[1].ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	j.claude.mu.Lock()
	stopped := slices.Clone(j.claude.stopped)
	j.claude.mu.Unlock()
	if got := stopped; !slices.Equal(got, []string{chat}) {
		t.Errorf("stopped = %v", got)
	}
	if got := j.jointStates(made...); got != "idle gone idle" {
		t.Errorf("states = %s", got)
	}
	if len(j.app.tasks.Sessions(made[0].ID)) != 0 || len(j.app.tasks.Sessions(made[2].ID)) != 0 {
		t.Error("the others still have the stopped chat")
	}
	if _, ok := j.app.orch.Get(chat); ok {
		t.Error("the stopped chat is still registered")
	}
	if !isDir(made[0].Worktree) || !isDir(made[2].Worktree) {
		t.Error("the others' worktrees went")
	}
	if g := j.app.taskStore().Joint(made[0].Joint); len(g) != 2 || g[0].ID != made[0].ID || !g[0].Lead {
		t.Errorf("group after removing a member = %+v", g)
	}
	// Without a chat, removing is nothing more than for a task on its own.
	if info, _ := j.api.RemoveCheck(made[0].ID); info.JointTasks != 1 || info.StopsJointChat {
		t.Errorf("RemoveCheck without a chat = %+v", info)
	}

	// Removing the lead: the next task leads (here the last one: a group of
	// one is no group).
	if _, err := j.api.Remove(made[0].ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	_, last, _ := j.app.taskStore().Find(made[2].ID)
	if last.Joint != "" || last.Lead {
		t.Errorf("the last task = %+v", last)
	}
	if info, _ := j.api.RemoveCheck(made[2].ID); info.JointTasks != 0 {
		t.Errorf("RemoveCheck of the last = %+v", info)
	}
}

func TestJointRemovingTheLeadOfThree(t *testing.T) {
	j := newJointFixture(t, 4, 0)
	made := j.createJoint("Joint")
	if _, err := j.api.Remove(made[0].ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	g := j.app.taskStore().Joint(made[0].Joint)
	if len(g) != 3 || !g[0].Lead || g[1].Lead || g[2].Lead {
		t.Fatalf("group = %+v", g)
	}
	// The new lead's worktree opens the group's chat, with the others' folders.
	info, err := (&AgentChatAPI{app: j.app}).Start("claude", g[0].Worktree)
	if err != nil {
		t.Fatal(err)
	}
	if got := j.claude.foldersOf(info.ID); !slices.Equal(got, worktrees(g[1:])) {
		t.Errorf("folders = %v", got)
	}
}

// A defined agent is judged by the engine it resolves to.
func TestJointDefinedAgents(t *testing.T) {
	j := newJointFixture(t, 2, 0)
	sh, _ := exec.LookPath("sh")
	j.app.settings.Agents["codex"] = settings.AgentSettings{Path: sh}
	personal := agentdefs.PersonalDir(j.app.configDir)
	writeT(t, filepath.Join(personal, "reviewer.md"), "---\nengine: claude\n---\nReview.")
	writeT(t, filepath.Join(personal, "backend.md"), "---\nengine: codex\n---\nBuild.")
	if _, err := j.app.createJointTasks(context.Background(), j.requests("x"), "backend", false); !errors.Is(err, errJointEngine) ||
		!strings.Contains(err.Error(), "backend runs on codex") {
		t.Errorf("backend (codex) = %v", err)
	}
	made, err := j.app.createJointTasks(context.Background(), j.requests("x"), "reviewer", true)
	if err != nil {
		t.Fatalf("reviewer (claude) = %v", err)
	}
	// The caller rolls the held group back: nothing is left.
	j.app.discardJointTasks(context.Background(), made)
	if len(j.app.taskStore()) != 0 || j.app.activeTasks() != 0 {
		t.Errorf("after discarding: %v, active %d", j.app.taskStore(), j.app.activeTasks())
	}
	for _, task := range made {
		if isDir(task.Worktree) {
			t.Errorf("worktree %s kept", task.Worktree)
		}
	}
}

func TestJointEngine(t *testing.T) {
	for _, c := range []struct{ agent, engine, want string }{
		{"claude", "claude", ""},
		{"reviewer", "claude", ""}, // a defined agent on Claude Code
		{"reviewer", "codex", "reviewer runs on codex, which cannot work in several folders at once: delegate per repository instead"},
		{"codex", "codex", "codex runs on codex, which cannot"},
		{"my-acp", "my-acp", "my-acp cannot work in several folders at once"},
	} {
		err := jointEngine(c.agent, c.engine)
		if (c.want == "") != (err == nil) || err != nil && (!strings.Contains(err.Error(), c.want) || !errors.Is(err, errJointEngine)) {
			t.Errorf("%s on %s: %v", c.agent, c.engine, err)
		}
	}
}

func TestJointPrompt(t *testing.T) {
	members := []agenttask.Task{
		{Prompt: "Do it", Worktree: "/wt/api", Checkout: "/src/api", Branch: "agent/do-it", Base: "main"},
		{Prompt: "Do it", Worktree: "/wt/web", Checkout: "/src/web", Branch: "agent/do-it", Base: "dev"},
		{Prompt: "Only the docs\nplease", Worktree: "/wt/docs", Checkout: "/src/docs", Branch: "agent/docs", Base: "main"},
	}
	got := jointPrompt(members)
	for _, want := range []string{
		"Do it\n\nThis task spans 3 repositories",
		"- /wt/api (your working directory): repository api, branch agent/do-it (from main)\n",
		"- /wt/web: repository web, branch agent/do-it (from dev)\n",
		"- /wt/docs: repository docs, branch agent/docs (from main)\n  What to do there:\n  Only the docs\n  please\n",
		"consistent",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt lacks %q:\n%s", want, got)
		}
	}
}
