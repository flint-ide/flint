package git

import (
	"context"
	"reflect"
	"testing"
)

// blameLines expands ranges into the hash of each line (1-based index 0 unused).
func blameLines(t *testing.T, b Blame) []string {
	t.Helper()
	lines := []string{""}
	for _, rg := range b.Ranges {
		if rg.Line != len(lines) {
			t.Fatalf("ranges not contiguous at line %d: %+v", rg.Line, b.Ranges)
		}
		for range rg.Count {
			lines = append(lines, b.Commits[rg.Commit].Hash)
		}
	}
	return lines[1:]
}

func TestBlame(t *testing.T) {
	ctx := context.Background()
	r := newEmptyRepo(t)
	c1 := commitFile(t, r.Root, "f.txt", "a\nb\nc\n", "first")
	mustGit(t, r.Root, "mv", "f.txt", "g.txt")
	commitAll(t, r.Root, "rename")
	c3 := commitFile(t, r.Root, "g.txt", "a\nB\nc\nd\n", "third")
	writeFile(t, r.Root, "g.txt", "a\nB\nc\nd\nx\n")

	b, err := r.Blame(ctx, "g.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Root != r.Root || b.Untracked {
		t.Errorf("Root = %q, Untracked = %v", b.Root, b.Untracked)
	}
	if got, want := blameLines(t, b), []string{c1, c3, c1, c3, uncommittedHash}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	if len(b.Commits) != 3 {
		t.Fatalf("commits listed once each, got %d: %+v", len(b.Commits), b.Commits)
	}
	byHash := map[string]BlameCommit{}
	for _, c := range b.Commits {
		byHash[c.Hash] = c
	}
	first := byHash[c1]
	if first.Author != "Test User" || first.Email != "test@example.com" || first.Summary != "first" || first.Time == 0 {
		t.Errorf("first = %+v", first)
	}
	// Blame follows the rename: the oldest lines come from f.txt, a root commit.
	if first.Path != "f.txt" || first.Previous != "" {
		t.Errorf("first path/previous = %q/%q", first.Path, first.Previous)
	}
	third := byHash[c3]
	if third.Path != "g.txt" || third.Previous == "" || third.PrevPath != "g.txt" || third.Uncommitted {
		t.Errorf("third = %+v", third)
	}
	if !byHash[uncommittedHash].Uncommitted {
		t.Errorf("working-tree line not marked uncommitted: %+v", byHash[uncommittedHash])
	}
}

func TestBlameContents(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t) // README.md = "hello\n"
	head := mustGit(t, r.Root, "rev-parse", "HEAD")
	buffer := "new first line\nhello\n"
	b, err := r.Blame(ctx, "README.md", &buffer)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := blameLines(t, b), []string{uncommittedHash, head}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	if readFile(t, r.Root, "README.md") != "hello\n" {
		t.Error("blaming a buffer must not touch the file")
	}
}

func TestBlameUntracked(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	writeFile(t, r.Root, "new.txt", "x\n")
	b, err := r.Blame(ctx, "new.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Untracked || len(b.Ranges) != 0 {
		t.Errorf("untracked blame = %+v", b)
	}

	empty := newEmptyRepo(t)
	writeFile(t, empty.Root, "a.txt", "x\n")
	if b, err := empty.Blame(ctx, "a.txt", nil); err != nil || !b.Untracked {
		t.Errorf("no commits: %+v, %v", b, err)
	}
}

func TestParseBlameIgnoresGarbage(t *testing.T) {
	b := parseBlame([]byte("garbage\n\nnot a header line\n"))
	if len(b.Commits) != 0 || len(b.Ranges) != 0 {
		t.Errorf("parseBlame(garbage) = %+v", b)
	}
}
