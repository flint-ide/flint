package git

import (
	"context"
	"strings"
)

// Object returns the content of a blob given by sha or "<rev>:<path>". ok is false
// when the object isn't in this clone (e.g. a pull request's commits not fetched yet)
// or the path doesn't exist at rev.
func (r *Repo) Object(ctx context.Context, spec string) ([]byte, bool) {
	if _, err := r.git(ctx, "cat-file", "-e", spec); err != nil {
		return nil, false
	}
	out, err := r.git(ctx, "cat-file", "blob", spec)
	if err != nil {
		return nil, false
	}
	return out, true
}

// MergeBase returns the best common ancestor of commits a and b; ok is false when
// either commit is missing from this clone.
func (r *Repo) MergeBase(ctx context.Context, a, b string) (string, bool) {
	out, err := r.git(ctx, "merge-base", a, b)
	sha := strings.TrimSpace(string(out))
	if err != nil || sha == "" {
		return "", false
	}
	return sha, true
}
