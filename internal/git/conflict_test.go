package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const conflictBase = "one\ntwo\nthree\nfour\nfive\nsix\nseven\n"

// conflictRepo has main and feature changing line two differently, and line six only
// on feature (a clean hunk). Returns the repo checked out on main.
func conflictRepo(t *testing.T) *Repo {
	t.Helper()
	r := newRepo(t)
	commitFile(t, r.Root, "f.txt", conflictBase, "base")
	mustGit(t, r.Root, "switch", "-q", "-c", "feature")
	commitFile(t, r.Root, "f.txt", strings.Replace(strings.Replace(conflictBase, "two", "two-feature", 1), "six", "six-feature", 1), "feature change")
	mustGit(t, r.Root, "switch", "-q", "main")
	commitFile(t, r.Root, "f.txt", strings.Replace(conflictBase, "two", "two-main", 1), "main change")
	return r
}

func TestConflictDuringMerge(t *testing.T) {
	ctx := context.Background()
	r := conflictRepo(t)
	if _, err := tryGit(r.Root, "merge", "feature"); err == nil {
		t.Fatal("merge should conflict")
	}

	c, err := r.Conflict(ctx, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if c.Base.Content != conflictBase || !strings.Contains(c.Current.Content, "two-main") || !strings.Contains(c.Incoming.Content, "two-feature") {
		t.Fatalf("stages: %+v", c)
	}
	if !strings.Contains(c.Working.Content, "<<<<<<< HEAD") || !strings.Contains(c.Working.Content, "six-feature") {
		t.Fatalf("working file should hold git's markers and the clean hunk:\n%s", c.Working.Content)
	}
	blocks := ParseConflictMarkers(c.Diff3)
	if len(blocks) != 1 || blocks[0].Current != "two-main\n" || blocks[0].Base != "two\n" || blocks[0].Incoming != "two-feature\n" {
		t.Fatalf("diff3 merge of the stages:\n%s", c.Diff3)
	}
	if !strings.Contains(c.Diff3, "six-feature") {
		t.Fatalf("the diff3 merge should hold the clean hunks too:\n%s", c.Diff3)
	}
	s := c.Sides
	if s.Operation != "merging" || s.Current.Label != "main" || s.Incoming.Label != "feature" {
		t.Fatalf("sides = %+v", s)
	}

	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if f := statusFile(t, st, "f.txt"); f.Kind != Conflicted || f.Conflicts != 1 {
		t.Fatalf("status entry = %+v, want conflicted with 1 conflict", f)
	}
}

func TestConflictSidesDuringRebase(t *testing.T) {
	ctx := context.Background()
	r := conflictRepo(t)
	mustGit(t, r.Root, "switch", "-q", "feature")
	if _, err := tryGit(r.Root, "rebase", "main"); err == nil {
		t.Fatal("rebase should conflict")
	}
	c, err := r.Conflict(ctx, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	s := c.Sides
	// Rebasing replays feature's commits onto main: HEAD (ours, current) is main.
	if s.Operation != "rebasing" || s.Current.Label != "main" || s.Incoming.Label != "feature" {
		t.Fatalf("sides = %+v", s)
	}
	if !strings.Contains(s.Incoming.Detail, "feature change") || !strings.Contains(c.Current.Content, "two-main") {
		t.Fatalf("incoming should name the replayed commit: %+v", s)
	}
}

func TestConflictSidesDuringCherryPickAndStash(t *testing.T) {
	ctx := context.Background()
	r := conflictRepo(t)
	pick := mustGit(t, r.Root, "rev-parse", "--short", "feature")
	if _, err := tryGit(r.Root, "cherry-pick", "feature"); err == nil {
		t.Fatal("cherry-pick should conflict")
	}
	s, err := r.ConflictSides(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Operation != "cherry-picking" || s.Current.Label != "main" || s.Incoming.Label != pick {
		t.Fatalf("cherry-pick sides = %+v (want incoming %s)", s, pick)
	}
	mustGit(t, r.Root, "cherry-pick", "--abort")

	// A stash pop leaves no operation behind.
	writeFile(t, r.Root, "f.txt", strings.Replace(conflictBase, "two", "two-stash", 1))
	mustGit(t, r.Root, "stash", "push", "-q")
	commitFile(t, r.Root, "f.txt", strings.Replace(conflictBase, "two", "two-later", 1), "later")
	if _, err := tryGit(r.Root, "stash", "pop"); err == nil {
		t.Fatal("stash pop should conflict")
	}
	s, err = r.ConflictSides(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Operation != "" || s.Current.Label != "main" || s.Incoming.Label == "" {
		t.Fatalf("stash sides = %+v", s)
	}
}

func TestMarkResolved(t *testing.T) {
	ctx := context.Background()
	r := conflictRepo(t)
	if _, err := tryGit(r.Root, "merge", "feature"); err == nil {
		t.Fatal("merge should conflict")
	}
	working := readFile(t, r.Root, "f.txt")

	if err := r.MarkResolved(ctx, "f.txt", working, working, false, false); !errors.Is(err, ErrConflictMarkers) {
		t.Fatalf("markers left: err = %v, want ErrConflictMarkers", err)
	}
	if readFile(t, r.Root, "f.txt") != working {
		t.Fatal("a refused resolve must not write the file")
	}
	if err := r.MarkResolved(ctx, "../f.txt", "x\n", "", false, false); err == nil {
		t.Fatal("a path outside the repository must be refused")
	}
	if err := r.MarkResolved(ctx, "README.md", "x\n", "", false, false); err == nil {
		t.Fatal("a file without a conflict must be refused")
	}

	resolved := strings.Replace(conflictBase, "two", "two-both", 1)
	if err := r.MarkResolved(ctx, "f.txt", resolved, working, false, false); err != nil {
		t.Fatal(err)
	}
	if readFile(t, r.Root, "f.txt") != resolved {
		t.Fatal("result not written")
	}
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if f := statusFile(t, st, "f.txt"); f.Kind == Conflicted || f.Index != "M" {
		t.Fatalf("after resolving: %+v, want staged", f)
	}
	if _, err := r.Conflict(ctx, "f.txt"); !errors.Is(err, ErrNoConflict) {
		t.Fatalf("Conflict after resolving: err = %v, want ErrNoConflict", err)
	}
}

func TestMarkResolvedKeepsMarkersWhenAllowed(t *testing.T) {
	ctx := context.Background()
	r := conflictRepo(t)
	if _, err := tryGit(r.Root, "merge", "feature"); err == nil {
		t.Fatal("merge should conflict")
	}
	if err := os.Chmod(filepath.Join(r.Root, "f.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	working := readFile(t, r.Root, "f.txt")
	if err := r.MarkResolved(ctx, "f.txt", working, working, true, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(r.Root, "f.txt"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("file mode should be kept: %v %v", info, err)
	}
	if st, _ := r.Status(ctx); statusFile(t, st, "f.txt").Kind == Conflicted {
		t.Fatal("the file should be staged")
	}
}

func TestMarkResolvedRefusesSymlinks(t *testing.T) {
	ctx := context.Background()
	r := conflictRepo(t)
	if _, err := tryGit(r.Root, "merge", "feature"); err == nil {
		t.Fatal("merge should conflict")
	}
	p := filepath.Join(r.Root, "f.txt")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	writeFile(t, filepath.Dir(outside), "outside.txt", "secret\n")
	if err := os.Symlink(outside, p); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkResolved(ctx, "f.txt", "x\n", "", false, true); err == nil {
		t.Fatal("writing through a symlink must be refused")
	}
	if readFile(t, filepath.Dir(outside), "outside.txt") != "secret\n" {
		t.Fatal("the symlink target was changed")
	}
	if _, err := r.Conflict(ctx, "f.txt"); err != nil {
		t.Fatalf("Conflict should still load the stages: %v", err)
	}
}

func TestConflictAddAddWithoutBase(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	mustGit(t, r.Root, "switch", "-q", "-c", "feature")
	commitFile(t, r.Root, "new.txt", "feature\n", "add on feature")
	mustGit(t, r.Root, "switch", "-q", "main")
	commitFile(t, r.Root, "new.txt", "main\n", "add on main")
	if _, err := tryGit(r.Root, "merge", "feature"); err == nil {
		t.Fatal("merge should conflict")
	}
	c, err := r.Conflict(ctx, "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if c.Base.Exists || !c.Current.Exists || !c.Incoming.Exists || len(ParseConflictMarkers(c.Diff3)) != 1 {
		t.Fatalf("add/add conflict = %+v", c)
	}
}
