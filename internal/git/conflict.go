package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"flint/internal/config"
)

// ErrNoConflict: the file has no unmerged entries in the index (never conflicted,
// or already resolved and staged).
var ErrNoConflict = errors.New("the file has no merge conflict (already resolved?)")

// ErrConflictMarkers: the result still contains conflict markers and the caller did
// not confirm staging it anyway.
var ErrConflictMarkers = errors.New("the result still contains conflict markers")

// ConflictFile is everything the merge editor needs for one conflicted file.
type ConflictFile struct {
	// Base, Current and Incoming are index stages 1, 2 and 3 (base, ours, theirs).
	// A side that is missing (added on both sides, deleted on one) has Exists false.
	Base     Revision `json:"base"`
	Current  Revision `json:"current"`
	Incoming Revision `json:"incoming"`
	// Working is the file in the work tree: git's merge with conflict markers, plus
	// whatever the user or an agent changed since. Exists is false when it is missing
	// or not a regular file.
	Working Revision `json:"working"`
	// Blocks are the conflicts of a fresh three-way merge of the stages, each with
	// its base, so a block of the working file can be shown with the base it lacks
	// (git's default conflict style leaves the base out).
	Blocks []ConflictBlock `json:"blocks"`
	Sides  ConflictSides   `json:"sides"`
}

// Conflict reads the stages and the working file of a conflicted path (repo-relative).
func (r *Repo) Conflict(ctx context.Context, rel string) (ConflictFile, error) {
	if err := r.requireUnmerged(ctx, rel); err != nil {
		return ConflictFile{}, err
	}
	var c ConflictFile
	var err error
	for _, s := range []struct {
		rev string
		dst *Revision
	}{{":1", &c.Base}, {":2", &c.Current}, {":3", &c.Incoming}} {
		if *s.dst, err = r.FileAt(ctx, s.rev, rel); err != nil {
			return ConflictFile{}, err
		}
	}
	if c.Working, err = r.workingFile(rel); err != nil {
		return ConflictFile{}, err
	}
	if c.Current.Exists && c.Incoming.Exists && !c.Base.Binary && !c.Current.Binary && !c.Incoming.Binary {
		merged, err := r.mergeStages(ctx, c.Current.Content, c.Base.Content, c.Incoming.Content)
		if err != nil {
			return ConflictFile{}, err
		}
		c.Blocks = ParseConflictMarkers(merged)
	}
	if c.Blocks == nil {
		c.Blocks = []ConflictBlock{}
	}
	c.Sides, err = r.ConflictSides(ctx)
	return c, err
}

// MarkResolved writes content as the file's resolution and stages it (git add). With
// conflict markers left in content it refuses unless allowMarkers. The path must be a
// regular file of the work tree that is still unmerged in the index.
func (r *Repo) MarkResolved(ctx context.Context, rel, content string, allowMarkers bool) error {
	if err := r.requireUnmerged(ctx, rel); err != nil {
		return err
	}
	if !allowMarkers && HasConflictMarkers(content) {
		return ErrConflictMarkers
	}
	abs := filepath.Join(r.Root, filepath.FromSlash(rel))
	info, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file; resolve it in the terminal", rel)
	}
	if err := config.WriteFileAtomic(abs, []byte(content), info.Mode().Perm()); err != nil {
		return err
	}
	_, err = r.git(ctx, "add", "--", literal(rel))
	return err
}

// requireUnmerged checks that rel is a plain path inside the work tree with
// unmerged index entries.
func (r *Repo) requireUnmerged(ctx context.Context, rel string) error {
	if rel == "" || path.IsAbs(rel) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("invalid path %q", rel)
	}
	out, err := r.git(ctx, "ls-files", "-u", "-z", "--", literal(rel))
	if err != nil {
		return err
	}
	for _, entry := range splitNUL(out) {
		if _, p, ok := strings.Cut(entry, "\t"); ok && p == rel {
			return nil
		}
	}
	return ErrNoConflict
}

// literal turns off pathspec magic and globbing for one path.
func literal(rel string) string { return ":(literal)" + rel }

// workingFile reads rel from the work tree; anything but a regular file (missing,
// a symlink, a directory) comes back with Exists false.
func (r *Repo) workingFile(rel string) (Revision, error) {
	abs := filepath.Join(r.Root, filepath.FromSlash(rel))
	info, err := os.Lstat(abs)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
		return Revision{}, nil
	}
	if err != nil {
		return Revision{}, err
	}
	if info.Size() > maxRevisionFile {
		return Revision{Exists: true, Binary: true}, nil
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return Revision{}, err
	}
	if isBinary(b) {
		return Revision{Exists: true, Binary: true}, nil
	}
	return Revision{Exists: true, Content: string(b)}, nil
}

// conflictCount is the number of conflict blocks left in a conflicted file of the
// work tree (0 when it can't be read).
func (r *Repo) conflictCount(rel string) int {
	f, err := r.workingFile(rel)
	if err != nil || !f.Exists || f.Binary {
		return 0
	}
	return len(ParseConflictMarkers(f.Content))
}

// mergeStages runs git merge-file on the three versions (in a private temporary
// folder) and returns the result with diff3 markers. Histogram diff matches what
// git's merge (ort) uses; older git without --diff-algorithm uses its default.
func (r *Repo) mergeStages(ctx context.Context, current, base, incoming string) (string, error) {
	dir, err := os.MkdirTemp("", "flint-merge-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	files := []string{filepath.Join(dir, "current"), filepath.Join(dir, "base"), filepath.Join(dir, "incoming")}
	for i, content := range []string{current, base, incoming} {
		if err := os.WriteFile(files[i], []byte(content), 0o600); err != nil {
			return "", err
		}
	}
	args := []string{"merge-file", "-p", "--diff3", "-L", "current", "-L", "base", "-L", "incoming"}
	out, err := r.git(ctx, append(append(slices.Clone(args), "--diff-algorithm=histogram"), files...)...)
	if err != nil && isUsageError(err) {
		out, err = r.git(ctx, append(args, files...)...)
	}
	if err != nil && !conflictsExit(err) {
		return "", err
	}
	return string(out), nil
}

// conflictsExit: merge-file exits with the number of conflicts (1–127).
func conflictsExit(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() > 0 && exit.ExitCode() < 128
}

// isUsageError: git did not understand an option (exit 129).
func isUsageError(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 129
}
