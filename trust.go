package main

import (
	"path/filepath"
	"slices"
	"strings"

	"flint/internal/config"
)

// Workspace trust (Restricted Mode). A folder Flint hasn't been told to
// trust may be hostile (a downloaded archive, a fresh clone), so nothing
// runs its code on its own: language servers that execute the project
// (rust-analyzer, Python) stay off, agents ignore the project's settings and
// hooks, and the frontend doesn't start a terminal there. Trusting a folder
// trusts everything under it. Git's repository-defined drivers never run
// for Flint's own git calls, trusted or not (internal/git).

func cleanFolder(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}

// isTrusted: the folder or one of its parents was trusted by the user.
func (a *App) isTrusted(path string) bool {
	if path == "" {
		return false
	}
	path = cleanFolder(path)
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	for _, t := range a.state.TrustedFolders {
		if path == t || strings.HasPrefix(path, t+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// setTrust adds or removes a folder from the trusted list and saves it.
func (a *App) setTrust(path string, trusted bool) error {
	path = cleanFolder(path)
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	list := slices.DeleteFunc(slices.Clone(a.state.TrustedFolders), func(p string) bool { return p == path })
	if trusted {
		list = append(list, path)
		slices.Sort(list)
	}
	next := a.state
	next.TrustedFolders = list
	if err := config.SaveJSON(a.statePath(), next); err != nil {
		return err
	}
	a.state = next
	return nil
}

func (a *App) trustedFolders() []string {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return append([]string{}, a.state.TrustedFolders...)
}

// IsTrusted reports whether a folder is trusted (Restricted Mode is off there).
func (w *WorkspaceAPI) IsTrusted(path string) bool { return w.app.isTrusted(path) }

// TrustFolder trusts a folder and starts what Restricted Mode held back.
func (w *WorkspaceAPI) TrustFolder(path string) error {
	if _, err := requireDir(path); err != nil {
		return err
	}
	if err := w.app.setTrust(path, true); err != nil {
		return err
	}
	for _, root := range w.app.projectRoots() {
		if w.app.isTrusted(root) {
			w.app.lsp.Retrust(root)
		}
	}
	w.app.features.RootsChanged()
	return nil
}

// UntrustFolder revokes trust (servers already running keep running until
// restart; extensions are told the folder is gone).
func (w *WorkspaceAPI) UntrustFolder(path string) error {
	if err := w.app.setTrust(path, false); err != nil {
		return err
	}
	w.app.features.RootsChanged()
	return nil
}

// TrustedFolders lists the trusted folders, for Settings.
func (w *WorkspaceAPI) TrustedFolders() []string { return w.app.trustedFolders() }
