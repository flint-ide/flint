package github

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flint/internal/github/githubtest"
)

// The tests never reach GitHub: every client talks to githubtest's fake on a
// loopback port, and git runs without the user's configuration.

func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// project makes a git repository whose remotes are name=url pairs.
func project(t *testing.T, remotes ...string) string {
	t.Helper()
	isolateGit(t)
	dir := filepath.Join(t.TempDir(), "project")
	runGit(t, t.TempDir(), "init", "-q", "-b", "main", dir)
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	for i := 0; i+1 < len(remotes); i += 2 {
		runGit(t, dir, "remote", "add", remotes[i], remotes[i+1])
	}
	return dir
}

// fixture is a fake GitHub, a client signed in to it and a project whose
// origin is the fake's acme/widgets.
type fixture struct {
	fake *githubtest.Fake
	c    *Client
	dir  string
	auth Auth
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	fake := githubtest.Start(t)
	e, err := LocalEndpoints(fake.Web)
	if err != nil {
		t.Fatal(err)
	}
	a := Auth{Endpoints: e, Token: fake.Token}
	c := New(Config{
		Auth:         func() (Auth, error) { return a, nil },
		UserAgent:    "Flint/test",
		MaxRetryWait: func() time.Duration { return 2 * time.Second },
	})
	dir := project(t, "origin", fake.RepoURL()+".git")
	return fixture{fake: fake, c: c, dir: dir, auth: a}
}

func ctx() context.Context { return context.Background() }

// paths lists the method and path of every request the fake saw.
func paths(f *githubtest.Fake) []string {
	var out []string
	for _, r := range f.Requests() {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}
