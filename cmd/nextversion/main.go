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
	"strings"

	"flint/internal/release"
)

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

func bumpOf(msg string) bump {
	c := release.ParseCommit(msg)
	if c.Type == "" {
		return bumpNone
	}
	if c.Breaking {
		return bumpMajor
	}
	switch c.Type {
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

func next(prev release.Version, b bump) release.Version {
	if b == bumpMajor && prev.Major == 0 {
		b = bumpMinor
	}
	switch b {
	case bumpMajor:
		return release.Version{Major: prev.Major + 1}
	case bumpMinor:
		return release.Version{Major: prev.Major, Minor: prev.Minor + 1}
	case bumpPatch:
		return release.Version{Major: prev.Major, Minor: prev.Minor, Patch: prev.Patch + 1}
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
	prev, tagged := release.Latest(strings.Split(tags, "\n"))
	logArgs := []string{"log", "--no-merges", "--format=%B%x1e"}
	if tagged {
		logArgs = append(logArgs, prev.String()+"..HEAD")
	}
	out, err := git(logArgs...)
	if err != nil {
		return "", err
	}
	msgs := release.SplitLog(out)

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
