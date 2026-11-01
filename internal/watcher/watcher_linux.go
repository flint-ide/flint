//go:build linux

package watcher

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// inotify watches single directories, so the Watcher adds one watch per
// directory under each root (skipping what Options.Skip names and git's object
// store) and follows directories created, moved in or moved out. One inotify
// descriptor per Watcher.
const watchMask = unix.IN_CREATE | unix.IN_DELETE | unix.IN_CLOSE_WRITE | unix.IN_MODIFY |
	unix.IN_MOVED_FROM | unix.IN_MOVED_TO | unix.IN_DELETE_SELF |
	unix.IN_ATTRIB | unix.IN_EXCL_UNLINK | unix.IN_ONLYDIR

// Watcher streams changes for a set of roots using inotify.
type Watcher struct {
	fd      int               // the inotify descriptor...
	file    *os.File          // ...as a non-blocking file, so Close wakes Read (never call Fd: it makes it blocking)
	mapping map[string]string // resolved root → root as the caller named it
	real    []string          // resolved roots
	opts    Options
	deb     *debouncer
	limit   sync.Once

	mu     sync.Mutex
	closed bool             // set by Close; fd may be reused after it, so nothing may use it
	dirs   map[int32]string // watch descriptor → resolved directory
}

// Watch reports changed absolute paths under any of roots to onChange (batched).
// Callers typically pass the project folder plus its git dir, which for linked
// worktrees lives outside the folder. A root may be a symlink: it is watched
// where it points, and paths are reported under the root as given.
func Watch(roots []string, onChange func(paths []string), opts Options) (*Watcher, error) {
	mapping := make(map[string]string, len(roots))
	real := make([]string, 0, len(roots))
	for _, root := range roots {
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", root, err)
		}
		mapping[r] = root
		real = append(real, r)
	}
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("inotify: %w", err)
	}
	w := &Watcher{
		fd:      fd,
		file:    os.NewFile(uintptr(fd), "inotify"),
		mapping: mapping,
		real:    real,
		opts:    opts,
		deb:     newDebouncer(onChange),
		dirs:    map[int32]string{},
	}
	for i, root := range real {
		if err := w.addTree(root, nil); err != nil {
			w.Close()
			return nil, fmt.Errorf("watch %s: %w", roots[i], err)
		}
	}
	go w.loop()
	return w, nil
}

// addTree watches dir and every directory below it. Files already inside a
// directory that appeared after the watch started are passed to found, since
// their own create events happened before the watch existed. Only a failure
// on dir itself is an error: an unreadable or vanished subdirectory, or
// running out of watches (reported once through Options.Limit), skips that part.
func (w *Watcher) addTree(dir string, found func(string)) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		root := path == dir
		if err != nil {
			if root {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			if found != nil {
				found(path)
			}
			return nil
		}
		if !root && w.skip(path) {
			return filepath.SkipDir
		}
		if err := w.add(path); err != nil {
			if errors.Is(err, unix.ENOSPC) {
				w.reportLimit(path)
			}
			if root {
				return err
			}
			return filepath.SkipDir
		}
		if found != nil && !root {
			found(path)
		}
		return nil
	})
}

func (w *Watcher) add(dir string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return os.ErrClosed
	}
	wd, err := unix.InotifyAddWatch(w.fd, dir, watchMask)
	if err != nil {
		return err
	}
	w.dirs[int32(wd)] = dir
	return nil
}

// skip reports a directory whose changes never matter: git's object store and
// reflogs (Classify ignores those; refs, HEAD and the index are still
// watched), and what Options.Skip names (dependency folders, user excludes).
func (w *Watcher) skip(path string) bool {
	name := filepath.Base(path)
	if name == "objects" || name == "logs" {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "HEAD")); err == nil {
			return true
		}
	}
	if w.opts.Skip != nil {
		return w.opts.Skip(path)
	}
	return defaultSkip[name]
}

// defaultSkip applies when Options.Skip is nil.
var defaultSkip = map[string]bool{"node_modules": true, ".idea": true, "__pycache__": true}

