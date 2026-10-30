package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// z joins porcelain fields with NUL terminators, like `git status -z`.
func z(fields ...string) []byte {
	return []byte(strings.Join(fields, "\x00") + "\x00")
}

const (
	oid1 = "1111111111111111111111111111111111111111"
	oid2 = "2222222222222222222222222222222222222222"
)

func TestParseStatusHeaders(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want Status
	}{
		{
			name: "branch with upstream",
			in: z("# branch.oid "+oid1, "# branch.head main", "# branch.upstream origin/main",
				"# branch.ab +1 -2"),
			want: Status{Head: oid1, Branch: "main", Upstream: "origin/main", Ahead: 1, Behind: 2},
		},
		{
			name: "initial commit",
			in:   z("# branch.oid (initial)", "# branch.head main"),
			want: Status{Branch: "main"},
		},
		{
			name: "detached",
			in:   z("# branch.oid "+oid2, "# branch.head (detached)"),
			want: Status{Head: oid2},
		},
		{
			name: "unknown header ignored",
			in:   z("# stash 3", "# branch.head dev"),
			want: Status{Branch: "dev"},
		},
		{
			name: "empty output",
			in:   nil,
			want: Status{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStatus(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			tt.want.Files = []FileChange{}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseStatus = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseStatusEntries(t *testing.T) {
	in := z(
		"# branch.oid "+oid1,
		"# branch.head main",
		"1 .M N... 100644 100644 100644 "+oid1+" "+oid1+" src/main.go",
		"1 A. N... 000000 100644 100644 "+oid1+" "+oid2+" file with spaces.txt",
		"1 D. N... 100644 000000 000000 "+oid1+" "+oid1+" gone.txt",
		"2 R. N... 100644 100644 100644 "+oid1+" "+oid1+" R100 new/name.go", "old/name.go",
		"u UU N... 100644 100644 100644 100644 "+oid1+" "+oid2+" "+oid1+" conflict.txt",
		"? dir/untracked.txt",
		"! ignored.log",
	)
	st, err := parseStatus(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileChange{
		{Path: "src/main.go", Kind: Modified, Index: ".", Worktree: "M"},
		{Path: "file with spaces.txt", Kind: Added, Index: "A", Worktree: "."},
		{Path: "gone.txt", Kind: Deleted, Index: "D", Worktree: "."},
		{Path: "new/name.go", OrigPath: "old/name.go", Kind: Renamed, Index: "R", Worktree: "."},
		{Path: "conflict.txt", Kind: Conflicted, Index: "U", Worktree: "U"},
		{Path: "dir/untracked.txt", Kind: Untracked, Index: "?", Worktree: "?"},
	}
	if !reflect.DeepEqual(st.Files, want) {
		t.Errorf("Files =\n%+v\nwant\n%+v", st.Files, want)
	}
}

func TestParseStatusMalformed(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
	}{
		{"short ordinary", z("1 .M N... 100644")},
		{"short rename", z("2 R. N... 100644 100644 100644 " + oid1 + " " + oid1 + " R100")},
		{"rename without orig field", []byte("2 R. N... 100644 100644 100644 " + oid1 + " " + oid1 + " R100 new.go")},
		{"short conflict", z("u UU N... 100644 100644 100644 100644 " + oid1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseStatus(tt.in); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestKindOf(t *testing.T) {
	tests := []struct{ x, y, want string }{
		{"R", ".", Renamed},
		{"R", "M", Renamed},
		{"C", ".", Copied},
		{"A", ".", Added},
		{"A", "M", Added},
		{"A", "D", Added},
		{"D", ".", Deleted},
		{".", "D", Deleted},
		{"M", "D", Deleted},
		{"T", ".", TypeChange},
		{".", "T", TypeChange},
		{"M", ".", Modified},
		{".", "M", Modified},
		{"M", "M", Modified},
	}
	for _, tt := range tests {
		if got := kindOf(tt.x, tt.y); got != tt.want {
			t.Errorf("kindOf(%q, %q) = %q, want %q", tt.x, tt.y, got, tt.want)
		}
	}
}

func TestStatusWorkingTree(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	commitFile(t, r.Root, "mod.txt", "v1\n", "add mod")
	commitFile(t, r.Root, "del.txt", "bye\n", "add del")
	commitFile(t, r.Root, "old.txt", "rename me\n", "add old")

	writeFile(t, r.Root, "mod.txt", "v2\n")
	writeFile(t, r.Root, "staged.txt", "new\n")
	mustGit(t, r.Root, "add", "staged.txt")
	if err := os.Remove(filepath.Join(r.Root, "del.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r.Root, "a/b/c/untracked.txt", "u\n")
	mustGit(t, r.Root, "mv", "old.txt", "renamed.txt")

	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" || st.Head != mustGit(t, r.Root, "rev-parse", "HEAD") {
		t.Errorf("Branch/Head = %q/%q", st.Branch, st.Head)
	}
	if st.Upstream != "" || st.Ahead != 0 || st.Behind != 0 || st.Operation != "" {
		t.Errorf("unexpected upstream/ab/operation: %+v", st)
	}
	tests := []FileChange{
		{Path: "mod.txt", Kind: Modified, Index: ".", Worktree: "M"},
		{Path: "staged.txt", Kind: Added, Index: "A", Worktree: "."},
		{Path: "del.txt", Kind: Deleted, Index: ".", Worktree: "D"},
		{Path: "a/b/c/untracked.txt", Kind: Untracked, Index: "?", Worktree: "?"},
		{Path: "renamed.txt", OrigPath: "old.txt", Kind: Renamed, Index: "R", Worktree: "."},
	}
	for _, want := range tests {
		if got := statusFile(t, st, want.Path); got != want {
			t.Errorf("entry = %+v, want %+v", got, want)
		}
	}
	if len(st.Files) != len(tests) {
		t.Errorf("got %d entries, want %d: %+v", len(st.Files), len(tests), st.Files)
	}
}

func TestStatusEmptyRepo(t *testing.T) {
	r := newEmptyRepo(t)
	writeFile(t, r.Root, "x.txt", "x")
	st, err := r.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Head != "" || st.Branch != "main" {
		t.Errorf("Head/Branch = %q/%q, want \"\"/main", st.Head, st.Branch)
	}
	if got := statusFile(t, st, "x.txt"); got.Kind != Untracked {
		t.Errorf("kind = %q", got.Kind)
	}
}

func TestStatusDetached(t *testing.T) {
	r := newRepo(t)
	mustGit(t, r.Root, "switch", "-q", "--detach")
	st, err := r.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "" || st.Head == "" {
		t.Errorf("Branch/Head = %q/%q, want detached with a head", st.Branch, st.Head)
	}
}

func TestStatusAheadBehind(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	bare := newBareRemote(t, r)
	mustGit(t, r.Root, "push", "-q", "-u", "origin", "main")

	other := cloneRepo(t, bare)
	commitFile(t, other.Root, "o1.txt", "1", "other 1")
	commitFile(t, other.Root, "o2.txt", "2", "other 2")
	mustGit(t, other.Root, "push", "-q")

	commitFile(t, r.Root, "local.txt", "l", "local")
	mustGit(t, r.Root, "fetch", "-q")

	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Upstream != "origin/main" || st.Ahead != 1 || st.Behind != 2 {
		t.Errorf("Upstream/Ahead/Behind = %q/%d/%d, want origin/main/1/2", st.Upstream, st.Ahead, st.Behind)
	}
}

func TestStatusMergeConflict(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	commitFile(t, r.Root, "f.txt", "base\n", "base")
	mustGit(t, r.Root, "switch", "-q", "-c", "feat")
	commitFile(t, r.Root, "f.txt", "feature\n", "feat change")
	mustGit(t, r.Root, "switch", "-q", "main")
	commitFile(t, r.Root, "f.txt", "main\n", "main change")
	if out, err := tryGit(r.Root, "merge", "feat"); err == nil {
		t.Fatalf("merge unexpectedly succeeded: %s", out)
	}

	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Operation != "merging" {
		t.Errorf("Operation = %q, want merging", st.Operation)
	}
	want := FileChange{Path: "f.txt", Kind: Conflicted, Index: "U", Worktree: "U"}
	if got := statusFile(t, st, "f.txt"); got != want {
		t.Errorf("entry = %+v, want %+v", got, want)
	}
}

func TestOperationMarkers(t *testing.T) {
	tests := []struct {
		marker string
		dir    bool
		want   string
	}{
		{"rebase-merge", true, "rebasing"},
		{"rebase-apply", true, "rebasing"},
		{"MERGE_HEAD", false, "merging"},
		{"CHERRY_PICK_HEAD", false, "cherry-picking"},
		{"REVERT_HEAD", false, "reverting"},
	}
	for _, tt := range tests {
		t.Run(tt.marker, func(t *testing.T) {
			r := &Repo{GitDir: t.TempDir()}
			if got := r.operation(); got != "" {
				t.Fatalf("operation() without markers = %q", got)
			}
			p := filepath.Join(r.GitDir, tt.marker)
			var err error
			if tt.dir {
				err = os.Mkdir(p, 0o755)
			} else {
				err = os.WriteFile(p, []byte(oid1+"\n"), 0o644)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := r.operation(); got != tt.want {
				t.Errorf("operation() = %q, want %q", got, tt.want)
			}
		})
	}
}
