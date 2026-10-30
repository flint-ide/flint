package git

import (
	"context"
	"strings"
	"testing"
)

func TestFileAt(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	first := commitFile(t, r.Root, "doc.txt", "v1\n", "v1")
	commitFile(t, r.Root, "doc.txt", "v2\n", "v2")
	commitFile(t, r.Root, "bin.dat", "PNG\x00\x01\x02", "binary")
	commitFile(t, r.Root, "temp.txt", "temp\n", "add temp")
	mustGit(t, r.Root, "rm", "-q", "temp.txt")
	commitAll(t, r.Root, "remove temp")
	writeFile(t, r.Root, "doc.txt", "v3 staged\n")
	mustGit(t, r.Root, "add", "doc.txt")
	writeFile(t, r.Root, "doc.txt", "v4 worktree\n")

	tests := []struct {
		name string
		rev  string
		rel  string
		want Revision
	}{
		{"HEAD", "HEAD", "doc.txt", Revision{Exists: true, Content: "v2\n"}},
		{"index", ":", "doc.txt", Revision{Exists: true, Content: "v3 staged\n"}},
		{"by hash", first, "doc.txt", Revision{Exists: true, Content: "v1\n"}},
		{"missing at HEAD", "HEAD", "nope.txt", Revision{}},
		{"missing in index", ":", "nope.txt", Revision{}},
		{"not yet at older rev", first, "temp.txt", Revision{}},
		{"deleted at HEAD", "HEAD", "temp.txt", Revision{}},
		{"before deletion", "HEAD~1", "temp.txt", Revision{Exists: true, Content: "temp\n"}},
		{"binary", "HEAD", "bin.dat", Revision{Exists: true, Binary: true}},
		{"empty file", "HEAD", "README.md", Revision{Exists: true, Content: "hello\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.FileAt(ctx, tt.rev, tt.rel)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("FileAt(%q, %q) = %+v, want %+v", tt.rev, tt.rel, got, tt.want)
			}
		})
	}
}

func TestFileAtIndexEmptyRepo(t *testing.T) {
	r := newEmptyRepo(t)
	got, err := r.FileAt(context.Background(), ":", "a.txt")
	if err != nil || got.Exists {
		t.Fatalf("FileAt(:, a.txt) = %+v, %v; want Exists=false, nil", got, err)
	}
}

func TestFileAtHeadEmptyRepo(t *testing.T) {
	r := newEmptyRepo(t)
	got, err := r.FileAt(context.Background(), "HEAD", "a.txt")
	if err != nil || got.Exists {
		t.Fatalf("FileAt = %+v, %v; want Exists=false, nil", got, err)
	}
}

func TestFileAtNewFileOnDisk(t *testing.T) {
	r := newRepo(t)
	writeFile(t, r.Root, "new.txt", "brand new\n")
	for _, rev := range []string{"HEAD", ":"} {
		got, err := r.FileAt(context.Background(), rev, "new.txt")
		if err != nil || got.Exists {
			t.Errorf("FileAt(%q, new.txt) = %+v, %v; want Exists=false, nil", rev, got, err)
		}
	}
}

func TestFileAtLargeFileIsBinary(t *testing.T) {
	r := newRepo(t)
	commitFile(t, r.Root, "big.txt", strings.Repeat("a", maxRevisionFile+1), "big")
	got, err := r.FileAt(context.Background(), "HEAD", "big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Exists || !got.Binary || got.Content != "" {
		t.Errorf("FileAt(big) = Exists %v Binary %v len %d", got.Exists, got.Binary, len(got.Content))
	}
}

func TestIsBinary(t *testing.T) {
	tests := []struct {
		in   []byte
		want bool
	}{
		{nil, false},
		{[]byte("plain text\n"), false},
		{[]byte("a\x00b"), true},
		{append([]byte(strings.Repeat("x", 8000)), 0), false}, // NUL beyond the sniffed prefix
		{append([]byte(strings.Repeat("x", 7999)), 0), true},
	}
	for i, tt := range tests {
		if got := isBinary(tt.in); got != tt.want {
			t.Errorf("case %d: isBinary = %v, want %v", i, got, tt.want)
		}
	}
}

// committedFiles lists the paths changed by HEAD.
func committedFiles(t *testing.T, dir string) string {
	t.Helper()
	return mustGit(t, dir, "show", "--name-status", "--format=", "HEAD")
}

