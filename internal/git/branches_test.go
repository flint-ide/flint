package git

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// findBranch returns the branch named name with the given remote flag.
func findBranch(bs []Branch, name string, remote bool) (Branch, bool) {
	for _, b := range bs {
		if b.Name == name && b.Remote == remote {
			return b, true
		}
	}
	return Branch{}, false
}

// remoteSetup returns a clone (with origin/HEAD) whose origin has main and feature.
func remoteSetup(t *testing.T) (src *Repo, clone *Repo) {
	t.Helper()
	src = newRepo(t)
	bare := newBareRemote(t, src)
	mustGit(t, src.Root, "push", "-q", "-u", "origin", "main")
	mustGit(t, src.Root, "switch", "-q", "-c", "feature")
	commitFile(t, src.Root, "feature.txt", "f\n", "feature work")
	mustGit(t, src.Root, "push", "-q", "-u", "origin", "feature")
	mustGit(t, src.Root, "switch", "-q", "main")
	return src, cloneRepo(t, bare)
}

func TestBranches(t *testing.T) {
	ctx := context.Background()
	_, clone := remoteSetup(t)
	mustGit(t, clone.Root, "branch", "local-only")
	wtPath := filepath.Join(t.TempDir(), "wt")
	mustGit(t, clone.Root, "worktree", "add", "-q", "-b", "in-wt", wtPath)

	bs, err := clone.Branches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bs {
		if strings.HasSuffix(b.Name, "/HEAD") || b.Name == "HEAD" {
			t.Errorf("symbolic ref listed: %+v", b)
		}
	}
	if len(bs) != 5 {
		t.Errorf("got %d branches, want 5 (main, local-only, in-wt, origin/main, origin/feature): %+v", len(bs), bs)
	}

	main, ok := findBranch(bs, "main", false)
	if !ok {
		t.Fatalf("main missing: %+v", bs)
	}
	if !main.Current || main.Upstream != "origin/main" || main.UpdatedAt == 0 {
		t.Errorf("main = %+v, want current with upstream origin/main and a timestamp", main)
	}
	samePath(t, "main.Worktree", main.Worktree, clone.Root)

	lo, ok := findBranch(bs, "local-only", false)
	if !ok || lo.Current || lo.Upstream != "" || lo.Worktree != "" {
		t.Errorf("local-only = %+v (found %v)", lo, ok)
	}
	wt, ok := findBranch(bs, "in-wt", false)
	if !ok {
		t.Fatalf("in-wt missing: %+v", bs)
	}
	if wt.Current {
		t.Error("in-wt must not be current in the main checkout")
	}
	samePath(t, "in-wt.Worktree", wt.Worktree, wtPath)

	for _, name := range []string{"origin/main", "origin/feature"} {
		if b, ok := findBranch(bs, name, true); !ok || b.Current {
			t.Errorf("remote %s = %+v (found %v)", name, b, ok)
		}
	}
}

func TestBranchesTrack(t *testing.T) {
	_, clone := remoteSetup(t)
	commitFile(t, clone.Root, "x.txt", "x", "ahead")
	bs, err := clone.Branches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if main, _ := findBranch(bs, "main", false); main.Track != "[ahead 1]" {
		t.Errorf("Track = %q, want [ahead 1]", main.Track)
	}
}

func TestBranchesEmptyRepo(t *testing.T) {
	r := newEmptyRepo(t)
	bs, err := r.Branches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bs == nil || len(bs) != 0 {
		t.Errorf("Branches = %#v, want empty non-nil slice", bs)
	}
}

func TestCreateBranch(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	first := mustGit(t, r.Root, "rev-parse", "HEAD")
	head := commitFile(t, r.Root, "a.txt", "a", "second")

	tests := []struct {
		name       string
		branch     string
		start      string
		checkout   bool
		wantTip    string
		wantSwitch bool
	}{
		{"no checkout", "plain", "", false, head, false},
		{"start point", "from-first", first, false, first, false},
		{"checkout", "switched", "", true, head, true},
		{"checkout with start point", "feat/nested", first, true, first, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := currentBranch(t, r.Root)
			if err := r.CreateBranch(ctx, tt.branch, tt.start, tt.checkout); err != nil {
				t.Fatal(err)
			}
			if got := mustGit(t, r.Root, "rev-parse", "refs/heads/"+tt.branch); got != tt.wantTip {
				t.Errorf("tip = %s, want %s", got, tt.wantTip)
			}
			want := before
			if tt.wantSwitch {
				want = tt.branch
			}
			if got := currentBranch(t, r.Root); got != want {
				t.Errorf("current = %q, want %q", got, want)
			}
		})
	}
}

