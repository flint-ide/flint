package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"flint/internal/git"
)

const (
	gitTimeout       = 30 * time.Second
	gitRemoteTimeout = 5 * time.Minute // fetch / pull / push over slow networks
)

// eventGitChanged fires when HEAD, the index or refs change (commits, checkouts,
// rebases made by agents in the terminal included).
const eventGitChanged = "git:changed"

// RepoInfo tells the frontend whether a folder is under git.
type RepoInfo struct {
	IsRepo    bool   `json:"isRepo"`
	Root      string `json:"root"`
	GitDir    string `json:"gitDir"`
	CommonDir string `json:"commonDir"`
}

// GitAPI exposes git operations for a work tree identified by any folder inside it.
type GitAPI struct {
	app *App

	mu    sync.Mutex
	repos map[string]*git.Repo
}

func (g *GitAPI) repo(ctx context.Context, dir string) (*git.Repo, error) {
	g.mu.Lock()
	r, ok := g.repos[dir]
	g.mu.Unlock()
	if ok {
		return r, nil
	}
	r, err := git.Open(ctx, dir)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	if g.repos == nil {
		g.repos = map[string]*git.Repo{}
	}
	g.repos[dir] = r
	g.mu.Unlock()
	return r, nil
}

// with runs fn against the repository containing dir under a timeout.
func with[T any](g *GitAPI, dir string, timeout time.Duration, fn func(context.Context, *git.Repo) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(g.app.ctx, timeout)
	defer cancel()
	var zero T
	r, err := g.repo(ctx, dir)
	if err != nil {
		return zero, err
	}
	return fn(ctx, r)
}

func do(g *GitAPI, dir string, timeout time.Duration, fn func(context.Context, *git.Repo) error) error {
	_, err := with(g, dir, timeout, func(ctx context.Context, r *git.Repo) (struct{}, error) {
		return struct{}{}, fn(ctx, r)
	})
	return err
}

// Info reports whether dir is inside a git work tree.
func (g *GitAPI) Info(dir string) (RepoInfo, error) {
	ctx, cancel := context.WithTimeout(g.app.ctx, gitTimeout)
	defer cancel()
	r, err := g.repo(ctx, dir)
	if errors.Is(err, git.ErrNotRepository) {
		return RepoInfo{}, nil
	}
	if err != nil {
		return RepoInfo{}, err
	}
	return RepoInfo{IsRepo: true, Root: r.Root, GitDir: r.GitDir, CommonDir: r.CommonDir}, nil
}

// Status returns branch info and changed files.
func (g *GitAPI) Status(dir string) (git.Status, error) {
	return with(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) (git.Status, error) {
		return r.Status(ctx)
	})
}

// FileAt returns an absolute path's content at rev ("HEAD", ":" for the index, a hash).
func (g *GitAPI) FileAt(dir, rev, path string) (git.Revision, error) {
	return with(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) (git.Revision, error) {
		rel, err := r.Rel(path)
		if err != nil {
			return git.Revision{}, err
		}
		return r.FileAt(ctx, rev, rel)
	})
}

// HeadFile returns path as committed in HEAD of the work tree that contains it.
// That may be a nested one, e.g. an agent's worktree under .claude/worktrees, so
// the repository is found from the file's own directory, not the open folder.
// Files outside any repository come back with Exists false.
func (g *GitAPI) HeadFile(path string) (git.Revision, error) {
	rev, err := g.FileAt(filepath.Dir(path), "HEAD", path)
	if errors.Is(err, git.ErrNotRepository) {
		return git.Revision{}, nil
	}
	return rev, err
}

// Commit commits the given repo-relative paths and returns the new short hash.
func (g *GitAPI) Commit(dir, message string, paths []string, amend bool) (string, error) {
	return with(g, dir, gitRemoteTimeout, func(ctx context.Context, r *git.Repo) (string, error) {
		return r.Commit(ctx, message, paths, amend) // hooks may run for a while
	})
}

// LastCommitMessage returns HEAD's message, used when switching to amend.
func (g *GitAPI) LastCommitMessage(dir string) (string, error) {
	return with(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) (string, error) {
		return r.LastCommitMessage(ctx)
	})
}

