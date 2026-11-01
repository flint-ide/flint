package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/config"
	"flint/internal/github"
	"flint/internal/settings"
)

// taskFixture is an App with one open repository (main checkout on main, one
// commit) and a custom agent that resolves, so tasks can be created.
type taskFixture struct {
	t    *testing.T
	app  *App
	api  *TaskAPI
	repo string
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitT(t *testing.T, dir, file, content, msg string) {
	t.Helper()
	writeT(t, filepath.Join(dir, file), content)
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", msg)
}

func newTaskFixture(t *testing.T) *taskFixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "proj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "init", "-q", "-b", "main")
	gitT(t, repo, "config", "user.name", "T")
	gitT(t, repo, "config", "user.email", "t@example.com")
	gitT(t, repo, "config", "commit.gpgsign", "false")
	commitT(t, repo, "a.txt", "one\n", "initial")

	a := &App{configDir: t.TempDir(), settings: settings.Default(), ctx: context.Background()}
	a.settings.CustomAgents = []settings.CustomAgent{{ID: "fake", Command: "sh"}}
	a.tasks = agenttask.NewTracker(a.taskByFolder, a.taskChanged)
	a.openProject(repo, nil)
	api := &TaskAPI{app: a, git: &GitAPI{app: a}}
	return &taskFixture{t: t, app: a, api: api, repo: repo}
}

