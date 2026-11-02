package main

import (
	"errors"
	"fmt"
	"slices"

	"flint/internal/watcher"
	"flint/internal/workspace"
)

// The workspace is a primary project folder plus folders attached to it (like
// IntelliJ's "attach project"). A single folder works exactly as before.

// openProject makes root the only folder of the workspace, replacing all watchers.
func (a *App) openProject(root string, w *watcher.Watcher) {
	a.projMu.Lock()
	prev, prevRoots := a.watchers, a.roots
	a.roots = []string{root}
	a.watchers = map[string]*watcher.Watcher{}
	if w != nil {
		a.watchers[root] = w
	}
	a.projMu.Unlock()
	closeAll(prev)
	a.files.Retain([]string{root})
	a.forgetMCPRoots(slices.DeleteFunc(slices.Clone(prevRoots), func(r string) bool { return r == root }))
	a.features.RootsChanged()
}

// addRoot attaches root to the workspace; nested or duplicate folders are rejected.
func (a *App) addRoot(root string, w *watcher.Watcher) error {
	a.projMu.Lock()
	if len(a.roots) == 0 {
		a.projMu.Unlock()
		return errors.New("open a folder first")
	}
	if err := workspace.CheckNewRoot(a.roots, root); err != nil {
		a.projMu.Unlock()
		return err
	}
	a.roots = append(slices.Clone(a.roots), root)
	if w != nil {
		a.watchers[root] = w
	}
	a.projMu.Unlock()
	a.features.RootsChanged()
	return nil
}

// removeRoot detaches an attached folder; the primary folder can't be removed.
func (a *App) removeRoot(root string) error {
	a.projMu.Lock()
	i := slices.Index(a.roots, root)
	switch {
	case i < 0:
		a.projMu.Unlock()
		return fmt.Errorf("%s is not in the workspace", root)
	case i == 0:
		a.projMu.Unlock()
		return errors.New("the main project can't be removed; open another folder instead")
	}
	a.roots = slices.Delete(slices.Clone(a.roots), i, i+1)
	w := a.watchers[root]
	delete(a.watchers, root)
	roots := a.roots
	a.projMu.Unlock()
	if w != nil {
		w.Close()
	}
	a.files.Retain(roots)
	a.forgetMCPRoots([]string{root})
	a.features.RootsChanged()
	return nil
}

// closeProject stops all watchers (shutdown).
func (a *App) closeProject() {
	a.projMu.Lock()
	prev := a.watchers
	a.roots, a.watchers = nil, nil
	a.projMu.Unlock()
	closeAll(prev)
}

// projectRoots returns the workspace folders, the primary one first.
func (a *App) projectRoots() []string {
	a.projMu.Lock()
	defer a.projMu.Unlock()
	return slices.Clone(a.roots)
}

// rootOf returns the workspace folder containing path.
func (a *App) rootOf(path string) (string, error) {
	roots := a.projectRoots()
	if len(roots) == 0 {
		return "", errors.New("no folder is open")
	}
	if root, ok := workspace.RootOf(roots, path); ok {
		return root, nil
	}
	return "", fmt.Errorf("%w: %s", workspace.ErrOutsideProject, path)
}

func closeAll(watchers map[string]*watcher.Watcher) {
	for _, w := range watchers {
		w.Close()
	}
}
