// Package github reviews pull requests through the GitHub CLI (gh). Flint never sees
// a token: gh authenticates exactly as in the terminal (keyring, GH_TOKEN, enterprise
// hosts), and every request is a gh invocation with a timeout.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"flint/internal/shellenv"
)

const (
	readTimeout  = 30 * time.Second
	writeTimeout = 60 * time.Second
	// checkout fetches the branch, which can take a while on big repositories.
	checkoutTimeout = 3 * time.Minute

	repoTTL    = 10 * time.Minute
	authTTL    = 5 * time.Minute
	listTTL    = 30 * time.Second
	detailsTTL = 30 * time.Second
	diffTTL    = 2 * time.Minute
)

var (
	// ErrNotInstalled: no gh on the login-shell PATH.
	ErrNotInstalled = errors.New("the GitHub CLI (gh) is not installed: install it with `brew install gh`, then run `gh auth login`")
	// ErrNotAuthenticated: gh is there but not logged in.
	ErrNotAuthenticated = errors.New("gh is not logged in to GitHub: run `gh auth login` in a terminal")
	// ErrNotGitHub: the project has no remote on a GitHub host gh knows.
	ErrNotGitHub = errors.New("this project has no GitHub remote")
)

// Error carries gh's own message (stderr), which is what the user needs to see.
type Error struct {
	Args []string
	Msg  string
	Err  error
}

func (e *Error) Error() string { return e.Msg }

func (e *Error) Unwrap() error { return e.Err }

// Client runs gh and caches its answers briefly. It never polls: everything is
// fetched on request (opening the view, focus, refresh) and mutations drop the
// entries they make stale.
type Client struct {
	lookPath func() (string, error)

	repos   *cache[Repo]
	auth    *cache[struct{}]
	lists   *cache[[]PullRequest]
	details *cache[*prData]
	diffs   *cache[FileDiff]
}

// New returns a client that finds gh on the login-shell PATH.
func New() *Client {
	return newClient(func() (string, error) {
		shellenv.Wait()
		return exec.LookPath("gh")
	})
}

func newClient(lookPath func() (string, error)) *Client {
	return &Client{
		lookPath: lookPath,
		repos:    newCache[Repo](repoTTL, 64),
		auth:     newCache[struct{}](authTTL, 1),
		lists:    newCache[[]PullRequest](listTTL, 32),
		details:  newCache[*prData](detailsTTL, 16),
		diffs:    newCache[FileDiff](diffTTL, 64),
	}
}

// run executes gh in dir. stdin (JSON request bodies) keeps user text out of argv,
// so nothing in a comment can be read as a flag or an @file reference.
func (c *Client) run(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	stdout, stderr, err := c.exec(ctx, dir, stdin, args...)
	if err == nil || errors.Is(err, ErrNotInstalled) {
		return stdout, err
	}
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("gh %s timed out", firstArgs(args))
	}
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = strings.TrimSpace(string(stdout))
	}
	if msg == "" {
		msg = err.Error()
	}
	return stdout, classify(args, msg, err)
}

// exec runs gh and returns its raw output.
func (c *Client) exec(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, string, error) {
	var stdout bytes.Buffer
	stderr, err := c.execTo(ctx, dir, stdin, &stdout, args...)
	return stdout.Bytes(), stderr, err
}

// execTo runs gh with its stdout going to w (e.g. a bounded buffer for logs).
func (c *Client) execTo(ctx context.Context, dir string, stdin []byte, w io.Writer, args ...string) (string, error) {
	bin, err := c.lookPath()
	if err != nil {
		return "", ErrNotInstalled
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GH_PROMPT_DISABLED=1", // never wait for an answer nobody can type
		"GH_NO_UPDATE_NOTIFIER=1",
		"GH_SPINNER_DISABLED=1",
		"GH_PAGER=cat",
		"NO_COLOR=1",
		"CLICOLOR=0",
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
	)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stdout = w
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stderr.String(), err
}

// runJSON runs gh and decodes its stdout into v.
func (c *Client) runJSON(ctx context.Context, dir string, stdin []byte, v any, args ...string) error {
	out, err := c.run(ctx, dir, stdin, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("gh %s: unexpected output: %w", firstArgs(args), err)
	}
	return nil
}

