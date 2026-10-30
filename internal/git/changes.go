package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxRevisionFile caps file contents fetched for diffs and gutter markers.
const maxRevisionFile = 10 << 20

// Revision is a file's content at some revision.
type Revision struct {
	Exists  bool   `json:"exists"`
	Binary  bool   `json:"binary"`
	Content string `json:"content"`
}

// FileAt returns rel's content at rev ("HEAD", a hash, or ":" for the index).
func (r *Repo) FileAt(ctx context.Context, rev, rel string) (Revision, error) {
	spec := rev + ":" + rel
	if rev == ":" {
		spec = ":" + rel
	}
	out, err := r.git(ctx, "cat-file", "blob", spec)
	if err != nil {
		var gerr *Error
		// Missing at that revision (new file, or no commits yet) is a normal answer.
		if errors.As(err, &gerr) && missingObject(gerr.Msg) {
			return Revision{}, nil
		}
		return Revision{}, err
	}
	if len(out) > maxRevisionFile || isBinary(out) {
		return Revision{Exists: true, Binary: true}, nil
	}
	return Revision{Exists: true, Content: string(out)}, nil
}

// missingObject recognises git's ways of saying "no such file at that revision".
func missingObject(msg string) bool {
	msg = strings.ToLower(msg)
	for _, s := range []string{
		"does not exist",
		"exists on disk, but not in", // new file: on disk, not in HEAD / the index
		"invalid object name",        // HEAD before the first commit
		"not a valid object",
		"bad file",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func isBinary(b []byte) bool {
	head := b[:min(len(b), 8000)]
	for _, c := range head {
		if c == 0 {
			return true
		}
	}
	return false
}

// Commit commits the given paths (all their current changes, including untracked and
// deleted files), like IntelliJ's commit dialog. Other staged changes are left alone.
// With amend and no paths it only rewrites the last commit message.
func (r *Repo) Commit(ctx context.Context, message string, paths []string, amend bool) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("commit message is empty")
	}
	if len(paths) == 0 && !amend {
		return "", errors.New("no files selected")
	}
	if len(paths) > 0 {
		stage, err := r.stageable(ctx, paths)
		if err != nil {
			return "", err
		}
		if len(stage) > 0 {
			if _, err := r.git(ctx, append([]string{"add", "-A", "--"}, stage...)...); err != nil {
				return "", err
			}
		}
	}
	args := []string{"commit", "--cleanup=strip", "-F", "-"}
	if amend {
		args = append(args, "--amend")
	}
	// --only commits exactly these paths, ignoring anything else in the index. A merge
	// commit must take the whole index (git refuses partial merge commits), so after
	// staging the selection we commit everything that is staged.
	if r.operation() != "merging" {
		args = append(args, "--only")
		if len(paths) > 0 {
			args = append(append(args, "--"), paths...)
		}
	}
	if _, err := r.gitInput(ctx, []byte(message), args...); err != nil {
		return "", err
	}
	out, err := r.git(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// stageable drops paths `git add` would reject: already-staged deletions (and the
// source of a staged rename) exist neither on disk nor in the index.
func (r *Repo) stageable(ctx context.Context, paths []string) ([]string, error) {
	out, err := r.git(ctx, append([]string{"ls-files", "-z", "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	inIndex := map[string]bool{}
	for _, p := range splitNUL(out) {
		inIndex[p] = true
	}
	var stage []string
	for _, p := range paths {
		if _, statErr := os.Lstat(filepath.Join(r.Root, filepath.FromSlash(p))); statErr == nil || inIndex[p] {
			stage = append(stage, p)
		}
	}
	return stage, nil
}

// LastCommitMessage returns HEAD's message (to prefill amend).
func (r *Repo) LastCommitMessage(ctx context.Context) (string, error) {
	out, err := r.git(ctx, "log", "-1", "--format=%B")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Rollback discards local changes of paths: tracked files are restored from HEAD, files
// that don't exist in HEAD (added / untracked) are removed from the index and deleted.
func (r *Repo) Rollback(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	inHead, err := r.pathsInHead(ctx, paths)
	if err != nil {
		return err
	}
	var restore, remove []string
	for _, p := range paths {
		if inHead[p] {
			restore = append(restore, p)
		} else {
			remove = append(remove, p)
		}
	}
	if len(restore) > 0 {
		args := append([]string{"restore", "--source=HEAD", "--staged", "--worktree", "--"}, restore...)
		if _, err := r.git(ctx, args...); err != nil {
			return err
		}
	}
	if len(remove) > 0 {
		args := append([]string{"rm", "--cached", "--ignore-unmatch", "-q", "--"}, remove...)
		if _, err := r.git(ctx, args...); err != nil {
			return err
		}
		for _, p := range remove {
			if err := os.Remove(filepath.Join(r.Root, filepath.FromSlash(p))); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("delete %s: %w", p, err)
			}
		}
	}
	return nil
}

func (r *Repo) pathsInHead(ctx context.Context, paths []string) (map[string]bool, error) {
	found := map[string]bool{}
	out, err := r.git(ctx, append([]string{"ls-tree", "-r", "-z", "--name-only", "HEAD", "--"}, paths...)...)
	if err != nil {
		var gerr *Error
		if errors.As(err, &gerr) && strings.Contains(gerr.Msg, "Not a valid object name") {
			return found, nil // no commits yet
		}
		return nil, err
	}
	for _, p := range splitNUL(out) {
		found[p] = true
	}
	return found, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
