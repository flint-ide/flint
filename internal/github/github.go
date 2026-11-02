// Package github reviews pull requests and follows CI through GitHub's REST and
// GraphQL APIs, with the token of the signed-in account (internal/accounts),
// or one from the environment. The token is sent only to the configured API
// host: never to a redirect target on another host, never to the web view,
// the log or an error message. Every request has a timeout, every answer a
// size limit, and nothing polls: callers decide when to ask (settings.CI).
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"flint/internal/git"
)

const (
	readTimeout  = 30 * time.Second
	writeTimeout = 60 * time.Second
	// checkoutTimeout: fetching the branch can take a while on big repositories.
	checkoutTimeout = 3 * time.Minute
	// logTimeout: a long job's log can be tens of megabytes.
	logTimeout = 2 * time.Minute

	repoTTL    = 10 * time.Minute
	listTTL    = 30 * time.Second
	detailsTTL = 30 * time.Second
	diffTTL    = 2 * time.Minute
)

var (
	// ErrNotAuthenticated: no token for the host, or GitHub rejected it. The
	// UI offers "Sign in with GitHub" for it (it matches on this text).
	ErrNotAuthenticated = errors.New("not signed in to GitHub: sign in with GitHub (Settings ▸ Accounts), or use the token of the GitHub CLI")
	// ErrNotGitHub: the project has no remote on the configured GitHub host.
	ErrNotGitHub = errors.New("this project has no GitHub remote")
)

// Config wires a Client to the app.
type Config struct {
	// Auth returns the host and token to use now; ErrNotAuthenticated when
	// there is no token.
	Auth func() (Auth, error)
	// UserAgent is sent with every request ("Flint/<version>").
	UserAgent string
	// MaxRetryWait is how long a read waits for "retry after" (settings.GitHub).
	MaxRetryWait func() time.Duration
	// HTTP replaces the API client (tests).
	HTTP *http.Client
	// Download fetches redirected log files, without the token (tests).
	Download *http.Client
}

// Client talks to GitHub and caches its answers briefly. It never polls:
// everything is fetched on request (opening the view, focus, refresh), and
// mutations drop the entries they make stale.
type Client struct {
	auth         func() (Auth, error)
	userAgent    string
	maxRetryWait func() time.Duration
	http         *http.Client
	download     *http.Client
	etags        etagCache

	repos   *cache[repoInfo]
	lists   *cache[[]PullRequest]
	details *cache[*prData]
	diffs   *cache[FileDiff]
}

// New returns a client.
func New(cfg Config) *Client {
	c := &Client{
		auth:         cfg.Auth,
		userAgent:    cfg.UserAgent,
		maxRetryWait: cfg.MaxRetryWait,
		http:         cfg.HTTP,
		download:     cfg.Download,
		repos:        newCache[repoInfo](repoTTL, 64),
		lists:        newCache[[]PullRequest](listTTL, 32),
		details:      newCache[*prData](detailsTTL, 16),
		diffs:        newCache[FileDiff](diffTTL, 64),
	}
	if c.auth == nil {
		c.auth = func() (Auth, error) { return Auth{}, ErrNotAuthenticated }
	}
	if c.userAgent == "" {
		c.userAgent = "Flint"
	}
	if c.maxRetryWait == nil {
		c.maxRetryWait = func() time.Duration { return time.Minute }
	}
	if c.http == nil {
		c.http = newAPIHTTP()
	}
	if c.download == nil {
		c.download = newDownloadHTTP()
	}
	return c
}

// Reset forgets every cached answer (the account or host changed).
func (c *Client) Reset() {
	c.repos.clear()
	c.lists.clear()
	c.details.clear()
	c.diffs.clear()
	c.etags.clear()
}

// session is the auth plus the repository of a folder, what most calls need.
func (c *Client) session(ctx context.Context, dir string) (Auth, repoInfo, error) {
	a, err := c.auth()
	if err != nil {
		return Auth{}, repoInfo{}, err
	}
	info, err := c.repo(ctx, a, dir)
	return a, info, err
}

// Repo is the GitHub repository a project folder belongs to.
type Repo struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	NameWithOwner string `json:"nameWithOwner"`
	URL           string `json:"url"`
	DefaultBranch string `json:"defaultBranch"`
	// Remote is the git remote that points to it.
	Remote string `json:"remote"`
}

// Repo resolves the GitHub repository of dir.
func (c *Client) Repo(ctx context.Context, dir string) (Repo, error) {
	_, info, err := c.session(ctx, dir)
	return info.Repo, err
}

// Status is what the Pull Requests view shows before (or instead of) a list.
type Status struct {
	// State: "ok", "unauthenticated", "not-github" or "error".
	State   string `json:"state"`
	Message string `json:"message"`
	Repo    Repo   `json:"repo"`
	// Host is the GitHub host Flint is configured for.
	Host string `json:"host"`
}

// Check reports whether Flint can talk to GitHub and which repository dir is.
func (c *Client) Check(ctx context.Context, dir string, force bool) Status {
	if force {
		c.repos.clear()
	}
	a, err := c.auth()
	if err != nil {
		return statusOf(err, a.Host)
	}
	info, err := c.repo(ctx, a, dir)
	if err != nil {
		return statusOf(err, a.Host)
	}
	return Status{State: "ok", Repo: info.Repo, Host: a.Host}
}

func statusOf(err error, host string) Status {
	switch {
	case errors.Is(err, ErrNotAuthenticated):
		return Status{State: "unauthenticated", Message: err.Error(), Host: host}
	case errors.Is(err, ErrNotGitHub):
		return Status{State: "not-github", Message: err.Error(), Host: host}
	default:
		return Status{State: "error", Message: err.Error(), Host: host}
	}
}

// Invalidate forgets cached answers about a pull request (and the lists), e.g. after
// the user changed it here or asked for a refresh.
func (c *Client) Invalidate(repo Repo, number int) {
	c.lists.clear()
	c.details.drop(prKey(repo, number))
}

func prKey(repo Repo, number int) string { return fmt.Sprintf("%s#%d", repo.NameWithOwner, number) }

// openGit opens the work tree of dir (remotes, checkout).
func openGit(ctx context.Context, dir string) (*git.Repo, error) {
	r, err := git.Open(ctx, dir)
	if errors.Is(err, git.ErrNotRepository) {
		return nil, fmt.Errorf("%w (not a git repository)", ErrNotGitHub)
	}
	return r, err
}