// classify maps gh's messages to the errors the UI explains with a hint.
func classify(args []string, msg string, err error) error {
	lower := strings.ToLower(msg)
	// Checked first: this message also mentions `gh auth login` (for new hosts).
	for _, s := range []string{
		"none of the git remotes configured for this repository point to a known github host",
		"no git remotes found",
		"not a git repository",
	} {
		if strings.Contains(lower, s) {
			return fmt.Errorf("%w (%s)", ErrNotGitHub, cleanMsg(msg))
		}
	}
	for _, s := range []string{"gh auth login", "not logged into", "http 401", "bad credentials", "authentication required"} {
		if strings.Contains(lower, s) {
			return ErrNotAuthenticated
		}
	}
	return &Error{Args: args, Msg: cleanMsg(msg), Err: err}
}

// cleanMsg drops gh's "gh: " prefix and keeps the first lines of long messages.
func cleanMsg(msg string) string {
	lines := strings.Split(msg, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(strings.TrimSpace(l), "gh: ")
	}
	if len(lines) > 6 {
		lines = append(lines[:6], "…")
	}
	return strings.Join(lines, "\n")
}

func firstArgs(args []string) string {
	if len(args) > 2 {
		args = args[:2]
	}
	return strings.Join(args, " ")
}

// Repo is the GitHub repository a project folder belongs to (as gh resolves it:
// remotes, `gh repo set-default`, enterprise hosts).
type Repo struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	NameWithOwner string `json:"nameWithOwner"`
	URL           string `json:"url"`
	DefaultBranch string `json:"defaultBranch"`
}

// Repo resolves the GitHub repository of dir.
func (c *Client) Repo(ctx context.Context, dir string) (Repo, error) {
	return c.repos.get(dir, false, func() (Repo, error) {
		var out struct {
			Name             string                 `json:"name"`
			NameWithOwner    string                 `json:"nameWithOwner"`
			URL              string                 `json:"url"`
			Owner            struct{ Login string } `json:"owner"`
			DefaultBranchRef struct{ Name string }  `json:"defaultBranchRef"`
		}
		ctx, cancel := context.WithTimeout(ctx, readTimeout)
		defer cancel()
		if err := c.runJSON(ctx, dir, nil, &out, "repo", "view", "--json", "name,nameWithOwner,owner,url,defaultBranchRef"); err != nil {
			return Repo{}, err
		}
		if out.NameWithOwner == "" {
			return Repo{}, ErrNotGitHub
		}
		return Repo{
			Owner:         out.Owner.Login,
			Name:          out.Name,
			NameWithOwner: out.NameWithOwner,
			URL:           out.URL,
			DefaultBranch: out.DefaultBranchRef.Name,
		}, nil
	})
}

// Status is what the Pull Requests view shows before (or instead of) a list.
type Status struct {
	// State: "ok", "missing" (no gh), "unauthenticated", "not-github" or "error".
	State   string `json:"state"`
	Message string `json:"message"`
	Repo    Repo   `json:"repo"`
}

// Check reports whether gh is installed and logged in, and which repository dir is.
func (c *Client) Check(ctx context.Context, dir string, force bool) Status {
	if force {
		c.auth.clear()
		c.repos.drop(dir)
	}
	_, err := c.auth.get("", force, func() (struct{}, error) {
		ctx, cancel := context.WithTimeout(ctx, readTimeout)
		defer cancel()
		stdout, stderr, err := c.exec(ctx, dir, nil, "auth", "status")
		switch {
		case err == nil, errors.Is(err, ErrNotInstalled):
			return struct{}{}, err
		case ctx.Err() != nil:
			return struct{}{}, fmt.Errorf("gh auth status timed out")
		case strings.Contains(string(stdout)+stderr, "Logged in to"):
			// Exit 1 also covers one broken host among several; any working login is enough.
			return struct{}{}, nil
		default:
			return struct{}{}, ErrNotAuthenticated
		}
	})
	if err != nil {
		return statusOf(err)
	}
	repo, err := c.Repo(ctx, dir)
	if err != nil {
		return statusOf(err)
	}
	return Status{State: "ok", Repo: repo}
}

func statusOf(err error) Status {
	switch {
	case errors.Is(err, ErrNotInstalled):
		return Status{State: "missing", Message: err.Error()}
	case errors.Is(err, ErrNotAuthenticated):
		return Status{State: "unauthenticated", Message: err.Error()}
	case errors.Is(err, ErrNotGitHub):
		return Status{State: "not-github", Message: err.Error()}
	default:
		return Status{State: "error", Message: err.Error()}
	}
}

// Invalidate forgets cached answers about a pull request (and the lists), e.g. after
// the user changed it here or asked for a refresh.
func (c *Client) Invalidate(repo Repo, number int) {
	c.lists.clear()
	c.details.drop(prKey(repo, number))
}

func prKey(repo Repo, number int) string { return fmt.Sprintf("%s#%d", repo.NameWithOwner, number) }
