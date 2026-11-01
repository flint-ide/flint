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
)

// The operations behind agent tasks (a branch in its own worktree): how far the
// branch got, whether it merges cleanly, and merging it.

// BranchExists reports whether the local branch name exists.
func (r *Repo) BranchExists(ctx context.Context, name string) bool {
	if name == "" || strings.HasPrefix(name, "-") {
		return false
	}
	_, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
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

// Ahead counts the commits of branch that base doesn't have.
func (r *Repo) Ahead(ctx context.Context, base, branch string) (int, error) {
	if strings.HasPrefix(base, "-") || strings.HasPrefix(branch, "-") {
		return 0, errors.New("invalid branch name")
	}
	out, err := r.git(ctx, "rev-list", "--count", base+".."+branch, "--")
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
	// Count, Added and Deleted cover every file, also those left out of Files.
	Count     int  `json:"count"`
	Added     int  `json:"added"`
	Deleted   int  `json:"deleted"`
	Truncated bool `json:"truncated"`
}

// untrackedReadLimit bounds how much of a new file is read to count its lines.
const untrackedReadLimit = 1 << 20

// DiffStat compares the work tree (commits, uncommitted edits and untracked
// files) with its merge base with base. At most max files are listed.
func (r *Repo) DiffStat(ctx context.Context, base string, max int) (DiffStat, error) {
	mb, ok := r.MergeBase(ctx, base, "HEAD")
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
	for _, p := range splitNUL(out) {
		f := FileStat{Path: p, Untracked: true}
		f.Added, f.Binary = countLines(filepath.Join(r.Root, filepath.FromSlash(p)))
		files = append(files, f)
	}
	st := DiffStat{MergeBase: mb, Files: []FileStat{}, Count: len(files)}
	for _, f := range files {
		st.Added += f.Added
		st.Deleted += f.Deleted
	}
	if len(files) > max {
		files, st.Truncated = files[:max], true
	}
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

// countLines counts the lines of a new file (up to untrackedReadLimit); a file
// with a NUL byte is binary.
func countLines(path string) (int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, untrackedReadLimit))
	if err != nil || len(b) == 0 {
		return 0, false
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return 0, true
	}
	n := bytes.Count(b, []byte{'\n'})
	if b[len(b)-1] != '\n' {
		n++
	}
	return n, false
}

// MergeConflictError: merging would conflict (or did, and was backed out).
type MergeConflictError struct {
	Files []string
}

func (e *MergeConflictError) Error() string {
	return fmt.Sprintf("the merge conflicts in %d file(s): %s", len(e.Files), strings.Join(e.Files, ", "))
}

// MergeConflicts lists the files that would conflict if branch were merged
// into into, without touching any work tree (git merge-tree). An old git
// without --write-tree reports none; Merge still backs out of a conflict.
func (r *Repo) MergeConflicts(ctx context.Context, into, branch string) ([]string, error) {
	out, err := r.git(ctx, "merge-tree", "--write-tree", "--name-only", "--no-messages", "-z", into, branch)
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

// Merge merges branch into the checked-out branch. A conflicting merge is
// aborted, leaving the work tree as it was, and reported as a
// *MergeConflictError.
func (r *Repo) Merge(ctx context.Context, branch string) error {
	_, err := r.git(ctx, "merge", "--no-edit", branch)
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

// MergeInto merges base into the checked-out branch and, on conflicts, leaves
// the merge in progress for someone (an agent) to resolve; it returns the
// conflicted files.
func (r *Repo) MergeInto(ctx context.Context, base string) ([]string, error) {
	_, err := r.git(ctx, "merge", "--no-edit", base)
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

// PruneWorktrees forgets worktrees whose folder is gone.
func (r *Repo) PruneWorktrees(ctx context.Context) error {
	_, err := r.git(ctx, "worktree", "prune")
	return err
}
