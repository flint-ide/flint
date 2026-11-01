package git

import (
	"context"
	"strings"
	"testing"
)

func TestParseRemotes(t *testing.T) {
	out := "remote.origin.url git@github.com:me/widgets.git\n" +
		"remote.upstream.url https://github.com/acme/widgets.git\n" +
		"remote.upstream.gh-resolved base\n" +
		"remote.my.fork.url https://github.com/me/fork\n" +
		"remote.origin.url https://second.example/ignored\n"
	got := parseRemotes(out)
	want := []Remote{
		{Name: "origin", URL: "git@github.com:me/widgets.git"},
		{Name: "upstream", URL: "https://github.com/acme/widgets.git", Resolved: "base"},
		{Name: "my.fork", URL: "https://github.com/me/fork"},
	}
	if len(got) != len(want) {
		t.Fatalf("parseRemotes = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("remote %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRemotesNone(t *testing.T) {
	r := newRepo(t)
	got, err := r.Remotes(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("Remotes = %v, %v", got, err)
	}
}

// prRemote makes a bare origin with main and a pull request head at
// refs/pull/7/head one commit ahead of main; it returns the head's sha.
func prRemote(t *testing.T, r *Repo) string {
	t.Helper()
	bare := newBareRemote(t, r)
	mustGit(t, r.Root, "push", "-q", "origin", "main")
	mustGit(t, r.Root, "switch", "-q", "-c", "scratch")
	head := commitFile(t, r.Root, "pr.txt", "from the pull request\n", "pr work")
	mustGit(t, r.Root, "push", "-q", "origin", head+":refs/pull/7/head")
	mustGit(t, r.Root, "switch", "-q", "main")
	mustGit(t, r.Root, "branch", "-q", "-D", "scratch")
	_ = bare
	return head
}

func TestCheckoutPullRequestCreatesBranch(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	head := prRemote(t, r)
	if err := r.CheckoutPullRequest(ctx, "origin", 7, "feat/x", "feat/x"); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, r.Root, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD = %s, want %s", got, head)
	}
	if got := mustGit(t, r.Root, "branch", "--show-current"); got != "feat/x" {
		t.Errorf("branch = %s", got)
	}
	if got := mustGit(t, r.Root, "config", "branch.feat/x.merge"); got != "refs/heads/feat/x" {
		t.Errorf("upstream merge = %s", got)
	}
}

func TestCheckoutPullRequestFastForwards(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	head := prRemote(t, r)
	mustGit(t, r.Root, "branch", "-q", "pr-7", "main") // behind the pull request
	if err := r.CheckoutPullRequest(ctx, "origin", 7, "pr-7", ""); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, r.Root, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD = %s, want %s", got, head)
	}
	if _, err := tryGit(r.Root, "config", "branch.pr-7.merge"); err == nil {
		t.Error("an existing branch got an upstream")
	}
}

func TestCheckoutPullRequestRefusesDiverged(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	prRemote(t, r)
	mustGit(t, r.Root, "switch", "-q", "-c", "feat/x")
	mine := commitFile(t, r.Root, "mine.txt", "local\n", "local work")
	mustGit(t, r.Root, "switch", "-q", "main")
	err := r.CheckoutPullRequest(ctx, "origin", 7, "feat/x", "")
	if err == nil || !strings.Contains(err.Error(), "diverged") {
		t.Fatalf("err = %v", err)
	}
	if got := mustGit(t, r.Root, "rev-parse", "refs/heads/feat/x"); got != mine {
		t.Error("diverged branch was moved")
	}
}

func TestCheckoutPullRequestValidates(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	prRemote(t, r)
	for _, c := range []struct {
		remote, branch string
		n              int
	}{
		{"origin", "ok", 0},
		{"-origin", "ok", 7},
		{"nope", "ok", 7},
		{"origin", "-x", 7},
		{"origin", "a..b", 7},
	} {
		if err := r.CheckoutPullRequest(ctx, c.remote, c.n, c.branch, ""); err == nil {
			t.Errorf("CheckoutPullRequest(%q, %d, %q) succeeded", c.remote, c.n, c.branch)
		}
	}
}
