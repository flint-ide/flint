package git

import (
	"context"
	"errors"
)

// WorkingDiff is the unified diff of paths between HEAD (the empty tree before
// the first commit) and the working tree: what the Commit panel shows for
// one change. A rename passes both of its paths. Diff drivers stay off.
func (r *Repo) WorkingDiff(ctx context.Context, paths ...string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("no paths")
	}
	base := "HEAD"
	if _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		base = emptyTree
	}
	args := append([]string{"diff", base, "--no-color", "--no-ext-diff", "--no-textconv", "--find-renames", "--"}, paths...)
	out, err := r.git(ctx, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
