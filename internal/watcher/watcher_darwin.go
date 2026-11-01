//go:build darwin

package watcher

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsevents"
)

// Watcher streams changes for one root using FSEvents: a single recursive kernel
// subscription, no per-file descriptors, so huge repositories cost nothing extra.
type Watcher struct {
	stream *fsevents.EventStream
	deb    *debouncer
	done   chan struct{}
}

// Watch reports changed absolute paths under any of roots to onChange (batched).
// Callers typically pass the project folder plus its git dir, which for linked
// worktrees lives outside the folder. FSEvents is one recursive stream, so
// Options.Skip and Options.Limit don't apply; Options.Lost does.
func Watch(roots []string, onChange func(paths []string), opts Options) (*Watcher, error) {
	// FSEvents reports resolved paths (e.g. /private/var/...); map them back.
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
	w := &Watcher{
		stream: &fsevents.EventStream{
			Paths:   real,
			Latency: 50 * time.Millisecond,
			Flags:   fsevents.FileEvents | fsevents.NoDefer,
		},
		deb:  newDebouncer(onChange),
		done: make(chan struct{}),
	}
	if err := w.stream.Start(); err != nil {
		return nil, fmt.Errorf("watch %v: %w", roots, err)
	}
	go w.loop(mapping, opts.Lost)
	return w, nil
}

// dropped: FSEvents lost events or coalesced them into a folder to rescan.
const dropped = fsevents.MustScanSubDirs | fsevents.KernelDropped | fsevents.UserDropped

func (w *Watcher) loop(mapping map[string]string, lost func()) {
	for {
		select {
		case <-w.done:
			return
		case events, ok := <-w.stream.Events:
			if !ok {
				return
			}
			paths := make([]string, 0, len(events))
			rescan := false
			for _, ev := range events {
				paths = append(paths, remap(ev.Path, mapping))
				rescan = rescan || ev.Flags&dropped != 0
			}
			if rescan && lost != nil {
				lost()
			}
			w.deb.add(paths...)
		}
	}
}

// Close stops the stream. The loop keeps draining Events until Stop returns: the
// FSEvents callback sends on an unbuffered channel and would block forever otherwise.
func (w *Watcher) Close() {
	w.stream.Stop()
	close(w.done)
	w.deb.close()
}
