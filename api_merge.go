package main

import (
	"context"
	"fmt"
	"path/filepath"

	"flint/internal/git"
)

// Conflict loads a conflicted file (absolute path) for the merge editor: the index
// stages, the working-tree file and what "current" and "incoming" are. Like HeadFile,
// the repository is found from the file's own directory.
func (g *GitAPI) Conflict(path string) (git.ConflictFile, error) {
	if !filepath.IsAbs(path) {
		return git.ConflictFile{}, fmt.Errorf("not an absolute path: %s", path)
	}
	return with(g, filepath.Dir(path), gitTimeout, func(ctx context.Context, r *git.Repo) (git.ConflictFile, error) {
		rel, err := r.Rel(path)
		if err != nil {
			return git.ConflictFile{}, err
		}
		return r.Conflict(ctx, rel)
	})
}

// MarkResolved saves the merge editor's result to a conflicted file (absolute path
// inside an open folder) and stages it. With conflict markers left in content it
// fails with git.ErrConflictMarkers unless the user confirmed (allowMarkers).
func (g *GitAPI) MarkResolved(path, content string, allowMarkers bool) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("not an absolute path: %s", path)
	}
	if _, err := g.app.rootOf(path); err != nil {
		return err
	}
	return do(g, filepath.Dir(path), gitTimeout, func(ctx context.Context, r *git.Repo) error {
		rel, err := r.Rel(path)
		if err != nil {
			return err
		}
		return r.MarkResolved(ctx, rel, content, allowMarkers)
	})
}