// Rollback discards local changes of repo-relative paths.
func (g *GitAPI) Rollback(dir string, paths []string) error {
	return do(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) error { return r.Rollback(ctx, paths) })
}

// Branches lists local and remote branches.
func (g *GitAPI) Branches(dir string) ([]git.Branch, error) {
	return with(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) ([]git.Branch, error) {
		return r.Branches(ctx)
	})
}

// Checkout switches the work tree to a branch.
func (g *GitAPI) Checkout(dir, name string, remote bool) error {
	return do(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) error { return r.Checkout(ctx, name, remote) })
}

// CreateBranch creates a branch, optionally switching to it.
func (g *GitAPI) CreateBranch(dir, name, startPoint string, checkout bool) error {
	return do(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) error {
		return r.CreateBranch(ctx, name, startPoint, checkout)
	})
}

// DeleteBranch deletes a local branch.
func (g *GitAPI) DeleteBranch(dir, name string, force bool) error {
	return do(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) error { return r.DeleteBranch(ctx, name, force) })
}

// Fetch fetches all remotes.
func (g *GitAPI) Fetch(dir string) error {
	return do(g, dir, gitRemoteTimeout, func(ctx context.Context, r *git.Repo) error { return r.Fetch(ctx) })
}

// Pull updates the current branch.
func (g *GitAPI) Pull(dir string) error {
	return do(g, dir, gitRemoteTimeout, func(ctx context.Context, r *git.Repo) error { return r.Pull(ctx) })
}

// Push pushes the current branch (setting upstream on first push).
func (g *GitAPI) Push(dir string) error {
	return do(g, dir, gitRemoteTimeout, func(ctx context.Context, r *git.Repo) error { return r.Push(ctx) })
}

// Worktrees lists the repository's work trees.
func (g *GitAPI) Worktrees(dir string) ([]git.Worktree, error) {
	return with(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) ([]git.Worktree, error) {
		return r.Worktrees(ctx)
	})
}

// DefaultWorktreePath suggests where a worktree for branch should live.
func (g *GitAPI) DefaultWorktreePath(dir, branch string) (string, error) {
	return with(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) (string, error) {
		return r.DefaultWorktreePath(ctx, branch)
	})
}

// AddWorktree creates a worktree at path for branch (new or existing).
func (g *GitAPI) AddWorktree(dir, path, branch string, newBranch bool, base string) error {
	return do(g, dir, gitRemoteTimeout, func(ctx context.Context, r *git.Repo) error {
		return r.AddWorktree(ctx, path, branch, newBranch, base)
	})
}

// RemoveWorktree deletes a linked worktree.
func (g *GitAPI) RemoveWorktree(dir, path string, force bool) error {
	return do(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) error {
		return r.RemoveWorktree(ctx, path, force)
	})
}

// Log lists commits.
func (g *GitAPI) Log(dir string, opts git.LogOptions) ([]git.Commit, error) {
	return with(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) ([]git.Commit, error) {
		return r.Log(ctx, opts)
	})
}

// CommitFiles lists the files a commit changed.
func (g *GitAPI) CommitFiles(dir, hash string) ([]git.FileChange, error) {
	return with(g, dir, gitTimeout, func(ctx context.Context, r *git.Repo) ([]git.FileChange, error) {
		return r.CommitFiles(ctx, hash)
	})
}

// OpenWindow opens path in a new Flint window (a separate process, like IntelliJ's
// "Open in new window"), e.g. for a worktree where another agent works.
func (g *GitAPI) OpenWindow(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate flint: %w", err)
	}
	var cmd *exec.Cmd
	if bundle, ok := appBundle(exe); ok {
		cmd = exec.Command("open", "-n", bundle, "--args", path)
	} else {
		cmd = exec.Command(exe, path) // dev builds aren't bundled
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open new window: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// appBundle returns the .app directory for an executable inside Contents/MacOS.
func appBundle(exe string) (string, bool) {
	dir := filepath.Dir(exe)
	if filepath.Base(dir) != "MacOS" {
		return "", false
	}
	bundle := filepath.Dir(filepath.Dir(dir))
	return bundle, strings.HasSuffix(bundle, ".app")
}
