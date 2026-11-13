package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/agenttask"
	"flint/internal/config"
	"flint/internal/settings"
)

// groupFixture has two agents that resolve and no limit of working tasks.
func groupFixture(t *testing.T) *taskFixture {
	t.Helper()
	f := newTaskFixture(t)
	f.app.settings.CustomAgents = append(f.app.settings.CustomAgents, settings.CustomAgent{ID: "other", Command: "sh"})
	f.app.settings.AgentTasks.MaxParallel = 0
	return f
}

func (f *taskFixture) group(desc string, agents ...string) []TaskView {
	f.t.Helper()
	views, err := f.api.CreateGroup(NewTaskGroup{Dir: f.repo, Description: desc, Agents: agents})
	if err != nil {
		f.t.Fatal(err)
	}
	return views
}

func TestCreateGroup(t *testing.T) {
	f := groupFixture(t)
	base := gitT(t, f.repo, "rev-parse", "HEAD")
	views := f.group("Add a Retry button\n\nDetails.", "fake", "other", "fake")
	if len(views) != 3 {
		t.Fatalf("%d tasks", len(views))
	}
	wantBranches := []string{"agent/add-a-retry-button-fake", "agent/add-a-retry-button-other", "agent/add-a-retry-button-fake-2"}
	wantAgents := []string{"fake", "other", "fake"}
	group := views[0].Group
	if !agenttask.ValidID(group) {
		t.Fatalf("group id = %q", group)
	}
	worktrees := map[string]bool{}
	for i, v := range views {
		if v.Branch != wantBranches[i] || v.Agent != wantAgents[i] || v.Group != group || v.Title != "Add a Retry button" {
			t.Errorf("task %d = %+v", i, v)
		}
		// Its own branch and worktree, from the same commit.
		if gitT(t, v.Worktree, "branch", "--show-current") != v.Branch {
			t.Errorf("%s is not on its branch", v.Worktree)
		}
		if got := gitT(t, v.Worktree, "rev-parse", "HEAD"); got != base {
			t.Errorf("%s starts at %s, want the base commit %s", v.Branch, got, base)
		}
		_, task, _ := f.app.taskStore().Find(v.ID)
		if task.BaseCommit != base || task.Group != group || task.Prompt != "Add a Retry button\n\nDetails." {
			t.Errorf("remembered task = %+v", task)
		}
		worktrees[v.Worktree] = true
	}
	if len(worktrees) != 3 {
		t.Errorf("worktrees are shared: %v", worktrees)
	}
	// The group is in state.json.
	var saved appState
	if err := config.LoadJSON(f.app.statePath(), &saved); err != nil {
		t.Fatal(err)
	}
	if n := len(saved.AgentTasks.Group(group)); n != 3 {
		t.Errorf("state.json has %d tasks of the group", n)
	}

	// One agent is a task on its own, named as before.
	single := f.group("Add a Retry button", "other")
	if len(single) != 1 || single[0].Group != "" || single[0].Branch != "agent/add-a-retry-button" {
		t.Errorf("single = %+v", single)
	}
	// The same description again: every branch is still unique.
	again := f.group("Add a Retry button", "fake", "other")
	if again[0].Branch != "agent/add-a-retry-button-fake-3" || again[1].Branch != "agent/add-a-retry-button-other-2" || again[0].Group == group {
		t.Errorf("second group = %+v", again)
	}
}