func TestCommitSelectedPathsOnly(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	commitFile(t, r.Root, "a.txt", "a1\n", "a")
	commitFile(t, r.Root, "b.txt", "b1\n", "b")
	writeFile(t, r.Root, "a.txt", "a2\n")
	writeFile(t, r.Root, "b.txt", "b2\n")
	mustGit(t, r.Root, "add", "b.txt")

	hash, err := r.Commit(ctx, "only a", []string{"a.txt"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := mustGit(t, r.Root, "rev-parse", "--short", "HEAD"); hash != want {
		t.Errorf("hash = %q, want short HEAD %q", hash, want)
	}
	if got := committedFiles(t, r.Root); got != "M\ta.txt" {
		t.Errorf("committed = %q, want only a.txt", got)
	}
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := statusFile(t, st, "b.txt"); got.Index != "M" {
		t.Errorf("b.txt should stay staged, got %+v", got)
	}
	if len(st.Files) != 1 {
		t.Errorf("status = %+v, want only b.txt", st.Files)
	}
}

func TestCommitUntrackedAndDeleted(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	commitFile(t, r.Root, "old.txt", "old\n", "old")
	if err := removeFile(r.Root, "old.txt"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r.Root, "dir/new.txt", "new\n")

	if _, err := r.Commit(ctx, "add and delete", []string{"dir/new.txt", "old.txt"}, false); err != nil {
		t.Fatal(err)
	}
	if got := committedFiles(t, r.Root); got != "A\tdir/new.txt\nD\told.txt" {
		t.Errorf("committed = %q", got)
	}
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Files) != 0 {
		t.Errorf("status not clean: %+v", st.Files)
	}
}

func TestCommitStagedDeleteAndRename(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	commitFile(t, r.Root, "old.txt", "old\n", "old")
	commitFile(t, r.Root, "src.txt", "src\n", "src")
	mustGit(t, r.Root, "rm", "-q", "old.txt")
	mustGit(t, r.Root, "mv", "src.txt", "dst.txt")

	if _, err := r.Commit(ctx, "rm and mv", []string{"old.txt", "src.txt", "dst.txt"}, false); err != nil {
		t.Fatal(err)
	}
	// git reports the staged move as a rename once committed.
	if got := committedFiles(t, r.Root); got != "R100\tsrc.txt\tdst.txt\nD\told.txt" {
		t.Errorf("committed = %q", got)
	}
}

func TestCommitUnstagedDelete(t *testing.T) {
	r := newRepo(t)
	commitFile(t, r.Root, "gone.txt", "x\n", "gone")
	if err := removeFile(r.Root, "gone.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(context.Background(), "rm", []string{"gone.txt"}, false); err != nil {
		t.Fatal(err)
	}
	if got := committedFiles(t, r.Root); got != "D\tgone.txt" {
		t.Errorf("committed = %q", got)
	}
}

func TestCommitFirstCommit(t *testing.T) {
	r := newEmptyRepo(t)
	writeFile(t, r.Root, "a.txt", "a")
	writeFile(t, r.Root, "b.txt", "b")
	if _, err := r.Commit(context.Background(), "root", []string{"a.txt"}, false); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, r.Root, "ls-tree", "--name-only", "HEAD"); got != "a.txt" {
		t.Errorf("root tree = %q, want a.txt", got)
	}
}

func TestCommitMessages(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	msg := "Subject line\n\nBody line one.\nBody line two."
	writeFile(t, r.Root, "a.txt", "a")
	if _, err := r.Commit(ctx, msg+"\n\n", []string{"a.txt"}, false); err != nil {
		t.Fatal(err)
	}
	got, err := r.LastCommitMessage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != msg {
		t.Errorf("LastCommitMessage = %q, want %q", got, msg)
	}
}

func TestCommitErrors(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	writeFile(t, r.Root, "a.txt", "a")
	head := mustGit(t, r.Root, "rev-parse", "HEAD")
	tests := []struct {
		name  string
		msg   string
		paths []string
		amend bool
	}{
		{"empty message", "", []string{"a.txt"}, false},
		{"blank message", " \n\t", []string{"a.txt"}, false},
		{"no paths without amend", "msg", nil, false},
		{"unknown path", "msg", []string{"missing.txt"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := r.Commit(ctx, tt.msg, tt.paths, tt.amend); err == nil {
				t.Error("expected an error")
			}
			if got := mustGit(t, r.Root, "rev-parse", "HEAD"); got != head {
				t.Error("HEAD moved after a failed commit")
			}
		})
	}
}

func TestCommitAmend(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	parent := mustGit(t, r.Root, "rev-parse", "HEAD")
	commitFile(t, r.Root, "a.txt", "a", "original message")
	writeFile(t, r.Root, "b.txt", "b")
	mustGit(t, r.Root, "add", "b.txt")

	t.Run("message only", func(t *testing.T) {
		if _, err := r.Commit(ctx, "reworded", nil, true); err != nil {
			t.Fatal(err)
		}
		if got, _ := r.LastCommitMessage(ctx); got != "reworded" {
			t.Errorf("message = %q", got)
		}
		if got := mustGit(t, r.Root, "rev-parse", "HEAD^"); got != parent {
			t.Error("amend changed the parent")
		}
		if got := committedFiles(t, r.Root); got != "A\ta.txt" {
			t.Errorf("amended commit files = %q, staged b.txt must not be included", got)
		}
		if got := mustGit(t, r.Root, "diff", "--cached", "--name-only"); got != "b.txt" {
			t.Errorf("staged = %q, want b.txt", got)
		}
	})

	t.Run("with paths", func(t *testing.T) {
		if _, err := r.Commit(ctx, "with b", []string{"b.txt"}, true); err != nil {
			t.Fatal(err)
		}
		if got := committedFiles(t, r.Root); got != "A\ta.txt\nA\tb.txt" {
			t.Errorf("amended commit files = %q", got)
		}
		if got := mustGit(t, r.Root, "rev-list", "--count", "HEAD"); got != "2" {
			t.Errorf("commit count = %s, want 2", got)
		}
	})
}

func TestLastCommitMessageEmptyRepo(t *testing.T) {
	r := newEmptyRepo(t)
	if _, err := r.LastCommitMessage(context.Background()); err == nil {
		t.Error("expected an error without commits")
	}
}
