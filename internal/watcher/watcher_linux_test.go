//go:build linux

package watcher

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
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

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// watched lists the directories with a watch.
func (w *Watcher) watched() map[string]bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string]bool{}
	for _, dir := range w.dirs {
		out[dir] = true
	}
	return out
}

func TestWatchFollowsNewDirectories(t *testing.T) {
	root := t.TempDir()
	c := &collector{}
	w, err := Watch([]string{root}, c.flush, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// A whole tree appears at once (mkdir -p, git checkout): files written
	// before inotify watches the new directory must still be reported.
	deep := filepath.Join(root, "a", "b")
	mustMkdir(t, deep)
	mustWrite(t, filepath.Join(deep, "early.go"))
	waitFor(t, c, filepath.Join(root, "a"))

	// Later writes in the new directory come from its own watch.
	time.Sleep(100 * time.Millisecond)
	late := filepath.Join(deep, "late.go")
	mustWrite(t, late)
	waitFor(t, c, late)
}

func TestWatchSkipsNoise(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"node_modules/pkg", ".git/objects/ab", ".git/refs/heads", "dist/js", "src"} {
		mustMkdir(t, filepath.Join(root, dir))
	}
	mustWrite(t, filepath.Join(root, ".git", "HEAD"))
	skip := func(dir string) bool { return filepath.Base(dir) == "dist" }
	w, err := Watch([]string{root}, func([]string) {}, Options{Skip: skip})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	watched := w.watched()
	for dir, want := range map[string]bool{
		root:                        true,
		filepath.Join(root, "src"):  true,
		filepath.Join(root, ".git"): true,
		filepath.Join(root, ".git", "refs", "heads"): true,
		filepath.Join(root, ".git", "objects"):       false, // always: git's object store
		filepath.Join(root, "dist"):                  false, // Options.Skip
		filepath.Join(root, "node_modules"):          true,  // a custom Skip replaces the default list
	} {
		if watched[dir] != want {
			t.Errorf("watching %s = %v, want %v", dir, watched[dir], want)
		}
	}

	// A skipped folder that appears later isn't walked either.
	mustMkdir(t, filepath.Join(root, "pkg", "dist", "deep"))
	time.Sleep(300 * time.Millisecond)
	if w.watched()[filepath.Join(root, "pkg", "dist")] {
		t.Error("a new skipped folder was watched")
	}
}

func TestWatchMissingRootFails(t *testing.T) {
	if _, err := Watch([]string{filepath.Join(t.TempDir(), "gone")}, func([]string) {}, Options{}); err == nil {
		t.Fatal("expected an error for a missing root")
	}
}

// A root that is a symlink (~/src → /data/src) is watched where it points,
// and changes are reported under the path the caller gave.
func TestWatchSymlinkedRoot(t *testing.T) {
	target := t.TempDir()
	mustMkdir(t, filepath.Join(target, "pkg"))
	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	c := &collector{}
	w, err := Watch([]string{link}, c.flush, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if !w.watched()[filepath.Join(target, "pkg")] {
		t.Fatalf("subfolders of a symlinked root aren't watched: %v", w.watched())
	}
	mustWrite(t, filepath.Join(target, "pkg", "a.go"))
	waitFor(t, c, filepath.Join(link, "pkg", "a.go"))
}

// A folder moved out of the tree (Flint's own Trash) must not keep its
// watches: they leak and report phantom paths under the project.
func TestWatchDropsFoldersMovedOut(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	sub := filepath.Join(root, "old")
	mustMkdir(t, filepath.Join(sub, "deep"))
	c := &collector{}
	w, err := Watch([]string{root}, c.flush, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	moved := filepath.Join(outside, "old")
	if err := os.Rename(sub, moved); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, sub)
	for dir := range w.watched() {
		if under(dir, sub) {
			t.Errorf("still watching %s after it moved out", dir)
		}
	}
	mustWrite(t, filepath.Join(moved, "deep", "ghost.go"))
	time.Sleep(quietPeriod + 300*time.Millisecond)
	for _, p := range c.all() {
		if strings.HasSuffix(p, "ghost.go") {
			t.Errorf("a write outside the tree was reported as %s", p)
		}
	}

	// Moved back in (renamed within the tree, too): watched again.
	back := filepath.Join(root, "new")
	if err := os.Rename(moved, back); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, back)
	time.Sleep(100 * time.Millisecond)
	late := filepath.Join(back, "deep", "late.go")
	mustWrite(t, late)
	waitFor(t, c, late)
}

// overflowEvent is the record the kernel queues when it dropped events.
func overflowEvent() []byte {
	buf := make([]byte, unix.SizeofInotifyEvent)
	binary.NativeEndian.PutUint32(buf[0:4], ^uint32(0)) // wd -1
	binary.NativeEndian.PutUint32(buf[4:8], unix.IN_Q_OVERFLOW)
	return buf
}

// After an overflow, directories created meanwhile get watched and gone ones
// forgotten, and the caller is told to reload (Options.Lost).
func TestWatchRescansAfterOverflow(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "kept"))
	gone := filepath.Join(root, "gone")
	mustMkdir(t, gone)
	w, err := Watch([]string{root}, func([]string) {}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	paths, lost := w.parse(overflowEvent())
	if !lost || len(paths) != 0 {
		t.Fatalf("parse(overflow) = %v, %v; want lost", paths, lost)
	}

	// Simulate what dropped events hide: a folder appeared unseen, another
	// vanished and its IN_IGNORED was dropped too.
	w.mu.Lock()
	for wd, dir := range w.dirs {
		if dir == filepath.Join(root, "kept") {
			delete(w.dirs, wd)
		}
	}
	w.mu.Unlock()
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	w.rescan()
	watched := w.watched()
	if !watched[filepath.Join(root, "kept")] || watched[gone] {
		t.Errorf("after rescan watching %v", watched)
	}
}

// Running out of watches is reported once, not silently and not per folder.
func TestWatchReportsTheLimitOnce(t *testing.T) {
	var calls atomic.Int32
	w := &Watcher{opts: Options{Limit: func(err error) {
		if !strings.Contains(err.Error(), "fs.inotify.max_user_watches") {
			t.Errorf("no sysctl hint: %v", err)
		}
		calls.Add(1)
	}}}
	w.reportLimit("/a")
	w.reportLimit("/b")
	if calls.Load() != 1 {
		t.Errorf("Limit called %d times", calls.Load())
	}
}

// After Close the descriptor number may belong to another file: nothing may
// add or remove watches through it.
func TestWatchClosedRefusesWatches(t *testing.T) {
	root := t.TempDir()
	w, err := Watch([]string{root}, func([]string) {}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	if err := w.add(root); !errors.Is(err, os.ErrClosed) {
		t.Errorf("add after Close = %v, want os.ErrClosed", err)
	}
	w.removeTree(root) // must not touch the fd
	w.rescan()
}
