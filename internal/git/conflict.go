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
	"sync"
	"sync/atomic"

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
	// Diff3 is a fresh three-way merge of the stages in diff3 style: every conflict
	// with its base. Git's default conflict style leaves the base out of the working
	// file (and joins nearby conflicts), so the merge editor finds it here.
	Diff3 string        `json:"diff3"`
	Sides ConflictSides `json:"sides"`
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
		c.Diff3 = merged
	}
	c.Sides, err = r.ConflictSides(ctx)
	return c, err
}

// MarkResolved writes content as the file's resolution and stages it (git add). With
// conflict markers left in content it refuses unless allowMarkers. The path must be a
// regular file of the work tree that is still unmerged in the index. expected is the
// file as the editor loaded it: see SaveResult.
func (r *Repo) MarkResolved(ctx context.Context, rel, content, expected string, allowMarkers, overwrite bool) error {
	if err := r.requireUnmerged(ctx, rel); err != nil {
		return err
	}
	if !allowMarkers && HasConflictMarkers(content) {
		return ErrConflictMarkers
	}
	if err := r.SaveResult(ctx, rel, content, expected, overwrite); err != nil {
		return err
	}
	_, err := r.git(ctx, "add", "--", literal(rel))
	return err
}

// SaveResult writes the merge editor's content to rel without staging it. Unless
// overwrite, it refuses with ErrChangedOnDisk when the file no longer holds expected
// (what the editor loaded or last wrote): another tool or an agent changed it since.
func (r *Repo) SaveResult(_ context.Context, rel, content, expected string, overwrite bool) error {
	abs, info, err := r.workTreeFile(rel)
	if err != nil {
		return err
	}
	if info == nil {
		return fmt.Errorf("%s is missing or not a regular file of the work tree; resolve it in the terminal", rel)
	}
	if !overwrite {
		current, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		if string(current) != expected {
			return ErrChangedOnDisk
		}
	}
	return config.WriteFileAtomic(abs, []byte(content), info.Mode().Perm())
}

// ErrChangedOnDisk: the file is no longer what the merge editor loaded.
var ErrChangedOnDisk = errors.New("the file changed on disk since you opened it — reload or overwrite")

// workTreeFile resolves rel to a regular file inside the work tree. info is nil when
// the file is missing or isn't a regular file. A path that leaves the work tree
// through a symlinked folder is an error, so nothing is read or written outside it.
func (r *Repo) workTreeFile(rel string) (string, fs.FileInfo, error) {
	if err := validRel(rel); err != nil {
		return "", nil, err
	}
	abs := filepath.Join(r.Root, filepath.FromSlash(rel))
	info, err := os.Lstat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return abs, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return abs, nil, nil
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", nil, err
	}
	root, err := filepath.EvalSymlinks(r.Root)
	if err != nil {
		return "", nil, err
	}
	if _, inside := relInside(root, resolved); !inside {
		return "", nil, fmt.Errorf("%s leads outside the repository through a symlink", rel)
	}
	return abs, info, nil
}

func validRel(rel string) error {
	if rel == "" || path.IsAbs(rel) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("invalid path %q", rel)
	}
	return nil
}

// requireUnmerged checks that rel is a plain path inside the work tree with
// unmerged index entries.
func (r *Repo) requireUnmerged(ctx context.Context, rel string) error {
	if err := validRel(rel); err != nil {
		return err
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

// workingFile reads rel from the work tree; anything but a regular file inside it
// (missing, a symlink, a directory, a file behind a symlinked folder) comes back
// with Exists false.
func (r *Repo) workingFile(rel string) (Revision, error) {
	abs, info, err := r.workTreeFile(rel)
	if info == nil || err != nil {
		return Revision{}, nil
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
// work tree (0 when it can't be read). Status asks on every refresh, so counts are
// kept until the file's size or modification time changes.
func (r *Repo) conflictCount(rel string) int {
	abs, info, err := r.workTreeFile(rel)
	if err != nil || info == nil {
		return 0
	}
	key := countKey{size: info.Size(), mtime: info.ModTime().UnixNano()}
	countMu.Lock()
	hit, ok := countCache[abs]
	countMu.Unlock()
	if ok && hit.countKey == key {
		return hit.n
	}
	conflictReads.Add(1)
	n := 0
	if f, err := r.workingFile(rel); err == nil && f.Exists && !f.Binary {
		n = len(ParseConflictMarkers(f.Content))
	}
	countMu.Lock()
	if len(countCache) >= maxCountCache {
		clear(countCache)
	}
	countCache[abs] = countEntry{countKey: key, n: n}
	countMu.Unlock()
	return n
}

type countKey struct {
	size  int64
	mtime int64
}

type countEntry struct {
	countKey
	n int
}

// maxCountCache bounds the conflict-count cache (one entry per conflicted file).
const maxCountCache = 256

var (
	countMu    sync.Mutex
	countCache = map[string]countEntry{}
	// conflictReads counts files read to count conflicts (tests check the cache).
	conflictReads atomic.Int64
)

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
