package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// newTaskRepo returns a repository on main plus a linked worktree on branch
// "agent/x" created from main, the layout an agent task uses.
func newTaskRepo(t *testing.T) (main, task *Repo) {
	t.Helper()
	ctx := context.Background()
	main = newRepo(t)
	writeFile(t, main.Root, "a.txt", "one\ntwo\nthree\n")
	commitAll(t, main.Root, "add a")
	wt := filepath.Join(t.TempDir(), "wt")
	if err := main.AddWorktree(ctx, wt, "agent/x", true, "main"); err != nil {
		t.Fatal(err)
	}
	return main, openRepo(t, wt)
}

func TestBranchAndCommitExist(t *testing.T) {
	ctx := context.Background()
	main, _ := newTaskRepo(t)
	if !main.BranchExists(ctx, "agent/x") || main.BranchExists(ctx, "agent/y") {
		t.Error("BranchExists wrong")
	}
	if !main.CommitExists(ctx, "main") || main.CommitExists(ctx, "nope") || main.CommitExists(ctx, "--all") {
		t.Error("CommitExists wrong")
	}
}

func TestDiffStatAgainstBase(t *testing.T) {
	ctx := context.Background()
	_, task := newTaskRepo(t)
	// A commit on the task branch, an uncommitted edit and a new untracked file.
	commitFile(t, task.Root, "b.txt", "x\ny\n", "add b")
	writeFile(t, task.Root, "a.txt", "one\nTWO\nthree\nfour\n")
	writeFile(t, task.Root, "new/c.txt", "1\n2\n3\n")

	st, err := task.DiffStat(ctx, "main", 100)
	if err != nil {
		t.Fatal(err)
	}
	if st.MergeBase == "" {
		t.Error("no merge base")
	}
	got := map[string][2]int{}
	for _, f := range st.Files {
		got[f.Path] = [2]int{f.Added, f.Deleted}
	}
	want := map[string][2]int{"a.txt": {2, 1}, "b.txt": {2, 0}, "new/c.txt": {3, 0}}
	if len(got) != len(want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s = %v, want %v", p, got[p], w)
		}
	}
	if st.Added != 7 || st.Deleted != 1 || st.Truncated {
		t.Errorf("totals +%d -%d truncated %v", st.Added, st.Deleted, st.Truncated)
	}

	limited, err := task.DiffStat(ctx, "main", 2)
	if err != nil || len(limited.Files) != 2 || !limited.Truncated || limited.Count != 3 {
		t.Errorf("limited = %+v, %v", limited, err)
	}
}

func TestParseNumstat(t *testing.T) {
	out := []byte("1\t2\ta.go\x00-\t-\timg.png\x00" + "3\t0\t\x00old.go\x00new.go\x00")
	got := parseNumstat(out)
	want := []FileStat{
		{Path: "a.go", Added: 1, Deleted: 2},
		{Path: "img.png", Binary: true},
		{Path: "new.go", OrigPath: "old.go", Added: 3},
	}
	if !slices.Equal(got, want) {
		t.Errorf("parseNumstat = %+v\nwant %+v", got, want)
	}
}

func TestDirtyAndAhead(t *testing.T) {
	ctx := context.Background()
	main, task := newTaskRepo(t)
	if d, err := task.Dirty(ctx); err != nil || d {
		t.Fatalf("fresh worktree dirty = %v, %v", d, err)
	}
	writeFile(t, task.Root, "untracked.txt", "u")
	if d, _ := task.Dirty(ctx); d {
		t.Error("an untracked file counts as dirty")
	}
	writeFile(t, task.Root, "a.txt", "changed\n")
	if d, _ := task.Dirty(ctx); !d {
		t.Error("tracked edit not dirty")
	}
	if n, err := main.Ahead(ctx, "main", "agent/x"); n != 0 || err != nil {
		t.Errorf("ahead = %d, %v, want 0", n, err)
	}
	commitAll(t, task.Root, "work")
	if n, _ := main.Ahead(ctx, "main", "agent/x"); n != 1 {
		t.Errorf("ahead = %d, want 1", n)
	}
	if _, err := main.Ahead(ctx, "gone", "agent/x"); err == nil {
		t.Error("Ahead of a missing base gave no error")
	}
	head, err := task.Resolve(ctx, "HEAD")
	if err != nil || len(head) != 40 {
		t.Errorf("Resolve(HEAD) = %q, %v", head, err)
	}
	if main.IsAncestor(ctx, "agent/x", "main") {
		t.Error("unmerged branch is an ancestor of main")
	}
}

func TestMergeBranch(t *testing.T) {
	ctx := context.Background()
	main, task := newTaskRepo(t)
	commitFile(t, task.Root, "b.txt", "b\n", "add b")

	if c, err := main.MergeConflicts(ctx, "main", "agent/x"); err != nil || len(c) != 0 {
		t.Fatalf("conflicts = %v, %v", c, err)
	}
	if err := main.Merge(ctx, "agent/x"); err != nil {
		t.Fatal(err)
	}
	if readFile(t, main.Root, "b.txt") != "b\n" {
		t.Error("merge did not bring b.txt")
	}
	if !main.IsAncestor(ctx, "agent/x", "main") {
		t.Error("merged branch is not an ancestor of main")
	}
}

func TestMergeConflictIsRefusedAndAborted(t *testing.T) {
	ctx := context.Background()
	main, task := newTaskRepo(t)
	commitFile(t, task.Root, "a.txt", "task\n", "task edit")
	commitFile(t, main.Root, "a.txt", "main\n", "main edit")

	c, err := main.MergeConflicts(ctx, "main", "agent/x")
	if err != nil || !slices.Equal(c, []string{"a.txt"}) {
		t.Fatalf("conflicts = %v, %v", c, err)
	}
	// Merge itself backs out of a conflict (for gits without merge-tree --write-tree).
	err = main.Merge(ctx, "agent/x")
	var conflict *MergeConflictError
	if !errors.As(err, &conflict) || !slices.Equal(conflict.Files, []string{"a.txt"}) {
		t.Fatalf("Merge = %v, want a conflict on a.txt", err)
	}
	if main.operation() != "" || readFile(t, main.Root, "a.txt") != "main\n" {
		t.Error("conflicting merge was not aborted")
	}

	// Bringing the base into the task leaves the conflict there for an agent.
	files, err := task.MergeInto(ctx, "main")
	if err != nil || !slices.Equal(files, []string{"a.txt"}) {
		t.Fatalf("MergeInto = %v, %v", files, err)
	}
	if task.operation() != "merging" {
		t.Error("task worktree not left merging")
	}
}

func TestPruneWorktrees(t *testing.T) {
	ctx := context.Background()
	main, task := newTaskRepo(t)
	if err := removeAll(task.Root); err != nil {
		t.Fatal(err)
	}
	if err := main.PruneWorktrees(ctx); err != nil {
		t.Fatal(err)
	}
	wts, _ := main.Worktrees(ctx)
	if len(wts) != 1 {
		t.Errorf("worktrees after prune = %+v", wts)
	}
}

func removeAll(dir string) error { return os.RemoveAll(dir) }
