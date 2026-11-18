// Package release reads what the release tooling needs from git history:
// vX.Y.Z versions and Conventional Commits. cmd/nextversion works out the next
// version with it and cmd/releasenotes writes the notes of a release.
package release

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a release version, vMAJOR.MINOR.PATCH.
type Version struct{ Major, Minor, Patch int }

func (v Version) String() string { return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Less reports whether v comes before o.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

var (
	versionRe  = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	subjectRe  = regexp.MustCompile(`^(?i)([a-z]+)(\(([^)]*)\))?(!)?: `)
	breakingRe = regexp.MustCompile(`(?m)^BREAKING[ -]CHANGE: `)
)

// ParseVersion reads a vX.Y.Z tag; anything else (no "v", a prerelease, a
// leading zero) is not a release version.
func ParseVersion(s string) (Version, bool) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return Version{}, false
	}
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	return Version{n(1), n(2), n(3)}, true
}

// Latest returns the highest vX.Y.Z among tags; other tags are ignored.
func Latest(tags []string) (Version, bool) {
	return latest(tags, func(Version) bool { return true })
}

// LatestBefore returns the highest vX.Y.Z among tags that comes before limit:
// the release before limit.
func LatestBefore(tags []string, limit Version) (Version, bool) {
	return latest(tags, func(v Version) bool { return v.Less(limit) })
}

func latest(tags []string, keep func(Version) bool) (Version, bool) {
	var best Version
	found := false
	for _, t := range tags {
		if v, ok := ParseVersion(strings.TrimSpace(t)); ok && keep(v) && (!found || best.Less(v)) {
			best, found = v, true
		}
	}
	return best, found
}

// Commit is what a Conventional Commits message (or a pull request title)
// says about a change.
type Commit struct {
	// Type is the lower-cased type ("feat", "fix", "chore", …), or "" when
	// the subject doesn't start with one.
	Type  string
	Scope string
	// Breaking: "type!:" or a "BREAKING CHANGE:" footer; only for a subject
	// with a type.
	Breaking bool
	// Subject is the first line; Description is the subject after "type: "
	// (the whole subject when it has no type).
	Subject     string
	Description string
}

// ParseCommit reads a commit message (subject, then body and footers).
func ParseCommit(msg string) Commit {
	subject, _, _ := strings.Cut(msg, "\n")
	subject = strings.TrimSpace(subject)
	c := Commit{Subject: subject, Description: subject}
	m := subjectRe.FindStringSubmatch(subject)
	if m == nil {
		return c
	}
	c.Type = strings.ToLower(m[1])
	c.Scope = m[3]
	c.Breaking = m[4] == "!" || breakingRe.MatchString(msg)
	c.Description = strings.TrimSpace(subject[len(m[0]):])
	return c
}

// SplitLog splits `git log --format=…%x1e` output into its records.
func SplitLog(out string) []string {
	var recs []string
	for r := range strings.SplitSeq(out, "\x1e") {
		if r = strings.TrimSpace(r); r != "" {
			recs = append(recs, r)
		}
	}
	return recs
}
