package git

import (
	"context"
	"testing"
)

func TestRollback(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	commitFile(t, r.Root, "mod.txt", "orig mod\n", "mod")
	commitFile(t, r.Root, "staged.txt", "orig staged\n", "staged")
	commitFile(t, r.Root, "deleted.txt", "orig deleted\n", "deleted")
	commitFile(t, r.Root, "keep.txt", "orig keep\n", "keep")

	writeFile(t, r.Root, "mod.txt", "changed\n")
	writeFile(t, r.Root, "staged.txt", "changed staged\n")
	mustGit(t, r.Root, "add", "staged.txt")
	if err := removeFile(r.Root, "deleted.txt"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r.Root, "added.txt", "new\n")
	mustGit(t, r.Root, "add", "added.txt")
	writeFile(t, r.Root, "dir/untracked.txt", "junk\n")
	writeFile(t, r.Root, "keep.txt", "keep my change\n")

	paths := []string{"mod.txt", "staged.txt", "deleted.txt", "added.txt", "dir/untracked.txt"}
	if err := r.Rollback(ctx, paths); err != nil {
		t.Fatal(err)
	}

	for rel, want := range map[string]string{
		"mod.txt":     "orig mod\n",
		"staged.txt":  "orig staged\n",
		"deleted.txt": "orig deleted\n",
		"keep.txt":    "keep my change\n",
	} {
		if got := readFile(t, r.Root, rel); got != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
	for _, rel := range []string{"added.txt", "dir/untracked.txt"} {
		if fileExists(r.Root, rel) {
			t.Errorf("%s should be deleted from disk", rel)
		}
	}
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Files) != 1 || st.Files[0].Path != "keep.txt" {
		t.Errorf("status = %+v, want only the untouched keep.txt", st.Files)
	}
}

func TestRollbackSingleCases(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, r *Repo)
		path   string
		exists bool
		want   string
	}{
		{
			name:   "modified tracked file restored",
			setup:  func(t *testing.T, r *Repo) { writeFile(t, r.Root, "README.md", "edited\n") },
			path:   "README.md",
			exists: true,
			want:   "hello\n",
		},
		{
			name: "staged change unstaged and restored",
			setup: func(t *testing.T, r *Repo) {
				writeFile(t, r.Root, "README.md", "staged\n")
				mustGit(t, r.Root, "add", "README.md")
			},
			path:   "README.md",
			exists: true,
			want:   "hello\n",
		},
		{
			name: "staged new file removed from index and disk",
			setup: func(t *testing.T, r *Repo) {
				writeFile(t, r.Root, "new.txt", "new\n")
				mustGit(t, r.Root, "add", "new.txt")
			},
			path: "new.txt",
		},
		{
			name:  "untracked file deleted",
			setup: func(t *testing.T, r *Repo) { writeFile(t, r.Root, "u.txt", "u\n") },
			path:  "u.txt",
		},
		{
			name:  "already missing untracked path is fine",
			setup: func(t *testing.T, r *Repo) {},
			path:  "ghost.txt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			r := newRepo(t)
			tt.setup(t, r)
			if err := r.Rollback(ctx, []string{tt.path}); err != nil {
				t.Fatal(err)
			}
			if got := fileExists(r.Root, tt.path); got != tt.exists {
				t.Fatalf("exists = %v, want %v", got, tt.exists)
			}
			if tt.exists {
				if got := readFile(t, r.Root, tt.path); got != tt.want {
					t.Errorf("content = %q, want %q", got, tt.want)
				}
			}
			st, err := r.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(st.Files) != 0 {
				t.Errorf("status not clean: %+v", st.Files)
			}
		})
	}
}

func TestRollbackNoCommits(t *testing.T) {
	ctx := context.Background()
	r := newEmptyRepo(t)
	writeFile(t, r.Root, "staged.txt", "s")
	mustGit(t, r.Root, "add", "staged.txt")
	writeFile(t, r.Root, "untracked.txt", "u")

	if err := r.Rollback(ctx, []string{"staged.txt", "untracked.txt"}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"staged.txt", "untracked.txt"} {
		if fileExists(r.Root, rel) {
			t.Errorf("%s should be deleted", rel)
		}
	}
	if got := mustGit(t, r.Root, "ls-files"); got != "" {
		t.Errorf("index not empty: %q", got)
	}
}

func TestRollbackNoPaths(t *testing.T) {
	r := newRepo(t)
	writeFile(t, r.Root, "README.md", "edited\n")
	if err := r.Rollback(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, r.Root, "README.md"); got != "edited\n" {
		t.Errorf("README.md changed by an empty rollback: %q", got)
	}
}
