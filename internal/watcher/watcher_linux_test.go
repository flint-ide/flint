//go:build linux

package watcher

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// waitFor polls the collector until it saw path.
func waitFor(t *testing.T, c *collector, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if slices.Contains(c.all(), path) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("change to %s not reported; got %v", path, c.all())
}

func TestWatchFollowsNewDirectories(t *testing.T) {
	root := t.TempDir()
	c := &collector{}
	w, err := Watch([]string{root}, c.flush)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// A whole tree appears at once (mkdir -p, git checkout): files written
	// before inotify watches the new directory must still be reported.
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	early := filepath.Join(deep, "early.go")
	if err := os.WriteFile(early, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, filepath.Join(root, "a"))

	// Later writes in the new directory come from its own watch.
	time.Sleep(100 * time.Millisecond)
	late := filepath.Join(deep, "late.go")
	if err := os.WriteFile(late, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, late)
}

func TestWatchSkipsNoise(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"node_modules/pkg", ".git/objects/ab", ".git/refs/heads"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := Watch([]string{root}, func([]string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.mu.Lock()
	watched := map[string]bool{}
	for _, dir := range w.dirs {
		watched[dir] = true
	}
	w.mu.Unlock()
	for dir, want := range map[string]bool{
		root:                        true,
		filepath.Join(root, ".git"): true,
		filepath.Join(root, ".git", "refs", "heads"): true,
		filepath.Join(root, ".git", "objects"):       false,
		filepath.Join(root, "node_modules"):          false,
	} {
		if watched[dir] != want {
			t.Errorf("watching %s = %v, want %v", dir, watched[dir], want)
		}
	}
}

func TestWatchMissingRootFails(t *testing.T) {
	if _, err := Watch([]string{filepath.Join(t.TempDir(), "gone")}, func([]string) {}); err == nil {
		t.Fatal("expected an error for a missing root")
	}
}
