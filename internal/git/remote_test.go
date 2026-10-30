package git

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushSetsUpstream(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	bare := newBareRemote(t, r)

	if err := r.Push(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Upstream != "origin/main" {
		t.Errorf("Upstream = %q, want origin/main", st.Upstream)
	}
	head := mustGit(t, r.Root, "rev-parse", "HEAD")
	if got := mustGit(t, bare, "rev-parse", "refs/heads/main"); got != head {
		t.Errorf("remote main = %s, want %s", got, head)
	}

	// Second push goes through the upstream branch.
	next := commitFile(t, r.Root, "b.txt", "b", "second")
	if err := r.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, bare, "rev-parse", "refs/heads/main"); got != next {
		t.Errorf("remote main after second push = %s, want %s", got, next)
	}
}

func TestPushPrefersOrigin(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	other := filepath.Join(t.TempDir(), "aaa.git")
	mustGit(t, r.Root, "init", "-q", "--bare", other)
	mustGit(t, r.Root, "remote", "add", "aaa", other)
	newBareRemote(t, r)

	if err := r.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, r.Root, "rev-parse", "--abbrev-ref", "main@{upstream}"); got != "origin/main" {
		t.Errorf("upstream = %q, want origin/main", got)
	}
}

func TestPushFallsBackToFirstRemote(t *testing.T) {
	r := newRepo(t)
	other := filepath.Join(t.TempDir(), "up.git")
	mustGit(t, r.Root, "init", "-q", "--bare", other)
	mustGit(t, r.Root, "remote", "add", "upstream", other)
	if err := r.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, r.Root, "rev-parse", "--abbrev-ref", "main@{upstream}"); got != "upstream/main" {
		t.Errorf("upstream = %q", got)
	}
}

func TestPushErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("detached HEAD", func(t *testing.T) {
		r := newRepo(t)
		newBareRemote(t, r)
		mustGit(t, r.Root, "switch", "-q", "--detach")
		err := r.Push(ctx)
		if err == nil || !strings.Contains(err.Error(), "detached") {
			t.Errorf("err = %v, want detached HEAD error", err)
		}
	})

	t.Run("no remote", func(t *testing.T) {
		r := newRepo(t)
		err := r.Push(ctx)
		if err == nil || !strings.Contains(err.Error(), "no remote") {
			t.Errorf("err = %v, want no remote error", err)
		}
	})

	t.Run("rejected non-fast-forward", func(t *testing.T) {
		r := newRepo(t)
		bare := newBareRemote(t, r)
		mustGit(t, r.Root, "push", "-q", "-u", "origin", "main")
		other := cloneRepo(t, bare)
		commitFile(t, other.Root, "o.txt", "o", "other")
		mustGit(t, other.Root, "push", "-q")
		commitFile(t, r.Root, "l.txt", "l", "local")
		var gerr *Error
		if err := r.Push(ctx); err == nil {
			t.Error("expected rejection")
		} else if !errors.As(err, &gerr) || gerr.Msg == "" {
			t.Errorf("err = %T %v, want *Error with git's stderr", err, err)
		}
	})
}

func TestFetchAndPull(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	bare := newBareRemote(t, r)
	if err := r.Push(ctx); err != nil {
		t.Fatal(err)
	}
	other := cloneRepo(t, bare)
	tip := commitFile(t, other.Root, "remote.txt", "from clone\n", "from clone")
	mustGit(t, other.Root, "push", "-q")

	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, r.Root, "rev-parse", "origin/main"); got != tip {
		t.Errorf("origin/main = %s, want %s", got, tip)
	}
	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Behind != 1 || st.Ahead != 0 {
		t.Errorf("Ahead/Behind = %d/%d, want 0/1", st.Ahead, st.Behind)
	}

	if err := r.Pull(ctx); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, r.Root, "rev-parse", "HEAD"); got != tip {
		t.Errorf("HEAD after pull = %s, want %s", got, tip)
	}
	if got := readFile(t, r.Root, "remote.txt"); got != "from clone\n" {
		t.Errorf("remote.txt = %q", got)
	}
}

func TestFetchPrunes(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	bare := newBareRemote(t, r)
	mustGit(t, r.Root, "push", "-q", "origin", "main", "main:gone")
	mustGit(t, r.Root, "fetch", "-q")
	mustGit(t, bare, "branch", "-D", "gone")

	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tryGit(r.Root, "rev-parse", "--verify", "-q", "refs/remotes/origin/gone"); err == nil {
		t.Error("origin/gone should have been pruned")
	}
}

func TestPullWithoutUpstreamFails(t *testing.T) {
	r := newRepo(t)
	if err := r.Pull(context.Background()); err == nil {
		t.Error("pull without upstream should fail")
	}
}