// reportLimit tells once that the kernel's watch limit was reached.
func (w *Watcher) reportLimit(dir string) {
	w.limit.Do(func() {
		err := fmt.Errorf("out of inotify watches at %s: folders below it aren't watched for changes; "+
			"raise the limit with `sudo sysctl fs.inotify.max_user_watches=524288` "+
			"(and in /etc/sysctl.d to keep it), or exclude big folders in Settings", dir)
		slog.Warn("watcher", "error", err)
		if w.opts.Limit != nil {
			w.opts.Limit(err)
		}
	})
}

// removeTree stops watching dir and everything below it (a directory moved
// out of the tree, e.g. to the Trash, keeps its inode and would keep reporting).
func (w *Watcher) removeTree(dir string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	for wd, path := range w.dirs {
		if under(path, dir) {
			_, _ = unix.InotifyRmWatch(w.fd, uint32(wd))
			delete(w.dirs, wd)
		}
	}
}

// rescan re-adds every directory under the roots after the kernel dropped
// events (directories created meanwhile were never watched) and forgets the
// watches of directories that are gone.
func (w *Watcher) rescan() {
	w.mu.Lock()
	before := make(map[int32]string, len(w.dirs))
	for wd, dir := range w.dirs {
		before[wd] = dir
	}
	w.dirs = map[int32]string{}
	w.mu.Unlock()
	for _, root := range w.real {
		_ = w.addTree(root, nil)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	for wd := range before {
		if _, ok := w.dirs[wd]; !ok {
			_, _ = unix.InotifyRmWatch(w.fd, uint32(wd))
		}
	}
}

func (w *Watcher) loop() {
	buf := make([]byte, 64*1024)
	for {
		n, err := w.file.Read(buf)
		switch {
		case errors.Is(err, os.ErrClosed):
			return
		case errors.Is(err, syscall.EINTR):
			continue
		case err != nil || n == 0:
			// Not expected from inotify; retrying would spin.
			slog.Warn("watcher stopped", "error", err)
			return
		}
		paths, lost := w.parse(buf[:n])
		if lost {
			w.rescan()
			if w.opts.Lost != nil {
				w.opts.Lost()
			}
		}
		w.deb.add(paths...)
	}
}

// parse turns a buffer of inotify events into changed paths (under the roots
// as the caller named them), following directories that appear and dropping
// the ones that leave. lost reports a queue overflow: events were dropped.
func (w *Watcher) parse(buf []byte) (paths []string, lost bool) {
	for off := 0; off+unix.SizeofInotifyEvent <= len(buf); {
		ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
		nameStart := off + unix.SizeofInotifyEvent
		nameEnd := min(nameStart+int(ev.Len), len(buf))
		name := string(trimNUL(buf[nameStart:nameEnd]))
		off = nameEnd

		if ev.Mask&unix.IN_Q_OVERFLOW != 0 {
			lost = true
			continue
		}
		w.mu.Lock()
		dir, ok := w.dirs[ev.Wd]
		if ev.Mask&unix.IN_IGNORED != 0 {
			delete(w.dirs, ev.Wd)
		}
		w.mu.Unlock()
		if !ok {
			continue
		}
		path := dir
		if name != "" {
			path = filepath.Join(dir, name)
		}
		paths = append(paths, remap(path, w.mapping))
		if ev.Mask&unix.IN_ISDIR == 0 {
			continue
		}
		switch {
		case ev.Mask&unix.IN_MOVED_FROM != 0:
			w.removeTree(path)
		case ev.Mask&(unix.IN_CREATE|unix.IN_MOVED_TO) != 0 && !w.skip(path):
			_ = w.addTree(path, func(p string) { paths = append(paths, remap(p, w.mapping)) })
		}
	}
	return paths, lost
}

// under reports whether path is dir or inside it.
func under(path, dir string) bool {
	rest, ok := strings.CutPrefix(path, dir)
	return ok && (rest == "" || rest[0] == '/')
}

func trimNUL(b []byte) []byte {
	for i, c := range b {
		if c == 0 {
			return b[:i]
		}
	}
	return b
}

// Close stops watching; pending events are dropped. The descriptor is closed
// under the lock, so no add or remove can use it (or a reused number) after.
func (w *Watcher) Close() {
	w.mu.Lock()
	w.closed = true
	_ = w.file.Close()
	w.mu.Unlock()
	w.deb.close()
}