func TestCreateBranchErrors(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	mustGit(t, r.Root, "branch", "exists")
	for _, name := range []string{"", "  ", "bad..name", "has space", "ends.lock", "trailing/", "-dash"} {
		if err := r.CreateBranch(ctx, name, "", false); err == nil {
			t.Errorf("CreateBranch(%q) succeeded, want error", name)
		}
	}
	if err := r.CreateBranch(ctx, "exists", "", false); err == nil {
		t.Error("creating an existing branch should fail")
	}
	if err := r.CreateBranch(ctx, "ok", "no-such-rev", false); err == nil {
		t.Error("unknown start point should fail")
	}
}

func TestCheckoutLocal(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	mustGit(t, r.Root, "branch", "other")
	if err := r.Checkout(ctx, "other", false); err != nil {
		t.Fatal(err)
	}
	if got := currentBranch(t, r.Root); got != "other" {
		t.Errorf("current = %q", got)
	}
	if err := r.Checkout(ctx, "missing", false); err == nil {
		t.Error("checkout of a missing branch should fail")
	}
}

func TestCheckoutRemote(t *testing.T) {
	ctx := context.Background()

	t.Run("creates tracking branch", func(t *testing.T) {
		_, clone := remoteSetup(t)
		if err := clone.Checkout(ctx, "origin/feature", true); err != nil {
			t.Fatal(err)
		}
		if got := currentBranch(t, clone.Root); got != "feature" {
			t.Errorf("current = %q, want feature", got)
		}
		if got := mustGit(t, clone.Root, "rev-parse", "--abbrev-ref", "feature@{upstream}"); got != "origin/feature" {
			t.Errorf("upstream = %q", got)
		}
		if !fileExists(clone.Root, "feature.txt") {
			t.Error("feature.txt not checked out")
		}
	})

	t.Run("reuses existing local branch", func(t *testing.T) {
		_, clone := remoteSetup(t)
		mustGit(t, clone.Root, "branch", "feature", "main") // local feature at a different commit
		localTip := mustGit(t, clone.Root, "rev-parse", "feature")
		if err := clone.Checkout(ctx, "origin/feature", true); err != nil {
			t.Fatal(err)
		}
		if got := currentBranch(t, clone.Root); got != "feature" {
			t.Errorf("current = %q", got)
		}
		if got := mustGit(t, clone.Root, "rev-parse", "HEAD"); got != localTip {
			t.Error("existing local branch was reset instead of reused")
		}
	})

	t.Run("invalid remote name", func(t *testing.T) {
		r := newRepo(t)
		if err := r.Checkout(ctx, "noslash", true); err == nil {
			t.Error("expected an error")
		}
	})
}

func TestDeleteBranch(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	mustGit(t, r.Root, "branch", "merged")
	mustGit(t, r.Root, "switch", "-q", "-c", "unmerged")
	commitFile(t, r.Root, "u.txt", "u", "unmerged work")
	mustGit(t, r.Root, "switch", "-q", "main")

	if err := r.DeleteBranch(ctx, "merged", false); err != nil {
		t.Fatalf("delete merged: %v", err)
	}
	if err := r.DeleteBranch(ctx, "unmerged", false); err == nil {
		t.Fatal("deleting an unmerged branch without force should fail")
	}
	if _, err := tryGit(r.Root, "rev-parse", "--verify", "-q", "refs/heads/unmerged"); err != nil {
		t.Fatal("unmerged branch vanished after the failed delete")
	}
	if err := r.DeleteBranch(ctx, "unmerged", true); err != nil {
		t.Fatalf("force delete: %v", err)
	}
	if got := mustGit(t, r.Root, "branch", "--list", "merged", "unmerged"); got != "" {
		t.Errorf("branches still present: %q", got)
	}
	if err := r.DeleteBranch(ctx, "main", true); err == nil {
		t.Error("deleting the checked-out branch should fail")
	}
}
