package git

import (
	"context"
	"testing"
)

func TestHead(t *testing.T) {
	ctx := context.Background()
	empty := newEmptyRepo(t)
	if branch, oid := empty.Head(ctx); branch != "main" || oid != "" {
		t.Errorf("before the first commit: %q %q", branch, oid)
	}
	r := newRepo(t)
	want := mustGit(t, r.Root, "rev-parse", "HEAD")
	if branch, oid := r.Head(ctx); branch != "main" || oid != want {
		t.Errorf("on main: %q %q, want main %q", branch, oid, want)
	}
	mustGit(t, r.Root, "switch", "-q", "-c", "feat/x")
	if branch, _ := r.Head(ctx); branch != "feat/x" {
		t.Errorf("branch = %q", branch)
	}
	mustGit(t, r.Root, "checkout", "-q", "--detach")
	if branch, oid := r.Head(ctx); branch != "" || oid != want {
		t.Errorf("detached: %q %q", branch, oid)
	}
}
