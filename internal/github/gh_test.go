package github

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no gh anywhere
	c := newClient(func() (string, error) { return exec.LookPath("gh") })
	ctx := context.Background()

	if st := c.Check(ctx, t.TempDir(), false); st.State != "missing" || !strings.Contains(st.Message, "brew install gh") {
		t.Fatalf("Check = %+v, want missing with an install hint", st)
	}
	if _, err := c.List(ctx, t.TempDir(), ListFilter{}, false); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("List error = %v, want ErrNotInstalled", err)
	}
}

func TestNotAuthenticated(t *testing.T) {
	f := newFake(t)
	f.onExit("auth status", "", "You are not logged into any GitHub hosts. To log in, run: gh auth login\n", 1)
	f.onExit("repo view", "", "To get started with GitHub CLI, please run:  gh auth login\n", 4)
	c := f.client()
	ctx := context.Background()

	if st := c.Check(ctx, t.TempDir(), false); st.State != "unauthenticated" || !strings.Contains(st.Message, "gh auth login") {
		t.Fatalf("Check = %+v, want unauthenticated", st)
	}
	if _, err := c.Repo(ctx, t.TempDir()); !errors.Is(err, ErrNotAuthenticated) {
		t.Fatalf("Repo error = %v, want ErrNotAuthenticated", err)
	}
}

func TestOneWorkingLoginIsEnough(t *testing.T) {
	f := newFake(t)
	// gh exits 1 when any host has a broken token.
	f.onExit("auth status", "github.com\n  ✓ Logged in to github.com account alice (keyring)\nghe.example.com\n  X Failed to log in\n", "", 1)
	f.on("repo view", repoJSON)
	st := f.client().Check(context.Background(), t.TempDir(), false)
	if st.State != "ok" || st.Repo.NameWithOwner != "acme/flint" || st.Repo.Owner != "acme" || st.Repo.DefaultBranch != "main" {
		t.Fatalf("Check = %+v", st)
	}
}

func TestNotGitHubRemote(t *testing.T) {
	f := newFake(t)
	f.on("auth status", "")
	f.onExit("repo view", "", "none of the git remotes configured for this repository point to a known GitHub host. To tell gh about a new GitHub host, please use `gh auth login`\n", 1)
	c := f.client()
	st := c.Check(context.Background(), t.TempDir(), false)
	if st.State != "not-github" {
		t.Fatalf("Check = %+v, want not-github (not unauthenticated, despite the gh auth login hint)", st)
	}
	if _, err := c.List(context.Background(), t.TempDir(), ListFilter{}, false); !errors.Is(err, ErrNotGitHub) {
		t.Fatalf("List error = %v, want ErrNotGitHub", err)
	}
}

func TestCheckIsCached(t *testing.T) {
	f := newFake(t)
	f.on("auth status", "")
	f.on("repo view", repoJSON)
	c := f.client()
	dir := t.TempDir()
	for range 3 {
		if st := c.Check(context.Background(), dir, false); st.State != "ok" {
			t.Fatalf("Check = %+v", st)
		}
	}
	if n := len(f.calls()); n != 2 {
		t.Fatalf("%d gh calls for three checks, want 2 (auth status + repo view, then cached)", n)
	}
	c.Check(context.Background(), dir, true)
	if n := len(f.calls()); n != 4 {
		t.Fatalf("%d gh calls after a forced check, want 4", n)
	}
}

func TestGhMessagesPassThrough(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.onExit("pr list", "", "gh: HTTP 502: Bad Gateway (https://api.github.com/graphql)\n", 1)
	_, err := f.client().List(context.Background(), t.TempDir(), ListFilter{}, false)
	var gerr *Error
	if !errors.As(err, &gerr) || gerr.Msg != "HTTP 502: Bad Gateway (https://api.github.com/graphql)" {
		t.Fatalf("error = %#v, want gh's message without the gh: prefix", err)
	}
}

func TestGhEnvironment(t *testing.T) {
	f := newFake(t)
	// The fake prints its environment instead of a repository.
	script := "#!/bin/sh\nprintf '{\"nameWithOwner\":\"%s|%s|%s\"}' \"$GH_PROMPT_DISABLED\" \"$GH_PAGER\" \"$NO_COLOR\"\n"
	if err := os.WriteFile(f.dir+"/bin/gh", []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := f.client().Repo(context.Background(), t.TempDir())
	if err != nil || repo.NameWithOwner != "1|cat|1" {
		t.Fatalf("Repo = %+v, %v: gh must run without prompts, pager or colours", repo, err)
	}
}
