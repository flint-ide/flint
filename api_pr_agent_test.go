package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flint/internal/git"
	"flint/internal/github"
	"flint/internal/github/githubtest"
	"flint/internal/settings"
)

// The unresolved and the resolved thread of the fake's #7.
const (
	fakeOpenThread     = "PRRT_kwDOAAABc84AAAAB"
	fakeResolvedThread = "PRRT_kwDOAAABc84AAAAD"
)

// prAgentFixture is a GitHubAPI signed in to the fake GitHub, with an open
// project whose origin is the fake's acme/widgets: main.go as it was before
// #7, on main.
func prAgentFixture(t *testing.T) (*GitHubAPI, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	fake := githubtest.Start(t)
	e, err := github.LocalEndpoints(fake.Web)
	if err != nil {
		t.Fatal(err)
	}
	auth := github.Auth{Endpoints: e, Token: fake.Token}
	gh := github.New(github.Config{
		Auth:         func() (github.Auth, error) { return auth, nil },
		UserAgent:    "Flint/test",
		MaxRetryWait: func() time.Duration { return time.Second },
	})
	dir := filepath.Join(t.TempDir(), "project")
	gitT(t, t.TempDir(), "init", "-q", "-b", "main", dir)
	gitT(t, dir, "config", "user.name", "Test")
	gitT(t, dir, "config", "user.email", "test@example.com")
	gitT(t, dir, "config", "commit.gpgsign", "false")
	gitT(t, dir, "remote", "add", "origin", fake.RepoURL()+".git")
	commitT(t, dir, "main.go", githubtest.MainBase, "initial")
	app := &App{ctx: context.Background(), configDir: t.TempDir(), settings: settings.Default(), roots: []string{dir}}
	return &GitHubAPI{app: app, gh: gh}, dir
}

// checkOutPullRequest puts the project on #7's branch with its version of main.go.
func checkOutPullRequest(t *testing.T, dir string) {
	t.Helper()
	gitT(t, dir, "switch", "-q", "-c", "feat/rate-limit")
	commitT(t, dir, "main.go", githubtest.MainHead, "feat: add rate limiting")
}

func wantParts(t *testing.T, s string, parts ...string) {
	t.Helper()
	at := 0
	for _, p := range parts {
		i := strings.Index(s[at:], p)
		if i < 0 {
			t.Fatalf("%q missing or out of order in:\n%s", p, s)
		}
		at += i + len(p)
	}
}

func TestReviewDraftOnAnotherBranchSaysSo(t *testing.T) {
	g, dir := prAgentFixture(t)
	d, err := g.ReviewDraft(dir, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if d.OnBranch || d.Branch != "main" || d.HeadBranch != "feat/rate-limit" || !d.Open || d.Number != 7 {
		t.Errorf("draft = %+v", d)
	}
	if d.Threads != 1 || d.Reviews != 1 || d.LeftOut != 0 {
		t.Errorf("counts: %d threads, %d reviews, %d left out", d.Threads, d.Reviews, d.LeftOut)
	}
	wantParts(t, d.Prompt,
		"Address the review comments on pull request #7",
		"which merges feat/rate-limit into main.",
		"Warning: the pull request's branch is feat/rate-limit, but this folder is on main",
		"> pull request title: Add rate limiting",
		"Review that requests changes:",
		"> dave (changes requested):\n> Please fix the *lint*.",
		"1 unresolved review thread in 1 file:",
		"## main.go",
		"### 1. main.go:6",
		"> dave:\n> Use `fmt`?",
		"The pull request's diff around it:",
		"+\tfmt.Println(\"hello\")",
		"only read and plan",
	)
	// The resolved, outdated thread (carol: "old") is not part of it, and the
	// working tree (main's main.go) is not shown as the pull request's code.
	for _, gone := range []string{"carol", "working tree", "change the code"} {
		if strings.Contains(d.Prompt, gone) {
			t.Errorf("%q in the draft:\n%s", gone, d.Prompt)
		}
	}
}

func TestReviewDraftReadsTheWorkingTreeOnThePullRequestsBranch(t *testing.T) {
	g, dir := prAgentFixture(t)
	checkOutPullRequest(t, dir)
	g.app.settings.PRReview.ContextLines = 1
	d, err := g.ReviewDraft(dir, 7, fakeOpenThread)
	if err != nil {
		t.Fatal(err)
	}
	if !d.OnBranch || d.Branch != "feat/rate-limit" || d.Threads != 1 || d.Reviews != 0 {
		t.Errorf("draft = %+v", d)
	}
	wantParts(t, d.Prompt,
		"Address this review comment on pull request #7",
		"The pull request's branch (feat/rate-limit) is checked out in this folder.",
		"### main.go:6",
		"> dave:\n> Use `fmt`?",
		"The code there now, in the working tree (lines 5-7;",
		"    5| func main() {\n    6| \tfmt.Println(\"hello\")\n    7| }",
		"If the reviewer is right, change the code",
	)
	for _, gone := range []string{"    4|", "Please fix the *lint*", "diff around it", "Warning"} {
		if strings.Contains(d.Prompt, gone) {
			t.Errorf("%q in the draft:\n%s", gone, d.Prompt)
		}
	}
}

func TestReviewDraftRefusesBadRequests(t *testing.T) {
	g, dir := prAgentFixture(t)
	outside := t.TempDir()
	cases := []struct {
		name, dir, thread, want string
		number                  int
	}{
		{"folder outside the workspace", outside, "", "outside the project", 7},
		{"relative folder", "project", "", "absolute", 7},
		{"thread id with spaces", dir, "PRRT x; rm -rf", "invalid review thread id", 7},
		{"resolved thread", dir, fakeResolvedThread, "resolved", 7},
		{"unknown thread", dir, "PRRT_unknown", "not found", 7},
		{"missing pull request", dir, "", "not found", 99},
		{"number 0", dir, "", "invalid pull request number", 0},
	}
	for _, c := range cases {
		d, err := g.ReviewDraft(c.dir, c.number, c.thread)
		if err == nil || !strings.Contains(err.Error(), c.want) || d.Prompt != "" {
			t.Errorf("%s: err %v (want %q), prompt %q", c.name, err, c.want, d.Prompt)
		}
	}
}

// Only files inside the open folder are read: with a subfolder open, a
// commented file elsewhere in the repository comes from the pull request's
// diff instead.
func TestReviewDraftReadsOnlyInsideTheOpenFolder(t *testing.T) {
	g, dir := prAgentFixture(t)
	checkOutPullRequest(t, dir)
	sub := filepath.Join(dir, "docs")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	g.app.roots = []string{sub}
	d, err := g.ReviewDraft(sub, 7, fakeOpenThread)
	if err != nil {
		t.Fatal(err)
	}
	if !d.OnBranch || strings.Contains(d.Prompt, "working tree") {
		t.Errorf("a file outside the open folder was read (on branch %v):\n%s", d.OnBranch, d.Prompt)
	}
	wantParts(t, d.Prompt, "### main.go:6", "The pull request's diff around it:")
}

func TestReviewDraftDoesNotFollowSymlinks(t *testing.T) {
	g, dir := prAgentFixture(t)
	checkOutPullRequest(t, dir)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	writeT(t, secret, strings.Repeat("TOP-SECRET-LINE\n", 10))
	target := filepath.Join(dir, "main.go")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, target); err != nil {
		t.Skip("no symlinks here")
	}
	d, err := g.ReviewDraft(dir, 7, fakeOpenThread)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d.Prompt, "TOP-SECRET") || strings.Contains(d.Prompt, "working tree") {
		t.Errorf("a symlink was followed:\n%s", d.Prompt)
	}
}

