package github

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"flint/internal/git"
)

// repoInfo is a resolved repository plus what else the remotes say: the
// owners of other remotes on the same host (forks a branch may be pushed to).
type repoInfo struct {
	Repo
	owners []string
}

// remoteRepo is a remote whose URL names a repository on the configured host.
type remoteRepo struct {
	remote, owner, name, resolved string
}

// nameRE: what GitHub allows in an owner or repository name.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

func validName(s string) bool {
	return nameRE.MatchString(s) && s != "." && s != ".." && !strings.HasPrefix(s, "-")
}

// parseRemoteURL reads host, owner and name from a remote URL: https, http,
// git, ssh:// and scp-like (git@github.com:owner/repo.git) forms. For ssh the
// host is compared without its port (that is the ssh port, not the web one).
func parseRemoteURL(raw string) (host, owner, name string, ok bool) {
	raw = strings.TrimSpace(raw)
	var path string
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		switch u.Scheme {
		case "https", "http", "git":
			host = u.Host
		case "ssh", "git+ssh", "ssh+git":
			host = u.Hostname()
		default:
			return "", "", "", false
		}
		path = u.Path
	} else if at, rest, found := strings.Cut(raw, ":"); found && at != "" && !strings.Contains(at, "/") && !strings.HasPrefix(rest, "/") {
		// scp-like: [user@]host:owner/repo
		host = at[strings.LastIndexByte(at, '@')+1:]
		path = rest
	} else {
		return "", "", "", false
	}
	host = strings.ToLower(host)
	if host == "ssh.github.com" || host == "www.github.com" {
		host = "github.com"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		return "", "", "", false
	}
	owner, name = parts[0], strings.TrimSuffix(parts[1], ".git")
	if !validName(owner) || !validName(name) {
		return "", "", "", false
	}
	return host, owner, name, true
}

// hostMatches compares a remote's host with the configured one: an ssh remote
// has no web port, so "ghe.corp" matches "ghe.corp:8443" there.
func hostMatches(remoteHost, want string) bool {
	if strings.EqualFold(remoteHost, want) {
		return true
	}
	name, _, found := strings.Cut(want, ":")
	return found && !strings.Contains(remoteHost, ":") && strings.EqualFold(remoteHost, name)
}

// pickRemote chooses the repository like the GitHub CLI: the remote marked by
// `gh repo set-default` (its own repository, or the one it names), else
// upstream, github, origin, then the others in config order.
func pickRemote(remotes []git.Remote, host string) (pick remoteRepo, owners []string, ok bool) {
	var found []remoteRepo
	for _, r := range remotes {
		h, owner, name, valid := parseRemoteURL(r.URL)
		if !valid || !hostMatches(h, host) {
			continue
		}
		found = append(found, remoteRepo{remote: r.Name, owner: owner, name: name, resolved: r.Resolved})
		if !slices.Contains(owners, owner) {
			owners = append(owners, owner)
		}
	}
	if len(found) == 0 {
		return remoteRepo{}, nil, false
	}
	for _, f := range found {
		switch {
		case f.resolved == "base":
			return f, owners, true
		case f.resolved != "":
			if o, n, cut := strings.Cut(f.resolved, "/"); cut && validName(o) && validName(n) {
				f.owner, f.name = o, n
				return f, owners, true
			}
		}
	}
	rank := func(name string) int {
		switch name {
		case "upstream":
			return 0
		case "github":
			return 1
		case "origin":
			return 2
		}
		return 3
	}
	best := found[0]
	for _, f := range found[1:] {
		if rank(f.remote) < rank(best.remote) {
			best = f
		}
	}
	return best, owners, true
}

// repo resolves the repository of dir from its remotes, then asks GitHub for
// its canonical name (renamed or transferred repositories) and default branch.
func (c *Client) repo(ctx context.Context, a Auth, dir string) (repoInfo, error) {
	return c.repos.get(a.Host+"|"+dir, false, func() (repoInfo, error) {
		ctx, cancel := context.WithTimeout(ctx, readTimeout)
		defer cancel()
		g, err := openGit(ctx, dir)
		if err != nil {
			return repoInfo{}, err
		}
		remotes, err := g.Remotes(ctx)
		if err != nil {
			return repoInfo{}, err
		}
		pick, owners, ok := pickRemote(remotes, a.Host)
		if !ok {
			return repoInfo{}, fmt.Errorf("%w on %s: add one with git remote add", ErrNotGitHub, a.Host)
		}
		var r struct {
			Name     string `json:"name"`
			FullName string `json:"full_name"`
			HTMLURL  string `json:"html_url"`
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
			DefaultBranch string `json:"default_branch"`
		}
		err = c.getJSON(ctx, a, call{url: rest(a, "/repos/%s/%s", pick.owner, pick.name)}, &r)
		if isStatus(err, 404) {
			return repoInfo{}, fmt.Errorf("%w: %s/%s was not found on %s (or the token can't see it)", ErrNotGitHub, pick.owner, pick.name, a.Host)
		}
		if err != nil {
			return repoInfo{}, err
		}
		if !validName(r.Owner.Login) || !validName(r.Name) {
			return repoInfo{}, fmt.Errorf("unexpected repository from GitHub: %q", r.FullName)
		}
		return repoInfo{
			Repo: Repo{
				Owner: r.Owner.Login, Name: r.Name, NameWithOwner: r.Owner.Login + "/" + r.Name,
				URL: r.HTMLURL, DefaultBranch: r.DefaultBranch, Remote: pick.remote,
			},
			owners: owners,
		}, nil
	})
}

// remoteOwner returns the owner and name of the repository remote points to.
func (c *Client) remoteOwner(ctx context.Context, a Auth, dir, remote string) (owner, name string, err error) {
	g, err := openGit(ctx, dir)
	if err != nil {
		return "", "", err
	}
	remotes, err := g.Remotes(ctx)
	if err != nil {
		return "", "", err
	}
	for _, r := range remotes {
		if r.Name != remote {
			continue
		}
		if h, o, n, ok := parseRemoteURL(r.URL); ok && hostMatches(h, a.Host) {
			return o, n, nil
		}
		return "", "", fmt.Errorf("the remote %s is not a repository on %s", remote, a.Host)
	}
	return "", "", fmt.Errorf("no remote named %q", remote)
}
