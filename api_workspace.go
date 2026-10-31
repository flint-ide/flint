package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"flint/internal/fuzzy"
	"flint/internal/git"
	"flint/internal/watcher"
	"flint/internal/workspace"
)

// eventFSChanged carries batches of changed absolute paths under the open folder.
const eventFSChanged = "fs:changed"

// WorkspaceAPI exposes the opened project folder to the frontend.
type WorkspaceAPI struct {
	app *App
}

// InitialFolder returns the folder to open on launch: the CLI argument, else the last used one.
func (w *WorkspaceAPI) InitialFolder() string {
	if w.app.argFolder != "" {
		return w.app.argFolder
	}
	for _, p := range w.app.recentFolders() {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
	return ""
}

// ForgetRecent removes a folder from the recent list.
func (w *WorkspaceAPI) ForgetRecent(path string) error { return w.app.forgetRecentFolder(path) }

// RecentFolders lists recently opened folders, newest first.
func (w *WorkspaceAPI) RecentFolders() []string {
	return w.app.recentFolders()
}

// PickFolder shows the native folder picker. Returns "" when the user cancels.
func (w *WorkspaceAPI) PickFolder() (string, error) {
	return w.app.pickFolder("Open folder")
}

// OpenFolder validates and records a folder as the current project.
func (w *WorkspaceAPI) OpenFolder(path string) (string, error) {
	abs, err := requireDir(path)
	if err != nil {
		return "", err
	}
	if err := w.app.rememberFolder(abs); err != nil {
		return "", err
	}
	watch, err := w.watchFolder(abs)
	if err != nil {
		// Not fatal: the tree still works, it just won't live-refresh.
		w.app.logf("file watcher disabled: %v", err)
	}
	w.app.openProject(abs, watch)
	w.updateTitle()
	return abs, nil
}

// AddRoot attaches another project folder to the workspace and returns its path.
func (w *WorkspaceAPI) AddRoot(path string) (string, error) {
	abs, err := requireDir(path)
	if err != nil {
		return "", err
	}
	if err := workspace.CheckNewRoot(w.app.projectRoots(), abs); err != nil {
		return "", err
	}
	watch, err := w.watchFolder(abs)
	if err != nil {
		w.app.logf("file watcher disabled for %s: %v", abs, err)
	}
	if err := w.app.addRoot(abs, watch); err != nil {
		if watch != nil {
			watch.Close()
		}
		return "", err
	}
	w.updateTitle()
	return abs, nil
}

// RemoveRoot detaches a folder added with AddRoot (files stay on disk).
func (w *WorkspaceAPI) RemoveRoot(path string) error {
	if err := w.app.removeRoot(path); err != nil {
		return err
	}
	w.updateTitle()
	return nil
}

// updateTitle shows the primary folder, plus how many are attached: "api + 2 — flint".
func (w *WorkspaceAPI) updateTitle() {
	roots := w.app.projectRoots()
	if len(roots) == 0 {
		return
	}
	title := filepath.Base(roots[0])
	if len(roots) > 1 {
		title += fmt.Sprintf(" + %d", len(roots)-1)
	}
	w.app.setWindowTitle(title + " — flint")
}

func requireDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return abs, nil
}

// ListDir lists one directory level for the file tree.
func (w *WorkspaceAPI) ListDir(path string) ([]workspace.Entry, error) {
	return workspace.ListDir(path)
}

// IndexFiles (re)lists the project files for quick-open and returns their count.
// The list stays in Go: the webview only ever receives the top matches.
func (w *WorkspaceAPI) IndexFiles(root string) (int, error) {
	ctx, cancel := context.WithTimeout(w.app.ctx, 30*time.Second)
	defer cancel()
	// Concurrent requests (⌘P typed before the first listing ends) share one walk.
	return w.app.files.Load(root, func() ([]string, error) { return workspace.ListFiles(ctx, root) })
}

