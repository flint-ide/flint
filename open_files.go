package main

import (
	"os"
	"slices"
	"sync"
)

// Files opened in Flint from outside: Finder's Open With and double-click (the
// document types of build/darwin/Info.plist), `flint README.md`, or a file
// manager on Linux (the .desktop file's %f). They open in tabs like files
// dragged into the window (eventFilesDropped).
//
// macOS hands the file to the launching app before its page has loaded, so
// files wait in openedFiles until the frontend takes them with InitialFiles;
// from then on they are sent at once.
type openedFiles struct {
	mu      sync.Mutex
	pending []string
	claimed bool // the frontend took the pending files; send new ones directly
}

// openFiles opens files in the editor, or keeps them for InitialFiles while
// the frontend has not started yet.
func (a *App) openFiles(paths []string) {
	if len(paths) == 0 {
		return
	}
	a.opened.mu.Lock()
	if !a.opened.claimed {
		a.opened.pending = append(slices.Clone(a.opened.pending), paths...)
		a.opened.mu.Unlock()
		return
	}
	a.opened.mu.Unlock()
	if dropped := droppedPaths(paths); len(dropped) > 0 {
		a.emit(eventFilesDropped, dropped)
	}
}

// takeOpenedFiles returns the files opened before the frontend started, once;
// files opened later are emitted instead.
func (a *App) takeOpenedFiles() []DroppedPath {
	a.opened.mu.Lock()
	pending := a.opened.pending
	a.opened.pending = nil
	a.opened.claimed = true
	a.opened.mu.Unlock()
	return droppedPaths(pending)
}

// filesFromArgs are the existing files (not folders) named by args, as
// absolute paths; see argPaths.
func filesFromArgs(args []string, cwd string) []string {
	var out []string
	for _, p := range argPaths(args, cwd) {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			out = append(out, p)
		}
	}
	return out
}
