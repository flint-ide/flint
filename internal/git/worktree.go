package git

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// Worktree is an entry of `git worktree list`.
type Worktree struct {
	Path     string `json:"path"`
	Head     string `json:"head"`
	Branch   string `json:"branch"` // short name, "" when detached
	Main     bool   `json:"main"`   // the original checkout
	Current  bool   `json:"current"`
	Detached bool   `json:"detached"`
	Locked   bool   `json:"locked"`
	Prunable bool   `json:"prunable"`
}

// Worktrees lists all work trees of the repository.
func (r *Repo) Worktrees(ctx context.Context) ([]Worktree, error) {
	out, err := r.git(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out, r.Root), nil
}

func parseWorktrees(out []byte, current string) []Worktree {
	list := []Worktree{}
	var wt *Worktree
	for _, field := range splitNUL(out) {
		if field == "" { // blank record separator
			wt = nil
			continue
		}
		key, val, _ := strings.Cut(field, " ")
		if key == "worktree" {
			list = append(list, Worktree{Path: val, Main: len(list) == 0, Current: sameDir(val, current)})
			wt = &list[len(list)-1]
			continue
		}
		if wt == nil {
			continue
		}
		switch key {
		case "HEAD":
			wt.Head = val
		case "branch":
			wt.Branch = strings.TrimPrefix(val, "refs/heads/")
		case "detached":
			wt.Detached = true
		case "locked":
			wt.Locked = true
		case "prunable":
			wt.Prunable = true
		}
	}
	return list
}

func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// AddWorktree checks out branch into path. With newBranch the branch is created from
// base (HEAD when empty) — the usual "give the agent its own branch" flow.
func (r *Repo) AddWorktree(ctx context.Context, path, branch string, newBranch bool, base string) error {
	if !filepath.IsAbs(path) {
		return errors.New("worktree path must be absolute")
	}
	args := []string{"worktree", "add"}
	if newBranch {
		if err := r.validBranchName(ctx, branch); err != nil {
			return err
		}
		args = append(args, "-b", branch, path)
		if base != "" {
			args = append(args, base)
		}
	} else {
		args = append(args, path, branch)
	}
	_, err := r.git(ctx, args...)
	return err
}

// RemoveWorktree deletes a linked worktree; force discards its uncommitted changes.
func (r *Repo) RemoveWorktree(ctx context.Context, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	_, err := r.git(ctx, append(args, path)...)
	return err
}

// DefaultWorktreePath suggests "<parent>/<repo>.worktrees/<branch>" for a new worktree,
// keeping worktrees next to (not inside) the main checkout.
func (r *Repo) DefaultWorktreePath(ctx context.Context, branch string) (string, error) {
	wts, err := r.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	main := r.Root
	if len(wts) > 0 {
		main = wts[0].Path
	}
	safe := strings.NewReplacer("/", "-", " ", "-").Replace(branch)
	return filepath.Join(filepath.Dir(main), filepath.Base(main)+".worktrees", safe), nil
}