// FindFiles ranks the files of the given workspace folders against a fuzzy query
// (⌘P), best `limit` first. With several folders paths start with the folder's label.
func (w *WorkspaceAPI) FindFiles(roots []string, query string, limit int) (fuzzy.Result, error) {
	labels := workspace.Labels(roots)
	targets := make([]fuzzy.Root, len(roots))
	for i, r := range roots {
		targets[i] = fuzzy.Root{Path: r, Label: labels[i]}
	}
	if res, ok := w.app.files.Search(targets, query, limit); ok {
		return res, nil
	}
	for _, r := range roots {
		if _, err := w.IndexFiles(r); err != nil {
			return fuzzy.Result{}, err
		}
	}
	res, _ := w.app.files.Search(targets, query, limit)
	return res, nil
}

// ReadFile loads a file for the editor.
func (w *WorkspaceAPI) ReadFile(path string) (workspace.FileContent, error) {
	return workspace.ReadFile(path)
}

// WriteFile saves editor content to disk.
func (w *WorkspaceAPI) WriteFile(path, content string) error {
	return workspace.WriteFile(path, content)
}

// CreateFile creates an empty file name in dir (inside the open folder) and returns its path.
func (w *WorkspaceAPI) CreateFile(dir, name string) (string, error) {
	root, err := w.app.rootOf(dir)
	if err != nil {
		return "", err
	}
	return workspace.CreateFile(root, dir, name)
}

// CreateFolder creates a folder name in dir (inside the open folder) and returns its path.
func (w *WorkspaceAPI) CreateFolder(dir, name string) (string, error) {
	root, err := w.app.rootOf(dir)
	if err != nil {
		return "", err
	}
	return workspace.CreateDir(root, dir, name)
}

// RenamePath renames a file or folder in place and returns its new path.
func (w *WorkspaceAPI) RenamePath(path, newName string) (string, error) {
	root, err := w.app.rootOf(path)
	if err != nil {
		return "", err
	}
	return workspace.Rename(root, path, newName)
}

// TrashPaths moves files and folders to the macOS Trash and returns the paths moved
// (nested selections collapse into their folder).
func (w *WorkspaceAPI) TrashPaths(paths []string) ([]string, error) {
	// Check every path before anything moves; then trash folder by folder.
	byRoot := map[string][]string{}
	var order []string
	for _, p := range paths {
		root, err := w.app.rootOf(p)
		if err != nil {
			return nil, err
		}
		if _, seen := byRoot[root]; !seen {
			order = append(order, root)
		}
		byRoot[root] = append(byRoot[root], p)
	}
	moved := []string{}
	for _, root := range order {
		done, err := workspace.Trash(root, byRoot[root], workspace.MoveToTrash)
		moved = append(moved, done...)
		if err != nil {
			return moved, err
		}
	}
	return moved, nil
}

// RevealInFinder selects path in a Finder window.
func (w *WorkspaceAPI) RevealInFinder(path string) error {
	if _, err := w.app.rootOf(path); err != nil {
		return err
	}
	if out, err := exec.Command("open", "-R", path).CombinedOutput(); err != nil {
		return fmt.Errorf("reveal %s: %s", filepath.Base(path), strings.TrimSpace(string(out)))
	}
	return nil
}

// watchFolder watches the project and, when it is a git work tree, its git dirs, so
// checkouts and commits made in the terminal refresh the UI too. A linked worktree's
// HEAD and index live in the main repository's .git/worktrees, outside the folder.
func (w *WorkspaceAPI) watchFolder(root string) (*watcher.Watcher, error) {
	roots := []string{root}
	var gitDirs []string
	ctx, cancel := context.WithTimeout(w.app.ctx, 5*time.Second)
	defer cancel()
	if repo, err := git.Open(ctx, root); err == nil {
		gitDirs = []string{repo.GitDir, repo.CommonDir}
		for _, d := range gitDirs {
			if !within(d, root) && !slices.Contains(roots, d) {
				roots = append(roots, d)
			}
		}
	}
	return watcher.Watch(roots, func(paths []string) {
		files, gitChanged := watcher.Classify(paths, gitDirs)
		if len(files) > 0 {
			w.app.emit(eventFSChanged, files)
		}
		if gitChanged {
			w.app.emit(eventGitChanged, root)
		}
	})
}

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}
