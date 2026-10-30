package profiling

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// dirPrefix names the temporary directories of profiles (and test binaries).
const dirPrefix = "flint-profile-"

// staleAfter is the age after which a directory left behind by a crashed Flint
// is removed when the next profile is recorded.
const staleAfter = 24 * time.Hour

// Dirs owns the temporary directories profiles are written to. A directory is
// removed when its tab closes (Remove) or when Flint quits (RemoveAll); paths
// Flint did not create, such as a profile opened from disk, are never touched.
type Dirs struct {
	base  string
	sweep sync.Once

	mu   sync.Mutex
	dirs map[string]bool
}

// NewDirs creates directories under base ("" = the system temp directory).
func NewDirs(base string) *Dirs {
	if base == "" {
		base = os.TempDir()
	}
	return &Dirs{base: base, dirs: map[string]bool{}}
}

// New creates a directory for one profiling run.
func (d *Dirs) New() (string, error) {
	d.sweep.Do(func() { removeStale(d.base, time.Now().Add(-staleAfter)) })
	dir, err := os.MkdirTemp(d.base, dirPrefix)
	if err != nil {
		return "", fmt.Errorf("create profile directory: %w", err)
	}
	d.mu.Lock()
	d.dirs[dir] = true
	d.mu.Unlock()
	return dir, nil
}

// Owns reports whether dir was created by New and not removed yet.
func (d *Dirs) Owns(dir string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dirs[filepath.Clean(dir)]
}

// Remove deletes a directory created by New; other paths are left alone.
func (d *Dirs) Remove(dir string) error {
	dir = filepath.Clean(dir)
	d.mu.Lock()
	owned := d.dirs[dir]
	delete(d.dirs, dir)
	d.mu.Unlock()
	if !owned {
		return nil
	}
	return removeDir(dir)
}

// RemoveAll deletes every directory still owned (Flint quits).
func (d *Dirs) RemoveAll() {
	d.mu.Lock()
	dirs := d.dirs
	d.dirs = map[string]bool{}
	d.mu.Unlock()
	for dir := range dirs {
		_ = removeDir(dir)
	}
}

func removeDir(dir string) error {
	cache.forget(func(path string) bool { return strings.HasPrefix(path, dir+string(filepath.Separator)) })
	return os.RemoveAll(dir)
}

// removeStale deletes profile directories older than before (left by a crash).
func removeStale(base string, before time.Time) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), dirPrefix) {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(before) {
			_ = os.RemoveAll(filepath.Join(base, e.Name()))
		}
	}
}
