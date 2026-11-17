package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"flint/internal/agenttask"
	"flint/internal/feature"
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
