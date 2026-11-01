package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// The operations behind agent tasks (a branch in its own worktree): how far the
// branch got, whether it merges cleanly, and merging it.

// HeadRef is the full ref of a local branch. Task code always names branches
// this way: a short name can resolve to a tag of the same name, and revision
// syntax (main@{1}, main^) is not a branch.
func HeadRef(branch string) string { return "refs/heads/" + branch }

// BranchExists reports whether name is a valid branch name and that local
// branch exists (no revision syntax, no tags).
func (r *Repo) BranchExists(ctx context.Context, name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || r.validBranchName(ctx, name) != nil {
		return false
	}
	_, err := r.git(ctx, "show-ref", "--verify", "--quiet", HeadRef(name))
	return err == nil
}

// CommitExists reports whether rev names a commit (a branch, tag or hash).
func (r *Repo) CommitExists(ctx context.Context, rev string) bool {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return false
	}
	_, err := r.git(ctx, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	return err == nil
}

// Resolve returns the full hash of the commit rev names.
func (r *Repo) Resolve(ctx context.Context, rev string) (string, error) {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return "", errors.New("invalid revision " + rev)
	}
	out, err := r.git(ctx, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	return strings.TrimSpace(string(out)), err
}

// Ahead counts the commits of local branch that local branch base doesn't have.
func (r *Repo) Ahead(ctx context.Context, base, branch string) (int, error) {
	if base == "" || branch == "" || strings.HasPrefix(base, "-") || strings.HasPrefix(branch, "-") {
		return 0, errors.New("invalid branch name")
	}
	out, err := r.git(ctx, "rev-list", "--count", HeadRef(base)+".."+HeadRef(branch), "--")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// IsAncestor reports whether commit a is reachable from b (a is merged into b).
func (r *Repo) IsAncestor(ctx context.Context, a, b string) bool {
	_, err := r.git(ctx, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// Dirty reports uncommitted changes to tracked files or a merge, rebase or
// cherry-pick in progress. Untracked files don't count: a merge refuses on
// its own when it would overwrite one.
func (r *Repo) Dirty(ctx context.Context) (bool, error) {
	st, err := r.Status(ctx)
	if err != nil {
		return false, err
	}
	if st.Operation != "" {
		return true, nil
	}
	return slices.ContainsFunc(st.Files, func(f FileChange) bool { return f.Kind != Untracked }), nil
}

// FileStat is one file's line counts in a DiffStat.
type FileStat struct {
	Path     string `json:"path"`     // relative to the work tree
	OrigPath string `json:"origPath"` // source of a rename
	Added    int    `json:"added"`
	Deleted  int    `json:"deleted"`
	Binary   bool   `json:"binary"`
	// Untracked: a new file git doesn't track yet.
	Untracked bool `json:"untracked"`
}

// DiffStat summarises what the work tree changed since it forked from base.
type DiffStat struct {
	// MergeBase is where the branch left base; diffs are taken against it.
	MergeBase string     `json:"mergeBase"`
	Files     []FileStat `json:"files"`
	// Count covers every file. Added and Deleted cover every tracked file but
	// only the new files that are listed (their lines are counted by reading
	// them, so the rest are left out).
	Count     int  `json:"count"`
	Added     int  `json:"added"`
	Deleted   int  `json:"deleted"`
	Truncated bool `json:"truncated"`
}

const (
	// untrackedReadLimit bounds how much of one new file is read to count its lines.
	untrackedReadLimit = 1 << 20
	// untrackedReadBudget bounds the bytes read for all new files of one DiffStat.
	untrackedReadBudget = 16 << 20
)

// DiffStat compares the work tree (commits, uncommitted edits and untracked
// files) with its merge base with base branch. At most max files are listed.
func (r *Repo) DiffStat(ctx context.Context, base string, max int) (DiffStat, error) {
	mb, ok := r.MergeBase(ctx, HeadRef(base), "HEAD")
	if !ok {
		return DiffStat{}, fmt.Errorf("%s and HEAD have no common commit", base)
	}
	out, err := r.git(ctx, "diff", "--numstat", "-z", "-M", "--no-ext-diff", mb, "--")
	if err != nil {
		return DiffStat{}, err
	}
	files := parseNumstat(out)
	out, err = r.git(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return DiffStat{}, err
	}
	untracked := splitNUL(out)
	st := DiffStat{MergeBase: mb, Files: []FileStat{}, Count: len(files) + len(untracked)}
	for _, f := range files {
		st.Added += f.Added
		st.Deleted += f.Deleted
	}
	budget := untrackedReadBudget
	for _, p := range untracked {
		if len(files) >= max {
			break
		}
		if err := ctx.Err(); err != nil {
			return DiffStat{}, err
		}
		f := FileStat{Path: p, Untracked: true}
		var read int
		f.Added, f.Binary, read = countLines(filepath.Join(r.Root, filepath.FromSlash(p)), min(budget, untrackedReadLimit))
		budget -= read
		st.Added += f.Added
		files = append(files, f)
	}
	if len(files) > max {
		files = files[:max]
	}
	st.Truncated = len(files) < st.Count
	st.Files = append(st.Files, files...)
	return st, nil
}

// parseNumstat reads `git diff --numstat -z`: "added\tdeleted\tpath\0", or
// "added\tdeleted\t\0old\0new\0" for a rename; binary files show "-".
func parseNumstat(out []byte) []FileStat {
	var files []FileStat
	fields := splitNUL(out)
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		f := FileStat{Path: parts[2]}
		if parts[0] == "-" {
			f.Binary = true
		} else {
			f.Added, _ = strconv.Atoi(parts[0])
			f.Deleted, _ = strconv.Atoi(parts[1])
		}
		if f.Path == "" && i+2 < len(fields) {
			f.OrigPath, f.Path = fields[i+1], fields[i+2]
			i += 2
		}
		files = append(files, f)
	}
	return files
}

// countLines counts the lines in the first limit bytes of a new regular file;
// a file with a NUL byte is binary. Symlinks, FIFOs, devices and sockets are
// never opened (a FIFO would block the reader forever). read is the number
// of bytes read.
func countLines(path string, limit int) (lines int, binary bool, read int) {
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() || limit <= 0 {
		return 0, false, 0
	}
	// O_NONBLOCK: should the file be swapped for a FIFO after Lstat, open
	// must not wait for a writer; the Stat below then refuses it.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return 0, false, 0
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return 0, false, 0
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)))
	if err != nil || len(b) == 0 {
		return 0, false, len(b)
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return 0, true, len(b)
	}
	n := bytes.Count(b, []byte{'\n'})
	if b[len(b)-1] != '\n' {
		n++
	}
	return n, false, len(b)
}

