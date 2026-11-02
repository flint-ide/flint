package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Remote is a configured remote: its fetch URL and the default-repository mark
// the GitHub CLI leaves (`gh repo set-default`), which Flint honours.
type Remote struct {
	Name string
	URL  string
	// Resolved is remote.<name>.gh-resolved: "base", or "owner/repo".
	Resolved string
}

// Remotes lists the remotes in the order git config has them.
func (r *Repo) Remotes(ctx context.Context) ([]Remote, error) {
	out, err := r.git(ctx, "config", "--get-regexp", `^remote\..*\.(url|gh-resolved)$`)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil, nil // nothing matched: no remotes
	}
	if err != nil {
		return nil, err
	}
	return parseRemotes(string(out)), nil
}

// parseRemotes reads `git config --get-regexp` lines ("remote.<name>.url <value>").
// Remote names may contain dots, so the key is cut at its last dot.
func parseRemotes(out string) []Remote {
	var list []Remote
	index := map[string]int{}
	for line := range strings.SplitSeq(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		rest, ok := strings.CutPrefix(key, "remote.")
		dot := strings.LastIndexByte(rest, '.')
		if !ok || dot <= 0 {
			continue
		}
		name, field := rest[:dot], rest[dot+1:]
		i, seen := index[name]
		if !seen {
			i = len(list)
			index[name] = i
			list = append(list, Remote{Name: name})
		}
		switch field {
		case "url":
			if list[i].URL == "" { // the first url is the fetch URL
				list[i].URL = strings.TrimSpace(value)
			}
		case "gh-resolved":
			list[i].Resolved = strings.TrimSpace(value)
		}
	}
	return list
}

// CheckoutPullRequest fetches refs/pull/<number>/head from remote and switches
// to branch at that commit: it creates the branch, or fast-forwards it when it
// is behind. A branch that has diverged from the pull request is left alone.
// upstream (a branch of remote, "" for none) becomes the new branch's upstream,
// so pull and push work as for a branch checked out from the remote.
func (r *Repo) CheckoutPullRequest(ctx context.Context, remote string, number int, branch, upstream string) error {
	if number <= 0 {
		return fmt.Errorf("invalid pull request number %d", number)
	}
	if err := r.validRemote(ctx, remote); err != nil {
		return err
	}
	if strings.HasPrefix(branch, "-") || r.validBranchName(ctx, branch) != nil {
		return fmt.Errorf("invalid branch name %q", branch)
	}
	if upstream != "" && (strings.HasPrefix(upstream, "-") || r.validBranchName(ctx, upstream) != nil) {
		return fmt.Errorf("invalid branch name %q", upstream)
	}
	ref := "refs/pull/" + strconv.Itoa(number) + "/head"
	if _, err := r.git(ctx, "fetch", "--no-tags", "--", remote, ref); err != nil {
		return err
	}
	out, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "FETCH_HEAD^{commit}")
	if err != nil {
		return fmt.Errorf("git fetch %s %s: no commit fetched", remote, ref)
	}
	sha := strings.TrimSpace(string(out))
	if !r.BranchExists(ctx, branch) {
		if _, err := r.git(ctx, "switch", "--no-track", "-c", branch, sha); err != nil {
			return err
		}
		if upstream == "" {
			return nil
		}
		if _, err := r.git(ctx, "config", "branch."+branch+".remote", remote); err != nil {
			return err
		}
		_, err := r.git(ctx, "config", "branch."+branch+".merge", HeadRef(upstream))
		return err
	}
	switch {
	case r.IsAncestor(ctx, sha, HeadRef(branch)): // up to date, or ahead with local commits
		_, err = r.git(ctx, "switch", branch)
	case r.IsAncestor(ctx, HeadRef(branch), sha):
		if _, err = r.git(ctx, "switch", branch); err == nil {
			_, err = r.git(ctx, "merge", "--ff-only", sha)
		}
	default:
		err = fmt.Errorf("your local branch %s has diverged from #%d: rename or delete it, then check out again", branch, number)
	}
	return err
}

// validRemote accepts the name of a configured remote.
func (r *Repo) validRemote(ctx context.Context, name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid remote %q", name)
	}
	remotes, err := r.Remotes(ctx)
	if err != nil {
		return err
	}
	for _, rm := range remotes {
		if rm.Name == name {
			return nil
		}
	}
	return fmt.Errorf("no remote named %q", name)
}

// PushRemote is the remote PushBranch pushes to.
func (r *Repo) PushRemote(ctx context.Context) (string, error) { return r.defaultRemote(ctx) }
