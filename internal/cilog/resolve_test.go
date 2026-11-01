package cilog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A log can name any number of packages and paths: the resolver must stay
// bounded (the reviewer's 20k FAIL lines × 500 refs took 15 s before).
func TestResolverIsBounded(t *testing.T) {
	root := repo(t, "go.mod", "p3/f3.go")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var log []string
	for i := range 20000 {
		log = append(log, fmt.Sprintf("FAIL example.com/m/p%d", i))
	}
	var lines []string
	for i := range 500 {
		lines = append(lines, fmt.Sprintf("x f%d.go:1", i))
	}
	start := time.Now()
	r := NewResolver(context.Background(), root, log)
	defer r.Close()
	if len(r.pkgDirs) != 1 || r.pkgDirs[0] != "p3" {
		t.Errorf("pkgDirs = %v, want only the existing p3", r.pkgDirs)
	}
	found := 0
	for _, ref := range Refs(lines, 500) {
		if _, ok := r.Resolve(ref.File); ok {
			found++
		}
	}
	if found != 1 {
		t.Errorf("found %d, want f3.go", found)
	}
	if r.stats > MaxStats {
		t.Errorf("%d stats, over the budget %d", r.stats, MaxStats)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestResolverPkgDirsCapped(t *testing.T) {
	var files []string
	var log []string
	for i := range MaxPkgDirs + 10 {
		files = append(files, fmt.Sprintf("p%d/x.go", i))
		log = append(log, fmt.Sprintf("FAIL\tm/p%d", i))
	}
	root := repo(t, append(files, "go.mod")...)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(context.Background(), root, log)
	defer r.Close()
	if len(r.pkgDirs) != MaxPkgDirs {
		t.Errorf("pkgDirs = %d, want %d", len(r.pkgDirs), MaxPkgDirs)
	}
}

func TestResolverStatBudget(t *testing.T) {
	root := repo(t, "a.go")
	r := NewResolver(context.Background(), root, nil)
	defer r.Close()
	for i := range MaxStats + 10 {
		r.Resolve(fmt.Sprintf("missing%d.go", i))
	}
	if r.stats != MaxStats {
		t.Errorf("stats = %d, want the budget %d", r.stats, MaxStats)
	}
	if _, ok := r.Resolve("a.go"); ok {
		t.Error("resolved after the budget ran out")
	}
}

func TestResolverStopsWithContext(t *testing.T) {
	root := repo(t, "a.go")
	ctx, cancel := context.WithCancel(context.Background())
	r := NewResolver(ctx, root, nil)
	defer r.Close()
	cancel()
	if _, ok := r.Resolve("a.go"); ok || r.stats != 0 {
		t.Errorf("resolved after cancel (%d stats)", r.stats)
	}
}

func TestResolverStaysInsideTheFolder(t *testing.T) {
	root := repo(t, "a/x.go")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "a", "secret.txt")); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(context.Background(), root, nil)
	defer r.Close()
	for _, p := range []string{"link/secret.txt", "a/secret.txt", "../" + filepath.Base(outside) + "/secret.txt", "/x/link/secret.txt"} {
		if got, ok := r.Resolve(p); ok {
			t.Errorf("Resolve(%q) = %q: followed a link out of the folder", p, got)
		}
	}
	if got, _ := r.Resolve(root + "/a/../a/x.go"); got != "a/x.go" || strings.Contains(got, "..") {
		t.Errorf("Resolve(abs with ..) = %q", got)
	}
}