func TestWorkingTreeReaderSkipsCredentialsAndBinaries(t *testing.T) {
	g, dir := prAgentFixture(t)
	writeT(t, filepath.Join(dir, ".env"), "KEY=value\n")
	writeT(t, filepath.Join(dir, "blob.bin"), "a\x00b\n")
	writeT(t, filepath.Join(dir, "ok.txt"), "one\ntwo\n")
	read := g.workingTreeReader(dir, dir)
	if lines, ok := read("ok.txt"); !ok || len(lines) != 2 {
		t.Errorf("ok.txt = %q %v", lines, ok)
	}
	for _, p := range []string{".env", "blob.bin", "missing.txt", "../outside.txt", "/etc/passwd", ""} {
		if lines, ok := read(p); ok {
			t.Errorf("%q was read: %q", p, lines)
		}
	}
}

func TestPRCheckedOut(t *testing.T) {
	_, dir := prAgentFixture(t)
	ctx := context.Background()
	r, err := git.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	base := gitT(t, dir, "rev-parse", "HEAD")
	other := strings.Repeat("b", 40)
	same := github.Details{HeadRef: "feat/x", HeadOID: other}
	fork := github.Details{HeadRef: "main", HeadOwner: "bob", CrossRepository: true, HeadOID: other}
	cases := []struct {
		name        string
		d           github.Details
		branch, oid string
		want        bool
	}{
		{"its branch", same, "feat/x", base, true},
		{"another branch", same, "main", base, false},
		{"its head commit on any branch", github.Details{HeadRef: "feat/x", HeadOID: base}, "main", base, true},
		{"its head commit, detached", github.Details{HeadRef: "feat/x", HeadOID: base}, "", base, true},
		{"detached elsewhere", same, "", base, false},
		{"a fork's branch as Check out names it", fork, "bob/main", base, true},
		{"this repository's main is not the fork's main", fork, "main", base, false},
		{"the fork author's own branch with the head commit in it", github.Details{HeadRef: "main", HeadOwner: "bob", CrossRepository: true, HeadOID: base}, "main", base, true},
		{"a deleted fork's main is not this repository's main", github.Details{HeadRef: "main", CrossRepository: true, HeadOID: other}, "main", base, false},
		{"a head id that is not a commit id never reaches git", github.Details{HeadRef: "main", HeadOwner: "bob", CrossRepository: true, HeadOID: "--upload-pack=x"}, "main", base, false},
	}
	for _, c := range cases {
		if got := prCheckedOut(ctx, r, c.d, c.branch, c.oid); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// Ahead of the fork's head (unpushed commits) still counts.
	commitT(t, dir, "more.go", "package main\n", "more")
	d := github.Details{HeadRef: "main", HeadOwner: "bob", CrossRepository: true, HeadOID: base}
	if !prCheckedOut(ctx, r, d, "main", gitT(t, dir, "rev-parse", "HEAD")) {
		t.Error("a branch ahead of the pull request's head is not its branch")
	}
}
