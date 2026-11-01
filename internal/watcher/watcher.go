// Package watcher reports file-system changes under a project root, so files edited by
// agents running in the terminal (claude, codex, ...) show up in Flint immediately.
package watcher

import (
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	quietPeriod = 120 * time.Millisecond // flush once events stop for this long
	maxDelay    = 500 * time.Millisecond // ...but never hold events longer than this
)

// Options tune a Watcher; the zero value works.
type Options struct {
	// Skip reports a directory below a root not to watch at all (Linux, where
	// every directory costs a kernel watch): dependency folders and the
	// user's excludes. nil skips node_modules, .idea and __pycache__.
	Skip func(dir string) bool
	// Lost is called when the system dropped events: anything under the roots
	// may have changed unseen, so loaded folders and open files need a reload.
	Lost func()
	// Limit is called once when the kernel's watch limit is reached (Linux):
	// folders from there on aren't watched. The error says how to raise it.
	Limit func(err error)
}

// remap turns a resolved path into one under the caller's (unresolved) root.
// FSEvents may leave out the leading slash.
func remap(path string, mapping map[string]string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	for real, root := range mapping {
		if rest, ok := strings.CutPrefix(path, real); ok && (rest == "" || rest[0] == '/') {
			return root + rest
		}
	}
	return path
}

// ignoredSegments are directories whose churn the UI never needs to see.
var ignoredSegments = []string{"/.git/", "/node_modules/", "/.idea/", "/__pycache__/"}

// Ignored reports whether a changed working-tree path is noise for the file tree.
func Ignored(path string) bool {
	for _, seg := range ignoredSegments {
		if strings.Contains(path, seg) {
			return true
		}
	}
	return strings.HasSuffix(path, "/.DS_Store")
}

// debouncer collects changed paths and flushes them in batches.
type debouncer struct {
	flush func([]string)

	mu      sync.Mutex
	pending map[string]struct{}
	first   time.Time
	timer   *time.Timer
	closed  bool
}

func newDebouncer(flush func([]string)) *debouncer {
	return &debouncer{flush: flush, pending: map[string]struct{}{}}
}

func (d *debouncer) add(paths ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	if len(paths) == 0 {
		return
	}
	for _, p := range paths {
		d.pending[p] = struct{}{}
	}
	now := time.Now()
	if d.first.IsZero() {
		d.first = now
	}
	wait := quietPeriod
	if remaining := maxDelay - now.Sub(d.first); remaining < wait {
		wait = max(remaining, 0)
	}
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(wait, d.fire)
}

func (d *debouncer) fire() {
	d.mu.Lock()
	if d.closed || len(d.pending) == 0 {
		d.mu.Unlock()
		return
	}
	paths := make([]string, 0, len(d.pending))
	for p := range d.pending {
		paths = append(paths, p)
	}
	d.pending = map[string]struct{}{}
	d.first = time.Time{}
	d.mu.Unlock()

	slices.Sort(paths)
	d.flush(paths)
}

func (d *debouncer) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.timer != nil {
		d.timer.Stop()
	}
}

// Classify splits a batch of changed paths into working-tree files the UI should
// refresh and whether git state (HEAD, index, refs) changed. Paths inside any of
// gitDirs or a /.git/ directory count as git; object and log churn is ignored.
func Classify(paths []string, gitDirs []string) (files []string, gitChanged bool) {
	files = []string{}
	for _, p := range paths {
		if rest, ok := gitRelative(p, gitDirs); ok {
			if relevantGitPath(rest) {
				gitChanged = true
			}
			continue
		}
		if !Ignored(p) {
			files = append(files, p)
		}
	}
	return files, gitChanged
}

func gitRelative(path string, gitDirs []string) (string, bool) {
	for _, dir := range gitDirs {
		if dir == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(path, dir); ok && (rest == "" || rest[0] == '/') {
			return strings.TrimPrefix(rest, "/"), true
		}
	}
	if i := strings.Index(path, "/.git/"); i >= 0 {
		return path[i+len("/.git/"):], true
	}
	return "", false
}

// relevantGitPath keeps changes that alter what `git status` / branches show.
func relevantGitPath(rel string) bool {
	switch {
	case rel == "" || strings.HasPrefix(rel, "objects/") || strings.HasPrefix(rel, "logs/"):
		return false
	case strings.HasSuffix(rel, ".lock"):
		return false // the real file's rename follows
	}
	return true
}
