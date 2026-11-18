// Command releasenotes writes the body of a GitHub release in Markdown, from
// the commits since the release before and the pull requests that brought
// them (.github/workflows/release.yml and release-notes.yml use it, through
// .github/scripts/release-notes.sh):
//
//	## Highlights        each pull request's "## Release notes" section
//	## Upgrade notes     their "### Upgrade notes" sub-sections
//	## All changes       one line per change: breaking changes, features,
//	                     fixes, performance, others; maintenance (chore, docs,
//	                     ci, test, refactor, build, dependencies) only counted
//	**Full Changelog**   the compare link
//
// It reads git itself and never the network: the workflow fetches the pull
// requests and passes them as a JSON array (`gh pr view --json
// number,title,body,author` objects, or the REST API's with user):
//
//	go run ./cmd/releasenotes -version v0.7.0 -list-prs        # 83
//	go run ./cmd/releasenotes -version v0.7.0 -prs prs.json    # the body
//
// -to is the release's commit (default HEAD); the release before is the
// highest vX.Y.Z tag below -version reachable from it, or -from. A pull
// request is found in the history by its merge commit ("Merge pull request
// #N from …") or a squashed commit's "(#N)"; commits without one are listed
// by themselves.
package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"flint/internal/release"
)

const (
	maxInput = 32 << 20 // bytes of pull request JSON
	maxPRs   = 2_000
)

var (
	repoRe   = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	refRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/^~-]*$`)
	mergeRe  = regexp.MustCompile(`^Merge pull request #([0-9]+) from `)
	squashRe = regexp.MustCompile(`\(#([0-9]+)\)$`)
)

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// resolve turns a ref into a commit hash.
func resolve(ref string) (string, error) {
	if !refRe.MatchString(ref) {
		return "", fmt.Errorf("not a git ref: %q", ref)
	}
	out, err := git("rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// previous is the release before version reachable from to, or "".
func previous(to string, version release.Version) (string, error) {
	tags, err := git("tag", "--merged", to)
	if err != nil {
		return "", err
	}
	if v, ok := release.LatestBefore(strings.Split(tags, "\n"), version); ok {
		return v.String(), nil
	}
	return "", nil
}

// loadCommits reads the commits in from..to (all of to's history when from is
// ""), oldest first, without merge commits, each with the pull request that
// brought it.
func loadCommits(from, to string) ([]Commit, error) {
	rng := to
	if from != "" {
		rng = from + ".." + to
	}
	out, err := git("log", "--topo-order", "--format=%H%x1f%P%x1f%B%x1e", rng)
	if err != nil {
		return nil, err
	}
	type rec struct {
		sha     string
		parents []string
		msg     string
	}
	var recs []rec
	for _, r := range release.SplitLog(out) {
		f := strings.SplitN(r, "\x1f", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("unexpected git log record %q", r)
		}
		recs = append(recs, rec{f[0], strings.Fields(f[1]), strings.TrimSpace(f[2])})
	}
	pr := map[string]int{}
	for _, r := range recs {
		m := mergeRe.FindStringSubmatch(r.msg)
		if len(r.parents) < 2 || m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		shas, err := git("rev-list", r.parents[0]+".."+r.parents[1])
		if err != nil {
			return nil, err
		}
		for _, s := range strings.Fields(shas) {
			if pr[s] == 0 {
				pr[s] = n
			}
		}
	}
	var commits []Commit
	for _, r := range slices.Backward(recs) {
		if len(r.parents) > 1 {
			continue
		}
		c := Commit{SHA: r.sha, Message: r.msg, PR: pr[r.sha]}
		if c.PR == 0 {
			subject, _, _ := strings.Cut(r.msg, "\n")
			if m := squashRe.FindStringSubmatch(strings.TrimSpace(subject)); m != nil {
				c.PR, _ = strconv.Atoi(m[1])
			}
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// readPRs reads the pull requests' JSON array.
func readPRs(r io.Reader) ([]PullRequest, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxInput+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxInput {
		return nil, fmt.Errorf("pull requests: more than %d bytes", maxInput)
	}
	var prs []PullRequest
	if err := json.Unmarshal(data, &prs); err != nil {
		return nil, fmt.Errorf("pull requests: %w", err)
	}
	if len(prs) > maxPRs {
		return nil, fmt.Errorf("pull requests: more than %d", maxPRs)
	}
	for i, p := range prs {
		if p.Number <= 0 {
			return nil, fmt.Errorf("pull requests: entry %d has no number", i+1)
		}
	}
	return prs, nil
}

// prNumbers lists the pull requests of the commits, in ascending order.
func prNumbers(commits []Commit) []int {
	var ns []int
	for _, c := range commits {
		if c.PR > 0 && !slices.Contains(ns, c.PR) {
			ns = append(ns, c.PR)
		}
	}
	slices.Sort(ns)
	return ns
}

type options struct {
	version, from, to, prs, repo string
	listPRs                      bool
}

func run(o options, stdin io.Reader, stdout, stderr io.Writer) error {
	v, ok := release.ParseVersion(o.version)
	if !ok {
		return fmt.Errorf("-version must be vX.Y.Z, not %q", o.version)
	}
	if !repoRe.MatchString(o.repo) {
		return fmt.Errorf("-repo must be owner/name, not %q", o.repo)
	}
	to, err := resolve(o.to)
	if err != nil {
		return err
	}
	prev := o.from
	if prev == "" {
		if prev, err = previous(to, v); err != nil {
			return err
		}
	} else if _, ok := release.ParseVersion(prev); !ok {
		return fmt.Errorf("-from must be vX.Y.Z, not %q", prev)
	}
	from := ""
	if prev != "" {
		if from, err = resolve(prev); err != nil {
			return err
		}
	}
	commits, err := loadCommits(from, to)
	if err != nil {
		return err
	}
	if o.listPRs {
		for _, n := range prNumbers(commits) {
			fmt.Fprintln(stdout, n)
		}
		return nil
	}
	var prs []PullRequest
	if o.prs != "" {
		r := stdin
		if o.prs != "-" {
			f, err := os.Open(o.prs)
			if err != nil {
				return err
			}
			defer f.Close()
			r = f
		}
		if prs, err = readPRs(r); err != nil {
			return err
		}
	}
	body := Render(Input{Repo: o.repo, Version: v.String(), Previous: prev, Commits: commits, PRs: prs})
	since := cmp.Or(prev, "the first commit")
	fmt.Fprintf(stderr, "releasenotes: %s since %s: %d commits, %d pull requests, %d bytes\n", v, since, len(commits), len(prs), len(body))
	_, err = io.WriteString(stdout, body)
	return err
}

func main() {
	var o options
	flag.StringVar(&o.version, "version", "", "the release, vX.Y.Z (required)")
	flag.StringVar(&o.from, "from", "", "the release before, vX.Y.Z (default: the highest one below -version reachable from -to)")
	flag.StringVar(&o.to, "to", "HEAD", "the release's commit")
	flag.StringVar(&o.prs, "prs", "", "a JSON array of the pull requests (- for stdin)")
	flag.StringVar(&o.repo, "repo", cmp.Or(os.Getenv("GITHUB_REPOSITORY"), "flint-ide/flint"), "the repository, owner/name")
	flag.BoolVar(&o.listPRs, "list-prs", false, "print the numbers of the range's pull requests, one per line, and stop")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "releasenotes: unexpected arguments:", flag.Args())
		os.Exit(2)
	}
	if err := run(o, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "releasenotes:", err)
		os.Exit(1)
	}
}
