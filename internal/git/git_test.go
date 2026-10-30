package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOpen(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	mainGit := filepath.Join(r.Root, ".git")

	t.Run("root", func(t *testing.T) {
		got := openRepo(t, r.Root)
		samePath(t, "Root", got.Root, r.Root)
		samePath(t, "GitDir", got.GitDir, mainGit)
		samePath(t, "CommonDir", got.CommonDir, mainGit)
	})

	t.Run("subdirectory", func(t *testing.T) {
		sub := filepath.Join(r.Root, "a", "b")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		got := openRepo(t, sub)
		samePath(t, "Root", got.Root, r.Root)
		samePath(t, "GitDir", got.GitDir, mainGit)
		samePath(t, "CommonDir", got.CommonDir, mainGit)
	})

	t.Run("not a repository", func(t *testing.T) {
		_, err := Open(ctx, t.TempDir())
		if !errors.Is(err, ErrNotRepository) {
			t.Fatalf("err = %v, want ErrNotRepository", err)
		}
	})

	t.Run("missing directory", func(t *testing.T) {
		_, err := Open(ctx, filepath.Join(t.TempDir(), "nope"))
		if err == nil {
			t.Fatal("expected an error for a missing directory")
		}
	})

	t.Run("linked worktree", func(t *testing.T) {
		wtPath := filepath.Join(t.TempDir(), "wt")
		mustGit(t, r.Root, "worktree", "add", "-q", "-b", "wt-branch", wtPath)
		sub := filepath.Join(wtPath, "deep")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, dir := range []string{wtPath, sub} {
			got := openRepo(t, dir)
			samePath(t, "Root", got.Root, wtPath)
			samePath(t, "CommonDir", got.CommonDir, mainGit)
			wantGitDir := filepath.Join(realPath(t, mainGit), "worktrees", "wt")
			if realPath(t, got.GitDir) != wantGitDir {
				t.Errorf("GitDir = %q, want %q", got.GitDir, wantGitDir)
			}
		}
	})
}

func TestRel(t *testing.T) {
	r := &Repo{Root: filepath.FromSlash("/work/repo")}
	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{"file in root", "/work/repo/a.txt", "a.txt", false},
		{"nested", "/work/repo/a/b/c.go", "a/b/c.go", false},
		{"root itself", "/work/repo", ".", false},
		{"sibling", "/work/other/a.txt", "", true},
		{"parent", "/work", "", true},
		{"prefix lookalike", "/work/repo2/a.txt", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Rel(filepath.FromSlash(tt.path))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Rel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelDotDotPrefixedName(t *testing.T) {
	r := &Repo{Root: filepath.FromSlash("/work/repo")}
	got, err := r.Rel(filepath.FromSlash("/work/repo/..env"))
	if err != nil || got != "..env" {
		t.Fatalf("Rel = %q, %v; want \"..env\", nil", got, err)
	}
}

func TestSplitNUL(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"\x00", nil},
		{"a", []string{"a"}},
		{"a\x00", []string{"a"}},
		{"a\x00b\x00", []string{"a", "b"}},
		{"a\x00\x00b\x00\x00", []string{"a", "", "b", ""}},
	}
	for _, tt := range tests {
		if got := splitNUL([]byte(tt.in)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitNUL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestErrorCarriesStderr(t *testing.T) {
	r := newRepo(t)
	_, err := r.git(context.Background(), "rev-parse", "--verify", "no-such-ref")
	var gerr *Error
	if !errors.As(err, &gerr) {
		t.Fatalf("err = %T %v, want *Error", err, err)
	}
	if gerr.Msg == "" || gerr.Error() != gerr.Msg {
		t.Errorf("Error() = %q, Msg = %q", gerr.Error(), gerr.Msg)
	}
	if len(gerr.Args) == 0 || gerr.Args[0] != "rev-parse" {
		t.Errorf("Args = %q", gerr.Args)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("Unwrap chain lacks *exec.ExitError: %v", err)
	}
}

func TestGitInputFeedsStdin(t *testing.T) {
	r := newRepo(t)
	out, err := r.gitInput(context.Background(), []byte("blob body"), "hash-object", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r.Root, "body.txt", "blob body")
	want := mustGit(t, r.Root, "hash-object", "body.txt")
	if got := string(out); got != want+"\n" {
		t.Errorf("hash of stdin = %q, want %q", got, want)
	}
}
