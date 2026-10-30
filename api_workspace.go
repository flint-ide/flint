package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

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
	return runtime.OpenDirectoryDialog(w.app.ctx, runtime.OpenDialogOptions{
		Title:                "Open folder",
		CanCreateDirectories: true,
	})
}

// OpenFolder validates and records a folder as the current project.
func (w *WorkspaceAPI) OpenFolder(path string) (string, error) {
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
	if err := w.app.rememberFolder(abs); err != nil {
		return "", err
	}
	watch, err := w.watchFolder(abs)
	if err != nil {
		// Not fatal: the tree still works, it just won't live-refresh.
		runtime.LogWarningf(w.app.ctx, "file watcher disabled: %v", err)
	}
	w.app.setWatcher(watch)
	runtime.WindowSetTitle(w.app.ctx, filepath.Base(abs)+" — flint")
	return abs, nil
}

// ListDir lists one directory level for the file tree.
func (w *WorkspaceAPI) ListDir(path string) ([]workspace.Entry, error) {
	return workspace.ListDir(path)
}

// ListFiles returns all project files (relative paths) for quick-open.
func (w *WorkspaceAPI) ListFiles(root string) ([]string, error) {
	ctx, cancel := context.WithTimeout(w.app.ctx, 30*time.Second)
	defer cancel()
	return workspace.ListFiles(ctx, root)
}

// ReadFile loads a file for the editor.
func (w *WorkspaceAPI) ReadFile(path string) (workspace.FileContent, error) {
	return workspace.ReadFile(path)
}

// WriteFile saves editor content to disk.
func (w *WorkspaceAPI) WriteFile(path, content string) error {
	return workspace.WriteFile(path, content)
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
			runtime.EventsEmit(w.app.ctx, eventFSChanged, files)
		}
		if gitChanged {
			runtime.EventsEmit(w.app.ctx, eventGitChanged)
		}
	})
}

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}
