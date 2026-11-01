package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func mkfifo(path string) error { return syscall.Mkfifo(path, 0o644) }

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

func TestBranchExistsRejectsRevisionSyntax(t *testing.T) {
	ctx := context.Background()
	main, _ := newTaskRepo(t)
	for _, rev := range []string{"main@{1}", "main^", "main~1", "HEAD", "refs/heads/main", "main:a.txt"} {
		if main.BranchExists(ctx, rev) {
			t.Errorf("BranchExists(%q) = true", rev)
		}
	}
	if !main.BranchExists(ctx, "main") {
		t.Error("main not found")
	}
}

func TestBranchRefsIgnoreSameNamedTags(t *testing.T) {
	ctx := context.Background()
	main, task := newTaskRepo(t)
	commitFile(t, task.Root, "b.txt", "b\n", "task work")
	// A tag named like the branch, pointing at base, must not hide the work.
	mustGit(t, main.Root, "tag", "agent/x", "main")
	if n, err := main.Ahead(ctx, "main", "agent/x"); err != nil || n != 1 {
		t.Errorf("Ahead = %d, %v, want 1 (the branch, not the tag)", n, err)
	}
	if c, err := main.MergeConflicts(ctx, "main", "agent/x"); err != nil || len(c) != 0 {
		t.Fatalf("conflicts = %v, %v", c, err)
	}
	if err := main.Merge(ctx, "agent/x"); err != nil {
		t.Fatal(err)
	}
	if !fileExists(main.Root, "b.txt") {
		t.Error("merged the tag instead of the branch")
	}
}

func TestDiffStatSkipsSpecialFiles(t *testing.T) {
	ctx := context.Background()
	_, task := newTaskRepo(t)
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := mkfifo(fifo); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	// A symlink to a FIFO would block a reader forever.
	if err := os.Symlink(fifo, filepath.Join(task.Root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := mkfifo(filepath.Join(task.Root, "pipe")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, task.Root, "new.txt", "1\n2\n")
	done := make(chan DiffStat, 1)
	go func() {
		st, err := task.DiffStat(ctx, "main", 100)
		if err != nil {
			t.Error(err)
		}
		done <- st
	}()
	select {
	case st := <-done:
		// git may or may not list the FIFO itself; only new.txt has lines.
		if st.Count < 2 || st.Added != 2 {
			t.Errorf("stat = %+v, want the link and new.txt, 2 added lines", st)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DiffStat blocked on a FIFO")
	}
}

func TestDiffStatReadsOnlyUpToTheLimit(t *testing.T) {
	ctx := context.Background()
	_, task := newTaskRepo(t)
	for i := range 5 {
		writeFile(t, task.Root, fmt.Sprintf("n%d.txt", i), "x\n")
	}
	st, err := task.DiffStat(ctx, "main", 2)
	if err != nil {
		t.Fatal(err)
	}
	if st.Count != 5 || len(st.Files) != 2 || !st.Truncated || st.Added != 2 {
		t.Errorf("stat = %+v, want 5 files, 2 listed and counted", st)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := task.DiffStat(cancelled, "main", 2); err == nil {
		t.Error("DiffStat ignored a cancelled context")
	}
}

func TestPushBranchPushesTheTaskBranch(t *testing.T) {
	ctx := context.Background()
	main, task := newTaskRepo(t)
	bare := newBareRemote(t, main)
	commitFile(t, task.Root, "b.txt", "b\n", "task work")
	// The worktree has something else checked out now.
	mustGit(t, task.Root, "switch", "-q", "-c", "other")
	if err := task.PushBranch(ctx, "agent/x"); err != nil {
		t.Fatal(err)
	}
	refs := mustGit(t, main.Root, "ls-remote", bare)
	if !strings.Contains(refs, "refs/heads/agent/x") || strings.Contains(refs, "refs/heads/other") {
		t.Errorf("remote refs = %s", refs)
	}
	if up := mustGit(t, main.Root, "config", "branch.agent/x.remote"); up != "origin" {
		t.Errorf("upstream remote = %q", up)
	}
}

func TestRemoveWorktreeEntryKeepsOthers(t *testing.T) {
	ctx := context.Background()
	main, task := newTaskRepo(t)
	other := filepath.Join(t.TempDir(), "other")
	if err := main.AddWorktree(ctx, other, "agent/y", true, "main"); err != nil {
		t.Fatal(err)
	}
	// Both folders vanish (one maybe only on an unmounted volume).
	for _, dir := range []string{task.Root, other} {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := main.RemoveWorktreeEntry(ctx, task.Root); err != nil {
		t.Fatal(err)
	}
	list := mustGit(t, main.Root, "worktree", "list", "--porcelain")
	if strings.Contains(list, task.Root) || !strings.Contains(list, "other") {
		t.Errorf("worktrees after removing one entry:\n%s", list)
	}
	if err := main.RemoveWorktreeEntry(ctx, filepath.Join(t.TempDir(), "unknown")); err == nil {
		t.Error("no error for a folder that is no worktree")
	}
}

func TestIgnoredFiles(t *testing.T) {
	ctx := context.Background()
	_, task := newTaskRepo(t)
	commitFile(t, task.Root, ".gitignore", ".env\nbuild/\n", "ignore")
	if n, err := task.IgnoredFiles(ctx); err != nil || n != 0 {
		t.Fatalf("ignored = %d, %v", n, err)
	}
	writeFile(t, task.Root, ".env", "TOKEN=x")
	writeFile(t, task.Root, "build/a/b.o", "x")
	if n, err := task.IgnoredFiles(ctx); err != nil || n != 2 {
		t.Errorf("ignored = %d, %v, want 2 (.env and build/)", n, err)
	}
}

func TestMergeIntoNeverFastForwards(t *testing.T) {
	ctx := context.Background()
	main, task := newTaskRepo(t)
	commitFile(t, main.Root, "m.txt", "m\n", "base moved")
	if files, err := task.MergeInto(ctx, "main"); err != nil || len(files) != 0 {
		t.Fatalf("MergeInto = %v, %v", files, err)
	}
	tip, _ := task.Resolve(ctx, "HEAD")
	base, _ := main.Resolve(ctx, "main")
	if tip == base {
		t.Error("the task branch was fast-forwarded to its base")
	}
}
