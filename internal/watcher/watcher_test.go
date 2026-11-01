package watcher

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestIgnored(t *testing.T) {
	tests := map[string]bool{
		"/p/src/main.go":              false,
		"/p/.git/index":               true,
		"/p/web/node_modules/x/a.js":  true,
		"/p/.DS_Store":                true,
		"/p/.github/workflows/ci.yml": false,
	}
	for path, want := range tests {
		if got := Ignored(path); got != want {
			t.Errorf("Ignored(%q) = %v, want %v", path, got, want)
		}
	}
}

type collector struct {
	mu      sync.Mutex
	batches [][]string
}

func (c *collector) flush(paths []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batches = append(c.batches, paths)
}

func (c *collector) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, b := range c.batches {
		out = append(out, b...)
	}
	return out
}

func TestDebouncerBatchesAndDedupes(t *testing.T) {
	c := &collector{}
	d := newDebouncer(c.flush)
	d.add("/p/b", "/p/a")
	d.add("/p/a")
	time.Sleep(quietPeriod + 100*time.Millisecond)

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.batches) != 1 {
		t.Fatalf("batches = %v, want one", c.batches)
	}
	if !slices.Equal(c.batches[0], []string{"/p/a", "/p/b"}) {
		t.Errorf("batch = %v", c.batches[0])
	}
}

func TestDebouncerMaxDelay(t *testing.T) {
	c := &collector{}
	d := newDebouncer(c.flush)
	stop := time.After(maxDelay + 200*time.Millisecond)
	tick := time.NewTicker(quietPeriod / 3)
	defer tick.Stop()
loop:
	for {
		select {
		case <-stop:
			break loop
		case <-tick.C:
			d.add("/p/busy") // constant churn must not starve the UI
		}
	}
	d.close()
	if len(c.all()) == 0 {
		t.Error("expected a flush despite continuous events")
	}
}

func TestDebouncerClosedDropsEvents(t *testing.T) {
	c := &collector{}
	d := newDebouncer(c.flush)
	d.close()
	d.add("/p/a")
	time.Sleep(quietPeriod + 50*time.Millisecond)
	if got := c.all(); len(got) != 0 {
		t.Errorf("closed debouncer flushed %v", got)
	}
}

func TestWatchReportsFileChanges(t *testing.T) {
	root := t.TempDir()
	c := &collector{}
	w, err := Watch([]string{root}, c.flush, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	time.Sleep(200 * time.Millisecond) // let the stream attach

	target := filepath.Join(root, "agent-wrote-this.go")
	if err := os.WriteFile(target, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if slices.Contains(c.all(), target) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("change to %s not reported; got %v", target, c.all())
}

func TestClassify(t *testing.T) {
	gitDirs := []string{"/main/.git", "/main/.git/worktrees/feat"}
	files, gitChanged := Classify([]string{
		"/wt/src/a.go",
		"/wt/node_modules/x.js",
		"/main/.git/worktrees/feat/HEAD",
		"/main/.git/objects/ab/cdef",
	}, gitDirs)
	if !slices.Equal(files, []string{"/wt/src/a.go"}) {
		t.Errorf("files = %v", files)
	}
	if !gitChanged {
		t.Error("worktree HEAD change must count as a git change")
	}

	_, gitChanged = Classify([]string{"/main/.git/objects/aa/bb", "/main/.git/index.lock", "/main/.git/logs/HEAD"}, gitDirs)
	if gitChanged {
		t.Error("object/log/lock churn must not trigger a git refresh")
	}

	_, gitChanged = Classify([]string{"/other/.git/refs/heads/main"}, nil)
	if !gitChanged {
		t.Error("paths inside a .git directory are git changes even without explicit dirs")
	}
}

func TestRemap(t *testing.T) {
	mapping := map[string]string{"/private/var/x": "/var/x", "/Users/me/repo/.git": "/Users/me/repo/.git"}
	tests := map[string]string{
		"private/var/x/a.go":       "/var/x/a.go",
		"/private/var/x":           "/var/x",
		"/private/var/xy/a.go":     "/private/var/xy/a.go", // prefix of another dir, not a child
		"/Users/me/repo/.git/HEAD": "/Users/me/repo/.git/HEAD",
		"/elsewhere/file.txt":      "/elsewhere/file.txt",
	}
	for in, want := range tests {
		if got := remap(in, mapping); got != want {
			t.Errorf("remap(%q) = %q, want %q", in, got, want)
		}
	}
}
