package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"flint/internal/ripgrep"
)

// newTree creates a small project and returns its root.
func newTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{
		"main.go",
		"pkg/util.go",
		".hidden/conf.yml",
		".env",
		".git/config",
		"node_modules/dep/index.js",
		"dist/bundle.js",
		"build/out.log",
		"secret.key",
	} {
		mustWrite(t, filepath.Join(root, f), []byte("x"), 0o644)
	}
	return root
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

func assertFiles(t *testing.T, got, want []string) {
	t.Helper()
	if g, w := sorted(got), sorted(want); !slices.Equal(g, w) {
		t.Errorf("files = %v, want %v", g, w)
	}
}

func TestListWithWalk(t *testing.T) {
	root := newTree(t)
	got, err := listWithWalk(context.Background(), root)
	if err != nil {
		t.Fatalf("listWithWalk() error = %v", err)
	}
	// .git, node_modules and dist are skipped; hidden files are kept; no .gitignore support.
	assertFiles(t, got, []string{
		"main.go", "pkg/util.go", ".hidden/conf.yml", ".env", "build/out.log", "secret.key",
	})
}

func TestListWithWalkCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := listWithWalk(ctx, newTree(t)); err == nil {
		t.Error("error = nil, want context error")
	}
}

func TestListWithWalkEmptyDir(t *testing.T) {
	got, err := listWithWalk(context.Background(), t.TempDir())
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want empty, nil", got, err)
	}
}

func requireRipgrep(t *testing.T) string {
	t.Helper()
	rg := ripgrep.Path()
	if rg == "" {
		t.Skip("rg not installed")
	}
	return rg
}

func TestListWithRipgrepIgnoreFile(t *testing.T) {
	rg := requireRipgrep(t)
	root := newTree(t)
	// .ignore is honoured by rg even outside a git repository.
	mustWrite(t, filepath.Join(root, ".ignore"), []byte("build/\n*.key\n"), 0o644)

	got, err := listWithRipgrep(context.Background(), rg, root)
	if err != nil {
		t.Fatalf("listWithRipgrep() error = %v", err)
	}
	// rg has no built-in node_modules/dist skipping, but excludes .git and ignored paths.
	assertFiles(t, got, []string{
		"main.go", "pkg/util.go", ".hidden/conf.yml", ".env", ".ignore",
		"node_modules/dep/index.js", "dist/bundle.js",
	})
}

func TestListWithRipgrepGitignore(t *testing.T) {
	requireRipgrep(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	if out, err := exec.Command(git, "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	mustWrite(t, filepath.Join(root, ".gitignore"), []byte("node_modules/\n*.log\n"), 0o644)
	mustWrite(t, filepath.Join(root, "a.go"), []byte("x"), 0o644)
	mustWrite(t, filepath.Join(root, "debug.log"), []byte("x"), 0o644)
	mustWrite(t, filepath.Join(root, "node_modules/x/y.js"), []byte("x"), 0o644)

	got, err := ListFiles(context.Background(), root)
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	assertFiles(t, got, []string{".gitignore", "a.go"})
}

func TestListWithRipgrepEmptyDir(t *testing.T) {
	rg := requireRipgrep(t)
	got, err := listWithRipgrep(context.Background(), rg, t.TempDir())
	if err != nil {
		t.Fatalf("rg exit 1 on empty dir must not be an error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestListWithRipgrepBadBinary(t *testing.T) {
	_, err := listWithRipgrep(context.Background(), filepath.Join(t.TempDir(), "no-rg"), t.TempDir())
	if err == nil {
		t.Error("error = nil, want start error")
	}
}

func TestListFilesRejectsRelative(t *testing.T) {
	if _, err := ListFiles(context.Background(), "relative/dir"); err == nil {
		t.Error("error = nil, want error")
	}
}

func TestSkipDir(t *testing.T) {
	tests := map[string]bool{
		".git": true, "node_modules": true, ".venv": true, "target": true,
		"src": false, ".github": false, "": false,
	}
	for name, want := range tests {
		if got := SkipDir(name); got != want {
			t.Errorf("SkipDir(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestListFilesDoesNotFollowIntoGit(t *testing.T) {
	requireRipgrep(t)
	root := newTree(t)
	got, err := ListFiles(context.Background(), root)
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	for _, f := range got {
		if f == ".git/config" || filepath.IsAbs(f) {
			t.Errorf("unexpected entry %q", f)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".git/config")); err != nil {
		t.Fatal(err)
	}
}