func (f *taskFixture) create(desc string) TaskView {
	f.t.Helper()
	v, err := f.api.Create(NewTask{Dir: f.repo, Description: desc, Agent: "fake"})
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

func TestCreateTask(t *testing.T) {
	f := newTaskFixture(t)
	v := f.create("Add a Retry button\n\nIt should retry the last request.")
	if v.Branch != "agent/add-a-retry-button" || v.Base != "main" || v.Title != "Add a Retry button" || v.Agent != "fake" {
		t.Fatalf("task = %+v", v)
	}
	want := filepath.Join(filepath.Dir(f.repo), "proj.worktrees", "agent-add-a-retry-button")
	if v.Worktree != want {
		t.Errorf("worktree = %q, want %q", v.Worktree, want)
	}
	if gitT(t, v.Worktree, "branch", "--show-current") != v.Branch {
		t.Error("worktree not on the task branch")
	}
	if v.State != agenttask.Idle {
		t.Errorf("state = %s", v.State)
	}

	// The same description again gets its own branch and folder.
	v2 := f.create("Add a Retry button")
	if v2.Branch != "agent/add-a-retry-button-2" || v2.Worktree == v.Worktree {
		t.Errorf("second task = %+v", v2)
	}

	// Listed from any worktree of the repository, and remembered in state.json.
	for _, dir := range []string{f.repo, v.Worktree} {
		list, err := f.api.List(dir)
		if err != nil || len(list) != 2 {
			t.Fatalf("List(%s) = %d tasks, %v", dir, len(list), err)
		}
	}
	var saved appState
	if err := config.LoadJSON(f.app.statePath(), &saved); err != nil {
		t.Fatal(err)
	}
	if n := len(saved.AgentTasks.List(repoKeyOf(t, f.repo))); n != 2 {
		t.Errorf("state.json has %d tasks", n)
	}
}

func repoKeyOf(t *testing.T, dir string) string {
	t.Helper()
	return cleanFolder(filepath.Join(dir, ".git"))
}

func TestCreateTaskFollowsTrust(t *testing.T) {
	f := newTaskFixture(t)
	untrusted := f.create("first")
	if f.app.isTrusted(untrusted.Worktree) {
		t.Error("task of an untrusted repository is trusted")
	}
	if err := f.app.setTrust(f.repo, true); err != nil {
		t.Fatal(err)
	}
	trusted := f.create("second")
	if !f.app.isTrusted(trusted.Worktree) {
		t.Error("task of a trusted repository is not trusted")
	}
	if _, err := f.api.Remove(trusted.ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if f.app.isTrusted(trusted.Worktree) || len(f.app.trustedFolders()) != 1 {
		t.Errorf("removed task still trusted: %v", f.app.trustedFolders())
	}
}

func TestCreateTaskValidates(t *testing.T) {
	f := newTaskFixture(t)
	gitT(t, f.repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	outside := t.TempDir()
	for name, req := range map[string]NewTask{
		"empty description": {Dir: f.repo, Description: "  \n ", Agent: "fake"},
		"long description":  {Dir: f.repo, Description: strings.Repeat("x", agenttask.MaxPrompt+1), Agent: "fake"},
		"outside workspace": {Dir: outside, Description: "x", Agent: "fake"},
		"unknown agent":     {Dir: f.repo, Description: "x", Agent: "nope"},
		"missing base":      {Dir: f.repo, Description: "x", Agent: "fake", Base: "nope"},
		"remote base":       {Dir: f.repo, Description: "x", Agent: "fake", Base: "origin/main"},
		"option as base":    {Dir: f.repo, Description: "x", Agent: "fake", Base: "--all"},
	} {
		if _, err := f.api.Create(req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if list, _ := f.api.List(f.repo); len(list) != 0 {
		t.Errorf("rejected tasks were kept: %+v", list)
	}
}

func TestTaskChanges(t *testing.T) {
	f := newTaskFixture(t)
	v := f.create("change things")
	commitT(t, v.Worktree, "b.txt", "1\n2\n", "add b")
	writeT(t, filepath.Join(v.Worktree, "c.txt"), "new\n")
	st, err := f.api.Changes(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Count != 2 || st.Added != 3 || st.Deleted != 0 {
		t.Errorf("changes = %+v", st)
	}
	if _, err := f.api.Changes("0123456789abcdef"); err == nil {
		t.Error("unknown task accepted")
	}
	if _, err := f.api.Changes("../../etc"); err == nil {
		t.Error("invalid id accepted")
	}
}

func TestMergeTaskSafety(t *testing.T) {
	f := newTaskFixture(t)
	v := f.create("merge me")

	if _, err := f.api.Merge(v.ID); err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Errorf("merge of an empty task = %v", err)
	}
	commitT(t, v.Worktree, "b.txt", "b\n", "add b")

	writeT(t, filepath.Join(v.Worktree, "b.txt"), "uncommitted\n")
	if _, err := f.api.Merge(v.ID); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("merge with a dirty task = %v", err)
	}
	gitT(t, v.Worktree, "checkout", "--", "b.txt")

	writeT(t, filepath.Join(f.repo, "a.txt"), "dirty base\n")
	if _, err := f.api.Merge(v.ID); err == nil || !strings.Contains(err.Error(), f.repo) {
		t.Errorf("merge into a dirty base = %v", err)
	}
	gitT(t, f.repo, "checkout", "--", "a.txt")

	res, err := f.api.Merge(v.ID)
	if err != nil || !res.Merged || len(res.Conflicts) != 0 {
		t.Fatalf("merge = %+v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(f.repo, "b.txt")); err != nil {
		t.Error("merge did not reach the base checkout")
	}
	list, _ := f.api.List(f.repo)
	if !list[0].Merged {
		t.Errorf("merged task not shown as merged: %+v", list[0])
	}
}

func TestMergeTaskConflict(t *testing.T) {
	f := newTaskFixture(t)
	v := f.create("conflict")
	commitT(t, v.Worktree, "a.txt", "task\n", "task edit")
	commitT(t, f.repo, "a.txt", "main\n", "main edit")

	res, err := f.api.Merge(v.ID)
	if err != nil || res.Merged || strings.Join(res.Conflicts, ",") != "a.txt" {
		t.Fatalf("merge = %+v, %v", res, err)
	}
	if got := gitT(t, f.repo, "status", "--porcelain"); got != "" {
		t.Errorf("base checkout touched: %q", got)
	}
	// The usual way out: bring base into the task and let an agent resolve it there.
	files, err := f.api.UpdateFromBase(v.ID)
	if err != nil || strings.Join(files, ",") != "a.txt" {
		t.Fatalf("UpdateFromBase = %v, %v", files, err)
	}
}

func TestRemoveTask(t *testing.T) {
	f := newTaskFixture(t)
	v := f.create("remove me")
	commitT(t, v.Worktree, "b.txt", "b\n", "unmerged work")
	writeT(t, filepath.Join(v.Worktree, "b.txt"), "dirty\n")

	if _, err := f.api.Remove(v.ID, RemoveOptions{}); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("remove of a dirty task = %v", err)
	}
	res, err := f.api.Remove(v.ID, RemoveOptions{DiscardChanges: true})
	if err != nil || !res.BranchKept {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	if _, err := os.Stat(v.Worktree); !os.IsNotExist(err) {
		t.Error("worktree folder left behind")
	}
	if gitT(t, f.repo, "branch", "--list", v.Branch) == "" {
		t.Error("unmerged branch deleted without being asked")
	}
	if list, _ := f.api.List(f.repo); len(list) != 0 {
		t.Errorf("task still listed: %+v", list)
	}

	// A task whose branch has nothing of its own loses the branch too.
	empty := f.create("nothing done")
	if res, err := f.api.Remove(empty.ID, RemoveOptions{}); err != nil || res.BranchKept {
		t.Fatalf("remove empty task = %+v, %v", res, err)
	}
	if gitT(t, f.repo, "branch", "--list", empty.Branch) != "" {
		t.Error("empty branch kept")
	}
}

func TestStaleTask(t *testing.T) {
	f := newTaskFixture(t)
	v := f.create("vanishing")
	if err := os.RemoveAll(v.Worktree); err != nil {
		t.Fatal(err)
	}
	list, err := f.api.List(f.repo)
	if err != nil || len(list) != 1 || !list[0].Stale {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if _, err := f.api.Merge(v.ID); err == nil {
		t.Error("merge of a stale task accepted")
	}
	if _, err := f.api.Remove(v.ID, RemoveOptions{DeleteBranch: true}); err != nil {
		t.Fatal(err)
	}
	if gitT(t, f.repo, "worktree", "list", "--porcelain") == "" || strings.Contains(gitT(t, f.repo, "worktree", "list"), "vanishing") {
		t.Error("stale worktree not pruned")
	}
	if gitT(t, f.repo, "branch", "--list", v.Branch) != "" {
		t.Error("branch kept although its deletion was confirmed")
	}
}

func TestTaskStateFollowsSessions(t *testing.T) {
	f := newTaskFixture(t)
	v := f.create("state")
	f.app.tasks.Bind("s1", v.Worktree+"/") // the agent may report the folder unclean
	f.app.tasks.Prompted("s1")
	if list, _ := f.api.List(f.repo); list[0].State != agenttask.Working {
		t.Fatalf("state = %s", list[0].State)
	}
	f.app.tasks.Observe(as.Event{SessionID: "s1", Kind: as.KindTurnEnd, StopReason: as.StopError})
	list, _ := f.api.List(f.repo)
	if list[0].State != agenttask.Failed {
		t.Fatalf("state = %s", list[0].State)
	}
	// The settled state survives a restart (the chat is gone then).
	var saved appState
	if err := config.LoadJSON(f.app.statePath(), &saved); err != nil {
		t.Fatal(err)
	}
	if _, task, _ := saved.AgentTasks.Find(v.ID); task.State != agenttask.Failed {
		t.Errorf("saved state = %q", task.State)
	}
	f.app.tasks.Unbind("s1")
	if list, _ := f.api.List(f.repo); list[0].State != agenttask.Failed {
		t.Errorf("after the chat closed: %s", list[0].State)
	}
}

type fakePR struct {
	calls []github.NewPullRequest
}

func (p *fakePR) CreatePullRequest(_ context.Context, _ string, pr github.NewPullRequest) (string, error) {
	p.calls = append(p.calls, pr)
	return "https://github.com/acme/proj/pull/1", nil
}

func TestCreateTaskPullRequest(t *testing.T) {
	f := newTaskFixture(t)
	bare := filepath.Join(t.TempDir(), "origin.git")
	gitT(t, f.repo, "init", "-q", "--bare", "-b", "main", bare)
	gitT(t, f.repo, "remote", "add", "origin", bare)
	pr := &fakePR{}
	f.api.pr = pr
	v := f.create("Publish me\n\nDetails here.")

	if _, err := f.api.CreatePullRequest(v.ID); err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Errorf("PR without commits = %v", err)
	}
	commitT(t, v.Worktree, "b.txt", "b\n", "work")

	asked := 0
	f.app.ask = func(_, msg, _ string) bool {
		asked++
		return false
	}
	if _, err := f.api.CreatePullRequest(v.ID); !errors.Is(err, errPRDeclined) || len(pr.calls) != 0 {
		t.Fatalf("declined PR = %v, %d calls", err, len(pr.calls))
	}
	if out := gitT(t, f.repo, "ls-remote", bare); strings.Contains(out, v.Branch) {
		t.Error("branch pushed although the user declined")
	}

	f.app.ask = func(_, msg, _ string) bool {
		asked++
		return strings.Contains(msg, v.Branch) && strings.Contains(msg, "Publish me")
	}
	url, err := f.api.CreatePullRequest(v.ID)
	if err != nil || url != "https://github.com/acme/proj/pull/1" {
		t.Fatalf("PR = %q, %v", url, err)
	}
	if asked != 2 || len(pr.calls) != 1 || pr.calls[0].Head != v.Branch || pr.calls[0].Base != "main" || pr.calls[0].Title != "Publish me" {
		t.Errorf("asked %d times, calls %+v", asked, pr.calls)
	}
	if !strings.Contains(gitT(t, f.repo, "ls-remote", bare), v.Branch) {
		t.Error("branch not pushed")
	}
	if list, _ := f.api.List(f.repo); list[0].PullRequest != url {
		t.Errorf("pull request not remembered: %+v", list[0])
	}
}
