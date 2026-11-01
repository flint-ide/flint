//go:build linux

package watcher

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// inotify watches single directories, so the Watcher adds one watch per
// directory under each root (skipping the noise Ignored drops anyway) and
// follows directories created or moved in. One inotify descriptor per Watcher.
const watchMask = unix.IN_CREATE | unix.IN_DELETE | unix.IN_CLOSE_WRITE | unix.IN_MODIFY |
	unix.IN_MOVED_FROM | unix.IN_MOVED_TO | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF |
	unix.IN_ATTRIB | unix.IN_EXCL_UNLINK | unix.IN_ONLYDIR

// skippedDirs are never descended into: their churn is noise for the UI.
var skippedDirs = map[string]bool{"node_modules": true, ".idea": true, "__pycache__": true}

// Watcher streams changes for a set of roots using inotify.
type Watcher struct {
	fd    int      // the inotify descriptor...
	file  *os.File // ...as a non-blocking file, so Close wakes Read (never call Fd: it makes it blocking)
	roots []string
	deb   *debouncer

	mu   sync.Mutex
	dirs map[int32]string // watch descriptor → directory
}

// Watch reports changed absolute paths under any of roots to onChange (batched).
// Callers typically pass the project folder plus its git dir, which for linked
// worktrees lives outside the folder.
func Watch(roots []string, onChange func(paths []string)) (*Watcher, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("inotify: %w", err)
	}
	w := &Watcher{
		fd:    fd,
		file:  os.NewFile(uintptr(fd), "inotify"),
		roots: roots,
		deb:   newDebouncer(onChange),
		dirs:  map[int32]string{},
	}
	for _, root := range roots {
		if err := w.addTree(root, nil); err != nil {
			w.Close()
			return nil, fmt.Errorf("watch %s: %w", root, err)
		}
	}
	go w.loop()
	return w, nil
}

// addTree watches dir and every directory below it. Files already inside a
// directory that appeared after the watch started are passed to found, since
// their own create events happened before the watch existed. Only a failure
// on dir itself is an error: an unreadable or vanished subdirectory, or
// running out of watches (fs.inotify.max_user_watches), skips that part.
func (w *Watcher) addTree(dir string, found func(string)) error {
	first := true
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		root := first
		first = false
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
		if !root && skipDir(path, d.Name()) {
			return filepath.SkipDir
		}
		if err := w.add(path); err != nil {
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
	wd, err := unix.InotifyAddWatch(w.fd, dir, watchMask)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.dirs[int32(wd)] = dir
	w.mu.Unlock()
	return nil
}

// skipDir reports whether a directory's changes never matter: dependency and
// tool caches, and a git dir's object store and reflogs (Classify ignores
// those; refs, HEAD and the index are still watched).
func skipDir(path, name string) bool {
	if skippedDirs[name] {
		return true
	}
	if name != "objects" && name != "logs" {
		return false
	}
	_, err := os.Stat(filepath.Join(filepath.Dir(path), "HEAD"))
	return err == nil
}

func (w *Watcher) loop() {
	buf := make([]byte, 64*1024)
	for {
		n, err := w.file.Read(buf)
		if err != nil {
			if errors.Is(err, os.ErrClosed) {
				return
			}
			continue // EINTR and the like
		}
		w.deb.add(w.parse(buf[:n])...)
	}
}

// parse turns a buffer of inotify events into changed paths, watching new
// directories as they appear.
func (w *Watcher) parse(buf []byte) []string {
	var paths []string
	for off := 0; off+unix.SizeofInotifyEvent <= len(buf); {
		ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
		nameStart := off + unix.SizeofInotifyEvent
		nameEnd := min(nameStart+int(ev.Len), len(buf))
		name := string(trimNUL(buf[nameStart:nameEnd]))
		off = nameEnd

		if ev.Mask&unix.IN_Q_OVERFLOW != 0 {
			paths = append(paths, w.roots...) // events were lost: refresh everything
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
		paths = append(paths, path)
		if ev.Mask&unix.IN_ISDIR != 0 && ev.Mask&(unix.IN_CREATE|unix.IN_MOVED_TO) != 0 && !skipDir(path, name) {
			_ = w.addTree(path, func(p string) { paths = append(paths, p) })
		}
	}
	return paths
}

func trimNUL(b []byte) []byte {
	for i, c := range b {
		if c == 0 {
			return b[:i]
		}
	}
	return b
}

// Close stops watching; pending events are dropped.
func (w *Watcher) Close() {
	_ = w.file.Close()
	w.deb.close()
}
