package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mergeConflictRepo is conflictRepo with the merge of feature started.
func mergeConflictRepo(t *testing.T) *Repo {
	t.Helper()
	r := conflictRepo(t)
	if _, err := tryGit(r.Root, "merge", "feature"); err == nil {
		t.Fatal("merge should conflict")
	}
	return r
}

func TestWritesRefuseAFileChangedOnDisk(t *testing.T) {
	ctx := context.Background()
	r := mergeConflictRepo(t)
	loaded := readFile(t, r.Root, "f.txt")
	// Someone else (an agent in a terminal, another editor) resolves the file meanwhile.
	theirs := strings.Replace(conflictBase, "two", "two-by-agent", 1)
	writeFile(t, r.Root, "f.txt", theirs)
	mine := strings.Replace(conflictBase, "two", "two-mine", 1)

	if err := r.SaveResult(ctx, "f.txt", mine, loaded, false); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("SaveResult: err = %v, want ErrChangedOnDisk", err)
	}
	if err := r.MarkResolved(ctx, "f.txt", mine, loaded, false, false); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatalf("MarkResolved: err = %v, want ErrChangedOnDisk", err)
	}
	if readFile(t, r.Root, "f.txt") != theirs {
		t.Fatal("a refused write must leave the other change alone")
	}
	if st, _ := r.Status(ctx); statusFile(t, st, "f.txt").Kind != Conflicted {
		t.Fatal("a refused resolve must not stage")
	}

	// Saving over what is on disk now (after a reload) works, and doesn't stage.
	if err := r.SaveResult(ctx, "f.txt", mine, theirs, false); err != nil {
		t.Fatal(err)
	}
	if readFile(t, r.Root, "f.txt") != mine {
		t.Fatal("SaveResult did not write")
	}
	if st, _ := r.Status(ctx); statusFile(t, st, "f.txt").Kind != Conflicted {
		t.Fatal("Save must not stage")
	}
	// The user chose to overwrite.
	writeFile(t, r.Root, "f.txt", theirs)
	if err := r.MarkResolved(ctx, "f.txt", mine, loaded, false, true); err != nil {
		t.Fatal(err)
	}
	if readFile(t, r.Root, "f.txt") != mine {
		t.Fatal("overwrite did not write")
	}
}

func TestWritesRefuseASymlinkedParentDirectory(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	commitFile(t, r.Root, "sub/f.txt", conflictBase, "base")
	mustGit(t, r.Root, "switch", "-q", "-c", "feature")
	commitFile(t, r.Root, "sub/f.txt", strings.Replace(conflictBase, "two", "two-feature", 1), "feature")
	mustGit(t, r.Root, "switch", "-q", "main")
	commitFile(t, r.Root, "sub/f.txt", strings.Replace(conflictBase, "two", "two-main", 1), "main")
	if _, err := tryGit(r.Root, "merge", "feature"); err == nil {
		t.Fatal("merge should conflict")
	}
	// sub becomes a link to a folder outside the repository holding an f.txt.
	outside := t.TempDir()
	writeFile(t, outside, "f.txt", "secret\n")
	if err := os.RemoveAll(filepath.Join(r.Root, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.Root, "sub")); err != nil {
		t.Fatal(err)
	}

	if err := r.MarkResolved(ctx, "sub/f.txt", "x\n", "secret\n", false, true); err == nil {
		t.Fatal("MarkResolved wrote through a symlinked folder")
	}
	if err := r.SaveResult(ctx, "sub/f.txt", "x\n", "secret\n", true); err == nil {
		t.Fatal("SaveResult wrote through a symlinked folder")
	}
	if readFile(t, outside, "f.txt") != "secret\n" {
		t.Fatal("the file outside the repository was changed")
	}
	c, err := r.Conflict(ctx, "sub/f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if c.Working.Exists || c.Working.Content != "" {
		t.Fatal("Conflict read a file outside the repository")
	}
}

func TestConflictCountIsCachedUntilTheFileChanges(t *testing.T) {
	ctx := context.Background()
	r := mergeConflictRepo(t)
	count := func() int {
		t.Helper()
		st, err := r.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return statusFile(t, st, "f.txt").Conflicts
	}
	before := conflictReads.Load()
	if count() != 1 || count() != 1 {
		t.Fatal("want 1 conflict")
	}
	if n := conflictReads.Load() - before; n != 1 {
		t.Fatalf("file read %d times for two refreshes, want 1", n)
	}
	writeFile(t, r.Root, "f.txt", conflictBase)
	if count() != 0 {
		t.Fatal("a changed file must be counted again")
	}
}
