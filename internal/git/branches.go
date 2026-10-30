package git

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// Branch is a local or remote-tracking branch.
type Branch struct {
	Name      string `json:"name"` // e.g. "main" or "origin/main"
	Remote    bool   `json:"remote"`
	Current   bool   `json:"current"`
	Upstream  string `json:"upstream"`
	Track     string `json:"track"`     // e.g. "[ahead 1, behind 2]"
	UpdatedAt int64  `json:"updatedAt"` // committer date of the tip (unix seconds)
	// Worktree is the path where this branch is checked out, if any.
	Worktree string `json:"worktree"`
}

// Branches lists local branches, then remote-tracking ones, most recently updated first.
func (r *Repo) Branches(ctx context.Context) ([]Branch, error) {
	out, err := r.git(ctx, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname)%00%(upstream:short)%00%(upstream:track)%00%(HEAD)%00%(committerdate:unix)%00%(worktreepath)",
		"refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	branches := []Branch{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) < 6 || strings.HasSuffix(f[0], "/HEAD") {
			continue
		}
		b := Branch{Upstream: f[1], Track: f[2], Current: f[3] == "*", Worktree: f[5]}
		b.UpdatedAt, _ = strconv.ParseInt(f[4], 10, 64)
		if name, ok := strings.CutPrefix(f[0], "refs/heads/"); ok {
			b.Name = name
		} else {
			b.Name = strings.TrimPrefix(f[0], "refs/remotes/")
			b.Remote = true
		}
		branches = append(branches, b)
	}
	return branches, nil
}

// Checkout switches to a branch. A remote branch ("origin/feature") gets a local
// tracking branch, reusing an existing local one of the same name.
func (r *Repo) Checkout(ctx context.Context, name string, remote bool) error {
	if !remote {
		_, err := r.git(ctx, "switch", name)
		return err
	}
	_, local, ok := strings.Cut(name, "/")
	if !ok {
		return errors.New("invalid remote branch " + name)
	}
	if _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+local); err == nil {
		_, err := r.git(ctx, "switch", local)
		return err
	}
	_, err := r.git(ctx, "switch", "--track", "-c", local, name)
	return err
}

// CreateBranch creates name at startPoint (HEAD when empty) and optionally switches to it.
func (r *Repo) CreateBranch(ctx context.Context, name, startPoint string, checkout bool) error {
	if err := r.validBranchName(ctx, name); err != nil {
		return err
	}
	args := []string{"branch", name}
	if checkout {
		args = []string{"switch", "-c", name}
	}
	if startPoint != "" {
		args = append(args, startPoint)
	}
	_, err := r.git(ctx, args...)
	return err
}

// DeleteBranch deletes a local branch; force allows deleting unmerged work.
func (r *Repo) DeleteBranch(ctx context.Context, name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := r.git(ctx, "branch", flag, "--", name)
	return err
}

func (r *Repo) validBranchName(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("branch name is empty")
	}
	if _, err := r.git(ctx, "check-ref-format", "--branch", name); err != nil {
		return errors.New("invalid branch name: " + name)
	}
	return nil
}

// Fetch updates all remotes.
func (r *Repo) Fetch(ctx context.Context) error {
	_, err := r.git(ctx, "fetch", "--all", "--prune")
	return err
}

// Pull updates the current branch from its upstream using the user's pull config.
func (r *Repo) Pull(ctx context.Context) error {
	_, err := r.git(ctx, "pull")
	return err
}

// Push pushes the current branch, setting the upstream on first push.
func (r *Repo) Push(ctx context.Context) error {
	st, err := r.Status(ctx)
	if err != nil {
		return err
	}
	if st.Branch == "" {
		return errors.New("cannot push a detached HEAD")
	}
	if st.Upstream != "" {
		_, err = r.git(ctx, "push")
		return err
	}
	remote, err := r.defaultRemote(ctx)
	if err != nil {
		return err
	}
	_, err = r.git(ctx, "push", "--set-upstream", remote, "HEAD")
	return err
}

func (r *Repo) defaultRemote(ctx context.Context) (string, error) {
	out, err := r.git(ctx, "remote")
	if err != nil {
		return "", err
	}
	remotes := strings.Fields(string(out))
	for _, rm := range remotes {
		if rm == "origin" {
			return rm, nil
		}
	}
	if len(remotes) == 0 {
		return "", errors.New("no remote configured")
	}
	return remotes[0], nil
}