func TestCreateGroupValidates(t *testing.T) {
	f := groupFixture(t)
	for name, agents := range map[string][]string{
		"no agent":      {},
		"unknown agent": {"fake", "nope"},
		"too many":      {"fake", "fake", "fake", "fake", "other", "other", "other"},
	} {
		if _, err := f.api.CreateGroup(NewTaskGroup{Dir: f.repo, Description: "x", Agents: agents}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if list, _ := f.api.List(f.repo); len(list) != 0 {
		t.Errorf("rejected groups left tasks: %+v", list)
	}
	if out := gitT(t, f.repo, "branch", "--list", "agent/*"); out != "" {
		t.Errorf("rejected groups left branches: %s", out)
	}
}

// A group is all or nothing: when one task cannot be created, the ones
// made before it are taken back.
func TestCreateGroupUndoesOnFailure(t *testing.T) {
	f := groupFixture(t)
	if err := f.app.setTrust(f.repo, true); err != nil {
		t.Fatal(err)
	}
	key := repoKeyOf(t, f.repo)
	err := f.app.saveTasks(func(s agenttask.Store) (agenttask.Store, error) {
		for i := range agenttask.MaxPerRepo - 1 {
			var err error
			if s, err = s.Add(key, agenttask.Task{ID: fmt.Sprintf("%016x", i+1), Worktree: fmt.Sprintf("/nowhere/%d", i)}); err != nil {
				return nil, err
			}
		}
		return s, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	trusted := len(f.app.trustedFolders())
	_, err = f.api.CreateGroup(NewTaskGroup{Dir: f.repo, Description: "too much", Agents: []string{"fake", "other"}})
	if err == nil || !strings.Contains(err.Error(), "already has") {
		t.Fatalf("err = %v", err)
	}
	if n := len(f.app.taskStore().List(key)); n != agenttask.MaxPerRepo-1 {
		t.Errorf("%d tasks kept", n)
	}
	if out := gitT(t, f.repo, "branch", "--list", "agent/*"); out != "" {
		t.Errorf("branches kept: %s", out)
	}
	if out := gitT(t, f.repo, "worktree", "list", "--porcelain"); strings.Count(out, "worktree ") != 1 {
		t.Errorf("worktrees kept:\n%s", out)
	}
	if len(f.app.trustedFolders()) != trusted {
		t.Errorf("trust kept: %v", f.app.trustedFolders())
	}
	if f.app.activeTasks() != 0 {
		t.Error("a slot is still held for a task that was taken back")
	}
}

func TestCreateGroupQueuesBeyondTheLimit(t *testing.T) {
	f := groupFixture(t)
	f.app.settings.AgentTasks.MaxParallel = 2
	views := f.group("compare", "fake", "other", "fake")
	if got := views[0].State + " " + views[1].State + " " + views[2].State; got != "idle idle queued" {
		t.Errorf("states = %s", got)
	}
}

// compareFixture is a group of two tasks: the first committed its work, the
// second left it uncommitted.
func compareFixture(t *testing.T) (*taskFixture, TaskView, TaskView) {
	t.Helper()
	f := groupFixture(t)
	commitT(t, f.repo, "keep.txt", "same\n", "more base")
	views := f.group("compare", "fake", "other")
	a, b := views[0], views[1]
	commitT(t, a.Worktree, "a.txt", "one\ntwo\n", "a: extend a.txt")
	commitT(t, a.Worktree, "only-a.txt", "x\ny\nz\n", "a: add a file")
	writeT(t, filepath.Join(b.Worktree, "a.txt"), "ONE\n")
	writeT(t, filepath.Join(b.Worktree, "sub/only-b.txt"), "new\n")
	return f, a, b
}

func TestCompareGroup(t *testing.T) {
	f, a, b := compareFixture(t)
	cmp, err := f.api.Compare(a.Group)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.Tasks) != 2 || cmp.Tasks[0].Task.ID != a.ID || cmp.Tasks[1].Task.ID != b.ID || cmp.Truncated {
		t.Fatalf("tasks = %+v", cmp.Tasks)
	}
	// Per task: totals, commits ahead, the commit it left the base at.
	base := gitT(t, f.repo, "rev-parse", "HEAD")
	ta, tb := cmp.Tasks[0], cmp.Tasks[1]
	if ta.Count != 2 || ta.Added != 4 || ta.Deleted != 0 || ta.Task.Ahead != 2 || ta.MergeBase != base {
		t.Errorf("first task = %+v", ta)
	}
	if tb.Count != 2 || tb.Added != 2 || tb.Deleted != 1 || tb.Task.Ahead != 0 || tb.MergeBase != base || tb.Task.Agent != "other" {
		t.Errorf("second task = %+v", tb)
	}
	// The union of files, each with what every task did to it.
	var paths []string
	for _, row := range cmp.Files {
		paths = append(paths, row.Path)
		if len(row.Changes) != 2 {
			t.Fatalf("%s has %d columns", row.Path, len(row.Changes))
		}
	}
	if got := strings.Join(paths, " "); got != "a.txt only-a.txt sub/only-b.txt" {
		t.Fatalf("files = %s", got)
	}
	both, onlyA, onlyB := cmp.Files[0], cmp.Files[1], cmp.Files[2]
	if both.Changes[0].Added != 1 || both.Changes[0].Deleted != 0 || both.Changes[1].Added != 1 || both.Changes[1].Deleted != 1 {
		t.Errorf("a.txt = %+v %+v", both.Changes[0], both.Changes[1])
	}
	if onlyA.Changes[0].Added != 3 || onlyA.Changes[1] != nil {
		t.Errorf("only-a.txt = %+v", onlyA.Changes)
	}
	if onlyB.Changes[0] != nil || onlyB.Changes[1].Added != 1 || !onlyB.Changes[1].Untracked {
		t.Errorf("sub/only-b.txt = %+v", onlyB.Changes)
	}

	for name, group := range map[string]string{"invalid": "../x", "unknown": "0123456789abcdef", "a task id": a.ID} {
		if _, err := f.api.Compare(group); err == nil {
			t.Errorf("%s group accepted", name)
		}
	}
}

func diffText(d TaskFileDiff) string {
	var b strings.Builder
	for _, l := range d.Lines {
		b.WriteString(map[string]string{"add": "+", "del": "-", "ctx": " ", "hunk": ""}[l.Kind] + l.Text + "\n")
	}
	return b.String()
}

func TestCompareFile(t *testing.T) {
	f, a, b := compareFixture(t)
	fc, err := f.api.CompareFile(a.Group, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.Tasks) != 2 || fc.Tasks[0].TaskID != a.ID || fc.Tasks[1].TaskID != b.ID {
		t.Fatalf("tasks = %+v", fc.Tasks)
	}
	if got := diffText(fc.Tasks[0]); got != "@@ -1,1 +1,2 @@\n one\n+two\n" || fc.Tasks[0].Added != 1 || !fc.Tasks[0].Changed {
		t.Errorf("first task's diff =\n%s%+v", got, fc.Tasks[0])
	}
	// Uncommitted work counts, as in the task's own diff.
	if got := diffText(fc.Tasks[1]); got != "@@ -1,1 +1,1 @@\n-one\n+ONE\n" || fc.Tasks[1].Deleted != 1 {
		t.Errorf("second task's diff =\n%s", got)
	}

	// A file only one task has: new there, absent in the other.
	fc, err = f.api.CompareFile(a.Group, "sub/only-b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if fc.Tasks[0].Changed || len(fc.Tasks[0].Lines) != 0 || fc.Tasks[0].Note == "" {
		t.Errorf("task without the file = %+v", fc.Tasks[0])
	}
	if got := diffText(fc.Tasks[1]); got != "@@ -0,0 +1,1 @@\n+new\n" || fc.Tasks[1].Note != "New file." {
		t.Errorf("new file =\n%s%+v", got, fc.Tasks[1])
	}
	// A file no task touched is unchanged in both.
	fc, _ = f.api.CompareFile(a.Group, "keep.txt")
	if fc.Tasks[0].Changed || fc.Tasks[1].Changed || fc.Tasks[0].Note != "Unchanged." {
		t.Errorf("untouched file = %+v", fc.Tasks)
	}

	// A rename is compared with the file it came from.
	gitT(t, a.Worktree, "mv", "keep.txt", "moved.txt")
	writeT(t, filepath.Join(a.Worktree, "moved.txt"), "same\nmore\n")
	fc, _ = f.api.CompareFile(a.Group, "moved.txt")
	if got := diffText(fc.Tasks[0]); fc.Tasks[0].OrigPath != "keep.txt" || got != "@@ -1,1 +1,2 @@\n same\n+more\n" {
		t.Errorf("rename = %+v\n%s", fc.Tasks[0], got)
	}

	// Binary files are named, not shown.
	if err := os.WriteFile(filepath.Join(b.Worktree, "img.bin"), []byte{1, 0, 2, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	fc, _ = f.api.CompareFile(a.Group, "img.bin")
	if !fc.Tasks[1].Changed || len(fc.Tasks[1].Lines) != 0 || !strings.Contains(fc.Tasks[1].Note, "Binary") {
		t.Errorf("binary = %+v", fc.Tasks[1])
	}
}

func TestCompareFileStaysInsideTheWorktree(t *testing.T) {
	f, a, _ := compareFixture(t)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	writeT(t, secret, "token\n")
	if err := os.Symlink(secret, filepath.Join(a.Worktree, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(secret), filepath.Join(a.Worktree, "leakdir")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"leak.txt", "leakdir/secret.txt"} {
		fc, err := f.api.CompareFile(a.Group, path)
		if err != nil {
			t.Fatal(err)
		}
		if got := diffText(fc.Tasks[0]); strings.Contains(got, "token") || fc.Tasks[0].Note == "" {
			t.Errorf("%s: a link out of the worktree was followed:\n%s%+v", path, got, fc.Tasks[0])
		}
	}
	for _, path := range []string{"", "../proj/a.txt", "/etc/passwd", secret, ".git", ".git/config", "a\x00b", strings.Repeat("a/", 3000)} {
		if _, err := f.api.CompareFile(a.Group, path); err == nil {
			t.Errorf("path %q accepted", path)
		}
		if _, err := f.api.DiffBetween(a.ID, a.ID, path); err == nil {
			t.Errorf("DiffBetween: path %q accepted", path)
		}
	}
}

func TestDiffBetweenTasks(t *testing.T) {
	f, a, b := compareFixture(t)
	d, err := f.api.DiffBetween(a.ID, b.ID, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	// What the second has that the first has not, work trees as they are.
	if got := diffText(d); got != "@@ -1,2 +1,1 @@\n-one\n-two\n+ONE\n" || d.TaskID != b.ID || !d.Changed {
		t.Errorf("a → b =\n%s%+v", got, d)
	}
	d, _ = f.api.DiffBetween(b.ID, a.ID, "only-a.txt")
	if got := diffText(d); got != "@@ -0,0 +1,3 @@\n+x\n+y\n+z\n" || d.Note != "New file." {
		t.Errorf("b → a of a file only a has =\n%s%+v", got, d)
	}
	d, _ = f.api.DiffBetween(a.ID, b.ID, "keep.txt")
	if d.Changed || len(d.Lines) != 0 {
		t.Errorf("the same file in both = %+v", d)
	}

	// Only tasks of one group are compared.
	lone := f.create("on its own")
	otherGroup := f.group("another", "fake", "other")
	for name, pair := range map[string][2]string{
		"itself":        {a.ID, a.ID},
		"no group":      {a.ID, lone.ID},
		"two loners":    {lone.ID, lone.ID},
		"another group": {a.ID, otherGroup[0].ID},
		"unknown":       {a.ID, "0123456789abcdef"},
		"invalid":       {"x", b.ID},
	} {
		if _, err := f.api.DiffBetween(pair[0], pair[1], "a.txt"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCompareGroupWithAStaleTask(t *testing.T) {
	f, a, b := compareFixture(t)
	if err := os.RemoveAll(b.Worktree); err != nil {
		t.Fatal(err)
	}
	cmp, err := f.api.Compare(a.Group)
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Tasks[1].Error == "" || !cmp.Tasks[1].Task.Stale || cmp.Tasks[0].Count != 2 {
		t.Errorf("tasks = %+v", cmp.Tasks)
	}
	if len(cmp.Files) != 2 || cmp.Files[0].Changes[1] != nil {
		t.Errorf("files = %+v", cmp.Files)
	}
	fc, err := f.api.CompareFile(a.Group, "a.txt")
	if err != nil || fc.Tasks[1].Note == "" || !fc.Tasks[0].Changed {
		t.Errorf("file = %+v, %v", fc, err)
	}
	if _, err := f.api.DiffBetween(a.ID, b.ID, "a.txt"); err == nil {
		t.Error("a diff with a task whose worktree is gone")
	}
}

// Keeping one task of a group is Merge for it, then Remove for the others,
// each with Remove's own rules: nothing is deleted without the user's answers.
func TestKeepOneTaskOfAGroup(t *testing.T) {
	f, a, b := compareFixture(t)
	if res, err := f.api.Merge(a.ID); err != nil || !res.Merged {
		t.Fatalf("merge = %+v, %v", res, err)
	}
	info, err := f.api.RemoveCheck(b.ID)
	if err != nil || info.Changes != 2 {
		t.Fatalf("RemoveCheck = %+v, %v", info, err)
	}
	if _, err := f.api.Remove(b.ID, RemoveOptions{}); err == nil {
		t.Fatal("the losing task's uncommitted work was deleted without asking")
	}
	if _, err := os.Stat(filepath.Join(b.Worktree, "sub/only-b.txt")); err != nil {
		t.Fatal("the refused removal deleted files")
	}
	if _, err := f.api.Remove(b.ID, RemoveOptions{DiscardChanges: true}); err != nil {
		t.Fatal(err)
	}
	// The kept task stays, still in its group (now alone).
	cmp, err := f.api.Compare(a.Group)
	if err != nil || len(cmp.Tasks) != 1 || !cmp.Tasks[0].Task.Merged {
		t.Errorf("after keeping one: %+v, %v", cmp.Tasks, err)
	}
}
