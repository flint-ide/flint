package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// logHashes returns the hashes of commits.
func logHashes(cs []Commit) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Hash
	}
	return out
}

func TestLog(t *testing.T) {
	ctx := context.Background()
	r := newEmptyRepo(t)
	c1 := commitFile(t, r.Root, "a.txt", "a", "first")
	c2 := commitFile(t, r.Root, "b/b.txt", "b", "second")
	c3 := commitFile(t, r.Root, "a.txt", "a2", "third")
	mustGit(t, r.Root, "branch", "side", c3)

	all, err := r.Log(ctx, LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := logHashes(all), []string{c3, c2, c1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	head := all[0]
	if head.Subject != "third" || head.Author != "Test User" || head.Email != "test@example.com" || head.Time == 0 {
		t.Errorf("head commit = %+v", head)
	}
	if !reflect.DeepEqual(head.Parents, []string{c2}) || len(all[2].Parents) != 0 {
		t.Errorf("parents = %v / root %v", head.Parents, all[2].Parents)
	}
	if !strings.Contains(head.Refs, "HEAD -> main") || !strings.Contains(head.Refs, "side") {
		t.Errorf("Refs = %q, want HEAD -> main and side", head.Refs)
	}
	if all[1].Refs != "" {
		t.Errorf("undecorated commit Refs = %q", all[1].Refs)
	}

	tests := []struct {
		name string
		opts LogOptions
		want []string
	}{
		{"limit", LogOptions{Limit: 2}, []string{c3, c2}},
		{"skip", LogOptions{Skip: 1}, []string{c2, c1}},
		{"skip and limit", LogOptions{Skip: 1, Limit: 1}, []string{c2}},
		{"skip past end", LogOptions{Skip: 10}, []string{}},
		{"path file", LogOptions{Path: "a.txt"}, []string{c3, c1}},
		{"path dir", LogOptions{Path: "b"}, []string{c2}},
		{"path missing", LogOptions{Path: "zzz"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Log(ctx, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(logHashes(got), tt.want) {
				t.Errorf("Log(%+v) = %v, want %v", tt.opts, logHashes(got), tt.want)
			}
		})
	}
}

func TestLogAllAndMerge(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	mustGit(t, r.Root, "switch", "-q", "-c", "side")
	side := commitFile(t, r.Root, "side.txt", "s", "side work")
	mustGit(t, r.Root, "switch", "-q", "main")
	mainTip := commitFile(t, r.Root, "main.txt", "m", "main work")

	only, err := r.Log(ctx, LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range only {
		if c.Hash == side {
			t.Error("side commit listed without All")
		}
	}
	all, err := r.Log(ctx, LogOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(logHashes(all), side) {
		t.Error("side commit missing with All")
	}

	mustGit(t, r.Root, "merge", "-q", "--no-edit", "side")
	merged, err := r.Log(ctx, LogOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{mainTip, side}; !reflect.DeepEqual(merged[0].Parents, want) {
		t.Errorf("merge parents = %v, want %v", merged[0].Parents, want)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestLogEmptyRepo(t *testing.T) {
	r := newEmptyRepo(t)
	got, err := r.Log(context.Background(), LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("Log = %#v, want empty non-nil slice", got)
	}
}

func TestCommitFiles(t *testing.T) {
	ctx := context.Background()
	r := newEmptyRepo(t)
	root := commitAll(t, r.Root, "empty root")
	writeFile(t, r.Root, "keep.txt", "keep\n")
	writeFile(t, r.Root, "mod.txt", "v1\n")
	writeFile(t, r.Root, "del.txt", "bye\n")
	writeFile(t, r.Root, "old.txt", strings.Repeat("rename detection needs content\n", 5))
	adds := commitAll(t, r.Root, "add files")

	writeFile(t, r.Root, "mod.txt", "v2\n")
	writeFile(t, r.Root, "new.txt", "new\n")
	mustGit(t, r.Root, "rm", "-q", "del.txt")
	if err := os.Mkdir(filepath.Join(r.Root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, r.Root, "mv", "old.txt", "dir/renamed.txt")
	changes := commitAll(t, r.Root, "changes")

	tests := []struct {
		name string
		hash string
		want []FileChange
	}{
		{"empty root commit", root, []FileChange{}},
		{"adds", adds, []FileChange{
			{Path: "del.txt", Kind: Added, Index: "A", Worktree: "."},
			{Path: "keep.txt", Kind: Added, Index: "A", Worktree: "."},
			{Path: "mod.txt", Kind: Added, Index: "A", Worktree: "."},
			{Path: "old.txt", Kind: Added, Index: "A", Worktree: "."},
		}},
		{"modify delete rename", changes, []FileChange{
			{Path: "del.txt", Kind: Deleted, Index: "D", Worktree: "."},
			{Path: "dir/renamed.txt", OrigPath: "old.txt", Kind: Renamed, Index: "R", Worktree: "."},
			{Path: "mod.txt", Kind: Modified, Index: "M", Worktree: "."},
			{Path: "new.txt", Kind: Added, Index: "A", Worktree: "."},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.CommitFiles(ctx, tt.hash)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CommitFiles =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestCommitFilesRootWithContent(t *testing.T) {
	r := newEmptyRepo(t)
	root := commitFile(t, r.Root, "first.txt", "1", "root")
	got, err := r.CommitFiles(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileChange{{Path: "first.txt", Kind: Added, Index: "A", Worktree: "."}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CommitFiles(root) = %+v, want %+v", got, want)
	}
}

func TestCommitFilesUnknownHash(t *testing.T) {
	r := newRepo(t)
	if _, err := r.CommitFiles(context.Background(), "deadbeef"); err == nil {
		t.Error("expected an error for an unknown commit")
	}
}

func TestCommitFilesMergeCommit(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	mustGit(t, r.Root, "switch", "-q", "-c", "side")
	commitFile(t, r.Root, "side.txt", "s", "side")
	mustGit(t, r.Root, "switch", "-q", "main")
	commitFile(t, r.Root, "main.txt", "m", "main")
	mustGit(t, r.Root, "merge", "-q", "--no-edit", "side")

	got, err := r.CommitFiles(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want := []FileChange{{Path: "side.txt", Kind: Added, Index: "A", Worktree: "."}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CommitFiles(merge) = %+v, want %+v", got, want)
	}
}
