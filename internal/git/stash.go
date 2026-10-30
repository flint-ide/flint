package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Stash is one entry of `git stash list`.
type Stash struct {
	// Hash identifies the stash; stash@{n} indices shift when others push or drop.
	Hash    string `json:"hash"`
	Index   int    `json:"index"`   // n in stash@{n} when listed
	Message string `json:"message"` // e.g. "On main: wip"
	Time    int64  `json:"time"`    // unix seconds
	// Untracked is set when the stash also saved untracked files (a third parent).
	Untracked bool `json:"untracked"`
}

// ErrNoLocalChanges is returned by StashPush when there is nothing to stash.
var ErrNoLocalChanges = errors.New("no local changes to stash")

// Stashes lists stashes, newest first.
func (r *Repo) Stashes(ctx context.Context) ([]Stash, error) {
	out, err := r.git(ctx, "stash", "list", "-z", "--format=%H%x1f%gs%x1f%ct%x1f%P")
	if err != nil {
		return nil, err
	}
	stashes := []Stash{}
	for i, rec := range splitNUL(out) {
		f := strings.Split(rec, "\x1f")
		if len(f) < 4 {
			continue
		}
		s := Stash{Hash: f[0], Index: i, Message: f[1], Untracked: len(strings.Fields(f[3])) > 2}
		s.Time, _ = strconv.ParseInt(f[2], 10, 64)
		stashes = append(stashes, s)
	}
	return stashes, nil
}

// StashPush stashes local changes (untracked files too when includeUntracked).
func (r *Repo) StashPush(ctx context.Context, message string, includeUntracked bool) error {
	args := []string{"stash", "push"}
	if includeUntracked {
		args = append(args, "--include-untracked")
	}
	if m := strings.TrimSpace(message); m != "" {
		args = append(args, "-m", m)
	}
	out, err := r.git(ctx, args...)
	if err != nil {
		return err
	}
	if strings.Contains(string(out), "No local changes to save") {
		return ErrNoLocalChanges
	}
	return nil
}

// StashApply applies a stash, keeping it in the list.
func (r *Repo) StashApply(ctx context.Context, hash string) error {
	ref, err := r.stashRef(ctx, hash)
	if err != nil {
		return err
	}
	_, err = r.git(ctx, "stash", "apply", ref)
	return err
}

// StashPop applies a stash and drops it (git keeps it when applying conflicts).
func (r *Repo) StashPop(ctx context.Context, hash string) error {
	ref, err := r.stashRef(ctx, hash)
	if err != nil {
		return err
	}
	_, err = r.git(ctx, "stash", "pop", ref)
	return err
}

// StashDrop deletes a stash.
func (r *Repo) StashDrop(ctx context.Context, hash string) error {
	ref, err := r.stashRef(ctx, hash)
	if err != nil {
		return err
	}
	_, err = r.git(ctx, "stash", "drop", ref)
	return err
}

// StashFiles lists what a stash changed: tracked changes against the commit it was
// made on, plus saved untracked files (Kind Untracked, contents in <hash>^3).
func (r *Repo) StashFiles(ctx context.Context, hash string) ([]FileChange, error) {
	files, err := r.CommitFiles(ctx, hash)
	if err != nil {
		return nil, err
	}
	if _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", hash+"^3"); err != nil {
		return files, nil // no untracked files saved
	}
	out, err := r.git(ctx, "ls-tree", "-r", "-z", "--name-only", hash+"^3")
	if err != nil {
		return nil, err
	}
	for _, p := range splitNUL(out) {
		files = append(files, FileChange{Path: p, Kind: Untracked, Index: "?", Worktree: "?"})
	}
	return files, nil
}

// stashRef finds the current stash@{n} for a stash hash. Indices move as stashes
// are pushed and dropped (also by agents in other worktrees: the list is shared),
// so commands address stashes by hash and resolve the index right before running.
func (r *Repo) stashRef(ctx context.Context, hash string) (string, error) {
	if hash == "" {
		return "", errors.New("no stash selected")
	}
	stashes, err := r.Stashes(ctx)
	if err != nil {
		return "", err
	}
	for _, s := range stashes {
		if s.Hash == hash || (len(hash) >= 7 && strings.HasPrefix(s.Hash, hash)) {
			return fmt.Sprintf("stash@{%d}", s.Index), nil
		}
	}
	return "", fmt.Errorf("stash %.7s no longer exists", hash)
}
