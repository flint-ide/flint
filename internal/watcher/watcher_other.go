//go:build !darwin

package watcher

// Watcher is a no-op outside macOS for now; the UI still refreshes on demand.
type Watcher struct{}

// Watch is not implemented on this platform yet.
func Watch([]string, func([]string)) (*Watcher, error) { return &Watcher{}, nil }

// Close is a no-op.
func (*Watcher) Close() {}
