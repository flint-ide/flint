package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"flint/internal/agenttask"
	"flint/internal/feature"
	"flint/internal/settings"
)

// addRepoT makes a second repository next to the fixture's, with a remote,
// attaches it to the workspace and trusts it.
func (f *taskFixture) addRepoT(name, remote string) string {
	f.t.Helper()
	dir := filepath.Join(filepath.Dir(f.repo), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	gitT(f.t, dir, "init", "-q", "-b", "main")
	gitT(f.t, dir, "config", "user.name", "T")
	gitT(f.t, dir, "config", "user.email", "t@example.com")
	gitT(f.t, dir, "config", "commit.gpgsign", "false")
	commitT(f.t, dir, "b.txt", "one\n", "initial")
	gitT(f.t, dir, "remote", "add", "origin", remote)
	if err := f.app.addRoot(dir, nil); err != nil {
		f.t.Fatal(err)
	}
	if err := f.app.setTrust(cleanFolder(dir), true); err != nil {
		f.t.Fatal(err)
	}
	return dir
}

// probeFeature counts the TasksChanged calls a feature gets.
func probeFeature(a *App) *atomic.Int32 {
	n := new(atomic.Int32)
	a.features = feature.New([]feature.Module{{ID: "probe", New: func(feature.Host) feature.Instance {
		return feature.Instance{TasksChanged: func() { n.Add(1) }}
	}}}, featureHost{a})
	return n
}

func TestFeatureHostCreatesAgentTasks(t *testing.T) {
	f := queueFixture(t, 0)
	web := f.addRepoT("web", "git@github.com:acme/web.git")
	calls := probeFeature(f.app)
	var mu sync.Mutex
	var opened []ChatOpened
	f.app.emitHook = func(name string, data ...any) {
		if name == eventChatOpened {
			mu.Lock()
			opened = append(opened, data[0].(ChatOpened))
			mu.Unlock()
		}
	}
	h := featureHost{f.app}
	ctx := context.Background()
	made, err := h.CreateAgentTasks(ctx, []feature.NewAgentTask{
		{Dir: f.repo, Description: "API part\n\nDo the API.", Agent: "fake"},
		{Dir: web, Description: "Web part", Agent: "fake"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 2 || made[0].Title != "API part" || made[1].Title != "Web part" ||
		!strings.HasPrefix(made[0].Worktree, f.repo) || !strings.HasPrefix(made[1].Worktree, web) || made[0].Agent != "fake" {
		t.Fatalf("made %+v", made)
	}
	if calls.Load() == 0 {
		t.Error("features were not told about the new tasks")
	}
	// Go starts both chats, with the descriptions, and lets go of the slots.
	f.waitStarts()
	mu.Lock()
	if len(opened) != 2 {
		t.Fatalf("chat:opened %+v", opened)
	}
	var taskIDs []string
	for _, o := range opened {
		taskIDs = append(taskIDs, o.TaskID)
	}
	mu.Unlock()
	if !slices.Contains(taskIDs, made[0].ID) || !slices.Contains(taskIDs, made[1].ID) {
		t.Errorf("chats for %v, want %s and %s", taskIDs, made[0].ID, made[1].ID)
	}
	f.app.queue.startMu.Lock()
	starting := len(f.app.queue.starting)
	f.app.queue.startMu.Unlock()
	if starting != 0 {
		t.Errorf("%d start slots still held", starting)
	}
	if !f.app.isTrusted(made[1].Worktree) {
		t.Error("the worktree of a trusted repository is not trusted")
	}

	// States, as the Agent Tasks panel shows them.
	got := h.AgentTasks(ctx, []string{made[0].ID, made[1].ID, "not-an-id", agenttask.NewID()})
	if len(got) != 2 || got[made[1].ID].Worktree != made[1].Worktree || got[made[0].ID].State != agenttask.Working {
		t.Errorf("AgentTasks = %+v", got)
	}
	// Merged comes from git; the pull request from the store.
	commitT(t, made[1].Worktree, "c.txt", "web\n", "web work")
	if _, err := (&TaskAPI{app: f.app, git: &GitAPI{app: f.app}}).Merge(made[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := f.app.saveTasks(func(s agenttask.Store) (agenttask.Store, error) {
		return s.Update(made[0].ID, func(x agenttask.Task) agenttask.Task { x.PullRequest = "https://example.com/pr/1"; return x }), nil
	}); err != nil {
		t.Fatal(err)
	}
	got = h.AgentTasks(ctx, []string{made[0].ID, made[1].ID})
	if !got[made[1].ID].Merged || got[made[0].ID].Merged || got[made[0].ID].PullRequest != "https://example.com/pr/1" {
		t.Errorf("after merge: %+v", got)
	}

	// Repositories and their remotes; a task's worktree says so.
	repo, err := h.Repository(ctx, web)
	if err != nil || cleanFolder(repo.Root) != cleanFolder(web) || !slices.Equal(repo.Remotes, []string{"git@github.com:acme/web.git"}) || repo.TaskWorktree {
		t.Errorf("Repository(web) = %+v, %v", repo, err)
	}
	if repo, err := h.Repository(ctx, made[0].Worktree); err != nil || !repo.TaskWorktree {
		t.Errorf("Repository(worktree) = %+v, %v", repo, err)
	}
	if _, err := h.Repository(ctx, t.TempDir()); err == nil {
		t.Error("a plain folder is a repository")
	}
	if agents := h.Agents(); !slices.Contains(agents.Available, "fake") || agents.Default == "" {
		t.Errorf("Agents = %+v", agents)
	}
}

func TestFeatureHostCreateAgentTasksIsAllOrNothing(t *testing.T) {
	f := queueFixture(t, 0)
	h := featureHost{f.app}
	ctx := context.Background()
	outside := t.TempDir()
	for _, reqs := range [][]feature.NewAgentTask{
		{{Dir: f.repo, Description: "first", Agent: "fake"}, {Dir: outside, Description: "second", Agent: "fake"}},
		{{Dir: f.repo, Description: "first", Agent: "fake"}, {Dir: f.repo, Description: "second", Agent: "missing"}},
		{{Dir: f.repo, Description: "", Agent: "fake"}},
		nil,
	} {
		if _, err := h.CreateAgentTasks(ctx, reqs); err == nil {
			t.Fatalf("CreateAgentTasks(%+v) succeeded", reqs)
		}
	}
	f.waitStarts()
	if n := len(f.app.taskStore().List(cleanFolder(filepath.Join(f.repo, ".git")))); n != 0 {
		t.Errorf("%d tasks kept", n)
	}
	if out := gitT(t, f.repo, "branch", "--list"); strings.TrimSpace(out) != "* main" {
		t.Errorf("branches kept: %q", out)
	}
	if out := gitT(t, f.repo, "worktree", "list"); strings.Count(out, "\n") != 0 {
		t.Errorf("worktrees kept: %q", out)
	}
	if n := len(f.app.orch.All()); n != 0 {
		t.Errorf("%d chats started", n)
	}
	f.app.queue.startMu.Lock()
	defer f.app.queue.startMu.Unlock()
	if len(f.app.queue.starting) != 0 {
		t.Errorf("start slots held: %v", f.app.queue.starting)
	}
}

// A queued task of a CreateAgentTasks call is not started while the call is
// still creating the next request, so a rollback never removes a task whose
// chat runs: the first request's task waits in the queue, its slot comes free
// meanwhile, and the second request fails (deterministic: afterRequest runs
// between the two requests).
func TestFeatureHostCreateHoldsTasksFromTheQueue(t *testing.T) {
	f := queueFixture(t, 1)
	first := f.create("first") // holds the only slot (the UI starts its chat)
	outside := t.TempDir()
	var held []string
	f.app.queue.afterRequest = func(done int) {
		for _, q := range f.app.taskStore().QueuedTasks() {
			held = append(held, q.Task.ID)
		}
		// The slot comes free: the queue looks, and must leave the task alone.
		if err := f.api.StartDone(first.ID); err != nil {
			t.Error(err)
		}
		f.waitStarts()
	}
	_, err := featureHost{f.app}.CreateAgentTasks(context.Background(), []feature.NewAgentTask{
		{Dir: f.repo, Description: "second", Agent: "fake"},
		{Dir: outside, Description: "third", Agent: "fake"},
	})
	if err == nil {
		t.Fatal("CreateAgentTasks succeeded with a request outside the workspace")
	}
	f.waitStarts()
	if len(held) != 1 {
		t.Fatalf("queued during the call: %v", held)
	}
	if n := len(f.app.orch.All()); n != 0 {
		t.Errorf("%d chats started for tasks that were rolled back", n)
	}
	tasks := f.app.taskStore().List(cleanFolder(filepath.Join(f.repo, ".git")))
	if len(tasks) != 1 || tasks[0].ID != first.ID {
		t.Errorf("tasks kept: %+v", tasks)
	}
	if f.app.isHeld(held[0]) {
		t.Error("the rolled-back task is still held")
	}
}

// When the call succeeds, its queued tasks are released to the queue at once:
// the slot that came free meanwhile starts one.
func TestFeatureHostCreateReleasesHeldTasks(t *testing.T) {
	f := queueFixture(t, 1)
	first := f.create("first")
	f.app.queue.afterRequest = func(int) {
		if err := f.api.StartDone(first.ID); err != nil {
			t.Error(err)
		}
		f.waitStarts()
		if n := len(f.app.orch.All()); n != 0 {
			t.Errorf("%d chats started while the call runs", n)
		}
	}
	made, err := featureHost{f.app}.CreateAgentTasks(context.Background(), []feature.NewAgentTask{{Dir: f.repo, Description: "second", Agent: "fake"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 1 || made[0].State != agenttask.Queued {
		t.Fatalf("made %+v", made)
	}
	f.waitStarts()
	if all := f.app.orch.All(); len(all) != 1 || cleanFolder(all[0].Cwd) != made[0].Worktree {
		t.Errorf("chats %+v, want one in %s", all, made[0].Worktree)
	}
}

// The slots the tasks of one call hold are kept while its other requests are
// created, however long that takes (startGrace does not free them).
func TestFeatureHostCreateKeepsSlotsPastTheGrace(t *testing.T) {
	f := queueFixture(t, 2)
	web := f.addRepoT("web", "git@github.com:acme/web.git")
	f.app.queue.afterRequest = func(done int) {
		if done != 1 {
			return
		}
		f.app.queue.startMu.Lock()
		for id := range f.app.queue.starting {
			f.app.queue.starting[id] = time.Now().Add(-2 * startGrace)
		}
		f.app.queue.startMu.Unlock()
		if n := f.app.activeTasks(); n != 1 {
			t.Errorf("active tasks after the grace = %d, want 1", n)
		}
	}
	made, err := featureHost{f.app}.CreateAgentTasks(context.Background(), []feature.NewAgentTask{
		{Dir: f.repo, Description: "api", Agent: "fake"},
		{Dir: web, Description: "web", Agent: "fake"},
	})
	if err != nil || len(made) != 2 || made[0].State == agenttask.Queued || made[1].State == agenttask.Queued {
		t.Fatalf("made %+v, %v", made, err)
	}
	f.waitStarts()
}

// A checkout on a detached HEAD fails with feature.DetachedHeadError naming
// it, and nothing is kept.
func TestFeatureHostCreateDetachedHead(t *testing.T) {
	f := queueFixture(t, 0)
	gitT(t, f.repo, "checkout", "-q", "--detach")
	_, err := featureHost{f.app}.CreateAgentTasks(context.Background(), []feature.NewAgentTask{{Dir: f.repo, Description: "x", Agent: "fake"}})
	var dh *feature.DetachedHeadError
	if !errors.As(err, &dh) || dh.Dir != f.repo {
		t.Fatalf("err = %v", err)
	}
	if n := len(f.app.taskStore().List(cleanFolder(filepath.Join(f.repo, ".git")))); n != 0 {
		t.Errorf("%d tasks kept", n)
	}
}

// CreateJointAgentTasks makes one joint group in two real repositories: one
// task per repository, linked, lead first, and one chat in the lead's
// worktree with the other worktree as an additional folder.
func TestFeatureHostCreatesJointAgentTasks(t *testing.T) {
	j := newJointFixture(t, 2, 0)
	h := featureHost{j.app}
	ctx := context.Background()
	made, err := h.CreateJointAgentTasks(ctx, []feature.NewAgentTask{
		{Dir: j.repos[0], Description: "Rename the field\n\nIn both.", Agent: "claude"},
		{Dir: j.repos[1], Description: "Rename the field\n\nIn both."},
	}, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(made) != 2 || made[0].Joint == "" || made[0].Joint != made[1].Joint || !made[0].Lead || made[1].Lead ||
		made[0].Agent != "claude" || made[0].Title != "Rename the field" ||
		!within(made[0].Worktree, filepath.Dir(j.repos[0])) || made[0].Worktree == made[1].Worktree {
		t.Fatalf("made %+v", made)
	}
	store := j.app.taskStore()
	if g := store.Joint(made[0].Joint); len(g) != 2 || g[0].ID != made[0].ID || g[1].ID != made[1].ID {
		t.Fatalf("group %+v", g)
	}
	j.waitStarts()
	chats := j.claude.chats()
	if len(chats) != 1 || len(chats[made[0].Worktree]) != 1 {
		t.Fatalf("chats = %v", chats)
	}
	if got := j.claude.foldersOf(chats[made[0].Worktree][0]); !slices.Equal(got, []string{made[1].Worktree}) {
		t.Errorf("folders = %v", got)
	}
	// The description is said once, then which folder is which.
	prompt := j.claude.prompt(t)
	if strings.Count(prompt, "In both.") != 1 || !strings.Contains(prompt, made[1].Worktree) {
		t.Errorf("prompt:\n%s", prompt)
	}
	got := h.AgentTasks(ctx, []string{made[0].ID, made[1].ID})
	if got[made[0].ID].Joint != made[0].Joint || !got[made[0].ID].Lead || got[made[1].ID].Joint != made[0].Joint || got[made[1].ID].Lead {
		t.Errorf("AgentTasks = %+v", got)
	}
}

// Its refusals are the feature errors, keep the app's text and leave nothing.
func TestFeatureHostCreateJointRefusals(t *testing.T) {
	j := newJointFixture(t, 3, 0)
	sh, _ := exec.LookPath("sh")
	j.app.settings.Agents["codex"] = settings.AgentSettings{Path: sh}
	h := featureHost{j.app}
	ctx := context.Background()
	reqs := func(dirs ...string) []feature.NewAgentTask {
		var out []feature.NewAgentTask
		for _, d := range dirs {
			out = append(out, feature.NewAgentTask{Dir: d, Description: "x"})
		}
		return out
	}
	_, err := h.CreateJointAgentTasks(ctx, reqs(j.repos[0], j.repos[1]), "codex")
	if !errors.Is(err, feature.ErrJointEngine) || errors.Is(err, feature.ErrJointUntrusted) ||
		err.Error() != "codex runs on codex, which cannot work in several folders at once: delegate per repository instead" {
		t.Errorf("codex: %v", err)
	}
	if _, err := h.CreateJointAgentTasks(ctx, reqs(j.repos[0], j.repos[1]), "fake"); !errors.Is(err, feature.ErrJointEngine) {
		t.Errorf("a custom ACP agent: %v", err)
	}
	if err := j.app.setTrust(cleanFolder(j.repos[1]), false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.CreateJointAgentTasks(ctx, reqs(j.repos[0], j.repos[1]), "claude"); !errors.Is(err, feature.ErrJointUntrusted) ||
		!strings.Contains(err.Error(), "is not trusted") {
		t.Errorf("untrusted: %v", err)
	}
	gitT(t, j.repos[2], "checkout", "-q", "--detach")
	_, err = h.CreateJointAgentTasks(ctx, reqs(j.repos[0], j.repos[2]), "claude")
	var dh *feature.DetachedHeadError
	if !errors.As(err, &dh) || dh.Dir != j.repos[2] {
		t.Errorf("detached HEAD: %v", err)
	}
	mixed := reqs(j.repos[0], j.repos[2])
	mixed[1].Agent = "fake"
	if _, err := h.CreateJointAgentTasks(ctx, mixed, "claude"); err == nil || !strings.Contains(err.Error(), "one agent") {
		t.Errorf("two agents: %v", err)
	}
	if n := len(j.app.taskStore()); n != 0 {
		t.Errorf("refusals left %d tasks", n)
	}
	if out := gitT(t, j.repos[0], "worktree", "list"); strings.Count(out, "\n") != 0 {
		t.Errorf("worktrees kept:\n%s", out)
	}
	if len(j.claude.chats()) != 0 {
		t.Error("a chat started")
	}
}
