//go:build !darwin && !linux

package watcher

// Watcher is a no-op where neither FSEvents nor inotify exist (Windows: TODO,
// ReadDirectoryChangesW); the UI still refreshes on demand.
type Watcher struct{}

// Watch is not implemented on this platform yet.
func Watch([]string, func([]string), Options) (*Watcher, error) { return &Watcher{}, nil }

// Close is a no-op.
func (*Watcher) Close() {}