// MergeConflictError: merging would conflict (or did, and was backed out).
type MergeConflictError struct {
	Files []string
}

func (e *MergeConflictError) Error() string {
	return fmt.Sprintf("the merge conflicts in %d file(s): %s", len(e.Files), strings.Join(e.Files, ", "))
}

// MergeConflicts lists the files that would conflict if local branch were merged
// into local branch into, without touching any work tree (git merge-tree). An old git
// without --write-tree reports none; Merge still backs out of a conflict.
func (r *Repo) MergeConflicts(ctx context.Context, into, branch string) ([]string, error) {
	out, err := r.git(ctx, "merge-tree", "--write-tree", "--name-only", "--no-messages", "-z", HeadRef(into), HeadRef(branch))
	if err == nil {
		return nil, nil
	}
	switch exitCode(err) {
	case 1: // conflicts
	case 129: // usage: this git predates --write-tree
		return nil, nil
	default:
		return nil, err
	}
	fields := splitNUL(out)
	var files []string
	for _, f := range fields[min(1, len(fields)):] { // the first field is the tree
		if f == "" {
			break
		}
		if !slices.Contains(files, f) {
			files = append(files, f)
		}
	}
	return files, nil
}

func exitCode(err error) int {
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// Merge merges local branch into the checked-out branch. A conflicting merge is
// aborted, leaving the work tree as it was, and reported as a
// *MergeConflictError.
func (r *Repo) Merge(ctx context.Context, branch string) error {
	_, err := r.git(ctx, "merge", "--no-edit", HeadRef(branch))
	if err == nil {
		return nil
	}
	if r.operation() != "merging" {
		return err
	}
	files := r.conflicted(ctx)
	if _, abortErr := r.git(ctx, "merge", "--abort"); abortErr != nil {
		return fmt.Errorf("%w; aborting the merge failed too: %v", err, abortErr)
	}
	return &MergeConflictError{Files: files}
}

// MergeInto merges local branch base into the checked-out branch, always with
// a merge commit (a fast-forward would make a task without work of its own
// look merged), and on conflicts leaves
// the merge in progress for someone (an agent) to resolve; it returns the
// conflicted files.
func (r *Repo) MergeInto(ctx context.Context, base string) ([]string, error) {
	_, err := r.git(ctx, "merge", "--no-ff", "--no-edit", HeadRef(base))
	if err == nil {
		return nil, nil
	}
	if r.operation() != "merging" {
		return nil, err
	}
	return r.conflicted(ctx), nil
}

func (r *Repo) conflicted(ctx context.Context) []string {
	st, err := r.Status(ctx)
	if err != nil {
		return nil
	}
	var files []string
	for _, f := range st.Files {
		if f.Kind == Conflicted {
			files = append(files, f.Path)
		}
	}
	return files
}

// RemoveWorktreeEntry forgets the one linked worktree whose folder was path
// (already deleted) by removing its admin dir in $GIT_COMMON_DIR/worktrees,
// found by the gitdir file that points back at path/.git. Unlike
// `git worktree prune` it leaves other missing worktrees alone (moved, or
// on a volume that isn't mounted).
func (r *Repo) RemoveWorktreeEntry(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	admin := filepath.Join(r.CommonDir, "worktrees")
	entries, err := os.ReadDir(admin)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	want := filepath.Join(filepath.Clean(path), ".git")
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(admin, e.Name(), "gitdir"))
		if err != nil || !e.IsDir() {
			continue
		}
		if got := filepath.Clean(strings.TrimSpace(string(b))); got == want || resolved(got) == resolved(want) {
			return os.RemoveAll(filepath.Join(admin, e.Name()))
		}
	}
	return fmt.Errorf("%w: %s", ErrNoWorktree, path)
}

// ErrNoWorktree: the repository records no worktree for that folder.
var ErrNoWorktree = errors.New("not a worktree of this repository")

// resolved resolves the symlinks of the longest existing prefix of p
// (/var → /private/var on macOS) so recorded and given paths compare.
func resolved(p string) string {
	dir, rest := p, ""
	for {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// PushBranch pushes local branch to the default remote under the same name
// and makes it the branch's upstream, whatever the work tree has checked out.
func (r *Repo) PushBranch(ctx context.Context, branch string) error {
	if !r.BranchExists(ctx, branch) {
		return errors.New("no local branch " + branch)
	}
	remote, err := r.defaultRemote(ctx)
	if err != nil {
		return err
	}
	_, err = r.git(ctx, "push", "--set-upstream", remote, HeadRef(branch)+":"+HeadRef(branch))
	return err
}

// IgnoredFiles counts the ignored files and folders in the work tree (.env,
// build output): git leaves them out of status, but deleting the worktree
// deletes them too.
func (r *Repo) IgnoredFiles(ctx context.Context) (int, error) {
	out, err := r.git(ctx, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return 0, err
	}
	return len(splitNUL(out)), nil
}
