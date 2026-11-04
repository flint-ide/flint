// Command nextversion prints the version of the next release, vX.Y.Z, worked
// out from the Conventional Commits since the latest release tag reachable
// from HEAD (.github/workflows/release.yml uses it):
//
//	feat!:, fix(x)!: or a BREAKING CHANGE: footer   major (minor before 1.0)
//	feat:                                            minor
//	fix:, perf:                                      patch
//	anything else (chore, docs, ci, refactor, …)     no release
//
// With -bump patch|minor|major the bump is forced. When nothing calls for a
// release it prints nothing and exits 0. Merge commits are skipped: the
// commits they bring in are counted instead.
//
//	go run ./cmd/nextversion                # e.g. v0.4.0, or nothing
//	go run ./cmd/nextversion -bump patch    # e.g. v0.3.1
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type version struct{ major, minor, patch int }

func (v version) String() string { return fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch) }

type bump int

const (
	bumpNone bump = iota
	bumpPatch
	bumpMinor
	bumpMajor
)

func (b bump) String() string {
	return [...]string{"none", "patch", "minor", "major"}[b]
}

var (
	versionRe  = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	subjectRe  = regexp.MustCompile(`^(?i)([a-z]+)(\([^)]*\))?(!)?: `)
	breakingRe = regexp.MustCompile(`(?m)^BREAKING[ -]CHANGE: `)
)

func parseVersion(s string) (version, bool) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return version{}, false
	}
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	return version{n(1), n(2), n(3)}, true
}

func (v version) less(o version) bool {
	if v.major != o.major {
		return v.major < o.major
	}
	if v.minor != o.minor {
		return v.minor < o.minor
	}
	return v.patch < o.patch
}

// latest returns the highest vX.Y.Z among tags; other tags are ignored.
func latest(tags []string) (version, bool) {
	var best version
	found := false
	for _, t := range tags {
		if v, ok := parseVersion(strings.TrimSpace(t)); ok && (!found || best.less(v)) {
			best, found = v, true
		}
	}
	return best, found
}

func bumpOf(msg string) bump {
	m := subjectRe.FindStringSubmatch(msg)
	if m == nil {
		return bumpNone
	}
	if m[3] == "!" || breakingRe.MatchString(msg) {
		return bumpMajor
	}
	switch strings.ToLower(m[1]) {
	case "feat":
		return bumpMinor
	case "fix", "perf":
		return bumpPatch
	}
	return bumpNone
}

// bumpFor is the largest bump any of the commit messages calls for.
func bumpFor(msgs []string) bump {
	b := bumpNone
	for _, m := range msgs {
		b = max(b, bumpOf(m))
	}
	return b
}

func next(prev version, b bump) version {
	if b == bumpMajor && prev.major == 0 {
		b = bumpMinor
	}
	switch b {
	case bumpMajor:
		return version{prev.major + 1, 0, 0}
	case bumpMinor:
		return version{prev.major, prev.minor + 1, 0}
	case bumpPatch:
		return version{prev.major, prev.minor, prev.patch + 1}
	}
	return prev
}

func parseBump(s string) (bump, error) {
	switch s {
	case "patch":
		return bumpPatch, nil
	case "minor":
		return bumpMinor, nil
	case "major":
		return bumpMajor, nil
	}
	return bumpNone, fmt.Errorf("bump must be auto, patch, minor or major, not %q", s)
}

// splitLog splits `git log --format=%B%x1e` output into commit messages.
func splitLog(out string) []string {
	var msgs []string
	for m := range strings.SplitSeq(out, "\x1e") {
		if m = strings.TrimSpace(m); m != "" {
			msgs = append(msgs, m)
		}
	}
	return msgs
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func run(bumpFlag string) (string, error) {
	tags, err := git("tag", "--merged", "HEAD")
	if err != nil {
		return "", err
	}
	prev, tagged := latest(strings.Split(tags, "\n"))
	logArgs := []string{"log", "--no-merges", "--format=%B%x1e"}
	if tagged {
		logArgs = append(logArgs, prev.String()+"..HEAD")
	}
	out, err := git(logArgs...)
	if err != nil {
		return "", err
	}
	msgs := splitLog(out)

	b := bumpFor(msgs)
	if bumpFlag != "auto" {
		if b, err = parseBump(bumpFlag); err != nil {
			return "", err
		}
	}
	if b == bumpNone || len(msgs) == 0 {
		return "", nil
	}
	return next(prev, b).String(), nil
}

func main() {
	bumpFlag := flag.String("bump", "auto", "auto (from the commits), patch, minor or major")
	flag.Parse()
	v, err := run(*bumpFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nextversion:", err)
		os.Exit(1)
	}
	if v != "" {
		fmt.Println(v)
	}
}
