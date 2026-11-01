package main

import (
	"context"
	"fmt"
	"path/filepath"

	"flint/internal/git"
	"flint/internal/redact"
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
// expected is the file as the editor loaded or last wrote it; when the file holds
// something else now, it fails with git.ErrChangedOnDisk unless overwrite.
func (g *GitAPI) MarkResolved(path, content, expected string, allowMarkers, overwrite bool) error {
	return g.inRepo(path, func(ctx context.Context, r *git.Repo, rel string) error {
		return r.MarkResolved(ctx, rel, content, expected, allowMarkers, overwrite)
	})
}

// SaveMergeResult writes the merge editor's result without staging it, with the
// same check as MarkResolved against changes made on disk since it was loaded.
func (g *GitAPI) SaveMergeResult(path, content, expected string, overwrite bool) error {
	return g.inRepo(path, func(ctx context.Context, r *git.Repo, rel string) error {
		return r.SaveResult(ctx, rel, content, expected, overwrite)
	})
}

// inRepo runs a write on path, which must be absolute and inside an open folder.
func (g *GitAPI) inRepo(path string, fn func(context.Context, *git.Repo, string) error) error {
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
		return fn(ctx, r, rel)
	})
}

// AgentText is text from a file on its way to an agent's chat draft.
type AgentText struct {
	// SecretFile: the file holds credentials as a whole (.env, keys…), so Texts is
	// empty and the hand-off leaves its contents out.
	SecretFile bool     `json:"secretFile"`
	Texts      []string `json:"texts"`
}

// RedactForAgent prepares parts of a file (path) for an agent hand-off, such as a
// merge conflict's sides: credentials and the home directory are redacted, and
// nothing of a secret file is passed on.
func (g *GitAPI) RedactForAgent(path string, texts []string) AgentText {
	if redact.SecretFile(path) {
		return AgentText{SecretFile: true, Texts: []string{}}
	}
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = redact.String(t)
	}
	return AgentText{Texts: out}
}
